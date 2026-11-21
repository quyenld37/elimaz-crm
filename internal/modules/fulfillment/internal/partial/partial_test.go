package partial_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/partial"
)

// Đơn D1 của kế hoạch: vA ×1, vB ×2; COD 630.000.
func d1() (a, b uuid.UUID, items []partial.Item) {
	a, b = uuid.New(), uuid.New()
	return a, b, []partial.Item{{ID: a, Qty: 1}, {ID: b, Qty: 2}}
}

func TestDecide(t *testing.T) {
	a, b, items := d1()
	shipping := partial.Shipment{Status: "shipping", COD: 630000}
	td09 := []partial.Line{{OrderItemID: a, Qty: 1}, {OrderItemID: b, Qty: 1}}
	cases := []struct {
		name      string
		sh        partial.Shipment
		req       []partial.Line
		collected int64
		want      partial.Refusal
	}{
		{"TD09 shipping", shipping, td09, 430000, partial.OK},
		{"delivery_failed", partial.Shipment{Status: "delivery_failed", COD: 630000}, td09, 0, partial.OK},
		{"thu đủ COD", shipping, td09, 630000, partial.OK},
		{
			"awaiting_pickup có partial_reported",
			partial.Shipment{Status: "awaiting_pickup", PartialReported: true, COD: 1},
			td09, 0, partial.OK,
		},
		{"awaiting_pickup không anomaly", partial.Shipment{Status: "awaiting_pickup", COD: 1}, td09, 0, partial.RefuseStatus},
		{"returning có anomaly", partial.Shipment{Status: "returning", PartialReported: true}, td09, 0, partial.RefuseStatus},
		{"delivered", partial.Shipment{Status: "delivered"}, td09, 0, partial.RefuseStatus},
		{
			"cancel_requested",
			partial.Shipment{Status: "cancel_requested", PartialReported: true},
			td09, 0,
			partial.RefuseStatus,
		},
		{
			"giao đủ mọi dòng", shipping,
			[]partial.Line{{OrderItemID: a, Qty: 1}, {OrderItemID: b, Qty: 2}},
			0,
			partial.RefuseQty,
		},
		{"không giao gì", shipping, []partial.Line{{OrderItemID: a}, {OrderItemID: b}}, 0, partial.RefuseQty},
		{"thiếu dòng", shipping, []partial.Line{{OrderItemID: b, Qty: 1}}, 0, partial.RefuseQty},
		{
			"dòng lạ", shipping,
			[]partial.Line{{OrderItemID: a, Qty: 1}, {OrderItemID: uuid.New(), Qty: 1}},
			0,
			partial.RefuseQty,
		},
		{
			"dòng trùng", shipping,
			[]partial.Line{{OrderItemID: a, Qty: 1}, {OrderItemID: a}, {OrderItemID: b}},
			0,
			partial.RefuseQty,
		},
		{"giao hơn số mua", shipping, []partial.Line{{OrderItemID: a}, {OrderItemID: b, Qty: 3}}, 0, partial.RefuseQty},
		{"số âm", shipping, []partial.Line{{OrderItemID: a, Qty: -1}, {OrderItemID: b, Qty: 2}}, 0, partial.RefuseQty},
		{"thu hơn COD", shipping, td09, 700000, partial.RefuseCOD},
		{"thu âm", shipping, td09, -1, partial.RefuseCOD},
		{"trạng thái xét trước số", partial.Shipment{Status: "delivered"}, nil, -1, partial.RefuseStatus},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, got := partial.Decide(c.sh, items, c.req, c.collected); got != c.want {
				t.Fatalf("Decide = %q, muốn %q", got, c.want)
			}
		})
	}
}

// TD09: giao vA ×1, vB ×1 → phần giao [vA 1, vB 1] theo thứ tự dòng đơn, phần còn lại [vB 1]; dòng giao 0 không vào
// phần giao.
func TestDecide_TD09Plan(t *testing.T) {
	a, b, items := d1()
	p, r := partial.Decide(partial.Shipment{Status: "shipping", COD: 630000}, items,
		[]partial.Line{{OrderItemID: b, Qty: 1}, {OrderItemID: a, Qty: 1}}, 430000)
	if r != partial.OK {
		t.Fatal(r)
	}
	if want := []partial.Line{{OrderItemID: a, Qty: 1}, {OrderItemID: b, Qty: 1}}; !equal(p.Delivered, want) {
		t.Errorf("phần giao = %v, muốn %v", p.Delivered, want)
	}
	if want := []partial.Line{{OrderItemID: b, Qty: 1}}; !equal(p.Rest, want) {
		t.Errorf("phần còn lại = %v, muốn %v", p.Rest, want)
	}
	p, _ = partial.Decide(partial.Shipment{Status: "shipping"}, items,
		[]partial.Line{{OrderItemID: a, Qty: 0}, {OrderItemID: b, Qty: 2}}, 0)
	if want := []partial.Line{{OrderItemID: b, Qty: 2}}; !equal(p.Delivered, want) {
		t.Errorf("phần giao = %v, muốn %v", p.Delivered, want)
	}
	if want := []partial.Line{{OrderItemID: a, Qty: 1}}; !equal(p.Rest, want) {
		t.Errorf("phần còn lại = %v, muốn %v", p.Rest, want)
	}
}

func equal(x, y []partial.Line) bool { return slices.Equal(x, y) }

// Quy luật: đầu vào nhận được thì mỗi dòng đơn có giao + còn lại = số mua (số > 0 mới có mặt), 0 < Σ giao < Σ mua,
// 0 ≤ tiền thu ≤ COD; nhận được đúng khi các điều đó đúng với yêu cầu.
func TestDecide_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(t, "dòng")
		items := make([]partial.Item, n)
		req := make([]partial.Line, n)
		var bought, gave int32
		valid := true
		for i := range items {
			items[i] = partial.Item{ID: uuid.New(), Qty: rapid.Int32Range(1, 5).Draw(t, "mua")}
			q := rapid.Int32Range(-1, 6).Draw(t, "giao")
			req[i] = partial.Line{OrderItemID: items[i].ID, Qty: q}
			bought += items[i].Qty
			gave += q
			valid = valid && q >= 0 && q <= items[i].Qty
		}
		cod := rapid.Int64Range(0, 1_000_000).Draw(t, "cod")
		collected := rapid.Int64Range(-1, 1_100_000).Draw(t, "thu")
		valid = valid && gave > 0 && gave < bought && collected >= 0 && collected <= cod
		p, r := partial.Decide(partial.Shipment{Status: "shipping", COD: cod}, items, req, collected)
		if (r == partial.OK) != valid {
			t.Fatalf("Decide = %q, đầu vào hợp lệ = %v", r, valid)
		}
		if r == partial.OK {
			checkPlan(t, items, p)
		}
	})
}

// checkPlan: mọi dòng của p có số > 0 và mỗi dòng đơn có giao + còn lại = số mua.
func checkPlan(t *rapid.T, items []partial.Item, p partial.Plan) {
	sum := map[uuid.UUID]int32{}
	for _, l := range slices.Concat(p.Delivered, p.Rest) {
		if l.Qty <= 0 {
			t.Fatalf("dòng %s số %d", l.OrderItemID, l.Qty)
		}
		sum[l.OrderItemID] += l.Qty
	}
	for _, it := range items {
		if sum[it.ID] != it.Qty {
			t.Fatalf("dòng %s: giao + còn lại = %d, mua %d", it.ID, sum[it.ID], it.Qty)
		}
	}
}
