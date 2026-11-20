package fulfillment_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/modules/order"
)

// trackJourney sinh hành trình hợp lệ như Task 98 (shipstate property_test): có thể báo chờ lấy, lấy hàng, di chuyển,
// thất bại và giao lại vài lần, kết thúc ở thành công, đang hoàn (có thể kèm đã hoàn về shop) hay dừng giữa đường.
// Thời điểm tăng dần nghiêm ngặt; khóa sự kiện duy nhất.
func trackJourney(t *rapid.T) []fulfillment.CarrierEvent {
	var evs []fulfillment.CarrierEvent
	add := func(c fulfillment.Canonical) {
		n := len(evs)
		evs = append(evs, fulfillment.CarrierEvent{
			Key: fmt.Sprintf("j%d", n), RawStatus: "raw-" + string(c), Canonical: c,
			OccurredAt: testDay().Add(8*time.Hour + time.Duration(n)*time.Minute),
		})
	}
	moving := rapid.SampledFrom([]fulfillment.Canonical{
		fulfillment.CanonPickedUp, fulfillment.CanonInTransit, fulfillment.CanonOutForDelivery,
	})
	if rapid.Bool().Draw(t, "báo chờ lấy") {
		add(fulfillment.CanonAwaitingPickup)
	}
	add(fulfillment.CanonPickedUp)
	for range rapid.IntRange(0, 2).Draw(t, "số lần thất bại") {
		for range rapid.IntRange(0, 1).Draw(t, "tin di chuyển") {
			add(moving.Draw(t, "di chuyển"))
		}
		add(fulfillment.CanonDeliveryFailed)
		if rapid.Bool().Draw(t, "giao lại") {
			add(moving.Draw(t, "giao lại bằng"))
		}
	}
	switch rapid.IntRange(0, 3).Draw(t, "kết thúc") {
	case 0:
		add(fulfillment.CanonDelivered)
	case 1:
		add(fulfillment.CanonReturning)
	case 2:
		add(fulfillment.CanonReturning)
		add(fulfillment.CanonReturnedToShop)
	default:
	}
	return evs
}

// Hành trình ngẫu nhiên, sự kiện xáo trộn chia 1–5 lô (có bản trùng), mỗi lô một transaction → vận đơn ở trạng thái
// của sự kiện mới nhất; đơn, tồn khớp trạng thái đó; xuất kho ≤ 1 lần (đúng 1 khi giao xong); sổ tồn khớp số dư; phiếu
// hoàn đúng 1 khi kết thúc ở hoàn.
func TestApply_PropertyShuffledBatches(t *testing.T) {
	e := newApplyEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		evs := trackJourney(rt)
		arrivals := append([]fulfillment.CarrierEvent(nil), evs...)
		for range rapid.IntRange(0, 3).Draw(rt, "số bản trùng") {
			arrivals = append(arrivals, rapid.SampledFrom(evs).Draw(rt, "bản trùng"))
		}
		arrivals = rapid.Permutation(arrivals).Draw(rt, "thứ tự đến")
		lots := rapid.IntRange(1, min(5, len(arrivals))).Draw(rt, "số lô")
		cuts := make([]int, 0, lots+1)
		cuts = append(cuts, 0)
		for i := 1; i < lots; i++ {
			cuts = append(cuts, rapid.IntRange(cuts[i-1], len(arrivals)).Draw(rt, "chỗ cắt"))
		}
		cuts = append(cuts, len(arrivals))

		o, v := e.order(2)
		s := e.awaiting(o)
		for i := range lots {
			if _, err := e.tryApply(e.input(s.ID, arrivals[cuts[i]:cuts[i+1]]...)); err != nil {
				rt.Fatalf("lô %d: %v", i, err)
			}
		}
		checkFinal(rt, e, o.ID, v, s.ID, evs[len(evs)-1].Canonical)
	})
}

// checkFinal: vận đơn ở trạng thái của sự kiện cuối last; đơn, tồn (qty 2, on_hand 5), xuất kho, phiếu hoàn khớp
// trạng thái đó; sổ tồn khớp số dư.
func checkFinal(rt *rapid.T, e *applyEnv, orderID, v, shipmentID uuid.UUID, last fulfillment.Canonical) {
	want, _ := shipstate.Target(last)
	if got := e.shipment(shipmentID); string(got.Status) != string(want) {
		rt.Fatalf("vận đơn = %s, muốn %s (sự kiện cuối %s)", got.Status, want, last)
	}
	gotOrder := e.getOrder(orderID)
	exports := e.exports(v)
	returns := e.count(`SELECT count(*) FROM ord.returns WHERE shipment_id = $1 AND kind = 'carrier_return'`, shipmentID)
	st := e.stock(v)
	wantOrder, wantStock, wantExports, wantReturns := order.StatusConfirmed, [3]int32{5, 0, 2}, 0, 0
	switch want {
	case shipstate.Delivered:
		wantOrder, wantStock, wantExports = order.StatusDelivered, [3]int32{3, 0, 0}, 1
	case shipstate.Returning:
		wantReturns = 1
	default:
	}
	if gotOrder.Status != wantOrder || st != wantStock || exports != wantExports || returns != wantReturns {
		rt.Fatalf("%s: đơn %s tồn %v xuất %d phiếu hoàn %d", want, gotOrder.Status, st, exports, returns)
	}
	ledgerMatches(rt, e, v)
}

// ledgerMatches: tổng sổ tồn từng ngăn bằng số dư (on_hand, held, in_transit) của biến thể v.
func ledgerMatches(rt *rapid.T, e *applyEnv, v uuid.UUID) {
	st := e.stock(v)
	for i, bucket := range []string{"on_hand", "held", "in_transit"} {
		sum := e.count(`SELECT coalesce(sum(delta), 0)::int FROM inventory.stock_ledger WHERE variant_id = $1
			AND bucket = $2`, v, bucket)
		if int32(sum) != st[i] { //nolint:gosec // số nhỏ của test
			rt.Fatalf("sổ %s = %d, số dư %d", bucket, sum, st[i])
		}
	}
}
