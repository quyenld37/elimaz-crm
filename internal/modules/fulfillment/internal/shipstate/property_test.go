package shipstate_test

import (
	"slices"
	"testing"
	"time"

	"pgregory.net/rapid"

	ss "elimaz.com.vn/crm/internal/modules/fulfillment/internal/shipstate"
)

type event struct {
	canon ss.Canonical
	at    time.Time
}

// journey sinh một hành trình hợp lệ của hãng từ "chờ lấy hàng": có thể báo lại chờ lấy, rồi di chuyển, thất bại
// và giao lại vài lần, kết thúc ở thành công, đang hoàn (có thể kèm "đã hoàn về shop") hay dừng giữa chừng. Thời điểm
// tăng dần nghiêm ngặt.
func journey(t *rapid.T) []event {
	base := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	var evs []event
	add := func(c ss.Canonical) {
		evs = append(evs, event{canon: c, at: base.Add(time.Duration(len(evs)) * time.Minute)})
	}
	movingEv := rapid.SampledFrom(moving())
	if rapid.Bool().Draw(t, "báo chờ lấy") {
		add(ss.CanonAwaitingPickup)
	}
	add(ss.CanonPickedUp)
	for range rapid.IntRange(0, 3).Draw(t, "số lần thất bại") {
		for range rapid.IntRange(0, 2).Draw(t, "tin di chuyển") {
			add(movingEv.Draw(t, "di chuyển"))
		}
		add(ss.CanonDeliveryFailed)
		if rapid.Bool().Draw(t, "giao lại") {
			add(movingEv.Draw(t, "giao lại bằng"))
		}
	}
	switch rapid.IntRange(0, 3).Draw(t, "kết thúc") {
	case 0:
		add(ss.CanonDelivered)
	case 1:
		add(ss.CanonReturning)
	case 2:
		add(ss.CanonReturning)
		add(ss.CanonReturnedToShop)
	default: // dừng giữa đường
	}
	return evs
}

// Hành trình hợp lệ, sự kiện bị nhân bản và đến lộn thứ tự: áp lần lượt bằng Decide → trạng thái cuối là trạng thái
// của sự kiện mới nhất; picked_up đúng 1 lần; delivered đúng 1 lần khi kết thúc ở thành công, 0 khi không; không bao
// giờ tới returned; không anomaly.
func TestDecide_ShuffledJourney(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		evs := journey(t)
		arrivals := slices.Clone(evs)
		for range rapid.IntRange(0, 4).Draw(t, "số bản trùng") {
			arrivals = append(arrivals, rapid.SampledFrom(evs).Draw(t, "bản trùng"))
		}
		cur, count := replay(t, rapid.Permutation(arrivals).Draw(t, "thứ tự đến"))
		checkOutcome(t, evs[len(evs)-1].canon, cur, count)
	})
}

// replay áp lần lượt các sự kiện như bên gọi (Task 99): đổi Current khi Apply, dời LastEventAt khi AdvanceLast, nhớ đã
// lấy hàng; trả trạng thái cuối và số lần mỗi tác động.
func replay(t *rapid.T, arrivals []event) (ss.Status, map[ss.Effect]int) {
	cur := ss.AwaitingPickup
	var last *time.Time
	count := map[ss.Effect]int{}
	for _, e := range arrivals {
		d := ss.Decide(ss.Input{
			Current: cur, LastEventAt: last, PickedUp: count[ss.EffPickedUp] > 0, IsCurrent: true,
			Event: e.canon, EventAt: e.at, Source: ss.SrcCarrierAPI,
		})
		if d.Anomaly != "" || (d.Apply && d.Next == ss.Returned) {
			t.Fatalf("%s+%s lúc %s: %+v", cur, e.canon, e.at.Format("15:04"), d)
		}
		if d.Apply {
			cur = d.Next
		}
		if d.AdvanceLast {
			last = &e.at
		}
		for _, ef := range d.Effects {
			count[ef]++
		}
	}
	return cur, count
}

func checkOutcome(t *rapid.T, newest ss.Canonical, cur ss.Status, count map[ss.Effect]int) {
	if want, _ := ss.Target(newest); cur != want {
		t.Fatalf("cuối %s, muốn %s (sự kiện mới nhất %s)", cur, want, newest)
	}
	if count[ss.EffPickedUp] != 1 {
		t.Fatalf("picked_up %d lần", count[ss.EffPickedUp])
	}
	wantDelivered := 0
	if newest == ss.CanonDelivered {
		wantDelivered = 1
	}
	if count[ss.EffDelivered] != wantDelivered {
		t.Fatalf("delivered %d lần, muốn %d", count[ss.EffDelivered], wantDelivered)
	}
	if (newest == ss.CanonReturning || newest == ss.CanonReturnedToShop) && count[ss.EffReturning] != 1 {
		t.Fatalf("returning %d lần, muốn 1", count[ss.EffReturning])
	}
}

// FuzzDecide: đầu vào bất kỳ không làm sập Decide và quyết định luôn nhất quán.
func FuzzDecide(f *testing.F) {
	f.Add("delivered", "in_transit", int64(600), int64(570), true, false, true, "carrier_api")
	f.Add("cancel_requested", "returning", int64(-1), int64(0), false, true, true, "staff")
	f.Add("", "", int64(0), int64(0), false, false, false, "")
	f.Fuzz(func(t *testing.T, cur, ev string, lastMin, atMin int64, hasLast, picked, current bool, src string) {
		base := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
		i := ss.Input{
			Current: ss.Status(cur), PickedUp: picked, IsCurrent: current, Event: ss.Canonical(ev),
			EventAt: base.Add(time.Duration(atMin%100000) * time.Minute), Source: ss.Source(src),
		}
		if hasLast {
			l := base.Add(time.Duration(lastMin%100000) * time.Minute)
			i.LastEventAt = &l
		}
		d := ss.Decide(i)
		if msg := shapeError(d); msg != "" {
			t.Fatalf("%s: %+v", msg, d)
		}
		if msg := ruleError(i, d); msg != "" {
			t.Fatalf("%s: %+v → %+v", msg, i, d)
		}
	})
}

// shapeError kiểm hình dạng của quyết định (không phụ thuộc đầu vào).
func shapeError(d ss.Decision) string {
	switch {
	case d.Apply == (d.Ignored != ""):
		return "Apply và Ignored không khớp"
	case !d.Apply && (d.Next != "" || len(d.Effects) > 0):
		return "không áp mà có Next/Effects"
	case d.Apply && (!slices.Contains(ss.Statuses(), d.Next) || d.Next == ss.Returned):
		return "Next không hợp lệ"
	case d.Apply && !d.AdvanceLast:
		return "áp mà không dời mốc"
	case !slices.Contains([]string{
		"", ss.ReasonOlder, ss.ReasonDuplicateState, ss.ReasonNotCurrent,
		ss.ReasonInvalidEdge, ss.ReasonUnmapped, ss.ReasonReview,
	}, d.Ignored):
		return "Ignored lạ"
	}
	return ""
}

// ruleError kiểm các luật phải luôn đúng giữa đầu vào và quyết định.
func ruleError(i ss.Input, d ss.Decision) string {
	older := i.LastEventAt != nil && i.EventAt.Before(*i.LastEventAt)
	switch {
	case d.Apply && (i.Current == ss.Cancelled || i.Current == ss.Returned):
		return "rời trạng thái cuối"
	case d.Apply && older:
		return "áp tin cũ"
	case d.Apply && !i.IsCurrent:
		return "áp vận đơn không hiện hành"
	case d.AdvanceLast && older:
		return "dời mốc lùi"
	case i.PickedUp && slices.Contains(d.Effects, ss.EffPickedUp):
		return "phát lại picked_up"
	}
	return ""
}
