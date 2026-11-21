package fulfillment

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/partial"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Vùng partial (kế hoạch M3, Task M3-26): giao một phần bằng ngoại lệ có quyền (TD09, Q-15). Hãng báo giao một phần
// không tự áp (shipstate: anomaly partial_reported); người có fulfillment.correct ghi số đã giao từng dòng và tiền hãng
// đã thu. Phần đã giao trừ tồn (order.MarkDeliveredPartial → inventory.ConsumePartial); phần còn lại vẫn in_transit,
// đi phiếu partial_delivery của M2 (order.CreatePartialReturn), kho nhận qua receiveReturn. Tiền COD do cash ghi khi
// nghe shipment.status_changed (collected = tiền đã thu, returned = phần còn lại). Quyết định thuần: internal/partial.

func (*Module) partialHandlers() []commands.Handler       { return nil }
func (*Module) partialInbound() []webhooks.InboundHandler { return nil }
func (*Module) partialSources() []webhooks.Source         { return nil }
func (*Module) partialConsumers() []events.Consumer       { return nil }
func (*Module) partialSchedules() []schedules.Schedule    { return nil }
func (*Module) partialCleanups() []schedules.CleanupFunc  { return nil }

// resolutionPartialRecorded là kết quả đóng anomaly partial_reported khi nhân viên đã ghi giao một phần.
const resolutionPartialRecorded = "partial_delivery_recorded"

// PartialLine là số khách đã nhận của một dòng đơn (0: không nhận dòng đó).
type PartialLine struct {
	OrderItemID  uuid.UUID
	DeliveredQty int32
}

// PartialInput là một lần ghi giao một phần: mọi dòng của đơn (mỗi dòng đúng một lần), tiền hãng đã thu (0 ≤ … ≤ COD).
// Reason chỉ để bên gọi ghi audit (đã qua logging.ScrubText), không lưu ở vận đơn hay phiếu.
type PartialInput struct {
	Lines        []PartialLine
	CODCollected int64
	Reason       string
}

// PartialResult là vận đơn sau lần ghi và phiếu partial_delivery của phần chưa giao.
type PartialResult struct {
	Shipment   ShipmentView
	ReturnID   uuid.UUID
	ReturnCode string
}

// RecordPartialDelivery ghi giao một phần cho vận đơn id trong tx của bên gọi (một transaction; lớp HTTP đã kiểm
// fulfillment.correct). Khóa đơn (ADR-46 nhóm 7) rồi vận đơn (nhóm 8); vận đơn phải là vận đơn hiện hành của đơn và
// qua partial.Decide (shipping, delivery_failed; chờ thu gom khi có partial_reported mở; số giao; tiền). Rồi:
// order.MarkDeliveredPartial cho các dòng đã giao (đơn delivered, phần còn lại in_transit); vận đơn delivered, partial,
// cod_collected_amount; order.CreatePartialReturn cho phần còn lại (phiếu requested, tiền hoàn 0); đóng việc gọi lại
// còn mở (kết quả delivered) và anomaly partial_reported; phát shipment.status_changed (Seq = version mới; effects
// delivered và close_callbacks khi từ giao thất bại; codCollected; partial: true); báo hiệu shipment.changed.
//
// Lỗi (bên gọi lùi cả transaction): không có vận đơn hay đơn → ErrNotFound; trạng thái không cho, không phải vận đơn
// hiện hành, đơn không giao được → ErrPartialNotAllowed; số giao sai → ErrPartialQty; tiền sai → ErrPartialCOD; tồn
// thực không đủ → ErrStockShort.
func (s *Service) RecordPartialDelivery(ctx context.Context, tx pgx.Tx, id uuid.UUID, in PartialInput, a order.Actor,
) (PartialResult, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return PartialResult{}, errNotWired
	}
	r, err := s.lockRun(ctx, tx, ApplyInput{ShipmentID: id}, a)
	if err != nil {
		return PartialResult{}, err
	}
	plan, err := r.planPartial(ctx, in)
	if err != nil {
		return PartialResult{}, err
	}
	if err := r.deliverPartial(ctx, plan, in.CODCollected); err != nil {
		return PartialResult{}, err
	}
	rest := make([]order.ReturnLineInput, len(plan.Rest))
	for i, l := range plan.Rest {
		rest[i] = order.ReturnLineInput{OrderItemID: l.OrderItemID, Quantity: int(l.Qty)}
	}
	ret, err := r.w.CreatePartialReturn(ctx, tx, r.sh.SellerID, r.sh.OrderID, r.sh.ID, rest, a, r.now)
	if err != nil {
		return PartialResult{}, fmt.Errorf("phiếu phần chưa giao: %w", err)
	}
	if err := r.q.ResolvePartialAnomaly(ctx, store.ResolvePartialAnomalyParams{
		ShipmentID: r.sh.ID, Resolution: pgtype.Text{String: resolutionPartialRecorded, Valid: true},
		ResolvedBy: pgtype.UUID{Bytes: a.UserID, Valid: a.UserID != uuid.Nil},
	}); err != nil {
		return PartialResult{}, fmt.Errorf("đóng việc hãng báo giao một phần: %w", err)
	}
	if err := s.publishShipment(ctx, tx, r.sh.TenantID, r.sh.ID, r.sh.WarehouseID); err != nil {
		return PartialResult{}, err
	}
	v, err := s.ShipmentView(ctx, tx, id)
	if err != nil {
		return PartialResult{}, err
	}
	return PartialResult{Shipment: v, ReturnID: ret.ID, ReturnCode: ret.Code}, nil
}

// planPartial xét lần ghi in trên vận đơn, đơn đã khóa.
func (r *applyRun) planPartial(ctx context.Context, in PartialInput) (partial.Plan, error) {
	if !r.isCurrent {
		return partial.Plan{}, fmt.Errorf("%w: không phải vận đơn hiện hành của đơn", ErrPartialNotAllowed)
	}
	reported, err := r.q.HasOpenAnomaly(ctx, store.HasOpenAnomalyParams{
		ShipmentID: r.sh.ID, Kind: shipstate.AnomalyPartialReported,
	})
	if err != nil {
		return partial.Plan{}, fmt.Errorf("đọc việc hãng báo giao một phần: %w", err)
	}
	o, err := order.Get(ctx, r.tx, r.sh.SellerID, r.sh.OrderID)
	if err != nil {
		return partial.Plan{}, fmt.Errorf("đọc đơn của vận đơn: %w", err)
	}
	items := make([]partial.Item, len(o.Items))
	for i, it := range o.Items {
		items[i] = partial.Item{ID: it.ID, Qty: it.Qty}
	}
	req := make([]partial.Line, len(in.Lines))
	for i, l := range in.Lines {
		req[i] = partial.Line{OrderItemID: l.OrderItemID, Qty: l.DeliveredQty}
	}
	plan, refusal := partial.Decide(partial.Shipment{Status: r.sh.Status, PartialReported: reported, COD: r.sh.CodAmount},
		items, req, in.CODCollected)
	switch refusal {
	case partial.OK:
		return plan, nil
	case partial.RefuseStatus:
		return plan, fmt.Errorf("%w: vận đơn %s", ErrPartialNotAllowed, r.sh.Status)
	case partial.RefuseQty:
		return plan, ErrPartialQty
	default:
		return plan, fmt.Errorf("%w: thu %d, COD %d", ErrPartialCOD, in.CODCollected, r.sh.CodAmount)
	}
}

// deliverPartial: đơn và tồn (MarkDeliveredPartial), vận đơn delivered partial, đóng việc gọi lại, phát
// shipment.status_changed.
func (r *applyRun) deliverPartial(ctx context.Context, plan partial.Plan, collected int64) error {
	lines := make([]order.DeliveredLine, len(plan.Delivered))
	for i, l := range plan.Delivered {
		lines[i] = order.DeliveredLine{OrderItemID: l.OrderItemID, Qty: l.Qty}
	}
	applied, err := r.w.MarkDeliveredPartial(ctx, r.tx, r.sh.SellerID, r.sh.OrderID, r.sh.ID, lines, r.actor, r.now)
	switch {
	case errors.Is(err, order.ErrStockShort):
		return fmt.Errorf("giao một phần: %w: %w", ErrStockShort, err)
	case errors.Is(err, order.ErrStatusConflict):
		return fmt.Errorf("giao một phần: %w: %w", ErrPartialNotAllowed, err)
	case err != nil:
		return fmt.Errorf("giao một phần: %w", err)
	case !applied:
		return fmt.Errorf("%w: đơn đã giao hay đã gỡ vận đơn", ErrPartialNotAllowed)
	}
	version, err := r.q.MarkShipmentDeliveredPartial(ctx, store.MarkShipmentDeliveredPartialParams{
		CodCollected: pgtype.Int8{Int64: collected, Valid: true}, Now: r.now, ID: r.sh.ID,
	})
	if err != nil {
		return fmt.Errorf("vận đơn sang delivered (giao một phần): %w", err) // đã khóa và xét trạng thái: không xảy ra
	}
	from := shipstate.Status(r.sh.Status)
	effects := []string{string(shipstate.EffDelivered)}
	if from == shipstate.DeliveryFailed {
		effects = append(effects, string(shipstate.EffCloseCallbacks))
	}
	if err := r.closeCallbacks(ctx, outcomeDelivered); err != nil {
		return err
	}
	r.sh.Status, r.sh.Version = string(shipstate.Delivered), version
	return r.emitPartial(ctx, from, effects, collected)
}

// emitPartial phát shipment.status_changed của lần giao một phần (Seq = version mới).
func (r *applyRun) emitPartial(ctx context.Context, from shipstate.Status, effects []string, collected int64) error {
	at := r.now
	if _, _, err := r.s.d.Events.Emit(ctx, r.tx, r.s.d.Queue, events.Event{
		TenantID: r.sh.TenantID, AggregateType: aggregateShipment, AggregateID: r.sh.ID.String(), Seq: r.sh.Version,
		Topic: TopicShipmentStatusChanged,
		Payload: StatusChanged{
			ShipmentID: r.sh.ID, Status: StatusDelivered, CODAmount: r.sh.CodAmount, OrderID: r.sh.OrderID,
			From: Status(from), Effects: effects, CODCollected: &collected, Partial: true, OccurredAt: &at,
		},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicShipmentStatusChanged, err)
	}
	return nil
}
