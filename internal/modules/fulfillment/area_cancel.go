package fulfillment

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/cancel"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/create"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng cancel (kế hoạch M3, Task M3-14): hủy vận đơn, đổi hãng, hủy đơn đã gửi hãng. Hủy đơn đi đúng một đường của M2
// (cancelOrder → order.cancel_requested, không nhả hàng); consumer fulfillment_order_cancel nghe sự kiện đó và gọi
// RequestCancel. Đổi hãng: cancelShipment → RequestCancel(replace). Lệnh hủy ở hãng: cancel_command.go; quyết định
// thuần: internal/cancel. Đơn, tồn chỉ đổi khi hãng xác nhận (qua ApplyTracking, đường EffCancelConfirmed,
// EffCancelRejected của Task M3-08).

// cancelHandlers: lệnh hủy ở hãng khi Carriers có adapter (cùng điều kiện với lệnh tạo).
func (m *Module) cancelHandlers() []commands.Handler {
	if m.d.Carriers == nil || len(m.d.Carriers.Keys()) == 0 {
		return nil
	}
	return []commands.Handler{NewCancelCommand(m.Service)}
}

func (*Module) cancelInbound() []webhooks.InboundHandler { return nil }
func (*Module) cancelSources() []webhooks.Source         { return nil }

// cancelConsumers: consumer order.cancel_requested (vận đơn creating chắc chưa tạo vẫn hủy được khi không có adapter).
func (m *Module) cancelConsumers() []events.Consumer {
	return []events.Consumer{OrderCancelConsumer{s: m.Service}}
}

func (*Module) cancelSchedules() []schedules.Schedule   { return nil }
func (*Module) cancelCleanups() []schedules.CleanupFunc { return nil }

const (
	// OrderCancelConsumerName là tên consumer (việc event.fulfillment_order_cancel); đổi tên làm mất tiến độ.
	OrderCancelConsumerName = "fulfillment_order_cancel"
	// topicOrderCancelRequested là sự kiện M2 phát khi đơn confirmed có vận đơn sang cancel_requested (applyTransition).
	topicOrderCancelRequested = "order." + order.StatusCancelRequested

	reasonOrderCancelAfterPickup = "order_cancel_after_pickup"
)

// CancelInput là một yêu cầu hủy vận đơn: Intent order_cancel (khách hủy đơn, consumer) hay replace (đổi hãng, nhân
// viên); Reason chỉ để bên gọi ghi audit; CarrierChecked: nhân viên đã tra trang hãng, hãng không có kiện (chỉ có
// nghĩa với vận đơn creating kẹt, Q-T91-12). Authorize nil: không kiểm phạm vi (consumer, hook của lệnh).
type CancelInput struct {
	Intent, Reason string
	CarrierChecked bool
	Authorize      Authorize
}

// lockedCancel là vận đơn đã khóa cho hủy kèm trạng thái đơn đọc dưới khóa đơn.
type lockedCancel struct {
	store.LockShipmentForCancelRow
	orderStatus string
}

// lockForCancel khóa đơn (ADR-46 nhóm 7) rồi vận đơn id (nhóm 8); authorize khác nil thì kiểm phạm vi theo kho của
// tài khoản hãng. Không có (hay tenant khác) → ErrNotFound.
func lockForCancel(ctx context.Context, tx pgx.Tx, q *store.Queries, id uuid.UUID, authorize Authorize,
) (lockedCancel, error) {
	ref, err := q.ShipmentOrderRef(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCancel{}, ErrNotFound
	}
	if err != nil {
		return lockedCancel{}, fmt.Errorf("đọc đơn của vận đơn: %w", err)
	}
	o, err := order.LockSnapshot(ctx, tx, ref.SellerID, ref.OrderID)
	if errors.Is(err, order.ErrNotFound) {
		return lockedCancel{}, ErrNotFound
	}
	if err != nil {
		return lockedCancel{}, fmt.Errorf("khóa đơn của vận đơn: %w", err)
	}
	row, err := q.LockShipmentForCancel(ctx, id)
	if err != nil {
		return lockedCancel{}, fmt.Errorf("khóa vận đơn: %w", err)
	}
	if authorize != nil {
		if err := authorize(row.WarehouseID); err != nil {
			return lockedCancel{}, err
		}
	}
	return lockedCancel{LockShipmentForCancelRow: row, orderStatus: o.Status}, nil
}

// creating là vận đơn l dưới dạng của lệnh tạo (cancelEnded, create.CancelState).
func (l lockedCancel) creating() (lockedCreate, create.Shipment) {
	lc := lockedCreate{LockShipmentForCreateRow: store.LockShipmentForCreateRow{
		ID: l.ID, OrderID: l.OrderID, SellerID: l.SellerID, CarrierAccountID: l.CarrierAccountID, Status: l.Status,
		CreateState: l.CreateState, CreateJobID: l.CreateJobID, CreateAttempts: l.CreateAttempts,
		CancelIntent: l.CancelIntent, CodAmount: l.CodAmount, Version: l.Version,
	}, orderStatus: l.orderStatus}
	return lc, create.Shipment{Creating: l.Status == string(StatusCreating), State: l.CreateState, Current: true}
}

// cancelAPI: hãng của vận đơn hủy qua API được (tài khoản api, adapter khai Cancel).
func (s *Service) cancelAPI(l lockedCancel) bool {
	ad, ok := s.adapter(l.CarrierKey)
	return ok && l.Mode == ModeAPI && ad.Capabilities().Cancel
}

// RequestCancel hủy vận đơn id theo in trong tx của bên gọi (một transaction): khóa đơn rồi vận đơn, quyết theo
// cancel.Decide, ý định ghi lên vận đơn là cancel.Effective (đơn đã cancel_requested → order_cancel):
//   - creating mà hãng chắc không có kiện (not_sent, rejected) → cancelled ngay (cancel_kind = ý định), gỡ khỏi đơn
//     (order_cancel: OnShipmentCancelConfirmed — đơn cancelled, nhả hàng; replace: DetachShipment — đơn confirmed).
//   - creating còn chờ (pending, unknown, created): order_cancel → ghi cancel_intent (lệnh tạo kết thúc tự hủy, hay gắn
//     mã xong tự ghi lệnh hủy); replace → ErrCreateInProgress, trừ khi CarrierChecked với pending/unknown → bỏ lệnh
//     tạo, đóng việc in_doubt_create, cancelled staff_after_unknown, gỡ khỏi đơn.
//   - awaiting_pickup → cancel_requested (cancel_intent), status_changed; hãng hủy qua API → lệnh hủy (DedupeKey
//     cancel:<id>); không → việc manual_cancel cho nhân viên.
//   - cancel_requested: order_cancel nâng ý định replace → order_cancel (không lệnh thứ hai), cùng ý định → không làm gì;
//     replace → ErrShipmentNotCancellable.
//   - cancelled: order_cancel → không làm gì; replace → ErrShipmentNotCancellable.
//   - shipping trở đi: replace → ErrShipmentNotCancellable; order_cancel → việc cancel_conflict, không gọi hãng.
//
// Không có vận đơn → ErrNotFound; ý định lạ → ErrShipInvalid; lỗi của Authorize trả nguyên. Audit do bên gọi ghi.
func (s *Service) RequestCancel(ctx context.Context, tx pgx.Tx, id uuid.UUID, in CancelInput, a order.Actor,
) (ShipmentView, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return ShipmentView{}, errNotWired
	}
	q := store.New(tx)
	l, err := lockForCancel(ctx, tx, q, id, in.Authorize)
	if err != nil {
		return ShipmentView{}, err
	}
	act := cancel.Decide(cancel.Shipment{
		Status: l.Status, CreateState: l.CreateState, Intent: l.CancelIntent.String, CarrierAPI: s.cancelAPI(l),
	}, cancel.Request{Intent: in.Intent, CarrierChecked: in.CarrierChecked})
	intent := cancel.Effective(in.Intent, l.orderStatus == order.StatusCancelRequested)
	switch act {
	case cancel.ActInvalid:
		return ShipmentView{}, fmt.Errorf("%w: ý định hủy %q", ErrShipInvalid, in.Intent)
	case cancel.ActCreateInProgress:
		return ShipmentView{}, fmt.Errorf("đổi hãng vận đơn %s (%s): %w", l.Status, l.CreateState, ErrCreateInProgress)
	case cancel.ActNotCancellable:
		return ShipmentView{}, fmt.Errorf("hủy vận đơn %s: %w", l.Status, ErrShipmentNotCancellable)
	case cancel.ActNone:
		return s.ShipmentView(ctx, tx, id)
	case cancel.ActCancelNow:
		lc, sh := l.creating()
		err = s.createCommand().cancelEnded(ctx, tx, q, a.TenantID, lc, sh, intent)
	case cancel.ActStaffCancel:
		err = s.staffCancel(ctx, tx, q, l, a)
	case cancel.ActRecordIntent, cancel.ActUpgradeIntent:
		err = s.setIntent(ctx, tx, q, l, a.TenantID, intent)
	case cancel.ActRequestCarrier, cancel.ActRequestManual:
		err = s.requestCarrierCancel(ctx, tx, q, l, a.TenantID, intent, act == cancel.ActRequestCarrier)
	case cancel.ActConflict:
		err = s.cancelConflict(ctx, tx, q, l, a.TenantID)
	}
	if err != nil {
		return ShipmentView{}, err
	}
	return s.ShipmentView(ctx, tx, id)
}

// staffCancel: vận đơn creating kẹt, nhân viên đã tra hãng (không có kiện) → đóng việc in_doubt_create, cancelled
// staff_after_unknown, gỡ khỏi đơn (DetachShipment: đơn confirmed giữ hàng; đơn cancel_requested → cancelled, nhả
// hàng), rồi bỏ lệnh tạo (platform.jobs cuối cùng). Lệnh tạo đang chạy → ErrCreateInProgress (transaction lùi).
func (s *Service) staffCancel(ctx context.Context, tx pgx.Tx, q *store.Queries, l lockedCancel, a order.Actor) error {
	if err := q.ResolveCreateAnomaly(ctx, store.ResolveCreateAnomalyParams{
		ShipmentID: l.ID, Resolution: pgtype.Text{String: cancel.KindStaffAfterUnknown, Valid: true},
		ResolvedBy: pgtype.UUID{Bytes: a.UserID, Valid: a.UserID != uuid.Nil},
	}); err != nil {
		return fmt.Errorf("đóng việc không rõ đã tạo: %w", err)
	}
	lc, sh := l.creating()
	if err := s.createCommand().cancelEnded(ctx, tx, q, a.TenantID, lc, sh, cancel.KindStaffAfterUnknown); err != nil {
		return err
	}
	if !l.CreateJobID.Valid {
		return nil
	}
	if err := commands.Abandon(ctx, tx, uuid.UUID(l.CreateJobID.Bytes), cancel.KindStaffAfterUnknown); err != nil {
		return commandBusy("bỏ lệnh tạo", err)
	}
	return nil
}

// setIntent ghi ý định hủy intent lên vận đơn (creating còn chờ, hay cancel_requested đổi hãng → hủy đơn), báo hiệu.
func (s *Service) setIntent(ctx context.Context, tx pgx.Tx, q *store.Queries, l lockedCancel, tenant uuid.UUID,
	intent string,
) error {
	if _, err := q.SetShipmentCancelIntent(ctx, store.SetShipmentCancelIntentParams{
		CancelIntent: pgtype.Text{String: intent, Valid: true}, ID: l.ID,
	}); err != nil {
		return fmt.Errorf("ghi ý định hủy: %w", err)
	}
	return s.publishShipment(ctx, tx, tenant, l.ID, l.WarehouseID)
}

// requestCarrierCancel: vận đơn chờ thu gom → cancel_requested (ý định intent), status_changed; byAPI → lệnh hủy
// (khóa cancel:<id>), không → việc manual_cancel.
func (s *Service) requestCarrierCancel(ctx context.Context, tx pgx.Tx, q *store.Queries, l lockedCancel,
	tenant uuid.UUID, intent string, byAPI bool,
) error {
	version, err := q.RequestShipmentCancel(ctx, store.RequestShipmentCancelParams{
		CancelIntent: pgtype.Text{String: intent, Valid: true}, ID: l.ID,
	})
	if err != nil {
		return fmt.Errorf("đổi vận đơn sang đang hủy: %w", err)
	}
	if err := emitStatus(ctx, tx, s.d.Events, s.d.Queue, tenant, l.ID, l.OrderID, StatusCancelRequested,
		StatusAwaitingPickup, l.CodAmount, version); err != nil {
		return err
	}
	if byAPI {
		if _, _, err := s.d.Queue.Enqueue(ctx, tx, CancelCommandKind(), queue.NewJob{
			TenantID: &tenant, Payload: commandPayload{ShipmentID: l.ID}, DedupeKey: cancel.DedupeKey(l.ID),
		}); err != nil {
			return fmt.Errorf("ghi lệnh hủy vận đơn: %w", err)
		}
	} else if _, err := openAnomaly(ctx, q, l.ID, cancel.AnomalyManual, map[string]any{"intent": intent}); err != nil {
		return err
	}
	return s.publishShipment(ctx, tx, tenant, l.ID, l.WarehouseID)
}

// cancelConflict: khách hủy đơn mà hàng đã đi → việc cancel_conflict (người xem quyết), không gọi hãng.
func (s *Service) cancelConflict(ctx context.Context, tx pgx.Tx, q *store.Queries, l lockedCancel, tenant uuid.UUID) error {
	if _, err := openAnomaly(ctx, q, l.ID, anomalyCancelConflict, map[string]any{
		"status": l.Status, "reason": reasonOrderCancelAfterPickup,
	}); err != nil {
		return err
	}
	return s.publishShipment(ctx, tx, tenant, l.ID, l.WarehouseID)
}

// cancelAfterAttach: vận đơn vừa gắn mã hãng (creating → awaiting_pickup) mà có ý định hủy, hay đơn đã cancel_requested
// (khách hủy trong lúc lệnh tạo gọi hãng) → ghi lệnh hủy ngay (RequestCancel bằng người làm system).
func (s *Service) cancelAfterAttach(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID, intent pgtype.Text,
	orderStatus string,
) error {
	if !intent.Valid && orderStatus != order.StatusCancelRequested {
		return nil
	}
	in := CancelInput{Intent: cmp.Or(intent.String, cancel.IntentOrderCancel)}
	if _, err := s.RequestCancel(ctx, tx, id, in, order.Actor{TenantID: tenant, Type: actorSystem}); err != nil {
		return fmt.Errorf("hủy vận đơn vừa tạo: %w", err)
	}
	return nil
}

// OrderCancelConsumer nghe order.cancel_requested của M2 (events.Consumer; cancelConsumers đăng ký): khóa đơn; đơn còn
// cancel_requested và có vận đơn hiện hành → RequestCancel(order_cancel) bằng người làm system. Đơn đã đổi (hãng đã trả
// lời, đơn đã hủy) hay sự kiện giao lại → không làm gì thêm (RequestCancel không ghi lệnh thứ hai). Payload hỏng, không
// có đơn → queue.ErrPermanent.
type OrderCancelConsumer struct {
	s *Service
}

// Name trả OrderCancelConsumerName.
func (OrderCancelConsumer) Name() string { return OrderCancelConsumerName }

// Topics trả order.cancel_requested.
func (OrderCancelConsumer) Topics() []string { return []string{topicOrderCancelRequested} }

// Handle xử lý sự kiện e trong tx của worker.
func (c OrderCancelConsumer) Handle(ctx context.Context, e events.StoredEvent, tx pgx.Tx) error {
	var p order.Changed
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.OrderID == uuid.Nil {
		return fmt.Errorf("sự kiện %s: payload không hợp lệ: %w", e.ID, queue.ErrPermanent)
	}
	q := store.New(tx)
	ref, err := q.OrderShipRef(ctx, p.OrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("sự kiện %s: không có đơn: %w", e.ID, queue.ErrPermanent)
	}
	if err != nil {
		return fmt.Errorf("đọc người bán của đơn: %w", err)
	}
	o, err := order.LockSnapshot(ctx, tx, ref.SellerID, p.OrderID)
	if err != nil {
		return fmt.Errorf("khóa đơn: %w", err)
	}
	if o.Status != order.StatusCancelRequested || o.CurrentShipmentID == nil {
		return nil
	}
	_, err = c.s.RequestCancel(ctx, tx, *o.CurrentShipmentID, CancelInput{Intent: cancel.IntentOrderCancel},
		order.Actor{TenantID: e.TenantID, Type: actorSystem})
	if err != nil {
		return fmt.Errorf("sự kiện %s: hủy vận đơn: %w", e.ID, err)
	}
	return nil
}
