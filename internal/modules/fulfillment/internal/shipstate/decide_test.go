package shipstate_test

import (
	"flag"
	"os"
	"slices"
	"testing"
	"time"

	ss "elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain đặt số lượt rapid 1.000 (kế hoạch Task 98), trừ khi người chạy đã đặt -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(1000)
	os.Exit(m.Run())
}

func at(hhmm string) time.Time {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(t time.Time) *time.Time { return &t }

// in dựng Input cho vận đơn hiện hành, nguồn API của hãng, chưa có sự kiện nào; hàm thay đổi từng ca.
func in(cur ss.Status, ev ss.Canonical, mods ...func(*ss.Input)) ss.Input {
	i := ss.Input{Current: cur, IsCurrent: true, Event: ev, EventAt: at("10:00"), Source: ss.SrcCarrierAPI}
	for _, m := range mods {
		m(&i)
	}
	return i
}

func pickedUp(i *ss.Input) { i.PickedUp = true }

func applied(next ss.Status, effects ...ss.Effect) ss.Decision {
	return ss.Decision{Apply: true, AdvanceLast: true, Next: next, Effects: effects}
}

func ignored(reason, anomaly string) ss.Decision {
	return ss.Decision{Ignored: reason, Anomaly: anomaly}
}

func check(t *testing.T, name string, got, want ss.Decision) {
	t.Helper()
	if got.Apply != want.Apply || got.AdvanceLast != want.AdvanceLast || got.Next != want.Next ||
		got.Ignored != want.Ignored || got.Anomaly != want.Anomaly || !slices.Equal(got.Effects, want.Effects) {
		t.Errorf("%s: Decide = %+v, muốn %+v", name, got, want)
	}
}

func moving() []ss.Canonical {
	return []ss.Canonical{ss.CanonPickedUp, ss.CanonInTransit, ss.CanonOutForDelivery}
}

// Mỗi dòng của bảng cạnh trong kế hoạch là một ca (kể cả từng sự kiện "đang di chuyển"), thêm các cạnh
// returned_to_shop (Quyết định thay chủ shop 2).
func TestDecide_Edges(t *testing.T) {
	cases := slices.Concat(movingEdges(), tableEdges(), returnedToShopEdges())
	for _, c := range cases {
		check(t, c.name, ss.Decide(c.in), c.want)
	}
}

type edgeCase struct {
	name string
	in   ss.Input
	want ss.Decision
}

func movingEdges() []edgeCase {
	var cases []edgeCase
	for _, ev := range moving() {
		cases = append(cases,
			edgeCase{"awaiting+" + string(ev), in(ss.AwaitingPickup, ev), applied(ss.Shipping, ss.EffPickedUp)},
			edgeCase{
				"cancel_requested+" + string(ev), in(ss.CancelRequested, ev),
				applied(ss.Shipping, ss.EffPickedUp, ss.EffCancelRejected),
			},
			edgeCase{"delivery_failed+" + string(ev) + " (giao lại)", in(ss.DeliveryFailed, ev, pickedUp), applied(ss.Shipping)},
		)
	}
	return cases
}

func tableEdges() []edgeCase {
	return []edgeCase{
		{
			"awaiting+delivered", in(ss.AwaitingPickup, ss.CanonDelivered),
			applied(ss.Delivered, ss.EffPickedUp, ss.EffDelivered),
		},
		{
			"awaiting+delivery_failed", in(ss.AwaitingPickup, ss.CanonDeliveryFailed),
			applied(ss.DeliveryFailed, ss.EffPickedUp, ss.EffDeliveryFailed),
		},
		{
			"awaiting+returning", in(ss.AwaitingPickup, ss.CanonReturning),
			applied(ss.Returning, ss.EffPickedUp, ss.EffReturning),
		},
		{"awaiting+cancelled", in(ss.AwaitingPickup, ss.CanonCancelled), applied(ss.Cancelled, ss.EffCancelConfirmed)},
		{
			"cancel_requested+cancelled", in(ss.CancelRequested, ss.CanonCancelled),
			applied(ss.Cancelled, ss.EffCancelConfirmed),
		},
		{
			"cancel_requested+returning, hàng còn held", in(ss.CancelRequested, ss.CanonReturning),
			applied(ss.Returning, ss.EffPickedUp, ss.EffCancelRejected, ss.EffReturning),
		},
		{
			"cancel_requested+returning, đã lấy hàng", in(ss.CancelRequested, ss.CanonReturning, pickedUp),
			applied(ss.Returning, ss.EffCancelRejected, ss.EffReturning),
		},
		{
			"shipping+delivered", in(ss.Shipping, ss.CanonDelivered, pickedUp),
			applied(ss.Delivered, ss.EffDelivered, ss.EffCloseCallbacks),
		},
		{
			"shipping+delivery_failed", in(ss.Shipping, ss.CanonDeliveryFailed, pickedUp),
			applied(ss.DeliveryFailed, ss.EffDeliveryFailed),
		},
		{
			"shipping+returning", in(ss.Shipping, ss.CanonReturning, pickedUp),
			applied(ss.Returning, ss.EffReturning, ss.EffCloseCallbacks),
		},
		{
			"delivery_failed+returning", in(ss.DeliveryFailed, ss.CanonReturning, pickedUp),
			applied(ss.Returning, ss.EffReturning, ss.EffCloseCallbacks),
		},
		{
			"delivery_failed+delivered", in(ss.DeliveryFailed, ss.CanonDelivered, pickedUp),
			applied(ss.Delivered, ss.EffDelivered, ss.EffCloseCallbacks),
		},
		{
			"TD10 delivered+returning API", in(ss.Delivered, ss.CanonReturning, pickedUp),
			applied(ss.Returning, ss.EffDeliveryReversed, ss.EffReturning),
		},
		{
			"TD10 delivered+returning nhân viên", in(ss.Delivered, ss.CanonReturning, pickedUp,
				func(i *ss.Input) { i.Source = ss.SrcStaff }),
			applied(ss.Returning, ss.EffDeliveryReversed, ss.EffReturning),
		},
		{
			"TD10 delivered+returning hệ thống", in(ss.Delivered, ss.CanonReturning, pickedUp,
				func(i *ss.Input) { i.Source = ss.SrcSystem }),
			ignored(ss.ReasonInvalidEdge, ss.AnomalyInvalidEdge),
		},
		{
			"TD10 delivered+returning nguồn rỗng", in(ss.Delivered, ss.CanonReturning, pickedUp,
				func(i *ss.Input) { i.Source = "" }),
			ignored(ss.ReasonInvalidEdge, ss.AnomalyInvalidEdge),
		},
	}
}

func returnedToShopEdges() []edgeCase {
	return []edgeCase{
		// Q-9: hãng báo đã hoàn về shop khi đang hoàn → vẫn returning, chỉ ghi mốc.
		{
			"returning+returned_to_shop", in(ss.Returning, ss.CanonReturnedToShop, pickedUp),
			applied(ss.Returning, ss.EffCarrierReturned),
		},
		// Thiếu tin "đang hoàn" (hãng chỉ trả trạng thái mới nhất khi tra): returned_to_shop làm luôn việc của returning.
		{
			"shipping+returned_to_shop", in(ss.Shipping, ss.CanonReturnedToShop, pickedUp),
			applied(ss.Returning, ss.EffReturning, ss.EffCloseCallbacks, ss.EffCarrierReturned),
		},
		{
			"delivery_failed+returned_to_shop", in(ss.DeliveryFailed, ss.CanonReturnedToShop, pickedUp),
			applied(ss.Returning, ss.EffReturning, ss.EffCloseCallbacks, ss.EffCarrierReturned),
		},
		{
			"awaiting+returned_to_shop", in(ss.AwaitingPickup, ss.CanonReturnedToShop),
			applied(ss.Returning, ss.EffPickedUp, ss.EffReturning, ss.EffCarrierReturned),
		},
		{
			"cancel_requested+returned_to_shop", in(ss.CancelRequested, ss.CanonReturnedToShop),
			applied(ss.Returning, ss.EffPickedUp, ss.EffCancelRejected, ss.EffReturning, ss.EffCarrierReturned),
		},
		{
			"delivered+returned_to_shop API", in(ss.Delivered, ss.CanonReturnedToShop, pickedUp),
			applied(ss.Returning, ss.EffDeliveryReversed, ss.EffReturning, ss.EffCarrierReturned),
		},
		{
			"delivered+returned_to_shop hệ thống", in(ss.Delivered, ss.CanonReturnedToShop, pickedUp,
				func(i *ss.Input) { i.Source = ss.SrcSystem }),
			ignored(ss.ReasonInvalidEdge, ss.AnomalyInvalidEdge),
		},
	}
}

// Các ca riêng của kế hoạch (VC08, VC09, VC11, VC12, thiếu tin lấy hàng, unmapped, giao một phần).
func TestDecide_PlanCases(t *testing.T) {
	last := func(hhmm string) func(*ss.Input) { return func(i *ss.Input) { i.LastEventAt = ptr(at(hhmm)) } }
	evAt := func(hhmm string) func(*ss.Input) { return func(i *ss.Input) { i.EventAt = at(hhmm) } }
	notCurrent := func(i *ss.Input) { i.IsCurrent = false }

	check(t, "VC09 tin cũ", ss.Decide(in(ss.Delivered, ss.CanonInTransit, pickedUp, last("10:00"), evAt("09:30"))),
		ignored(ss.ReasonOlder, ""))
	check(t, "VC08 trùng trạng thái, mới hơn",
		ss.Decide(in(ss.Delivered, ss.CanonDelivered, pickedUp, last("10:00"), evAt("10:05"))),
		ss.Decision{Ignored: ss.ReasonDuplicateState, AdvanceLast: true})
	check(t, "VC08 trùng trạng thái, cũ hơn",
		ss.Decide(in(ss.Delivered, ss.CanonDelivered, pickedUp, last("10:00"), evAt("09:55"))),
		ignored(ss.ReasonDuplicateState, ""))
	check(t, "trùng trạng thái, chưa có sự kiện", ss.Decide(in(ss.Shipping, ss.CanonInTransit, pickedUp)),
		ss.Decision{Ignored: ss.ReasonDuplicateState, AdvanceLast: true})
	check(t, "in_transit sau picked_up", ss.Decide(in(ss.Shipping, ss.CanonInTransit, pickedUp, last("09:00"))),
		ss.Decision{Ignored: ss.ReasonDuplicateState, AdvanceLast: true})
	check(t, "delivered rồi in_transit mới hơn",
		ss.Decide(in(ss.Delivered, ss.CanonInTransit, pickedUp, last("10:00"), evAt("11:00"))),
		ignored(ss.ReasonInvalidEdge, ss.AnomalyInvalidEdge))
	check(t, "cùng thời điểm không phải cũ",
		ss.Decide(in(ss.Shipping, ss.CanonDelivered, pickedUp, last("10:00"), evAt("10:00"))),
		applied(ss.Delivered, ss.EffDelivered, ss.EffCloseCallbacks))
	check(t, "đang yêu cầu hủy, kiện vẫn chờ thu gom (Task M3-14)",
		ss.Decide(in(ss.CancelRequested, ss.CanonAwaitingPickup, last("09:00"))),
		ss.Decision{Ignored: ss.ReasonDuplicateState, AdvanceLast: true})
	check(t, "VC11 không hiện hành, hủy", ss.Decide(in(ss.AwaitingPickup, ss.CanonCancelled, notCurrent)),
		ignored(ss.ReasonNotCurrent, ""))
	check(t, "VC11 không hiện hành, lấy hàng", ss.Decide(in(ss.Cancelled, ss.CanonPickedUp, notCurrent)),
		ignored(ss.ReasonNotCurrent, ss.AnomalyNotCurrentMoved))
	for _, ev := range []ss.Canonical{
		ss.CanonInTransit, ss.CanonOutForDelivery, ss.CanonDeliveryFailed,
		ss.CanonDelivered, ss.CanonReturning, ss.CanonReturnedToShop,
	} {
		check(t, "không hiện hành, hàng rời kho "+string(ev), ss.Decide(in(ss.Cancelled, ev, notCurrent)),
			ignored(ss.ReasonNotCurrent, ss.AnomalyNotCurrentMoved))
	}
	check(t, "không hiện hành, chờ lấy", ss.Decide(in(ss.Cancelled, ss.CanonAwaitingPickup, notCurrent)),
		ignored(ss.ReasonNotCurrent, ""))
	check(t, "không hiện hành, tin cũ", ss.Decide(in(ss.Cancelled, ss.CanonPickedUp, notCurrent, last("10:00"),
		evAt("09:00"))), ignored(ss.ReasonOlder, ""))
	check(t, "VC12 đang yêu cầu hủy, lấy hàng", ss.Decide(in(ss.CancelRequested, ss.CanonPickedUp)),
		applied(ss.Shipping, ss.EffPickedUp, ss.EffCancelRejected))
	check(t, "thiếu tin lấy hàng", ss.Decide(in(ss.AwaitingPickup, ss.CanonDelivered)),
		applied(ss.Delivered, ss.EffPickedUp, ss.EffDelivered))
	check(t, "đã lấy hàng thì không phát lại picked_up", ss.Decide(in(ss.AwaitingPickup, ss.CanonDelivered, pickedUp)),
		applied(ss.Delivered, ss.EffDelivered))
	check(t, "unknown", ss.Decide(in(ss.Shipping, ss.CanonUnknown)), ignored(ss.ReasonUnmapped, ss.AnomalyUnmappedStatus))
	check(t, "mã chuẩn lạ", ss.Decide(in(ss.Shipping, "lạ")), ignored(ss.ReasonUnmapped, ss.AnomalyUnmappedStatus))
	check(t, "giao một phần", ss.Decide(in(ss.Shipping, ss.CanonPartiallyDelivered)),
		ignored(ss.ReasonReview, ss.AnomalyPartialReported))
	check(t, "giao một phần, không hiện hành", ss.Decide(in(ss.Shipping, ss.CanonPartiallyDelivered, notCurrent)),
		ignored(ss.ReasonReview, ss.AnomalyPartialReported))
	for _, ev := range []ss.Canonical{ss.CanonLost, ss.CanonDamaged} {
		check(t, string(ev), ss.Decide(in(ss.Shipping, ev)), ignored(ss.ReasonReview, ss.AnomalyLostOrDamaged))
	}
	check(t, "returning+returning", ss.Decide(in(ss.Returning, ss.CanonReturning, pickedUp)),
		ss.Decision{Ignored: ss.ReasonDuplicateState, AdvanceLast: true})
	check(t, "shipping lùi về chờ lấy", ss.Decide(in(ss.Shipping, ss.CanonAwaitingPickup, pickedUp)),
		ignored(ss.ReasonInvalidEdge, ss.AnomalyInvalidEdge))
	check(t, "creating không nhận tin hãng", ss.Decide(in(ss.Creating, ss.CanonPickedUp)),
		ignored(ss.ReasonInvalidEdge, ss.AnomalyInvalidEdge))
}

// Không sự kiện nào của hãng đưa tới returned hay rời cancelled, returned (trừ trùng trạng thái).
func TestDecide_TerminalAndReturned(t *testing.T) {
	for _, cur := range ss.Statuses() {
		for _, ev := range ss.Canonicals() {
			for _, src := range []ss.Source{ss.SrcCarrierAPI, ss.SrcStaff, ss.SrcSystem} {
				d := ss.Decide(in(cur, ev, func(i *ss.Input) { i.Source = src }))
				if d.Apply && d.Next == ss.Returned {
					t.Errorf("%s+%s: tới returned", cur, ev)
				}
				if d.Apply && (cur == ss.Cancelled || cur == ss.Returned) {
					t.Errorf("%s+%s: rời trạng thái cuối", cur, ev)
				}
			}
		}
	}
}

func TestTarget(t *testing.T) {
	want := map[ss.Canonical]ss.Status{
		ss.CanonAwaitingPickup: ss.AwaitingPickup, ss.CanonPickedUp: ss.Shipping, ss.CanonInTransit: ss.Shipping,
		ss.CanonOutForDelivery: ss.Shipping, ss.CanonDeliveryFailed: ss.DeliveryFailed, ss.CanonDelivered: ss.Delivered,
		ss.CanonReturning: ss.Returning, ss.CanonReturnedToShop: ss.Returning, ss.CanonCancelled: ss.Cancelled,
	}
	for _, c := range append(ss.Canonicals(), "lạ") {
		got, ok := ss.Target(c)
		w, has := want[c]
		if ok != has || got != w {
			t.Errorf("Target(%q) = %q %v, muốn %q %v", c, got, ok, w, has)
		}
	}
}

func TestDisplay(t *testing.T) {
	want := map[ss.Status]string{
		ss.Creating: "Đang gửi hãng", ss.AwaitingPickup: "Chờ thu gom", ss.Shipping: "Đang chuyển",
		ss.DeliveryFailed: "Thất bại", ss.Delivered: "Thành công", ss.Returning: "Đang chuyển hoàn", ss.Returned: "Đã hoàn",
		ss.CancelRequested: "Đang yêu cầu hủy", ss.Cancelled: "Đã hủy",
	}
	if len(ss.Statuses()) != len(want) {
		t.Fatalf("Statuses() có %d trạng thái, bảng nhãn %d", len(ss.Statuses()), len(want))
	}
	for _, s := range ss.Statuses() {
		for _, intent := range []string{"", "order_cancel", "replace"} {
			if got := ss.Display(s, "giao lần 2", intent); got != want[s] {
				t.Errorf("Display(%s, %q) = %q, muốn %q", s, intent, got, want[s])
			}
		}
	}
	if got := ss.Display("lạ", "", ""); got != "lạ" {
		t.Errorf("Display(lạ) = %q, muốn giữ nguyên mã", got)
	}
}

// Vận đơn đã giao một phần (Task M3-26): mọi cạnh đảo "đã giao" (returning, returned_to_shop) không áp, kể cả từ API có
// xác thực hay nhân viên ghi; mở invalid_edge để người xem. Vận đơn giao đủ vẫn đảo được.
func TestDecide_PartialNoReversal(t *testing.T) {
	partial := func(i *ss.Input) { i.Partial = true }
	for _, src := range []ss.Source{ss.SrcCarrierAPI, ss.SrcStaff} {
		for _, ev := range []ss.Canonical{ss.CanonReturning, ss.CanonReturnedToShop} {
			src := func(i *ss.Input) { i.Source = src }
			check(t, "partial "+string(ev), ss.Decide(in(ss.Delivered, ev, pickedUp, partial, src)),
				ignored(ss.ReasonInvalidEdge, ss.AnomalyInvalidEdge))
		}
	}
	check(t, "đủ returning", ss.Decide(in(ss.Delivered, ss.CanonReturning, pickedUp)),
		applied(ss.Returning, ss.EffDeliveryReversed, ss.EffReturning))
	check(t, "partial delivered trùng", ss.Decide(in(ss.Delivered, ss.CanonDelivered, pickedUp, partial)),
		ss.Decision{Ignored: ss.ReasonDuplicateState, AdvanceLast: true})
}
