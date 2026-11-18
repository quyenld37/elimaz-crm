package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lệnh của fulfillment.
const (
	createCommandName  = "fulfillment.create_shipment"
	refreshCommandName = "fulfillment.refresh_status"
	createExpires      = 10 * time.Minute
	commandTimeout     = 30 * time.Second

	// TopicShipmentStatusChanged phát mỗi lần vận đơn tiến trạng thái, hay sang cancelled khi lệnh tạo kết thúc hỏng
	// (Seq = shipments.version; docs/views.md).
	TopicShipmentStatusChanged = "shipment.status_changed"
	aggregateShipment          = "shipment"

	reasonNoAdapter     = "no_adapter"
	reasonRejected      = "carrier_rejected"
	reasonNotSent       = "carrier_not_sent"
	reasonUnknownStatus = "unknown_status"
	reasonNotFound      = "carrier_not_found"
	reasonReadFailed    = "carrier_read_failed"
	reasonStale         = "shipment_not_creating"
	reasonBadPayload    = "bad_payload"
)

// StatusChanged là payload của shipment.status_changed: chỉ mã và số (A16).
type StatusChanged struct {
	ShipmentID uuid.UUID `json:"shipmentId"`
	Status     Status    `json:"status"`
	CODAmount  int64     `json:"codAmount"`
}

// CreateCommandKind là loại lệnh fulfillment.create_shipment: hết hạn sau 10 phút chờ, mỗi lần gửi tối đa 30 s.
func CreateCommandKind() queue.Kind {
	return commands.CommandKind(createCommandName, createExpires, commandTimeout)
}

// RefreshCommandKind là loại lệnh fulfillment.refresh_status (không hết hạn, 30 s). Thứ tự best_effort: một lần tra
// dead (trạng thái lạ) không chặn các lần tra sau của cùng vận đơn; chỉ lần đang chạy chặn.
func RefreshCommandKind() queue.Kind {
	k := commands.CommandKind(refreshCommandName, 0, commandTimeout)
	k.Policy.OrderMode = queue.OrderBestEffort
	return k
}

// CommandOptions là phụ thuộc của hai lệnh (cmd/crm nối): database (đọc ngoài transaction), adapter theo Key(), và
// đường ghi của order (Events, Queue, Realtime) cho bước áp lên đơn và tồn.
type CommandOptions struct {
	DB       *db.DB
	Adapters []CarrierAdapter
	Events   *events.Registry
	Queue    queue.JobQueue
	Realtime realtime.Bus
}

// commandPayload là payload của cả hai lệnh: chỉ mã vận đơn.
type commandPayload struct {
	ShipmentID uuid.UUID `json:"shipmentId"`
}

type commandBase struct {
	o        CommandOptions
	adapters map[string]CarrierAdapter
}

func newBase(o CommandOptions) commandBase {
	m := make(map[string]CarrierAdapter, len(o.Adapters))
	for _, a := range o.Adapters {
		m[a.Key()] = a
	}
	return commandBase{o: o, adapters: m}
}

// loadOutcome như load cho Execute: chưa gọi hãng nên lỗi đọc là not_sent (đọc lại sau), payload hỏng là rejected,
// thiếu adapter là not_sent; ok = false kèm Outcome để trả ngay.
func (b commandBase) loadOutcome( //nolint:ireturn // adapter theo carrier_key
	ctx context.Context, j queue.Job,
) (Shipment, CarrierAdapter, commands.Outcome, bool) {
	s, ad, err := b.load(ctx, j)
	switch {
	case errors.Is(err, queue.ErrPermanent):
		return s, nil, commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}, false
	case err != nil:
		return s, nil, commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, false
	case ad == nil:
		return s, nil, commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNoAdapter}, false
	}
	return s, ad, commands.Outcome{}, true
}

// load đọc vận đơn của lệnh j trong một transaction đọc ngắn (ngoài transaction của lệnh) và adapter của hãng.
func (b commandBase) load( //nolint:ireturn // adapter theo carrier_key
	ctx context.Context, j queue.Job,
) (Shipment, CarrierAdapter, error) {
	p, err := parsePayload(j)
	if err != nil {
		return Shipment{}, nil, err
	}
	var s Shipment
	if err := tenancy.WithTenant(ctx, b.o.DB, *j.TenantID, func(tx pgx.Tx) error {
		s, err = Get(ctx, tx, p.ShipmentID)
		return err
	}, db.TxWorker); err != nil {
		return Shipment{}, nil, fmt.Errorf("đọc vận đơn của lệnh: %w", err)
	}
	return s, b.adapters[s.CarrierKey], nil
}

func parsePayload(j queue.Job) (commandPayload, error) {
	var p commandPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.ShipmentID == uuid.Nil || j.TenantID == nil {
		return p, fmt.Errorf("lệnh %s: payload hoặc tenant không hợp lệ: %w", j.ID, queue.ErrPermanent)
	}
	return p, nil
}

func (s Shipment) account() Account {
	return Account{ID: s.CarrierAccountID, ExternalAccountRef: s.accountRef}
}

// emitStatus phát shipment.status_changed với Seq = version mới của vận đơn.
func (b commandBase) emitStatus(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID, st Status, cod, version int64) error {
	if _, _, err := b.o.Events.Emit(ctx, tx, b.o.Queue, events.Event{
		TenantID: tenant, AggregateType: aggregateShipment, AggregateID: id.String(), Seq: version,
		Topic: TopicShipmentStatusChanged, Payload: StatusChanged{ShipmentID: id, Status: st, CODAmount: cod},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicShipmentStatusChanged, err)
	}
	return nil
}

// CreateCommand là lệnh fulfillment.create_shipment (commands.Handler, Looker, DoneHook, queue.TerminalHook).
type CreateCommand struct{ commandBase }

// NewCreateCommand tạo lệnh tạo vận đơn.
func NewCreateCommand(o CommandOptions) *CreateCommand { return &CreateCommand{newBase(o)} }

// Kind trả CreateCommandKind().
func (*CreateCommand) Kind() queue.Kind { return CreateCommandKind() }

// AutoRetryNotSent trả true: chắc chắn chưa gửi thì tự gửi lại.
func (*CreateCommand) AutoRetryNotSent() bool { return true }

type createResult struct {
	TrackingCode string `json:"trackingCode"`
	Fee          int64  `json:"fee"`
}

// Execute gọi CreateShipment theo shipments.code (ngoài transaction). ErrRejected → rejected; ErrNotSent → not_sent;
// lỗi khác hay quá giờ → lỗi (Runner: in_doubt, không gửi lại). Vận đơn không còn creating → rejected (không gửi).
func (c *CreateCommand) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	s, ad, out, ok := c.loadOutcome(ctx, j)
	if !ok {
		return out, nil
	}
	if s.Status != StatusCreating {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonStale}, nil
	}
	req, err := c.request(ctx, j, s)
	if err != nil {
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}, nil //nolint:nilerr // chưa gửi gì
	}
	res, err := ad.CreateShipment(ctx, s.account(), req)
	switch {
	case errors.Is(err, ErrRejected):
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonRejected}, nil
	case errors.Is(err, ErrNotSent):
		return commands.Outcome{Status: commands.StatusNotSent, Reason: reasonNotSent}, nil
	case err != nil:
		return commands.Outcome{}, fmt.Errorf("tạo vận đơn ở hãng: %w", err)
	}
	b, err := json.Marshal(createResult(res))
	if err != nil {
		return commands.Outcome{}, fmt.Errorf("mã hóa kết quả tạo vận đơn: %w", err)
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// request dựng yêu cầu tạo vận đơn từ đơn (địa chỉ giao chụp lúc đặt).
func (c *CreateCommand) request(ctx context.Context, j queue.Job, s Shipment) (CreateRequest, error) {
	var o order.Order
	if err := tenancy.WithTenant(ctx, c.o.DB, *j.TenantID, func(tx pgx.Tx) error {
		var err error
		o, err = order.Get(ctx, tx, s.SellerID, s.OrderID)
		return err //nolint:wrapcheck // bọc ở dưới
	}, db.TxWorker); err != nil {
		return CreateRequest{}, fmt.Errorf("đọc đơn của vận đơn: %w", err)
	}
	return CreateRequest{
		Code: s.Code, CODAmount: s.CODAmount, ReceiverName: o.ShipAddress.Name, ReceiverPhone: o.ShipAddress.Phone,
		Address: string(o.ShipAddress.Line),
	}, nil
}

// Lookup tra hãng theo mã vận đơn của CRM: có → done, chưa có → not_sent, lỗi → không rõ.
func (c *CreateCommand) Lookup(ctx context.Context, j queue.Job) (string, error) {
	s, ad, err := c.load(ctx, j)
	if err != nil {
		return commands.StatusUnknown, err
	}
	if ad == nil {
		return commands.StatusUnknown, nil
	}
	r, err := ad.LookupByCode(ctx, s.account(), s.Code)
	if err != nil {
		return commands.StatusUnknown, fmt.Errorf("tra vận đơn ở hãng: %w", err)
	}
	if r.Found {
		return commands.StatusDone, nil
	}
	return commands.StatusNotSent, nil
}

// OnDone (trong transaction đổi lệnh sang done): khóa vận đơn (nhóm 8), điền mã hãng, phí, sang awaiting_pickup, phát
// shipment.status_changed. result nil (done do tra lại, không có mã hãng): ghi lệnh tra trạng thái để điền mã hãng
// (khóa chống trùng created:<id>). Vận đơn đã có mã hãng → không đổi gì.
func (c *CreateCommand) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	p, err := parsePayload(j)
	if err != nil {
		return err
	}
	if result == nil {
		return enqueueRefresh(ctx, tx, c.o.Queue, *j.TenantID, p.ShipmentID, "created:"+p.ShipmentID.String())
	}
	var r createResult
	if err := json.Unmarshal(result, &r); err != nil || r.TrackingCode == "" {
		return fmt.Errorf("lệnh %s: kết quả tạo vận đơn không đọc được: %w", j.ID, queue.ErrPermanent)
	}
	q := store.New(tx)
	cur, err := q.LockShipment(ctx, p.ShipmentID)
	if err != nil {
		return fmt.Errorf("khóa vận đơn: %w", err)
	}
	if cur.TrackingCode.Valid {
		return nil
	}
	next, advanced := Advance(Status(cur.Status), StatusAwaitingPickup)
	version, err := q.UpdateShipmentProgress(ctx, store.UpdateShipmentProgressParams{
		Status: string(next), TrackingCode: pgtype.Text{String: r.TrackingCode, Valid: true},
		Fee: pgtype.Int8{Int64: r.Fee, Valid: true}, ID: cur.ID,
	})
	if err != nil {
		return fmt.Errorf("ghi mã hãng của vận đơn: %w", err)
	}
	if !advanced {
		return nil
	}
	return c.emitStatus(ctx, tx, *j.TenantID, cur.ID, next, cur.CodAmount, version)
}

// OnTerminal (queue.TerminalHook, trong transaction đổi lệnh sang trạng thái kết thúc, trước câu đổi platform.jobs):
//   - in_doubt (hãng có thể đã tạo kiện): vận đơn còn creating được ghi dấu create_in_doubt_at, không đổi gì khác.
//   - dead hay expired: lần gửi này chắc chắn không tạo kiện (từ chối, hết lượt not_sent, tra lại xác nhận chưa gửi,
//     quá hạn khi chưa gửi). Vận đơn còn creating và chưa có dấu in_doubt → cancelled, gỡ khỏi vai vận đơn hiện hành
//     của đơn (order.DetachShipment) để tạo lại được, phát shipment.status_changed (D-03). Có dấu (lệnh từng in_doubt
//     rồi được người chạy lại: kiện của lần gửi trước có thể còn ở hãng) → không đổi gì, người tra hãng rồi xử lý.
//
// Giữ hàng của đơn không đổi. Khóa đơn (nhóm 7) rồi vận đơn (nhóm 8) như đường tra trạng thái. Vận đơn không còn
// creating, không có, hay payload hỏng → không đổi gì.
func (c *CreateCommand) OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error {
	if status != queue.StatusDead && status != queue.StatusExpired && status != queue.StatusInDoubt {
		return nil
	}
	p, err := parsePayload(j)
	if err != nil {
		return nil //nolint:nilerr // payload hỏng: không có vận đơn để đổi, lệnh vẫn kết thúc
	}
	q := store.New(tx)
	if status == queue.StatusInDoubt {
		if err := q.MarkCreateInDoubt(ctx, p.ShipmentID); err != nil {
			return fmt.Errorf("ghi dấu in_doubt của vận đơn: %w", err)
		}
		return nil
	}
	s, err := Get(ctx, tx, p.ShipmentID)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil
	case err != nil:
		return err
	case s.Status != StatusCreating:
		return nil
	}
	if err := order.Lock(ctx, tx, s.SellerID, s.OrderID); err != nil {
		return fmt.Errorf("khóa đơn của vận đơn: %w", err)
	}
	if _, err := q.LockShipment(ctx, s.ID); err != nil {
		return fmt.Errorf("khóa vận đơn: %w", err)
	}
	version, err := q.CancelCreatingShipment(ctx, s.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hủy vận đơn creating: %w", err)
	}
	if err := order.DetachShipment(ctx, tx, s.SellerID, s.OrderID, s.ID); err != nil {
		return fmt.Errorf("gỡ vận đơn khỏi đơn: %w", err)
	}
	return c.emitStatus(ctx, tx, *j.TenantID, s.ID, StatusCancelled, s.CODAmount, version)
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
