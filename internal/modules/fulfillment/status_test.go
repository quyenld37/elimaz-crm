package fulfillment_test

import (
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
)

func forwardStatuses() []fulfillment.Status {
	return []fulfillment.Status{
		fulfillment.StatusCreating, fulfillment.StatusAwaitingPickup, fulfillment.StatusShipping, fulfillment.StatusDelivered,
	}
}

// Advance chỉ tiến: lùi hoặc bằng → giữ nguyên, không đổi (VC09 bản đầu); trạng thái lạ hay cancelled → không đổi.
func TestAdvance(t *testing.T) {
	for i, cur := range forwardStatuses() {
		for j, next := range forwardStatuses() {
			got, ok := fulfillment.Advance(cur, next)
			if j > i && (got != next || !ok) {
				t.Errorf("Advance(%s, %s) = %s %v; muốn tiến", cur, next, got, ok)
			}
			if j <= i && (got != cur || ok) {
				t.Errorf("Advance(%s, %s) = %s %v; muốn giữ nguyên", cur, next, got, ok)
			}
		}
	}
	for _, c := range [][2]fulfillment.Status{
		{fulfillment.StatusCancelled, fulfillment.StatusDelivered},
		{fulfillment.StatusAwaitingPickup, fulfillment.StatusCancelled},
		{fulfillment.StatusShipping, "returning"},
		{"lạ", fulfillment.StatusDelivered},
	} {
		if got, ok := fulfillment.Advance(c[0], c[1]); ok || got != c[0] {
			t.Errorf("Advance(%s, %s) = %s %v; muốn không đổi", c[0], c[1], got, ok)
		}
	}
}

// Chuỗi tin ngẫu nhiên (lặp, đảo thứ tự) → trạng thái cuối là tin có hạng cao nhất, không bao giờ lùi.
func TestAdvance_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		msgs := rapid.SliceOf(rapid.SampledFrom(forwardStatuses())).Draw(t, "msgs")
		cur, best := fulfillment.StatusCreating, 0
		for _, m := range msgs {
			prev := cur
			cur, _ = fulfillment.Advance(cur, m)
			if rank(cur) < rank(prev) {
				t.Fatalf("lùi từ %s về %s", prev, cur)
			}
			best = max(best, rank(m))
		}
		if rank(cur) != best {
			t.Fatalf("cuối %s, muốn hạng %d", cur, best)
		}
	})
}

func rank(s fulfillment.Status) int {
	for i, f := range forwardStatuses() {
		if f == s {
			return i
		}
	}
	return -1
}

// shipping → bước picked_up của đơn, delivered → delivered; trạng thái khác không có bước.
func TestStatusStep(t *testing.T) {
	want := map[fulfillment.Status]order.ShipmentStep{
		fulfillment.StatusShipping: order.StepPickedUp, fulfillment.StatusDelivered: order.StepDelivered,
	}
	for _, s := range append(forwardStatuses(), fulfillment.StatusCancelled) {
		step, ok := s.Step()
		if w, has := want[s]; ok != has || step != w {
			t.Errorf("%s.Step() = %q %v", s, step, ok)
		}
	}
}
