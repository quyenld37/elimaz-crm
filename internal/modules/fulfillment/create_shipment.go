package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/create"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh của fulfillment.
const (
	createCommandName  = "fulfillment.create_shipment"
	refreshCommandName = "fulfillment.refresh_status"
	// createTimeout là hạn của một lần gửi hãng (04 mục 5.3); lệnh tạo không hết hạn khi chờ (Q-5: người quyết).
	createTimeout = 25 * time.Second

	// TopicShipmentStatusChanged phát mỗi lần vận đơn tiến trạng thái, hay sang cancelled khi lệnh tạo kết thúc hỏng
	// (Seq = shipments.version; docs/views.md).
	TopicShipmentStatusChanged = "shipment.status_changed"
	aggregateShipment          = "shipment"

	reasonNoAdapter    = "no_adapter"
	reasonRejected     = "carrier_rejected"
	reasonNotSent      = "carrier_not_sent"
	reasonNotFound     = "carrier_not_found"
	reasonReadFailed   = "carrier_read_failed"
	reasonStale        = "shipment_not_creating"
	reasonBadPayload   = "bad_payload"
	reasonNotSupported = "not_supported"
	// reasonOrderCancelRequested: đơn đã yêu cầu hủy trước khi lệnh gửi hãng (chắc chưa gửi; OnTerminal hủy vận đơn).
	reasonOrderCancelRequested = "order_cancel_requested"
	reasonKeys                 = "credentials_unreadable"
	reasonConfirmed            = "confirmed_not_sent"
	reasonInconclusive         = "lookup_inconclusive"

	anomalyInDoubtCreate = "in_doubt_create"
)

// StatusChanged là payload của shipment.status_changed: chỉ mã và số (A16). Ba trường đầu của M1; M3 (Task M3-08,
// ApplyTracking) thêm phần sau, bỏ trống khi không có (sự kiện của M1 không có): OrderID, From (trạng thái trước),
// Effects (tác động của bước, theo thứ tự), CODCollected (tiền hãng báo đã thu), Partial (giao một phần), OccurredAt
// (giờ sự kiện của hãng).
type StatusChanged struct {
	ShipmentID   uuid.UUID  `json:"shipmentId"`
	Status       Status     `json:"status"`
	CODAmount    int64      `json:"codAmount"`
	OrderID      uuid.UUID  `json:"orderId,omitzero"`
	From         Status     `json:"from,omitempty"`
	Effects      []string   `json:"effects,omitempty"`
	CODCollected *int64     `json:"codCollected,omitempty"`
	Partial      bool       `json:"partial,omitempty"`
	OccurredAt   *time.Time `json:"occurredAt,omitempty"`
	// Source là nguồn của bước khi bước đi qua luồng áp tin (carrier_api, staff, system; rỗng ở sự kiện cũ và luồng
	// khác): sổ COD ghi lý do bút toán đảo theo nguồn (Task 138).
	Source string `json:"source,omitempty"`
}

// CreateCommandKind là loại lệnh fulfillment.create_shipment của M1 (giữ tên): không hết hạn khi chờ, mỗi lần gửi tối
// đa 25 s (kế hoạch M3, Task M3-12).
func CreateCommandKind() queue.Kind {
	return commands.CommandKind(createCommandName, 0, createTimeout)
}

// CommandOptions là phụ thuộc của lệnh tạo vận đơn (cmd/crm nối; lệnh tra dựng từ Service, NewRefreshCommand):
// database (đọc ngoài transaction), adapter theo Key(), đường ghi của order (Events, Queue, Realtime), bộ khóa giải mã
// khóa API của tài khoản hãng (nil: tài khoản có khóa thì không gửi được), đồng hồ (nil: time.Now).
type CommandOptions struct {
	DB       *db.DB
	Adapters []CarrierAdapter
	Events   *events.Registry
	Queue    queue.JobQueue
	Realtime realtime.Bus
	Keys     *fieldcrypt.Keyring
	Now      func() time.Time
}

// commandPayload là payload của lệnh tạo và lệnh tra: chỉ mã vận đơn.
type commandPayload struct {
	ShipmentID uuid.UUID `json:"shipmentId"`
}

func parsePayload(j queue.Job) (commandPayload, error) {
	var p commandPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.ShipmentID == uuid.Nil || j.TenantID == nil {
		return p, fmt.Errorf("lệnh %s: payload hoặc tenant không hợp lệ: %w", j.ID, queue.ErrPermanent)
	}
	return p, nil
}

// emitStatus phát shipment.status_changed với Seq = version mới của vận đơn.
func emitStatus(ctx context.Context, tx pgx.Tx, ev *events.Registry, jq queue.JobQueue, tenant, id, orderID uuid.UUID,
	st, from Status, cod, version int64,
) error {
	if _, _, err := ev.Emit(ctx, tx, jq, events.Event{
		TenantID: tenant, AggregateType: aggregateShipment, AggregateID: id.String(), Seq: version,
		Topic:   TopicShipmentStatusChanged,
		Payload: StatusChanged{ShipmentID: id, Status: st, CODAmount: cod, OrderID: orderID, From: from},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicShipmentStatusChanged, err)
	}
	return nil
}

// CreateCommand là lệnh fulfillment.create_shipment (commands.Handler, Looker, DoneHook, queue.TerminalHook). Gửi hãng
// theo mã vận đơn của CRM; không rõ thì tra trước, không bao giờ tự gửi lại (AutoRetryNotSent = false, Q-5): chỉ người
// bấm "Gửi lại" (Service.Resend).
type CreateCommand struct {
	svc      *Service
	adapters map[string]CarrierAdapter
}

// NewCreateCommand tạo lệnh tạo vận đơn.
func NewCreateCommand(o CommandOptions) *CreateCommand {
	m := make(map[string]CarrierAdapter, len(o.Adapters))
	for _, a := range o.Adapters {
		m[a.Key()] = a
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	// Bộ hãng của Service: gắn mã hãng xong mà vận đơn có ý định hủy thì RequestCancel cần biết hãng hủy qua API được
	// không (Task M3-14). Adapter trùng khóa đã bị map trên gộp; lỗi dựng (khóa rỗng) → nil: hủy đi đường nhập tay.
	carriers, err := NewRegistry(slices.Collect(maps.Values(m))...)
	if err != nil {
		carriers = nil
	}
	return &CreateCommand{
		svc: &Service{d: Deps{
			DB: o.DB, Queue: o.Queue, Events: o.Events, Bus: o.Realtime, Keys: o.Keys, Now: now, Carriers: carriers,
		}},
		adapters: m,
	}
}

// Kind trả CreateCommandKind().
func (*CreateCommand) Kind() queue.Kind { return CreateCommandKind() }

// AutoRetryNotSent trả false (Q-5, 04 mục 5.3): chắc chắn chưa gửi thì lệnh dead, vận đơn not_sent chờ người gửi lại
// (M1 để true cho lát mỏng).
func (*CreateCommand) AutoRetryNotSent() bool { return false }

type createResult struct {
	TrackingCode string `json:"trackingCode"`
	Fee          *int64 `json:"fee,omitempty"`
}

// createTarget là vận đơn của lệnh đã đọc kèm tài khoản (giải mã), adapter, yêu cầu gửi; refused khác nil: không gửi
// được (lý do trong outcome).
type createTarget struct {
	row     store.CreateTargetRow
	ship    create.Shipment
	acc     Account
	ad      CarrierAdapter
	refused *commands.Outcome
}

// target đọc vận đơn và tài khoản trong một transaction ngắn (ngoài transaction của lệnh). Không có vận đơn →
// ErrNotFound; lỗi đọc trả lên. Tài khoản không active, nhập tay, không có, hãng không có adapter → refused rejected
// (VC15, không đổi sang tài khoản khác); khóa API không giải được → refused not_sent (cấu hình, người sửa rồi gửi lại).
func (c *CreateCommand) target(ctx context.Context, j queue.Job, id uuid.UUID) (createTarget, error) {
	var t createTarget
	err := tenancy.WithTenant(ctx, c.svc.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		t = createTarget{} // RetryTx có thể chạy lại
		q := store.New(tx)
		row, err := q.CreateTarget(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("đọc vận đơn của lệnh tạo: %w", err)
		}
		t.row = row
		t.ship = create.Shipment{
			Creating: row.Status == string(StatusCreating), State: row.CreateState,
			Current: !row.CreateJobID.Valid || uuid.UUID(row.CreateJobID.Bytes) == j.ID,
		}
		ad, ok := c.adapters[row.CarrierKey]
		if !ok || row.Mode != ModeAPI {
			t.refused = &commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnavailable}
			if !ok {
				t.refused = &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNoAdapter}
			}
			return nil
		}
		t.ad = ad
		t.acc, err = c.svc.ResolveAccount(ctx, tx, row.CarrierAccountID)
		switch {
		case errors.Is(err, ErrAccountUnavailable), errors.Is(err, ErrAccountNotFound):
			t.refused = &commands.Outcome{Status: commands.StatusRejected, Reason: reasonUnavailable}
			return nil
		case errors.Is(err, ErrKeysMissing), errors.Is(err, ErrCredentialsUnreadable):
			t.refused = &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonKeys}
			return nil
		case err != nil:
			return err
		}
		if ad.Capabilities().Locations {
			if t.acc.Locations, err = c.svc.LocationMap(ctx, tx, row.CarrierKey); err != nil {
				return err
			}
		}
		return nil
	}, db.TxWorker)
	return t, err //nolint:wrapcheck // lỗi đã bọc trong thân
}

// Execute (ngoài transaction): vận đơn còn chờ kết quả của lần gửi này → CreateShipment theo shipments.code; vận đơn
// "không rõ" (lệnh in_doubt được người chạy lại) → chỉ tra theo mã, không gửi; lệnh của lần gửi cũ hay vận đơn đã có
// kết quả → rejected, không gọi hãng. Kết quả gửi: thành công → done (mã, phí); chắc chưa gửi (ErrNotSent, 429, 503,
// thiếu adapter, khóa API) → not_sent (dead, chờ người gửi lại); từ chối, khóa sai, tài khoản không dùng được, hãng
// chưa hỗ trợ → ghi dấu rejected rồi rejected (OnTerminal hủy vận đơn); lỗi khác, quá giờ → lỗi (in_doubt).
func (c *CreateCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	p, perr := parsePayload(j)
	if perr != nil {
		return rejectBadPayload()
	}
	t, err := c.target(ctx, j, p.ShipmentID)
	if out, failed := targetFailed(err); failed {
		return out, nil
	}
	switch create.ExecuteGate(t.ship) {
	case create.GateStale:
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonStale}, nil
	case create.GateLookup:
		return c.lookupOutcome(ctx, t)
	case create.GateSend:
	}
	if t.row.OrderStatus == order.StatusCancelRequested { // đơn đã yêu cầu hủy trước khi gửi: không tạo kiện
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonOrderCancelRequested}, nil
	}
	if t.refused != nil {
		return c.refuse(ctx, j, t, *t.refused, "")
	}
	req, err := createRequest(t)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // chưa gửi gì
	}
	res, err := t.ad.CreateShipment(ctx, t.acc, req)
	if err == nil {
		return doneOutcome(res.TrackingCode, res.Fee, res.FeeKnown)
	}
	out, authCode, known := classifyCreateError(err)
	if !known {
		return commands.Outcome{}, fmt.Errorf("tạo vận đơn ở hãng: %w", err)
	}
	return c.refuse(ctx, j, t, out, authCode)
}

// targetFailed: đọc vận đơn của lệnh (trước khi gọi hãng) lỗi → không có vận đơn: rejected (dừng hẳn); lỗi đọc:
// not_sent (chưa gửi gì). err nil → failed = false.
func targetFailed(err error) (commands.Outcome, bool) {
	switch {
	case err == nil:
		return commands.Outcome{}, false
	case errors.Is(err, ErrNotFound):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonStale}, true
	}
	return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, true
}

func doneOutcome(tracking string, fee int64, feeKnown bool) (commands.Outcome, error) {
	r := createResult{TrackingCode: tracking}
	if feeKnown {
		r.Fee = &fee
	}
	b, err := json.Marshal(r)
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả tạo vận đơn: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// refuse: kết quả chắc chắn không tạo. rejected → ghi dấu create_state rejected (chỉ khi vận đơn còn chờ đúng lệnh
// này) và MarkAuthFailed khi hãng từ chối khóa, trong một transaction ngắn với hạn riêng; ghi lỗi → not_sent (vẫn chắc
// chưa tạo: OnTerminal để vận đơn chờ người gửi lại thay vì hủy).
func (c *CreateCommand) refuse(ctx context.Context, j queue.Job, t createTarget, out commands.Outcome, authCode string,
) (commands.Outcome, error) {
	if out.Status != commands.StatusRejected {
		return out, nil
	}
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := tenancy.WithTenant(mctx, c.svc.d.DB, *j.TenantID, func(tx pgx.Tx) error {
		if authCode != "" { // ADR-46 nhóm 8: tài khoản hãng trước vận đơn
			if err := MarkAuthFailed(mctx, tx, t.row.CarrierAccountID, authCode); err != nil {
				return err
			}
		}
		if _, err := store.New(tx).MarkCreateRejected(mctx, store.MarkCreateRejectedParams{
			ID: t.row.ID, JobID: pgtype.UUID{Bytes: j.ID, Valid: true},
		}); err != nil {
			return fmt.Errorf("ghi dấu hãng từ chối: %w", err)
		}
		return nil
	}, db.TxWorker)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // hãng chắc chưa tạo
	}
	return out, nil
}

// classifyCreateError phân lớp lỗi CreateShipment: known = false là không rõ hãng đã tạo chưa (quá giờ, mất kết nối
// sau khi gửi, lỗi lạ) → in_doubt. authCode khác rỗng khi hãng từ chối khóa API (MarkAuthFailed).
func classifyCreateError(err error) (out commands.Outcome, authCode string, known bool) {
	if errors.Is(err, ErrNotSupported) {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNotSupported}, "", true
	}
	if ce, ok := errors.AsType[*CarrierError](err); ok {
		code := safeCode(ce.Code)
		switch ce.Class {
		case ClassNotSentRetry, ClassRateLimited:
			return commands.Outcome{Status: commands.StatusNotSent, Reason: code}, "", true
		case ClassAuth:
			return commands.Outcome{Status: commands.StatusRejected, Reason: code}, code, true
		case ClassRejected, ClassPermanent:
			return commands.Outcome{Status: commands.StatusRejected, Reason: code}, "", true
		case ClassUnknownOutcome:
			return commands.Outcome{}, "", false
		}
	}
	switch {
	case errors.Is(err, ErrNotSent):
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNotSent}, "", true
	case errors.Is(err, ErrRejected):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonRejected}, "", true
	}
	return commands.Outcome{}, "", false
}

// createRequest dựng yêu cầu gửi hãng từ vận đơn (bản chụp địa chỉ giao, kiện, dịch vụ, tùy chọn lúc Ship).
func createRequest(t createTarget) (CreateRequest, error) {
	var to shipToDoc
	if len(t.row.ShipTo) > 0 {
		if err := json.Unmarshal(t.row.ShipTo, &to); err != nil {
			return CreateRequest{}, fmt.Errorf("đọc địa chỉ giao của vận đơn: %w", err)
		}
	}
	var o optionsDoc
	if err := json.Unmarshal(t.row.Options, &o); err != nil {
		return CreateRequest{}, fmt.Errorf("đọc tùy chọn giao của vận đơn: %w", err)
	}
	addr := to.address()
	return CreateRequest{
		Code: t.row.Code, CODAmount: t.row.CodAmount, ReceiverName: addr.Name, ReceiverPhone: addr.Phone,
		Address: addr.Line, From: t.acc.Pickup, To: addr, Service: t.row.Service, Options: o.options(),
		Parcel: Parcel{
			WeightGrams: int(t.row.WeightGrams.Int32), LengthCm: int(t.row.LengthCm.Int32),
			WidthCm: int(t.row.WidthCm.Int32), HeightCm: int(t.row.HeightCm.Int32), DeclaredValue: t.row.DeclaredValue,
		},
	}, nil
}

// lookupOutcome tra hãng theo mã vận đơn cho Execute của vận đơn "không rõ": có → done (kèm mã hãng khi hãng trả);
// chắc chắn không có → not_sent (dead, chờ người gửi lại); không chắc, lỗi, không tra được → không rõ (in_doubt).
func (c *CreateCommand) lookupOutcome(ctx context.Context, t createTarget) (commands.Outcome, error) {
	st, tracking, err := c.lookup(ctx, t)
	switch {
	case err != nil:
		return commands.Outcome{}, err
	case st == commands.StatusDone && tracking != "":
		return doneOutcome(tracking, 0, false)
	case st == commands.StatusDone:
		return commands.Outcome{Status: commands.StatusDone}, nil
	case st == commands.StatusNotSent:
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonConfirmed}, nil
	}
	return commands.Outcome{Status: commands.StatusUnknown, Reason: reasonInconclusive}, nil
}

// lookup tra hãng theo mã vận đơn của CRM (LookupByCode): có → done kèm mã hãng; !Found và hãng trả lời chắc chắn →
// not_sent; Inconclusive, không có adapter, tài khoản không dùng được → unknown; lỗi → unknown kèm lỗi.
func (c *CreateCommand) lookup(ctx context.Context, t createTarget) (status, tracking string, err error) {
	if t.refused != nil || t.ad == nil {
		return commands.StatusUnknown, "", nil
	}
	r, err := t.ad.LookupByCode(ctx, t.acc, t.row.Code)
	switch {
	case err != nil:
		return commands.StatusUnknown, "", fmt.Errorf("tra vận đơn ở hãng: %w", err)
	case r.Inconclusive:
		return commands.StatusUnknown, "", nil
	case r.Found:
		return commands.StatusDone, r.TrackingCode, nil
	}
	return commands.StatusNotSent, "", nil
}

// Lookup (commands.Looker, ResolveInDoubt) tra hãng theo mã vận đơn của CRM, kể cả khi vận đơn đã qua creating (lần
// tra trạng thái đã thấy kiện: lệnh thành done, không đổi vận đơn). Lệnh của lần gửi cũ → unknown (giữ in_doubt cho
// người xem).
func (c *CreateCommand) Lookup(ctx context.Context, j queue.Job) (string, error) {
	p, err := parsePayload(j)
	if err != nil {
		return commands.StatusUnknown, err
	}
	t, err := c.target(ctx, j, p.ShipmentID)
	if err != nil {
		return commands.StatusUnknown, err
	}
	if !t.ship.Current {
		return commands.StatusUnknown, nil
	}
	st, _, err := c.lookup(ctx, t)
	return st, err
}

// OnDone (trong transaction đổi lệnh sang done): có mã hãng → AttachTrackingCode (Task M3-11: awaiting_pickup, created,
// phí; đã có đúng mã → không làm gì; mã khác → anomaly tracking_conflict) rồi đóng việc in_doubt_create. Không có mã
// (done do tra lại): create_state created (không còn "không tạo", "Gửi lại"), đóng việc in_doubt_create và ghi lệnh
// tra trạng thái để điền mã hãng (khóa created:<id>).
// Ý định hủy trong lúc gửi (Task M3-14): AttachTrackingCode ghi lệnh hủy ngay khi vận đơn có ý định hủy hay đơn đã
// cancel_requested.
func (c *CreateCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	p, err := parsePayload(j)
	if err != nil {
		return err
	}
	q := store.New(tx)
	if len(result) == 0 || string(result) == "null" {
		// Khóa đơn rồi vận đơn (ADR-46 nhóm 7 → 8) trước khi đóng việc của vận đơn.
		_, err := lockForCreate(ctx, tx, q, p.ShipmentID, nil)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := q.MarkCreateFound(ctx, p.ShipmentID); err != nil {
			return fmt.Errorf("ghi hãng đã có kiện: %w", err)
		}
		if err := q.ResolveCreateAnomaly(ctx, store.ResolveCreateAnomalyParams{
			ShipmentID: p.ShipmentID, Resolution: pgtype.Text{String: "carrier_found", Valid: true},
		}); err != nil {
			return fmt.Errorf("đóng việc không rõ đã tạo: %w", err)
		}
		return enqueueRefresh(ctx, tx, c.svc.d.Queue, *j.TenantID, p.ShipmentID, "created:"+p.ShipmentID.String())
	}
	var r createResult
	if err := json.Unmarshal(result, &r); err != nil || r.TrackingCode == "" {
		return fmt.Errorf("lệnh %s: kết quả tạo vận đơn không đọc được: %w", j.ID, queue.ErrPermanent)
	}
	res, err := c.svc.AttachTrackingCode(ctx, tx, p.ShipmentID, r.TrackingCode, r.Fee)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil || res.Conflict {
		return err
	}
	if err := q.ResolveCreateAnomaly(ctx, store.ResolveCreateAnomalyParams{
		ShipmentID: p.ShipmentID, Resolution: pgtype.Text{String: "created", Valid: true},
	}); err != nil {
		return fmt.Errorf("đóng việc không rõ đã tạo: %w", err)
	}
	return nil
}

// OnTerminal (queue.TerminalHook, trong transaction đổi lệnh sang trạng thái kết thúc, trước câu đổi platform.jobs):
// khóa đơn (nhóm 7) rồi vận đơn (nhóm 8), quyết theo create.Terminal:
//   - in_doubt → create_state unknown, needs_review, anomaly in_doubt_create (VC16): không tự gửi lại; tra lại
//     (ResolveInDoubt) hay nhân viên xử lý (ResolveCreate).
//   - dead, expired mà chắc chưa tạo (chưa gửi được, tra lại xác nhận chưa có) → create_state not_sent; vận đơn vẫn
//     creating và gắn đơn, chờ "Gửi lại" (Q-5).
//   - dead do hãng từ chối (dấu rejected của Execute), hay chắc chưa tạo mà vận đơn có ý định hủy → cancelled
//     (cancel_kind create_rejected hay ý định hủy), gỡ khỏi đơn (order.Writer.DetachShipment; ý định order_cancel:
//     OnShipmentCancelConfirmed, đơn cancel_requested thành cancelled, nhả held), phát shipment.status_changed.
//
// Lệnh của lần gửi cũ, vận đơn đã qua creating, không có, payload hỏng → không đổi gì.
func (c *CreateCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status != queue.StatusDead && status != queue.StatusExpired && status != queue.StatusInDoubt {
		return nil
	}
	p, err := parsePayload(j)
	if err != nil {
		return nil //nolint:nilerr // payload hỏng: không có vận đơn để đổi, lệnh vẫn kết thúc
	}
	q := store.New(tx)
	row, err := lockForCreate(ctx, tx, q, p.ShipmentID, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s := row.createShipment(j.ID)
	intent := row.intent(s)
	switch create.Terminal(s, status, intent) {
	case create.ActNone:
		return nil
	case create.ActUnknown:
		return c.markUnknown(ctx, tx, q, j, row)
	case create.ActNotSent:
		if _, err := q.MarkCreateNotSent(ctx, store.MarkCreateNotSentParams{Bump: 0, ID: row.ID}); err != nil {
			return fmt.Errorf("ghi chưa gửi: %w", err)
		}
		return c.publish(ctx, tx, *j.TenantID, row)
	case create.ActCancel:
		return c.cancelEnded(ctx, tx, q, *j.TenantID, row, s, intent)
	}
	return nil
}

func (c *CreateCommand) markUnknown(ctx context.Context, tx pgx.Tx, q *store.Queries, j queue.Job, row lockedCreate) error {
	if err := q.MarkCreateUnknown(ctx, row.ID); err != nil {
		return fmt.Errorf("ghi không rõ đã tạo: %w", err)
	}
	if !row.CreateJobID.Valid { // vận đơn của M1: lệnh in_doubt này là lệnh của lần gửi hiện tại
		if err := q.SetShipmentCreateJob(ctx, store.SetShipmentCreateJobParams{
			JobID: pgtype.UUID{Bytes: j.ID, Valid: true}, ID: row.ID,
		}); err != nil {
			return fmt.Errorf("ghi mã lệnh tạo của vận đơn: %w", err)
		}
	}
	if _, err := openAnomaly(ctx, q, row.ID, anomalyInDoubtCreate, map[string]any{
		"jobId": j.ID.String(), "attempt": row.CreateAttempts,
	}); err != nil {
		return err
	}
	return c.publish(ctx, tx, *j.TenantID, row)
}

// cancelEnded hủy vận đơn creating row (đã khóa cùng đơn) khi hãng chắc chưa tạo: cancel_kind theo ý định intent
// (create.EffectiveIntent), gỡ khỏi đơn (order_cancel: OnShipmentCancelConfirmed; khác: DetachShipment — cả hai đưa
// đơn cancel_requested thành cancelled, nhả held), phát shipment.status_changed, báo hiệu.
func (c *CreateCommand) cancelEnded(ctx context.Context, tx pgx.Tx, q *store.Queries, tenant uuid.UUID,
	row lockedCreate, s create.Shipment, intent string,
) error {
	version, err := q.CancelEndedCreate(ctx, store.CancelEndedCreateParams{
		CancelKind: pgtype.Text{String: create.CancelKind(intent), Valid: true}, CreateState: create.CancelState(s),
		ID: row.ID,
	})
	if err != nil {
		return fmt.Errorf("hủy vận đơn creating: %w", err)
	}
	ow := order.Writer{Events: c.svc.d.Events, Queue: c.svc.d.Queue, Realtime: c.svc.d.Bus}
	system := order.Actor{TenantID: tenant, Type: "system"}
	now := c.svc.d.Now().UTC()
	if intent == "order_cancel" {
		_, err = ow.OnShipmentCancelConfirmed(ctx, tx, row.SellerID, row.OrderID, row.ID, system, now)
	} else {
		_, err = ow.DetachShipment(ctx, tx, row.SellerID, row.OrderID, row.ID, system, now)
	}
	if err != nil {
		return fmt.Errorf("gỡ vận đơn khỏi đơn: %w", err)
	}
	if err := emitStatus(ctx, tx, c.svc.d.Events, c.svc.d.Queue, tenant, row.ID, row.OrderID, StatusCancelled,
		StatusCreating, row.CodAmount, version); err != nil {
		return err
	}
	return c.publish(ctx, tx, tenant, row)
}

// publish báo hiệu shipment.changed ở kho của tài khoản hãng của vận đơn.
func (c *CreateCommand) publish(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, row lockedCreate) error {
	if c.svc.d.Bus == nil {
		return nil
	}
	wh, err := AccountWarehouse(ctx, tx, row.CarrierAccountID)
	if err != nil {
		return err
	}
	return c.svc.publishShipment(ctx, tx, tenant, row.ID, wh)
}

// enqueueRefresh ghi lệnh tra trạng thái của vận đơn id trong tx.
func enqueueRefresh(ctx context.Context, tx pgx.Tx, jq queue.JobQueue, tenant, id uuid.UUID, dedupe string) error {
	if _, _, err := jq.Enqueue(ctx, tx, RefreshCommandKind(), queue.NewJob{
		TenantID: &tenant, Payload: commandPayload{ShipmentID: id}, DedupeKey: dedupe, OrderKey: shipmentOrderKey(id),
	}); err != nil {
		return fmt.Errorf("ghi lệnh tra trạng thái vận đơn: %w", err)
	}
	return nil
}

// rejectBadPayload: payload của lệnh tạo không đọc được → dừng hẳn (rejected), không gọi hãng.
func rejectBadPayload() (commands.Outcome, error) {
	return commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, nil
}
