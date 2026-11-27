package reporting

import (
	"encoding/json"
	"testing"
)

// Trung bình làm tròn nửa lên tới đồng (số nguyên, không số thực); 0 đơn → nil.
func TestDashboard_AvgRoundHalfUp(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		goods, orders int64
		want          *int64
	}{
		{1_910_000, 7, new(int64(272_857))}, // 272.857,14
		{5, 2, new(int64(3))},               // 2,5 → 3
		{7, 3, new(int64(2))},               // 2,33 → 2
		{8, 3, new(int64(3))},               // 2,67 → 3
		{0, 4, new(int64(0))},
		{100, 0, nil},
	} {
		got := avgGoods(Amounts{Orders: c.orders, GoodsAmount: c.goods})
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("avgGoods(%d/%d) = %v, mong %v", c.goods, c.orders, got, c.want)
		}
	}
}

// JSON của kết quả (lần chạy nền dạng json) đúng hình dạng SalesDashboardReport của hợp đồng: khóa camelCase, kỳ
// {from, to}, avgGoodsAmount null khi không có đơn, byChannel và byDay là mảng (không null), tiền là số nguyên.
func TestDashboard_JSONShape(t *testing.T) {
	t.Parallel()
	p, err := ParsePeriod("2026-11-17", "2026-11-17", MaxDays)
	if err != nil {
		t.Fatal(err)
	}
	r := newDashboardReport(p)
	r.Placed = Amounts{Orders: 1, GoodsAmount: 150_000, ShippingFeeCharged: 30_000}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	keys := []string{
		"status", "period", "generatedAt", "placed", "active", "cancelled", "delivered", "returned",
		"avgGoodsAmount", "customerReturns", "pending", "byChannel", "byDay",
	}
	if len(m) != len(keys) {
		t.Errorf("JSON có %d khóa, mong %d: %s", len(m), len(keys), b)
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("thiếu khóa %s: %s", k, b)
		}
	}
	want := map[string]string{
		"status": `"ready"`, "period": `{"from":"2026-11-17","to":"2026-11-17"}`, "avgGoodsAmount": "null",
		"byChannel": "[]", "byDay": "[]", "placed": `{"orders":1,"goodsAmount":150000,"shippingFeeCharged":30000}`,
		"customerReturns": `{"items":0,"refundAmount":0}`, "pending": `{"confirm":0,"ship":0,"cancelRequested":0}`,
	}
	for k, v := range want {
		if string(m[k]) != v {
			t.Errorf("%s = %s, mong %s", k, m[k], v)
		}
	}
}
