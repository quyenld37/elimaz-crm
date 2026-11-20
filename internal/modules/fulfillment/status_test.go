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

// Advance gọi shipstate.Decide nhưng giữ đúng luật thứ hạng của M1 (creating 0 < awaiting_pickup 1 < shipping 2 <
// delivered 4; trạng thái khác không có hạng) trên mọi cặp của chín trạng thái và một mã lạ (Task 98: không đổi hành vi).
func TestAdvance_SameAsM1Rank(t *testing.T) {
	m1Rank := map[fulfillment.Status]int{
		fulfillment.StatusCreating: 0, fulfillment.StatusAwaitingPickup: 1, fulfillment.StatusShipping: 2,
		fulfillment.StatusDelivered: 4,
	}
	all := []fulfillment.Status{
		fulfillment.StatusCreating, fulfillment.StatusAwaitingPickup, fulfillment.StatusShipping,
		fulfillment.StatusDeliveryFailed, fulfillment.StatusDelivered, fulfillment.StatusReturning,
		fulfillment.StatusReturned, fulfillment.StatusCancelRequested, fulfillment.StatusCancelled, "lạ",
	}
	for _, cur := range all {
		for _, next := range all {
			rc, okc := m1Rank[cur]
			rn, okn := m1Rank[next]
			wantOK := okc && okn && rn > rc
			want := cur
			if wantOK {
				want = next
			}
			if got, ok := fulfillment.Advance(cur, next); got != want || ok != wantOK {
				t.Errorf("Advance(%s, %s) = %s %v, muốn %s %v", cur, next, got, ok, want, wantOK)
			}
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
	for _, s := range append(forwardStatuses(), fulfillment.StatusCancelled, fulfillment.StatusDeliveryFailed,
		fulfillment.StatusReturning, fulfillment.StatusReturned, fulfillment.StatusCancelRequested) {
		step, ok := s.Step()
		if w, has := want[s]; ok != has || step != w {
			t.Errorf("%s.Step() = %q %v", s, step, ok)
		}
	}
}
