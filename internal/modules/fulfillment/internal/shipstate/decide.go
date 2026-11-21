package shipstate

import (
	"slices"
	"time"
)

// Input là một sự kiện chuẩn của hãng (hay của nhân viên, của hệ thống) cùng trạng thái hiện tại của vận đơn.
type Input struct {
	Current Status
	// LastEventAt là thời điểm của sự kiện mới nhất đã xét (Decision.AdvanceLast); nil khi chưa có.
	LastEventAt *time.Time
	// PickedUp: tác động EffPickedUp đã chạy (hàng không còn held); khi true Decide không phát lại EffPickedUp.
	PickedUp bool
	// IsCurrent: là vận đơn hiện hành của đơn (orders.current_shipment_id).
	IsCurrent bool
	Event     Canonical
	EventAt   time.Time
	Source    Source
}

// Decision là kết quả của Decide.
type Decision struct {
	// Apply: đổi vận đơn sang Next và chạy Effects theo thứ tự.
	Apply   bool
	Next    Status
	Effects []Effect
	// AdvanceLast: bên gọi dời LastEventAt (cột last_carrier_event_at) tới EventAt. Đúng khi Apply, và khi sự kiện trùng
	// trạng thái nhưng mới hơn (không thì tin cũ đến sau một tin trùng mới hơn sẽ bị áp, lùi trạng thái).
	AdvanceLast bool
	// Ignored: lý do không áp (Reason*), rỗng khi Apply.
	Ignored string
	// Anomaly: rỗng hoặc một loại của shipment_anomalies (Anomaly*) bên gọi phải mở.
	Anomaly string
}

// Decide là luật duy nhất của máy trạng thái vận đơn. Thứ tự xét: mã lạ → giao một phần, mất, hỏng → trùng trạng thái
// → tin cũ (VC09) → không phải vận đơn hiện hành (VC11) → bảng cạnh (graph.go).
func Decide(in Input) Decision {
	switch in.Event {
	case CanonPartiallyDelivered: // Q-15: không tự động, đưa vào "Cần xem"
		return Decision{Ignored: ReasonReview, Anomaly: AnomalyPartialReported}
	case CanonLost, CanonDamaged:
		return Decision{Ignored: ReasonReview, Anomaly: AnomalyLostOrDamaged}
	default:
	}
	target, ok := Target(in.Event)
	if !ok {
		return Decision{Ignored: ReasonUnmapped, Anomaly: AnomalyUnmappedStatus}
	}
	older := in.LastEventAt != nil && in.EventAt.Before(*in.LastEventAt)
	e, hasEdge := lookup(in.Current, in.Event)
	// returning + returned_to_shop là cạnh, không phải trùng. Đang chờ hãng trả lời yêu cầu hủy mà hãng báo kiện vẫn chờ
	// thu gom: cũng là trùng (kiện chưa đổi; Task M3-14, hãng trả CancelPending rồi tra bù).
	waiting := in.Current == CancelRequested && in.Event == CanonAwaitingPickup
	if (target == in.Current || waiting) && !hasEdge {
		return Decision{Ignored: ReasonDuplicateState, AdvanceLast: !older}
	}
	if older {
		return Decision{Ignored: ReasonOlder}
	}
	if !in.IsCurrent {
		d := Decision{Ignored: ReasonNotCurrent}
		if moved(in.Event) {
			d.Anomaly = AnomalyNotCurrentMoved
		}
		return d
	}
	if !hasEdge || (len(e.sources) > 0 && !slices.Contains(e.sources, in.Source)) {
		return Decision{Ignored: ReasonInvalidEdge, Anomaly: AnomalyInvalidEdge}
	}
	effects := make([]Effect, 0, len(e.effects))
	for _, ef := range e.effects {
		if ef != EffPickedUp || !in.PickedUp {
			effects = append(effects, ef)
		}
	}
	if len(effects) == 0 {
		effects = nil
	}
	return Decision{Apply: true, Next: e.to, Effects: effects, AdvanceLast: true}
}
