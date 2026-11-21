package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"
)

// Lịch sử thao tác của đơn dùng một đồng hồ (Task 436): đồng hồ của máy chủ ứng dụng (Deps.Now) lệch đồng hồ của
// PostgreSQL ±5 phút, tạo → sửa → hủy vẫn ra đúng thứ tự mới nhất trước, và mọi mốc (cả mục sửa ghi qua audit) theo
// đồng hồ của máy chủ ứng dụng, không theo now() của database.
func TestOrderTimeline_ClockSkew(t *testing.T) {
	for _, skew := range []time.Duration{5 * time.Minute, -5 * time.Minute} {
		t.Run(skew.String(), func(t *testing.T) { checkTimelineClockSkew(t, skew) })
	}
}

// checkTimelineClockSkew: một lượt của TestOrderTimeline_ClockSkew với đồng hồ máy chủ = giờ thật + skew.
func checkTimelineClockSkew(t *testing.T, skew time.Duration) {
	e := newOrderReadEnv(t)
	e.grantCanceller()
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Now = func() time.Time { return time.Now().Add(skew) }
	e.srv = NewServer(d)
	start := time.Now().Add(skew)

	v := e.variant(e.shop.Seller, 250000, 5)
	o := e.mustCreate("sk1", m2Body("new", fullAddr, 0, m2Line(v, 1, 0)))
	id := fmt.Sprint(o["id"])
	rec := e.patchOrder(readSale, "sk2", id, fmt.Sprintf(`{"version":%v,"shippingFee":30000}`, o["version"]))
	if rec.Code != http.StatusOK {
		t.Fatalf("sửa phí giao: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.cancelOrder(orderCanceller, "sk3", id, decodeJSON(t, rec)["version"], "customer_changed_mind", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("hủy: %d %s", rec.Code, rec.Body.String())
	}
	end := time.Now().Add(skew)

	rec = e.get(orderViewer, "/api/v1/orders/"+id+"/timeline")
	items, _ := decodeJSON(t, rec)["items"].([]any)
	var got []string
	for _, it := range items {
		m := it.(map[string]any)
		sum, _ := m["summary"].(map[string]any)
		got = append(got, fmt.Sprint(m["kind"], "/", sum["to"], sum["shippingFee"]))
	}
	if want := []string{"status/cancelled<nil>", "edit/<nil>changed", "status/new<nil>"}; rec.Code != http.StatusOK ||
		!slices.Equal(got, want) {
		t.Fatalf("timeline mới nhất trước: %d %v, muốn %v", rec.Code, got, want)
	}
	for i, it := range items {
		at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(it.(map[string]any)["at"]))
		if err != nil || at.Before(start.Add(-time.Second)) || at.After(end.Add(time.Second)) {
			t.Errorf("mục %d lúc %v (%v), muốn theo đồng hồ của máy chủ trong [%v, %v]", i, at, err, start, end)
		}
	}
}
