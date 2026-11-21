package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/address"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng address (kế hoạch M3, Task M3-17, DH15, Q-23): đổi địa chỉ sau khi đã gửi hãng. Địa chỉ trên đơn chỉ đổi khi hãng
// đã nhận: hãng sửa địa chỉ qua API (tài khoản api, adapter khai UpdateAddress) → yêu cầu api, lệnh
// command.shipment.update_address (address_command.go); không → yêu cầu manual, nhân viên gọi hãng rồi xác nhận
// (ResolveAddressChange). Quyết định thuần: internal/address. Lệnh sửa địa chỉ cần hàm ghi audit (fulfillment không nhập
// identity) nên không nối qua addressHandlers mà qua Module.AddressCommand (cmd/crm truyền hàm ghi).

func (*Module) addressHandlers() []commands.Handler       { return nil }
func (*Module) addressInbound() []webhooks.InboundHandler { return nil }
func (*Module) addressSources() []webhooks.Source         { return nil }
func (*Module) addressConsumers() []events.Consumer       { return nil }
func (*Module) addressSchedules() []schedules.Schedule    { return nil }
func (*Module) addressCleanups() []schedules.CleanupFunc  { return nil }

// Lỗi của đổi địa chỉ.
var (
	// ErrAddressChangeOpen: vận đơn đã có một yêu cầu đổi địa chỉ còn mở (pending, sent, unknown).
	ErrAddressChangeOpen = errors.New("fulfillment: vận đơn đã có yêu cầu đổi địa chỉ đang mở")
	// ErrAddressChangeNotAllowed: vận đơn không còn đổi được địa chỉ (chưa có ở hãng, đang hủy, đã giao, đang hoàn),
	// hay yêu cầu không xác nhận tay được (đã đóng, yêu cầu api còn chờ lệnh).
	ErrAddressChangeNotAllowed = errors.New("fulfillment: không đổi được địa chỉ ở trạng thái này")
	// errNoAudit: Module.AddressCommand thiếu hàm ghi audit.
	errNoAudit = errors.New("fulfillment: lệnh sửa địa chỉ thiếu hàm ghi audit")
)

const (
	maxAddressNote = 1000 // maxLength của note (RequestAddressChangeRequest, ResolveAddressChangeRequest; CHECK 0329)
	// auditAddressChange là action của dòng audit khi địa chỉ đơn đổi theo yêu cầu đổi địa chỉ.
	auditAddressChange = "order.address_change"
	// auditFieldShipAddress là tên trường của dòng audit (changed_fields); giá trị bị che theo khóa "address".
	auditFieldShipAddress = "ship_address"
	actorStaff            = "staff"
)

// AuditRecord là một dòng audit mà fulfillment dựng cho bên gọi ghi (identity.RecordAudit ở lớp HTTP hay cmd/crm;
// fulfillment không nhập identity). ActorID uuid.Nil: không có người (hệ thống). Before, After chỉ có tên trường và mã;
// địa chỉ nằm dưới khóa ship_address (bị che khi lưu).
type AuditRecord struct {
	TenantID, ActorID, EntityID   uuid.UUID
	ActorType, Action, EntityType string
	Before, After                 map[string]any
}

// AuditFunc ghi r trong tx của bên gọi (cùng transaction với thay đổi).
type AuditFunc func(ctx context.Context, tx pgx.Tx, r AuditRecord) error

// AddressChangeInput là yêu cầu đổi địa chỉ giao của một vận đơn: địa chỉ mới (kiểu địa chỉ của M2), ghi chú (che SĐT,
// địa chỉ có số trước khi lưu). Authorize nil: không kiểm phạm vi (test).
type AddressChangeInput struct {
	Address   order.ShipAddress
	Note      string
	Authorize Authorize
}

// AddressResolveInput là kết quả nhân viên xác nhận (confirmed: hãng đã nhận; rejected: hãng không nhận) và ghi chú
// (khác rỗng: thay ghi chú của yêu cầu). Authorize nil: không kiểm phạm vi.
type AddressResolveInput struct {
	Result, Note string
	Authorize    Authorize
}

// AddressChange là yêu cầu đổi địa chỉ cho màn hình (không có địa chỉ). WarehouseID là kho của tài khoản hãng của vận
// đơn (phạm vi quyền, không trả ra API).
type AddressChange struct {
	ID, ShipmentID, OrderID, WarehouseID uuid.UUID
	Mode, Status, Note                   string
	CreatedAt                            time.Time
	ResolvedAt                           *time.Time
}

// AuditFields là phần sau của dòng audit cho yêu cầu c: mã, chế độ, trạng thái (không địa chỉ, không ghi chú).
func (c AddressChange) AuditFields() map[string]any {
	return map[string]any{
		"shipmentId": c.ShipmentID.String(), "orderId": c.OrderID.String(), "mode": c.Mode, "status": c.Status,
		"fields": []string{auditFieldShipAddress},
	}
}

// AddressChangeResult là kết quả xác nhận: yêu cầu sau khi đổi và dòng audit của đơn khi địa chỉ đơn thật sự đổi (nil:
// không đổi — rejected, cùng địa chỉ, đơn đã xong hay vận đơn không còn hiện hành).
type AddressChangeResult struct {
	Change     AddressChange
	OrderAudit *AuditRecord
}

// RequestAddressChange tạo yêu cầu đổi địa chỉ giao của vận đơn shipmentID trong tx của bên gọi: khóa đơn rồi vận đơn
// (ADR-46 nhóm 7 → 8), Authorize với kho của tài khoản hãng, vận đơn phải awaiting_pickup, shipping hay delivery_failed;
// địa chỉ kiểm như order.CheckShipAddress, khớp mã địa giới của hãng khi adapter có bảng địa giới (Locations) và vận
// đơn api; một yêu cầu mở mỗi vận đơn. Hãng sửa địa chỉ qua API (vận đơn api, adapter khai UpdateAddress, có mã hãng) →
// mode api, lệnh UpdateAddressKind (khóa address:<id>); không → mode manual (nhân viên gọi hãng rồi xác nhận). Cả hai
// pending; địa chỉ trên đơn và ship_to không đổi. Audit của yêu cầu do bên gọi ghi.
//
// Lỗi (không đổi gì): không có vận đơn → ErrNotFound; lỗi của Authorize trả nguyên; vận đơn không đổi được địa chỉ →
// ErrAddressChangeNotAllowed; ghi chú quá dài → ErrShipInvalid; địa chỉ sai → lỗi của order.CheckShipAddress; chưa khớp
// mã hãng → ErrAddressUnmapped; còn yêu cầu mở → ErrAddressChangeOpen.
func (s *Service) RequestAddressChange(ctx context.Context, tx pgx.Tx, shipmentID uuid.UUID, in AddressChangeInput,
	a order.Actor,
) (AddressChange, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return AddressChange{}, errNotWired
	}
	note, err := addressNote(in.Note)
	if err != nil {
		return AddressChange{}, err
	}
	q := store.New(tx)
	l, err := lockForCancel(ctx, tx, q, shipmentID, in.Authorize)
	if err != nil {
		return AddressChange{}, err
	}
	addr, byAPI, err := s.checkAddressRequest(ctx, tx, q, l, in.Address)
	if err != nil {
		return AddressChange{}, err
	}
	row, err := s.insertAddressChange(ctx, tx, q, l.ID, addr, byAPI, note, a)
	if err != nil {
		return AddressChange{}, err
	}
	if err := s.publishShipment(ctx, tx, a.TenantID, l.ID, l.WarehouseID); err != nil {
		return AddressChange{}, err
	}
	return changeView(l, row.ID, row.Mode, row.Status, row.Note, row.CreatedAt, row.ResolvedAt), nil
}

// checkAddressRequest kiểm yêu cầu đổi địa chỉ trên vận đơn l đã khóa: trạng thái đổi được, địa chỉ hợp lệ (trả bản đã
// chuẩn hóa), khớp mã hãng khi cần, chưa có yêu cầu mở; byAPI: hãng sửa địa chỉ qua API.
func (s *Service) checkAddressRequest(ctx context.Context, tx pgx.Tx, q *store.Queries, l lockedCancel,
	in order.ShipAddress,
) (order.ShipAddress, bool, error) {
	if !address.Changeable(l.Status) {
		return order.ShipAddress{}, false, fmt.Errorf("đổi địa chỉ vận đơn %s: %w", l.Status, ErrAddressChangeNotAllowed)
	}
	addr, err := order.CheckShipAddress(ctx, tx, in)
	if err != nil {
		return order.ShipAddress{}, false, fmt.Errorf("kiểm địa chỉ mới: %w", err)
	}
	ad, hasAdapter := s.adapter(l.CarrierKey)
	api := hasAdapter && l.Mode == ModeAPI
	if api && ad.Capabilities().Locations {
		if err := s.checkMapped(ctx, tx, l.CarrierKey, shipAddress(addr)); err != nil {
			return order.ShipAddress{}, false, err
		}
	}
	open, err := q.AddressChangeOpen(ctx, l.ID)
	if err != nil {
		return order.ShipAddress{}, false, fmt.Errorf("đọc yêu cầu đổi địa chỉ đang mở: %w", err)
	}
	if open {
		return order.ShipAddress{}, false, ErrAddressChangeOpen
	}
	return addr, api && ad.Capabilities().UpdateAddress && l.TrackingCode.Valid, nil
}

// insertAddressChange chèn yêu cầu pending (api hay manual theo byAPI) cho vận đơn shipmentID; byAPI → lệnh
// UpdateAddressKind (khóa address:<id>) gắn vào yêu cầu.
func (s *Service) insertAddressChange(ctx context.Context, tx pgx.Tx, q *store.Queries, shipmentID uuid.UUID,
	addr order.ShipAddress, byAPI bool, note pgtype.Text, a order.Actor,
) (store.InsertAddressChangeRow, error) {
	mode := address.ModeManual
	if byAPI {
		mode = address.ModeAPI
	}
	raw, err := json.Marshal(addr)
	if err != nil {
		return store.InsertAddressChangeRow{}, fmt.Errorf("mã hóa địa chỉ mới: %w", err)
	}
	row, err := q.InsertAddressChange(ctx, store.InsertAddressChangeParams{
		ShipmentID: shipmentID, NewAddress: raw, Mode: mode, RequestedBy: a.UserID, Note: note,
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgUniqueViolation {
		return store.InsertAddressChangeRow{}, ErrAddressChangeOpen // chốt cuối: index address_change_requests_open
	}
	if err != nil {
		return store.InsertAddressChangeRow{}, fmt.Errorf("ghi yêu cầu đổi địa chỉ: %w", err)
	}
	if !byAPI {
		return row, nil
	}
	jobID, _, err := s.d.Queue.Enqueue(ctx, tx, UpdateAddressKind(), queue.NewJob{
		TenantID: &a.TenantID, Payload: addressPayload{RequestID: row.ID}, DedupeKey: address.DedupeKey(row.ID),
	})
	if err != nil {
		return store.InsertAddressChangeRow{}, fmt.Errorf("ghi lệnh sửa địa chỉ: %w", err)
	}
	if err := q.SetAddressChangeJob(ctx, store.SetAddressChangeJobParams{
		JobID: pgtype.UUID{Bytes: jobID, Valid: true}, ID: row.ID,
	}); err != nil {
		return store.InsertAddressChangeRow{}, fmt.Errorf("gắn lệnh vào yêu cầu đổi địa chỉ: %w", err)
	}
	return row, nil
}

// ResolveAddressChange ghi kết quả nhân viên xác nhận cho yêu cầu id trong tx của bên gọi: khóa đơn, vận đơn rồi yêu
// cầu; chỉ yêu cầu manual còn chờ, hay yêu cầu đã gửi hãng (sent) hay không rõ (unknown). confirmed → địa chỉ đơn
// (order.ApplyAddressChange), ship_to của vận đơn mới, dòng audit đơn trong kết quả để bên gọi ghi; rejected → địa chỉ
// cũ. Lệnh sửa địa chỉ còn in_doubt bị bỏ (commands.Abandon); lệnh đã bị dọn khỏi platform.jobs → bỏ qua.
//
// Lỗi (không đổi gì): kết quả lạ, ghi chú quá dài → ErrShipInvalid; không có yêu cầu → ErrNotFound; lỗi của Authorize trả
// nguyên; yêu cầu không xác nhận tay được, lệnh đang chạy lại → ErrAddressChangeNotAllowed; lỗi database trả lên.
func (s *Service) ResolveAddressChange(ctx context.Context, tx pgx.Tx, id uuid.UUID, in AddressResolveInput,
	a order.Actor,
) (AddressChangeResult, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return AddressChangeResult{}, errNotWired
	}
	status, ok := address.Resolution(in.Result)
	if !ok {
		return AddressChangeResult{}, fmt.Errorf("%w: kết quả đổi địa chỉ %q", ErrShipInvalid, in.Result)
	}
	note, err := addressNote(in.Note)
	if err != nil {
		return AddressChangeResult{}, err
	}
	q := store.New(tx)
	l, r, err := lockAddressChange(ctx, tx, q, id, in.Authorize)
	if err != nil {
		return AddressChangeResult{}, err
	}
	if !address.Resolvable(r.Mode, r.Status) {
		return AddressChangeResult{}, fmt.Errorf("xác nhận yêu cầu %s %s: %w", r.Mode, r.Status, ErrAddressChangeNotAllowed)
	}
	actor := auditActor{tenant: a.TenantID, id: a.UserID, kind: actorStaff}
	res, err := s.closeAddressChange(ctx, tx, q, l, r, status, note, actor)
	if err != nil {
		return AddressChangeResult{}, err
	}
	if r.JobID.Valid { // platform.jobs sau mọi dòng nghiệp vụ (ADR-46 nhóm 13); done, expired → không đổi
		err := commands.Abandon(ctx, tx, uuid.UUID(r.JobID.Bytes), "address_resolved_by_staff")
		switch {
		case errors.Is(err, commands.ErrCommandRunning):
			return AddressChangeResult{}, fmt.Errorf("bỏ lệnh sửa địa chỉ (%w): %w", ErrAddressChangeNotAllowed, err)
		case errors.Is(err, commands.ErrCommandNotFound): // lệnh đã xong và đã bị dọn (DeleteFinishedJobs)
		case err != nil:
			return AddressChangeResult{}, fmt.Errorf("bỏ lệnh sửa địa chỉ: %w", err) // lỗi database: WriteTx xử lý
		}
	}
	return res, nil
}

// auditActor là người làm của dòng audit đơn (staff khi nhân viên xác nhận, system khi hook của lệnh).
type auditActor struct {
	tenant, id uuid.UUID
	kind       string
}

// lockAddressChange khóa đơn, vận đơn của yêu cầu id (lockForCancel, Authorize) rồi yêu cầu (ADR-46 nhóm 7 → 8). Không có
// → ErrNotFound.
func lockAddressChange(ctx context.Context, tx pgx.Tx, q *store.Queries, id uuid.UUID, authorize Authorize,
) (lockedCancel, store.LockAddressChangeRow, error) {
	shipmentID, err := q.AddressChangeShipment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCancel{}, store.LockAddressChangeRow{}, ErrNotFound
	}
	if err != nil {
		return lockedCancel{}, store.LockAddressChangeRow{}, fmt.Errorf("đọc vận đơn của yêu cầu đổi địa chỉ: %w", err)
	}
	l, err := lockForCancel(ctx, tx, q, shipmentID, authorize)
	if err != nil {
		return lockedCancel{}, store.LockAddressChangeRow{}, err
	}
	r, err := q.LockAddressChange(ctx, id)
	if err != nil {
		return lockedCancel{}, store.LockAddressChangeRow{}, fmt.Errorf("khóa yêu cầu đổi địa chỉ: %w", err)
	}
	return l, r, nil
}

// closeAddressChange đổi yêu cầu r (đã khóa, còn mở) sang status (confirmed, rejected, sent, unknown). confirmed: ship_to
// của vận đơn l là địa chỉ mới, địa chỉ đơn qua order.ApplyAddressChange (đơn đã xong hay vận đơn không còn hiện hành:
// không đổi đơn), dòng audit khi đơn thật sự đổi. Báo hiệu vận đơn.
func (s *Service) closeAddressChange(ctx context.Context, tx pgx.Tx, q *store.Queries, l lockedCancel,
	r store.LockAddressChangeRow, status string, note pgtype.Text, actor auditActor,
) (AddressChangeResult, error) {
	var res AddressChangeResult
	if status == address.StatusConfirmed {
		rec, err := s.applyAddress(ctx, tx, q, l, r, actor)
		if err != nil {
			return AddressChangeResult{}, err
		}
		res.OrderAudit = rec
	}
	row, err := q.SetAddressChangeStatus(ctx, store.SetAddressChangeStatusParams{
		Status: status, ResolvedBy: pgtype.UUID{Bytes: actor.id, Valid: actor.id != uuid.Nil}, Note: note, ID: r.ID,
	})
	if err != nil {
		return AddressChangeResult{}, fmt.Errorf("đổi trạng thái yêu cầu đổi địa chỉ: %w", err) // đã khóa, còn mở
	}
	if err := s.publishShipment(ctx, tx, actor.tenant, l.ID, l.WarehouseID); err != nil {
		return AddressChangeResult{}, err
	}
	res.Change = changeView(l, row.ID, row.Mode, row.Status, row.Note, row.CreatedAt, row.ResolvedAt)
	return res, nil
}

// applyAddress: hãng đã nhận địa chỉ mới của yêu cầu r → ship_to của vận đơn l, rồi địa chỉ đơn. Trả dòng audit đơn khi
// địa chỉ đơn thật sự đổi.
func (s *Service) applyAddress(ctx context.Context, tx pgx.Tx, q *store.Queries, l lockedCancel,
	r store.LockAddressChangeRow, actor auditActor,
) (*AuditRecord, error) {
	var addr order.ShipAddress
	if err := json.Unmarshal(r.NewAddress, &addr); err != nil {
		return nil, fmt.Errorf("đọc địa chỉ mới của yêu cầu %s: %w", r.ID, err)
	}
	to := shipAddress(addr)
	shipTo, err := json.Marshal(shipToDoc{
		Name: to.Name, Phone: string(to.Phone), Line: to.Line, ProvinceCode: to.ProvinceCode, WardCode: to.WardCode,
	})
	if err != nil {
		return nil, fmt.Errorf("mã hóa địa chỉ giao của vận đơn: %w", err)
	}
	if err := q.SetShipmentShipTo(ctx, store.SetShipmentShipToParams{ShipTo: shipTo, ID: l.ID}); err != nil {
		return nil, fmt.Errorf("ghi địa chỉ giao của vận đơn: %w", err)
	}
	before, err := order.Get(ctx, tx, l.SellerID, l.OrderID)
	if err != nil {
		return nil, fmt.Errorf("đọc đơn của vận đơn: %w", err)
	}
	w := order.Writer{Events: s.d.Events, Queue: s.d.Queue, Realtime: s.d.Bus}
	changed, err := w.ApplyAddressChange(ctx, tx, l.SellerID, l.OrderID, l.ID, addr,
		order.Actor{TenantID: actor.tenant, UserID: actor.id, Type: actor.kind}, s.d.Now().UTC())
	switch {
	case errors.Is(err, order.ErrInvalidTransition):
		return nil, nil // đơn đã xong, hay vận đơn không còn hiện hành: hãng đã nhận nhưng đơn không đổi
	case err != nil:
		return nil, fmt.Errorf("đổi địa chỉ đơn: %w", err)
	case !changed:
		return nil, nil
	}
	ids := map[string]any{"changeId": r.ID.String(), "shipmentId": l.ID.String()} // khóa có "address" bị che
	return &AuditRecord{
		TenantID: actor.tenant, ActorID: actor.id, ActorType: actor.kind, Action: auditAddressChange,
		EntityType: "order", EntityID: l.OrderID,
		Before: withField(ids, before.ShipAddress), After: withField(ids, addr),
	}, nil
}

// withField chép ids rồi thêm địa chỉ v dưới tên trường ship_address (bị che khi lưu audit).
func withField(ids map[string]any, v order.ShipAddress) map[string]any {
	m := make(map[string]any, len(ids)+1)
	for k, x := range ids {
		m[k] = x
	}
	m[auditFieldShipAddress] = v
	return m
}

// shipAddress đổi địa chỉ của M2 sang địa chỉ theo mã địa giới của CRM cho hãng.
func shipAddress(a order.ShipAddress) Address {
	return Address{Name: a.Name, Phone: a.Phone, Line: string(a.Line), ProvinceCode: a.ProvinceCode, WardCode: a.WardCode}
}

// addressNote che SĐT, địa chỉ có số trong ghi chú s (rỗng → NULL); quá maxAddressNote ký tự → ErrShipInvalid.
func addressNote(s string) (pgtype.Text, error) {
	if s == "" {
		return pgtype.Text{}, nil
	}
	clean := []rune(logging.ScrubText(s))
	if len([]rune(s)) > maxAddressNote {
		return pgtype.Text{}, fmt.Errorf("%w: ghi chú đổi địa chỉ quá dài", ErrShipInvalid)
	}
	if len(clean) > maxAddressNote { // bản che có thể dài hơn bản gốc
		clean = clean[:maxAddressNote]
	}
	return pgtype.Text{String: string(clean), Valid: true}, nil
}

// changeView dựng AddressChange của yêu cầu id trên vận đơn l.
func changeView(l lockedCancel, id uuid.UUID, mode, status string, note pgtype.Text, created time.Time,
	resolved pgtype.Timestamptz,
) AddressChange {
	c := AddressChange{
		ID: id, ShipmentID: l.ID, OrderID: l.OrderID, WarehouseID: l.WarehouseID, Mode: mode, Status: status,
		Note: note.String, CreatedAt: created,
	}
	if resolved.Valid {
		t := resolved.Time
		c.ResolvedAt = &t
	}
	return c
}

// OpenAddressChanges đọc yêu cầu đổi địa chỉ còn mở (pending, sent, unknown; nhiều nhất một) của vận đơn s, không có
// địa chỉ (Task 134: màn hình cần id yêu cầu để nhân viên xác nhận sau khi tải lại trang). Chạy trong tx tenant của bên
// gọi; bên gọi đã đọc s bằng Get và kiểm phạm vi theo kho.
func OpenAddressChanges(ctx context.Context, tx pgx.Tx, s Shipment) ([]AddressChange, error) {
	rows, err := store.New(tx).OpenAddressChanges(ctx, s.ID)
	if err != nil {
		return nil, fmt.Errorf("đọc yêu cầu đổi địa chỉ đang mở: %w", err)
	}
	out := make([]AddressChange, 0, len(rows))
	for _, r := range rows {
		c := AddressChange{
			ID: r.ID, ShipmentID: r.ShipmentID, OrderID: s.OrderID, WarehouseID: s.WarehouseID, Mode: r.Mode,
			Status: r.Status, Note: r.Note.String, CreatedAt: r.CreatedAt,
		}
		if r.ResolvedAt.Valid {
			t := r.ResolvedAt.Time
			c.ResolvedAt = &t
		}
		out = append(out, c)
	}
	return out, nil
}
