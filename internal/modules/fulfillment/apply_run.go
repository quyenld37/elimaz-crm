package fulfillment

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/store"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

const (
	// actorCarrier, actorSystem: loại người làm của order.Actor (cùng chuỗi với inventory.ActorCarrier, ActorSystem).
	actorCarrier = "carrier"
	actorSystem  = "system"
	maxEventKey  = 200 // CHECK của shipment_events.event_key
	maxRawStatus = 64  // CHECK của shipment_events.raw_status
)

// applyRun là trạng thái của một lần ApplyTracking: vận đơn đã khóa (cập nhật theo từng bước), đơn có đang gắn vận
// đơn này không, bộ ghi của order, báo cáo.
type applyRun struct {
	s         *Service
	tx        pgx.Tx
	q         *store.Queries
	in        ApplyInput
	w         order.Writer
	sh        store.LockShipmentForApplyRow
	isCurrent bool
	actor     order.Actor
	now       time.Time
	rep       ApplyReport
	changed   bool
	callbackH int // CallbackDeadlineHours, đọc lần đầu cần (0: chưa đọc)
}

// ApplyTracking áp kết quả tra in lên vận đơn, đơn, tồn trong tx của bên gọi (một transaction cho cả lô). Thứ tự khóa
// (ADR-46): đọc đơn của vận đơn không khóa → order.LockSnapshot (nhóm 7) → vận đơn FOR UPDATE (nhóm 8). Sự kiện xét
// theo OccurredAt tăng dần (bằng nhau: theo khóa); mỗi sự kiện chèn vào shipment_events kèm quyết định của
// shipstate.Decide (đã có cùng khóa → bỏ qua cả quyết định, VC08); áp thì cập nhật vận đơn (version + 1), chạy tác động
// (apply_effects.go), phát shipment.status_changed khi trạng thái đổi (Seq = version); anomaly mở một lần mỗi loại.
// Cuối: mốc đồng bộ, phí thật, tiền COD hãng báo; báo hiệu shipment.changed khi có bước áp.
//
// Lỗi (bên gọi lùi cả transaction): không có vận đơn hay đơn (tenant khác) → ErrNotFound; khóa sự kiện sai →
// ErrInvalidTracking; tồn thực không đủ → ErrStockShort (bọc cả inventory.ErrStockShort); lỗi của order trả bọc %w.
func (s *Service) ApplyTracking(ctx context.Context, tx pgx.Tx, in ApplyInput) (ApplyReport, error) {
	if s.d.Events == nil || s.d.Queue == nil {
		return ApplyReport{}, errNotWired
	}
	for _, e := range in.Tracking.Events {
		if e.Key == "" || len(e.Key) > maxEventKey {
			return ApplyReport{}, fmt.Errorf("%w: khóa sự kiện dài %d", ErrInvalidTracking, len(e.Key))
		}
	}
	r, err := s.lockRun(ctx, tx, in, actorFor(in))
	if err != nil {
		return ApplyReport{}, err
	}
	r.rep.From = shipstate.Status(r.sh.Status)
	evs := slices.Clone(in.Tracking.Events)
	slices.SortStableFunc(evs, func(a, b CarrierEvent) int {
		return cmp.Or(a.OccurredAt.Compare(b.OccurredAt), cmp.Compare(a.Key, b.Key))
	})
	for _, e := range evs {
		if err := r.event(ctx, e); err != nil {
			return ApplyReport{}, err
		}
	}
	if err := r.q.FinishShipmentSync(ctx, store.FinishShipmentSyncParams{
		SyncedAt: pgtype.Timestamptz{Time: in.SyncedAt, Valid: true}, FeeActual: optInt8(in.Tracking.FeeActual),
		CodCollected: optInt8(in.Tracking.CODCollected), ID: r.sh.ID,
	}); err != nil {
		return ApplyReport{}, fmt.Errorf("ghi mốc đồng bộ vận đơn: %w", err)
	}
	r.rep.To = shipstate.Status(r.sh.Status)
	if r.changed {
		if err := s.publishShipment(ctx, tx, r.sh.TenantID, r.sh.ID, r.sh.WarehouseID); err != nil {
			return ApplyReport{}, err
		}
	}
	return r.rep, nil
}

// lockRun dựng một lần áp in (người làm actor) trong tx rồi khóa đơn, vận đơn (lock).
func (s *Service) lockRun(ctx context.Context, tx pgx.Tx, in ApplyInput, actor order.Actor) (*applyRun, error) {
	r := &applyRun{
		s: s, tx: tx, q: store.New(tx), in: in, actor: actor,
		w:   order.Writer{Events: s.d.Events, Queue: s.d.Queue, Realtime: s.d.Bus},
		now: s.d.Now().UTC(),
	}
	if err := r.lock(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// lock khóa đơn rồi vận đơn (ADR-46 nhóm 7 → 8) và ghi đơn có đang gắn vận đơn này không.
func (r *applyRun) lock(ctx context.Context) error {
	ref, err := r.q.ShipmentOrderRef(ctx, r.in.ShipmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("đọc đơn của vận đơn: %w", err)
	}
	snap, err := order.LockSnapshot(ctx, r.tx, ref.SellerID, ref.OrderID)
	if errors.Is(err, order.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("khóa đơn của vận đơn: %w", err)
	}
	if r.sh, err = r.q.LockShipmentForApply(ctx, r.in.ShipmentID); err != nil {
		return fmt.Errorf("khóa vận đơn: %w", err)
	}
	r.isCurrent = snap.CurrentShipmentID != nil && *snap.CurrentShipmentID == r.sh.ID
	return nil
}

// actorFor: người làm của lần áp; loại rỗng lấy theo nguồn.
func actorFor(in ApplyInput) order.Actor {
	a := in.Actor
	if a.Type == "" {
		switch in.Source {
		case SrcCarrierAPI:
			a.Type = actorCarrier
		case SrcSystem:
			a.Type = actorSystem
		case SrcStaff:
		}
	}
	return a
}

// event xét một sự kiện: quyết định, chèn lịch sử (đã có → không làm gì), mở anomaly, áp.
func (r *applyRun) event(ctx context.Context, e CarrierEvent) error {
	var last *time.Time
	if r.sh.LastCarrierEventAt.Valid {
		last = &r.sh.LastCarrierEventAt.Time
	}
	d := shipstate.Decide(shipstate.Input{
		Current: shipstate.Status(r.sh.Status), LastEventAt: last, PickedUp: r.sh.PickedUpAt.Valid,
		IsCurrent: r.isCurrent, Partial: r.sh.Partial, Event: e.Canonical, EventAt: e.OccurredAt, Source: r.in.Source,
	})
	eventID, inserted, err := r.insertEvent(ctx, e, d)
	if err != nil || !inserted {
		return err
	}
	r.rep.Inserted++
	if d.Anomaly != "" {
		if err := r.openAnomaly(ctx, d.Anomaly, map[string]any{
			"eventId": eventID.String(), "canonical": string(e.Canonical), "status": r.sh.Status,
		}); err != nil {
			return err
		}
	}
	if !d.Apply {
		r.rep.Ignored++
		if !d.AdvanceLast {
			return nil
		}
		if err := r.q.AdvanceShipmentLastEvent(ctx, store.AdvanceShipmentLastEventParams{
			EventAt: e.OccurredAt, ID: r.sh.ID,
		}); err != nil {
			return fmt.Errorf("dời mốc sự kiện của vận đơn: %w", err)
		}
		r.advanceLast(e.OccurredAt)
		return nil
	}
	return r.applyStep(ctx, e, d, eventID)
}

// insertEvent chèn dòng lịch sử kèm quyết định d; inserted = false khi đã có cùng khóa. raw chỉ mang trường đã chuẩn
// hóa, chuỗi tự do qua logging.ScrubText (không SĐT, địa chỉ).
func (r *applyRun) insertEvent(ctx context.Context, e CarrierEvent, d shipstate.Decision) (uuid.UUID, bool, error) {
	target, _ := shipstate.Target(e.Canonical)
	rawStatus := truncateRunes(logging.ScrubText(e.RawStatus), maxRawStatus)
	if rawStatus == "" {
		rawStatus = string(e.Canonical)
	}
	raw, err := json.Marshal(map[string]string{
		"key": e.Key, "rawStatus": rawStatus, "canonical": string(e.Canonical),
		"subStatus": logging.ScrubText(e.SubStatus), "reason": logging.ScrubText(e.Reason),
	})
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("mã hóa sự kiện vận đơn: %w", err)
	}
	id, err := r.q.InsertTrackedEvent(ctx, store.InsertTrackedEventParams{
		ShipmentID: r.sh.ID, EventKey: e.Key, RawStatus: rawStatus, Status: string(target),
		OccurredAt: e.OccurredAt, Raw: raw, Canonical: pgtype.Text{String: string(e.Canonical), Valid: true},
		Source: pgtype.Text{String: string(r.in.Source), Valid: true}, SubStatus: logging.ScrubText(e.SubStatus),
		Applied:       pgtype.Bool{Bool: d.Apply, Valid: true},
		IgnoredReason: pgtype.Text{String: d.Ignored, Valid: d.Ignored != ""},
		ActorID:       pgtype.UUID{Bytes: r.actor.UserID, Valid: r.actor.UserID != uuid.Nil},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("ghi sự kiện vận đơn: %w", err)
	}
	return id, true, nil
}

// applyStep cập nhật vận đơn sang d.Next (mốc theo tác động), chạy tác động theo thứ tự, phát status_changed khi
// trạng thái đổi.
func (r *applyRun) applyStep(ctx context.Context, e CarrierEvent, d shipstate.Decision, eventID uuid.UUID) error {
	at := pgtype.Timestamptz{Time: e.OccurredAt, Valid: true}
	p := store.ApplyShipmentStatusParams{
		Status: string(d.Next), SubStatus: logging.ScrubText(e.SubStatus), EventAt: e.OccurredAt, ID: r.sh.ID,
	}
	for _, ef := range d.Effects {
		switch ef {
		case shipstate.EffPickedUp:
			p.PickedUpAt = at
		case shipstate.EffDelivered:
			p.DeliveredAt = at
		case shipstate.EffCarrierReturned:
			p.ReturnedReportedAt = at
		case shipstate.EffDeliveryFailed, shipstate.EffReturning, shipstate.EffCancelConfirmed,
			shipstate.EffCancelRejected, shipstate.EffDeliveryReversed, shipstate.EffCloseCallbacks:
		}
	}
	version, err := r.q.ApplyShipmentStatus(ctx, p)
	if err != nil {
		return fmt.Errorf("cập nhật trạng thái vận đơn: %w", err)
	}
	from := shipstate.Status(r.sh.Status)
	r.sh.Status, r.sh.Version = string(d.Next), version
	if p.PickedUpAt.Valid && !r.sh.PickedUpAt.Valid {
		r.sh.PickedUpAt = p.PickedUpAt
	}
	r.advanceLast(e.OccurredAt)
	for _, ef := range d.Effects {
		if err := r.effect(ctx, ef, d.Next, eventID, e); err != nil {
			return err
		}
	}
	r.rep.Applied++
	r.rep.Effects = append(r.rep.Effects, d.Effects...)
	r.changed = true
	if from == d.Next {
		return nil
	}
	return r.emitChanged(ctx, from, d, e)
}

func (r *applyRun) advanceLast(at time.Time) {
	if !r.sh.LastCarrierEventAt.Valid || at.After(r.sh.LastCarrierEventAt.Time) {
		r.sh.LastCarrierEventAt = pgtype.Timestamptz{Time: at, Valid: true}
	}
}

// emitChanged phát shipment.status_changed của một bước (Seq = version sau bước).
func (r *applyRun) emitChanged(ctx context.Context, from shipstate.Status, d shipstate.Decision, e CarrierEvent) error {
	effects := make([]string, 0, len(d.Effects))
	for _, ef := range d.Effects {
		effects = append(effects, string(ef))
	}
	at := e.OccurredAt.UTC()
	if _, _, err := r.s.d.Events.Emit(ctx, r.tx, r.s.d.Queue, events.Event{
		TenantID: r.sh.TenantID, AggregateType: aggregateShipment, AggregateID: r.sh.ID.String(), Seq: r.sh.Version,
		Topic: TopicShipmentStatusChanged,
		Payload: StatusChanged{
			ShipmentID: r.sh.ID, Status: Status(d.Next), CODAmount: r.sh.CodAmount, OrderID: r.sh.OrderID,
			From: Status(from), Effects: effects, CODCollected: r.in.Tracking.CODCollected, OccurredAt: &at,
			Source: string(r.in.Source),
		},
	}); err != nil {
		return fmt.Errorf("phát %s: %w", TopicShipmentStatusChanged, err)
	}
	return nil
}

// openAnomaly mở bất thường kind của vận đơn (đã mở cùng loại → không thêm) và đặt needs_review.
func (r *applyRun) openAnomaly(ctx context.Context, kind string, detail map[string]any) error {
	opened, err := openAnomaly(ctx, r.q, r.sh.ID, kind, detail)
	if err != nil {
		return err
	}
	if opened {
		r.rep.Anomalies = append(r.rep.Anomalies, kind)
		r.changed = true
	}
	return nil
}

// openAnomaly chèn bất thường kind (đã có bất thường mở cùng loại → opened = false) và đặt needs_review của vận đơn.
func openAnomaly(ctx context.Context, q *store.Queries, shipmentID uuid.UUID, kind string, detail map[string]any,
) (opened bool, err error) {
	b, err := json.Marshal(detail)
	if err != nil {
		return false, fmt.Errorf("mã hóa chi tiết bất thường: %w", err)
	}
	n, err := q.OpenShipmentAnomaly(ctx, store.OpenShipmentAnomalyParams{ShipmentID: shipmentID, Kind: kind, Detail: b})
	if err != nil {
		return false, fmt.Errorf("mở bất thường %s: %w", kind, err)
	}
	if err := q.MarkShipmentNeedsReview(ctx, shipmentID); err != nil {
		return false, fmt.Errorf("đánh dấu vận đơn cần xem: %w", err)
	}
	return n > 0, nil
}

// publishShipment báo hiệu shipment.changed (fulfillment.read ở kho của tài khoản hãng); Bus nil → bỏ qua.
func (s *Service) publishShipment(ctx context.Context, tx pgx.Tx, tenant, id, warehouse uuid.UUID) error {
	if s.d.Bus == nil {
		return nil
	}
	if err := s.d.Bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: signalShipmentChanged, Requires: permShipmentRead,
		Scope: realtime.WarehouseScope(warehouse.String()), Entity: realtime.Entity{Type: "shipment", ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", signalShipmentChanged, err)
	}
	return nil
}

// optInt8: số tiền hãng báo (nil: chưa báo → NULL, câu SQL giữ giá trị cũ).
func optInt8(v *int64) pgtype.Int8 {
	var n pgtype.Int8
	if v != nil {
		n.Int64, n.Valid = *v, true
	}
	return n
}

// truncateRunes cắt s còn tối đa n ký tự (không cắt giữa ký tự UTF-8).
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
