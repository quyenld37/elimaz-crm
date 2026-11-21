package fulfillment

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/create"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng create (kế hoạch M3, Task M3-12): gửi hãng — Ship (vận đơn creating, lệnh fulfillment.create_shipment của M1),
// xem vận đơn (ShipmentView), "Gửi lại" (Resend), xử lý "không rõ hãng đã tạo chưa" (ResolveCreate). Lệnh tạo
// (CreateCommand, create_shipment.go) do wire.go đăng ký như M1; quyết định thuần ở internal/create.

func (*Module) createHandlers() []commands.Handler       { return nil }
func (*Module) createInbound() []webhooks.InboundHandler { return nil }
func (*Module) createSources() []webhooks.Source         { return nil }
func (*Module) createConsumers() []events.Consumer       { return nil }
func (*Module) createSchedules() []schedules.Schedule    { return nil }
func (*Module) createCleanups() []schedules.CleanupFunc  { return nil }

// Authorize kiểm người gọi có quyền ở kho warehouseID (lớp HTTP truyền vào; Service gọi sau khi đã khóa đơn, vận đơn
// để kho đọc dưới khóa). Lỗi trả nguyên.
type Authorize func(warehouseID uuid.UUID) error

// ShipInput là yêu cầu gửi hãng một đơn. Trường số 0, chuỗi rỗng, Options rỗng: dùng mặc định (cân nặng theo cài đặt
// vận chuyển × số món, dịch vụ và tùy chọn mặc định của tài khoản). TrackingCode, FeeActual chỉ cho tài khoản nhập tay
// (Task M3-15); tài khoản api có hai trường này → ErrShipInvalid. Authorize nil: không kiểm phạm vi (seed, test).
type ShipInput struct {
	OrderID, CarrierAccountID                uuid.UUID
	Service                                  string
	QuoteID                                  *uuid.UUID
	WeightGrams, LengthCm, WidthCm, HeightCm int
	DeclaredValue                            int64
	Options                                  Options
	TrackingCode                             string
	FeeActual                                *int64
	Authorize                                Authorize
}

// ShipmentView là vận đơn cho màn hình (A10, B6): chỉ mã, trạng thái, số tiền, mốc giờ; không có địa chỉ giao
// (ship_to). WarehouseID là kho của tài khoản hãng (phạm vi quyền, không trả ra API).
type ShipmentView struct {
	ID, OrderID, CarrierAccountID               uuid.UUID
	Code, CarrierKey, AccountName, Mode         string
	TrackingCode, Service, Status, Label        string
	SubStatus, CreateState, CancelKind          string
	CODAmount                                   int64
	CODCollectedAmount, Fee, FeeActual          *int64
	WeightGrams                                 *int
	Options                                     Options
	PickedUpAt, DeliveredAt, LastCarrierEventAt *time.Time
	LastSyncedAt, StuckSince                    *time.Time
	LastStatusAt, CreatedAt                     time.Time
	LastSyncError                               string
	NeedsReview, LabelReady, Partial            bool
	Version                                     int64
	WarehouseID                                 uuid.UUID
}

// AuditFields là phần sau của dòng audit cho vận đơn: chỉ mã, trạng thái, số tiền (không địa chỉ).
func (v ShipmentView) AuditFields() map[string]any {
	return map[string]any{
		"code": v.Code, "orderId": v.OrderID.String(), "carrierAccountId": v.CarrierAccountID.String(),
		"status": v.Status, "createState": v.CreateState, "codAmount": v.CODAmount, "version": v.Version,
	}
}

// shipToDoc là cột shipments.ship_to: bản chụp địa chỉ giao của đơn lúc gửi hãng (chỉ lệnh tạo đọc, không ra API).
type shipToDoc struct {
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	Line         string `json:"line"`
	ProvinceCode string `json:"provinceCode,omitempty"`
	WardCode     string `json:"wardCode,omitempty"`
}

func (d shipToDoc) address() Address {
	return Address{Name: d.Name, Phone: logging.Phone(d.Phone), Line: d.Line, ProvinceCode: d.ProvinceCode, WardCode: d.WardCode}
}

// Ship gửi hãng đơn in.OrderID bằng tài khoản in.CarrierAccountID trong tx của bên gọi (một transaction; VC03): khóa
// đơn (ADR-46 nhóm 7; đơn confirmed, chưa có vận đơn hiện hành), Authorize với kho của đơn, đọc tài khoản FOR SHARE
// (active, mode api, cùng người bán và cùng kho với đơn; VC15), báo phí in.QuoteID (nếu có) còn khớp, địa chỉ khớp mã
// địa giới của hãng (hãng có bảng địa giới), chèn vận đơn creating (mã NewShipmentCode, sinh lại khi trùng tối đa 3
// lần, create_state pending, bản chụp địa chỉ, kiện), gắn làm vận đơn hiện hành (order.Writer.AttachShipmentBy), ghi
// lệnh fulfillment.create_shipment (khóa create:<id>:1). Audit do bên gọi ghi.
//
// Lỗi: không có đơn, hay Authorize từ chối → ErrOrderNotFound (hay lỗi của Authorize); đơn đã có vận đơn →
// ErrShipmentExists; đơn không confirmed hay còn chờ lý do → ErrOrderNotShippable; không có tài khoản →
// ErrAccountNotFound; tài khoản không dùng được → ErrAccountUnavailable; báo phí cũ → ErrQuoteStale; địa chỉ chưa khớp
// → ErrAddressUnmapped; mã, phí nhập tay cho tài khoản api → ErrShipInvalid.
func (s *Service) Ship(ctx context.Context, tx pgx.Tx, in ShipInput, a order.Actor) (ShipmentView, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return ShipmentView{}, errNotWired
	}
	if in.TrackingCode != "" || in.FeeActual != nil {
		return ShipmentView{}, fmt.Errorf("%w: mã hãng, phí nhập tay chỉ cho tài khoản nhập tay", ErrShipInvalid)
	}
	q := store.New(tx)
	o, err := s.lockShippable(ctx, tx, q, in)
	if err != nil {
		return ShipmentView{}, err
	}
	acc, err := shareAccount(ctx, q, in.CarrierAccountID, o)
	if err != nil {
		return ShipmentView{}, err
	}
	ad, hasAdapter := s.adapter(acc.CarrierKey)
	if s.d.Carriers != nil && !hasAdapter {
		return ShipmentView{}, fmt.Errorf("hãng %s không có adapter: %w", acc.CarrierKey, ErrAccountUnavailable)
	}
	full, err := order.Get(ctx, tx, o.SellerID, o.ID)
	if err != nil {
		return ShipmentView{}, fmt.Errorf("đọc đơn: %w", err)
	}
	p, err := s.shipPlan(ctx, tx, in, acc, full)
	if err != nil {
		return ShipmentView{}, err
	}
	if in.QuoteID != nil {
		if err := s.checkQuote(ctx, q, *in.QuoteID, o, acc, p); err != nil {
			return ShipmentView{}, err
		}
	}
	if hasAdapter && ad.Capabilities().Locations {
		if err := s.checkMapped(ctx, tx, acc.CarrierKey, p.to); err != nil {
			return ShipmentView{}, err
		}
	}
	id, err := insertForShip(ctx, q, s.d.Now(), o, acc, in, p, a.UserID)
	if err != nil {
		return ShipmentView{}, err
	}
	ow := order.Writer{Events: s.d.Events, Queue: s.d.Queue, Realtime: s.d.Bus}
	switch err := ow.AttachShipmentBy(ctx, tx, o.SellerID, o.ID, id, a, s.d.Now().UTC()); {
	case errors.Is(err, order.ErrShipmentAlreadyAttached):
		return ShipmentView{}, ErrShipmentExists
	case errors.Is(err, order.ErrInvalidTransition):
		return ShipmentView{}, fmt.Errorf("gắn vận đơn: %w", ErrOrderNotShippable)
	case err != nil:
		return ShipmentView{}, fmt.Errorf("gắn vận đơn vào đơn: %w", err)
	}
	if err := s.enqueueCreate(ctx, tx, q, a.TenantID, id, 1); err != nil {
		return ShipmentView{}, err
	}
	return s.ShipmentView(ctx, tx, id)
}

// lockShippable khóa đơn của in (người bán đọc từ ord.orders_v), kiểm phạm vi và trạng thái giao được.
func (*Service) lockShippable(ctx context.Context, tx pgx.Tx, q *store.Queries, in ShipInput) (order.Snapshot, error) {
	ref, err := q.OrderShipRef(ctx, in.OrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return order.Snapshot{}, ErrOrderNotFound
	}
	if err != nil {
		return order.Snapshot{}, fmt.Errorf("đọc người bán của đơn: %w", err)
	}
	o, err := order.LockSnapshot(ctx, tx, ref.SellerID, in.OrderID)
	if errors.Is(err, order.ErrNotFound) {
		return order.Snapshot{}, ErrOrderNotFound
	}
	if err != nil {
		return order.Snapshot{}, fmt.Errorf("khóa đơn: %w", err)
	}
	if in.Authorize != nil {
		if err := in.Authorize(o.WarehouseID); err != nil {
			return order.Snapshot{}, err
		}
	}
	if o.CurrentShipmentID != nil {
		return order.Snapshot{}, ErrShipmentExists
	}
	if o.Status != order.StatusConfirmed {
		return order.Snapshot{}, fmt.Errorf("đơn %s: %w", o.Status, ErrOrderNotShippable)
	}
	return o, nil
}

// shareAccount đọc tài khoản id FOR SHARE và kiểm dùng được cho đơn o: active, mode api, cùng người bán (ADR-29), cùng
// kho giữ hàng của đơn (ADR-18).
func shareAccount(ctx context.Context, q *store.Queries, id uuid.UUID, o order.Snapshot) (store.ShareAccountForShipRow, error) {
	acc, err := q.ShareAccountForShip(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return acc, ErrAccountNotFound
	}
	if err != nil {
		return acc, fmt.Errorf("đọc tài khoản hãng: %w", err)
	}
	if acc.Status != AccountActive || acc.Mode != ModeAPI || acc.SellerID != o.SellerID || acc.WarehouseID != o.WarehouseID {
		return acc, fmt.Errorf("tài khoản hãng %s (%s, %s): %w", id, acc.Status, acc.Mode, ErrAccountUnavailable)
	}
	return acc, nil
}

// shipPlan là dữ liệu gửi đã điền mặc định: nơi nhận, kiện, dịch vụ, tùy chọn, COD.
type shipPlan struct {
	to      Address
	parcel  Parcel
	service string
	options Options
	cod     int64
}

// shipPlan dựng dữ liệu gửi: nơi nhận shipTo(o), kiện QuoteParcel (cùng nguồn với báo phí để InputHash khớp) cộng
// kích thước của in, dịch vụ và tùy chọn mặc định của tài khoản khi in để trống. Cài đặt chỉ đọc khi cần cân mặc định.
func (*Service) shipPlan(ctx context.Context, tx pgx.Tx, in ShipInput, acc store.ShareAccountForShipRow, o order.Order,
) (shipPlan, error) {
	var st Settings
	if in.WeightGrams <= 0 {
		var err error
		if st, _, err = LoadSettings(ctx, tx); err != nil {
			return shipPlan{}, err
		}
	}
	parcel := QuoteParcel(o, in.WeightGrams, in.DeclaredValue, st)
	parcel.LengthCm, parcel.WidthCm, parcel.HeightCm = in.LengthCm, in.WidthCm, in.HeightCm
	p := shipPlan{to: shipTo(o), parcel: parcel, service: in.Service, options: in.Options, cod: o.CODAmount}
	if p.service == "" {
		p.service = acc.DefaultService
	}
	if p.options == (Options{}) {
		var d optionsDoc
		if err := json.Unmarshal(acc.DefaultOptions, &d); err != nil {
			return shipPlan{}, fmt.Errorf("đọc tùy chọn mặc định của tài khoản hãng: %w", err)
		}
		p.options = d.options()
	}
	return p, nil
}

// checkQuote: báo phí id phải ok, của đúng đơn và tài khoản, chưa hết valid_until (đồng hồ của Service), input_hash
// khớp dữ liệu gửi hiện tại; không thì ErrQuoteStale.
func (s *Service) checkQuote(ctx context.Context, q *store.Queries, id uuid.UUID, o order.Snapshot,
	acc store.ShareAccountForShipRow, p shipPlan,
) error {
	r, err := q.QuoteForShip(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("báo phí %s không có: %w", id, ErrQuoteStale)
	}
	if err != nil {
		return fmt.Errorf("đọc báo phí: %w", err)
	}
	fresh := r.Status == "ok" && r.OrderID == o.ID && r.CarrierAccountID == acc.ID && r.Service == p.service &&
		r.ValidUntil.Valid && s.d.Now().Before(r.ValidUntil.Time) &&
		bytes.Equal(r.InputHash, InputHash(p.to, p.parcel, p.cod, p.service, p.options))
	if !fresh {
		return fmt.Errorf("báo phí %s: %w", id, ErrQuoteStale)
	}
	return nil
}

// checkMapped: tỉnh và phường của địa chỉ giao phải có mã của hãng carrierKey (carrier_locations); chưa gọi hãng.
func (s *Service) checkMapped(ctx context.Context, tx pgx.Tx, carrierKey string, to Address) error {
	lm, err := s.LocationMap(ctx, tx, carrierKey)
	if err != nil {
		return err
	}
	_, okP := lm.Carrier("province", to.ProvinceCode)
	_, okW := lm.Carrier("ward", to.WardCode)
	if to.ProvinceCode == "" || to.WardCode == "" || !okP || !okW {
		return ErrAddressUnmapped
	}
	return nil
}

// insertForShip chèn vận đơn creating; không dòng nào: đơn đã có vận đơn chưa hủy → ErrShipmentExists, ngược lại mã
// trùng → sinh lại (tối đa codeAttempts lần).
func insertForShip(ctx context.Context, q *store.Queries, now time.Time, o order.Snapshot,
	acc store.ShareAccountForShipRow, in ShipInput, p shipPlan, userID uuid.UUID,
) (uuid.UUID, error) {
	opts, err := json.Marshal(optionsJSON(p.options))
	if err != nil {
		return uuid.Nil, fmt.Errorf("mã hóa tùy chọn giao: %w", err)
	}
	shipTo, err := json.Marshal(shipToDoc{
		Name: p.to.Name, Phone: string(p.to.Phone), Line: p.to.Line, ProvinceCode: p.to.ProvinceCode, WardCode: p.to.WardCode,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("mã hóa địa chỉ giao: %w", err)
	}
	params := store.InsertShipmentForShipParams{
		SellerID: o.SellerID, OrderID: o.ID, CarrierKey: acc.CarrierKey, CarrierAccountID: acc.ID, CodAmount: o.CODAmount,
		CreatedBy: pgtype.UUID{Bytes: userID, Valid: userID != uuid.Nil}, Mode: acc.Mode, Service: p.service,
		WeightGrams: optInt4(p.parcel.WeightGrams), LengthCm: optInt4(p.parcel.LengthCm), WidthCm: optInt4(p.parcel.WidthCm),
		HeightCm: optInt4(p.parcel.HeightCm), DeclaredValue: p.parcel.DeclaredValue, Options: opts, ShipTo: shipTo,
	}
	if in.QuoteID != nil {
		params.QuoteID = pgtype.UUID{Bytes: *in.QuoteID, Valid: true}
	}
	for range codeAttempts {
		if params.ID, err = uuid.NewV7(); err != nil {
			return uuid.Nil, fmt.Errorf("sinh mã vận đơn: %w", err)
		}
		params.Code = NewShipmentCode(now, rand.Reader)
		_, err := q.InsertShipmentForShip(ctx, params)
		if err == nil {
			return params.ID, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("chèn vận đơn: %w", err)
		}
		active, err := q.HasActiveShipment(ctx, o.ID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("kiểm vận đơn của đơn: %w", err)
		}
		if active {
			return uuid.Nil, ErrShipmentExists
		}
	}
	return uuid.Nil, errCodeExhausted
}

func optInt4(v int) pgtype.Int4 {
	return pgtype.Int4{Int32: int32(min(max(v, 0), 1<<31-1)), Valid: v > 0}
}

// enqueueCreate ghi lệnh tạo cho lần gửi attempt của vận đơn id và lưu mã lệnh vào create_job_id (cùng tx). Lệnh tạo
// không có order_key: lệnh của lần gửi trước kết thúc dead (chắc chưa gửi, Q-5) sẽ chặn mãi mọi việc cùng khóa theo
// thứ tự strict (lần gửi lại, lệnh tra trạng thái của vận đơn); hai lần gửi không bao giờ cùng chờ (Resend chỉ khi
// not_sent).
func (s *Service) enqueueCreate(ctx context.Context, tx pgx.Tx, q *store.Queries, tenant, id uuid.UUID, attempt int) error {
	jobID, _, err := s.d.Queue.Enqueue(ctx, tx, CreateCommandKind(), queue.NewJob{
		TenantID: &tenant, Payload: commandPayload{ShipmentID: id}, DedupeKey: create.DedupeKey(id, attempt),
	})
	if err != nil {
		return fmt.Errorf("ghi lệnh tạo vận đơn: %w", err)
	}
	if err := q.SetShipmentCreateJob(ctx, store.SetShipmentCreateJobParams{
		JobID: pgtype.UUID{Bytes: jobID, Valid: true}, ID: id,
	}); err != nil {
		return fmt.Errorf("ghi mã lệnh tạo của vận đơn: %w", err)
	}
	return nil
}

// ShipmentView đọc vận đơn id cho màn hình; không có (hay tenant khác) → ErrNotFound.
func (*Service) ShipmentView(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ShipmentView, error) {
	r, err := store.New(tx).ShipmentViewByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShipmentView{}, ErrNotFound
	}
	if err != nil {
		return ShipmentView{}, fmt.Errorf("đọc vận đơn: %w", err)
	}
	return viewOf(store.OrderShipmentViewsRow(r)), nil
}

// OrderShipments trả mọi vận đơn của đơn orderID (cả đã hủy), mới nhất trước, tối đa limit dòng sau after, kèm kho
// của đơn (phạm vi quyền của bên gọi) và more khi còn trang sau. Không có đơn → ErrOrderNotFound.
func (*Service) OrderShipments(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, after *uuid.UUID, limit int,
) (views []ShipmentView, warehouseID uuid.UUID, more bool, err error) {
	q := store.New(tx)
	ref, err := q.OrderShipRef(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, uuid.Nil, false, ErrOrderNotFound
	}
	if err != nil {
		return nil, uuid.Nil, false, fmt.Errorf("đọc kho của đơn: %w", err)
	}
	p := store.OrderShipmentViewsParams{OrderID: orderID, Lim: int32(min(max(limit, 1), 200)) + 1}
	if after != nil {
		p.After = pgtype.UUID{Bytes: *after, Valid: true}
	}
	rows, err := q.OrderShipmentViews(ctx, p)
	if err != nil {
		return nil, uuid.Nil, false, fmt.Errorf("đọc vận đơn của đơn: %w", err)
	}
	more = len(rows) > int(p.Lim)-1
	if more {
		rows = rows[:p.Lim-1]
	}
	views = make([]ShipmentView, 0, len(rows))
	for _, r := range rows {
		views = append(views, viewOf(r))
	}
	return views, ref.WarehouseID, more, nil
}

func viewOf(r store.OrderShipmentViewsRow) ShipmentView {
	v := ShipmentView{
		ID: r.ID, OrderID: r.OrderID, CarrierAccountID: r.CarrierAccountID, Code: r.Code, CarrierKey: r.CarrierKey,
		AccountName: r.AccountName, Mode: r.Mode, TrackingCode: r.TrackingCode.String, Service: r.Service,
		Status: r.Status, Label: shipstate.Display(shipstate.Status(r.Status), r.SubStatus, ""), SubStatus: r.SubStatus,
		CreateState: r.CreateState, CancelKind: r.CancelKind.String, CODAmount: r.CodAmount,
		CODCollectedAmount: int8Ptr(r.CodCollectedAmount), Fee: int8Ptr(r.Fee), FeeActual: int8Ptr(r.FeeActual),
		PickedUpAt: tsPtr(r.PickedUpAt), DeliveredAt: tsPtr(r.DeliveredAt), LastCarrierEventAt: tsPtr(r.LastCarrierEventAt),
		LastSyncedAt: tsPtr(r.LastSyncedAt), StuckSince: tsPtr(r.StuckSince), LastStatusAt: r.LastStatusAt.UTC(),
		CreatedAt: r.CreatedAt.UTC(), LastSyncError: r.LastSyncError.String, NeedsReview: r.NeedsReview,
		LabelReady: r.LabelReady, Partial: r.Partial, Version: r.Version, WarehouseID: r.WarehouseID,
	}
	if r.WeightGrams.Valid {
		w := int(r.WeightGrams.Int32)
		v.WeightGrams = &w
	}
	var d optionsDoc
	if json.Unmarshal(r.Options, &d) == nil {
		v.Options = d.options()
	}
	return v
}

func int8Ptr(v pgtype.Int8) *int64 {
	if v.Valid {
		n := v.Int64
		return &n
	}
	return nil
}

func tsPtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

// lockForCreate khóa đơn (nhóm 7) rồi vận đơn id (nhóm 8) và kiểm phạm vi theo kho của tài khoản hãng của vận đơn.
func lockForCreate(ctx context.Context, tx pgx.Tx, q *store.Queries, id uuid.UUID, authorize Authorize,
) (lockedCreate, error) {
	ref, err := q.ShipmentOrderRef(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCreate{}, ErrNotFound
	}
	if err != nil {
		return lockedCreate{}, fmt.Errorf("đọc đơn của vận đơn: %w", err)
	}
	o, err := order.LockSnapshot(ctx, tx, ref.SellerID, ref.OrderID)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			return lockedCreate{}, ErrNotFound
		}
		return lockedCreate{}, fmt.Errorf("khóa đơn của vận đơn: %w", err)
	}
	row, err := q.LockShipmentForCreate(ctx, id)
	if err != nil {
		return lockedCreate{}, fmt.Errorf("khóa vận đơn: %w", err)
	}
	l := lockedCreate{LockShipmentForCreateRow: row, orderStatus: o.Status}
	if authorize != nil {
		wh, err := AccountWarehouse(ctx, tx, row.CarrierAccountID)
		if err != nil {
			return l, err
		}
		if err := authorize(wh); err != nil {
			return l, err
		}
	}
	return l, nil
}

// lockedCreate là vận đơn đã khóa cho lệnh tạo kèm trạng thái đơn đọc dưới khóa đơn.
type lockedCreate struct {
	store.LockShipmentForCreateRow
	orderStatus string
}

// cancelRequested: đơn đã yêu cầu hủy (M2 đưa đơn confirmed có vận đơn sang cancel_requested).
func (l lockedCreate) cancelRequested() bool { return l.orderStatus == order.StatusCancelRequested }

// intent là ý định hủy dùng khi lệnh tạo kết thúc mà hãng chắc chưa tạo (create.EffectiveIntent).
func (l lockedCreate) intent(s create.Shipment) string {
	return create.EffectiveIntent(s, l.CancelIntent.String, l.cancelRequested())
}

// createShipment là vận đơn l dưới dạng của các quyết định thuần (create.Shipment) cho lệnh jobID.
func (l lockedCreate) createShipment(jobID uuid.UUID) create.Shipment {
	return create.Shipment{
		Creating: l.Status == string(StatusCreating), State: l.CreateState,
		Current: !l.CreateJobID.Valid || uuid.UUID(l.CreateJobID.Bytes) == jobID,
	}
}

// Resend ("Gửi lại", Q-5) ghi lần gửi mới cho vận đơn id trong tx của bên gọi: chỉ khi vận đơn creating và chắc chắn
// hãng chưa tạo (create_state not_sent); create_attempts + 1, create_state pending, xóa dấu in_doubt, version + 1, lệnh
// mới (khóa create:<id>:<lần>). Lỗi: không có → ErrNotFound; trạng thái khác → ErrCreateInProgress; lỗi của authorize
// trả nguyên. Audit do bên gọi ghi.
func (s *Service) Resend(ctx context.Context, tx pgx.Tx, id uuid.UUID, a order.Actor, authorize Authorize,
) (ShipmentView, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return ShipmentView{}, errNotWired
	}
	q := store.New(tx)
	row, err := lockForCreate(ctx, tx, q, id, authorize)
	if err != nil {
		return ShipmentView{}, err
	}
	if !create.CanResend(row.Status == string(StatusCreating), row.CreateState) {
		return ShipmentView{}, fmt.Errorf("gửi lại vận đơn %s (%s): %w", row.Status, row.CreateState, ErrCreateInProgress)
	}
	if row.orderStatus != order.StatusConfirmed { // đơn đã yêu cầu hủy: không gửi kiện mới cho đơn khách hủy
		return ShipmentView{}, fmt.Errorf("gửi lại vận đơn của đơn %s: %w", row.orderStatus, ErrOrderNotShippable)
	}
	r, err := q.ResendShipmentCreate(ctx, id)
	if err != nil {
		return ShipmentView{}, fmt.Errorf("ghi lần gửi mới: %w", err)
	}
	if err := s.enqueueCreate(ctx, tx, q, a.TenantID, id, int(r.CreateAttempts)); err != nil {
		return ShipmentView{}, err
	}
	return s.afterCreateChange(ctx, tx, a.TenantID, id)
}

// afterCreateChange đọc lại vận đơn id và báo hiệu shipment.changed.
func (s *Service) afterCreateChange(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (ShipmentView, error) {
	v, err := s.ShipmentView(ctx, tx, id)
	if err != nil {
		return ShipmentView{}, err
	}
	return v, s.publishShipment(ctx, tx, tenant, id, v.WarehouseID)
}

// ResolveInput là kết quả nhân viên tra trang hãng cho vận đơn "không rõ": Result created (TrackingCode là mã hãng)
// hay not_created; Reason là lý do nhân viên ghi.
type ResolveInput struct {
	Result, TrackingCode, Reason string
}

// ResolveCreate xử lý "không rõ hãng đã tạo vận đơn chưa" (create_state unknown) theo kết quả nhân viên tra trong tx
// của bên gọi: created → gắn mã hãng (AttachTrackingCode: awaiting_pickup, created) và đặt lệnh tạo thành done
// (commands.ResolveManually); not_created → create_state not_sent (version + 1, chờ "Gửi lại") và bỏ lệnh
// (commands.Abandon). Cả hai đóng việc in_doubt_create. Lỗi: dữ liệu sai → ErrShipInvalid; không có → ErrNotFound;
// không ở unknown → ErrCreateInProgress; mã hãng đã thuộc vận đơn khác → ErrTrackingCodeConflict; lỗi của authorize
// trả nguyên. Audit do bên gọi ghi.
func (s *Service) ResolveCreate(ctx context.Context, tx pgx.Tx, id uuid.UUID, in ResolveInput, a order.Actor,
	authorize Authorize,
) (ShipmentView, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return ShipmentView{}, errNotWired
	}
	if !create.ValidResolution(in.Result, in.TrackingCode, in.Reason) || len(in.TrackingCode) > maxTrackingCode {
		return ShipmentView{}, fmt.Errorf("%w: kết quả xử lý không rõ", ErrShipInvalid)
	}
	q := store.New(tx)
	row, err := lockForCreate(ctx, tx, q, id, authorize)
	if err != nil {
		return ShipmentView{}, err
	}
	if !create.CanResolve(row.Status == string(StatusCreating), row.CreateState) || !row.CreateJobID.Valid {
		return ShipmentView{}, fmt.Errorf("xử lý không rõ vận đơn %s (%s): %w", row.Status, row.CreateState, ErrCreateInProgress)
	}
	jobID := uuid.UUID(row.CreateJobID.Bytes)
	by := pgtype.UUID{Bytes: a.UserID, Valid: a.UserID != uuid.Nil}
	if in.Result == create.ResultCreated {
		return s.resolveCreated(ctx, tx, q, id, jobID, in.TrackingCode, by)
	}
	return s.resolveNotCreated(ctx, tx, q, row, jobID, by, a.TenantID)
}

// resolveCreated: nhân viên thấy kiện ở hãng với mã trackingCode → gắn mã, đóng việc "không rõ", lệnh tạo done.
func (s *Service) resolveCreated(ctx context.Context, tx pgx.Tx, q *store.Queries, id, jobID uuid.UUID,
	trackingCode string, by pgtype.UUID,
) (ShipmentView, error) {
	res, err := s.AttachTrackingCode(ctx, tx, id, trackingCode, nil)
	switch {
	case err != nil:
		return ShipmentView{}, err
	case res.Conflict:
		return ShipmentView{}, ErrTrackingCodeConflict
	}
	if err := q.ResolveCreateAnomaly(ctx, store.ResolveCreateAnomalyParams{
		ShipmentID: id, Resolution: pgtype.Text{String: create.ResultCreated, Valid: true}, ResolvedBy: by,
	}); err != nil {
		return ShipmentView{}, fmt.Errorf("đóng việc không rõ đã tạo: %w", err)
	}
	result, err := json.Marshal(createResult{TrackingCode: trackingCode})
	if err != nil {
		return ShipmentView{}, fmt.Errorf("mã hóa kết quả tạo vận đơn: %w", err)
	}
	if err := commands.ResolveManually(ctx, tx, jobID, result, []commands.Handler{s.createCommand()}); err != nil {
		return ShipmentView{}, commandBusy("đặt lệnh tạo đã xong", err)
	}
	return s.ShipmentView(ctx, tx, id)
}

// resolveNotCreated: nhân viên xác nhận hãng chưa tạo → not_sent (chờ "Gửi lại"), đóng việc "không rõ", bỏ lệnh; đơn
// đã yêu cầu hủy → hủy vận đơn và đơn, nhả hàng.
func (s *Service) resolveNotCreated(ctx context.Context, tx pgx.Tx, q *store.Queries, row lockedCreate,
	jobID uuid.UUID, by pgtype.UUID, tenant uuid.UUID,
) (ShipmentView, error) {
	if _, err := q.MarkCreateNotSent(ctx, store.MarkCreateNotSentParams{Bump: 1, ID: row.ID}); err != nil {
		return ShipmentView{}, fmt.Errorf("ghi chưa gửi: %w", err)
	}
	if err := q.ResolveCreateAnomaly(ctx, store.ResolveCreateAnomalyParams{
		ShipmentID: row.ID, Resolution: pgtype.Text{String: create.ResultNotCreated, Valid: true}, ResolvedBy: by,
	}); err != nil {
		return ShipmentView{}, fmt.Errorf("đóng việc không rõ đã tạo: %w", err)
	}
	if err := commands.Abandon(ctx, tx, jobID, "confirmed_not_created"); err != nil {
		return ShipmentView{}, commandBusy("bỏ lệnh tạo", err)
	}
	if !row.cancelRequested() {
		return s.afterCreateChange(ctx, tx, tenant, row.ID)
	}
	sh := row.createShipment(jobID)
	if err := s.createCommand().cancelEnded(ctx, tx, q, tenant, row, sh, row.intent(sh)); err != nil {
		return ShipmentView{}, err
	}
	return s.ShipmentView(ctx, tx, row.ID)
}

// createCommand là lệnh tạo dựng từ phụ thuộc của Service (OnDone cho ResolveManually; không gọi hãng).
func (s *Service) createCommand() *CreateCommand {
	return NewCreateCommand(CommandOptions{
		DB: s.d.DB, Events: s.d.Events, Queue: s.d.Queue, Realtime: s.d.Bus, Keys: s.d.Keys, Now: s.d.Now,
	})
}

// commandBusy: lệnh tạo vừa được chạy lại (pending, running sau queue.RetryJob) hay đã xong theo đường khác →
// ErrCreateInProgress (409, transaction lùi); lỗi khác bọc với việc đang làm.
func commandBusy(doing string, err error) error {
	if errors.Is(err, commands.ErrCommandRunning) || errors.Is(err, commands.ErrCommandNotResolvable) {
		return fmt.Errorf("%s (%w): %w", doing, err, ErrCreateInProgress)
	}
	return fmt.Errorf("%s: %w", doing, err)
}
