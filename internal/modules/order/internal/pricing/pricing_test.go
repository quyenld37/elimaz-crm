package pricing_test

import (
	"errors"
	"flag"
	"math"
	"os"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain đặt số lượt rapid 1.000 (kế hoạch Task M2-07), trừ khi người chạy đã đặt -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(1000)
	os.Exit(m.Run())
}

func mustCompute(t *testing.T, in pricing.Input) pricing.Totals {
	t.Helper()
	got, err := pricing.Compute(in)
	if err != nil {
		t.Fatalf("Compute(%+v): %v", in, err)
	}
	return got
}

func allocated(t pricing.Totals) []int64 {
	out := make([]int64, len(t.Lines))
	for i, l := range t.Lines {
		out[i] = l.Allocated
	}
	return out
}

func TestMaxMoney_SameAsCatalog(t *testing.T) {
	if pricing.MaxMoney != catalog.MaxMoney {
		t.Fatalf("pricing.MaxMoney %d != catalog.MaxMoney %d", pricing.MaxMoney, catalog.MaxMoney)
	}
}

// DH01: V1 ×2 giá 250.000 giảm dòng 20.000; V2 ×1 giá 180.000; ship 30.000.
func TestCompute_DH01(t *testing.T) {
	got := mustCompute(t, pricing.Input{
		Lines: []pricing.Line{
			{Key: 1, Qty: 2, UnitPrice: 250000, LineDiscount: 20000},
			{Key: 2, Qty: 1, UnitPrice: 180000},
		},
		ShippingFee: 30000,
	})
	want := pricing.Totals{
		Subtotal: 680000, LineDiscountTotal: 20000, DiscountTotal: 20000, ShippingFee: 30000, GrandTotal: 690000,
		Lines: []pricing.LineTotals{
			{Key: 1, Gross: 500000, Base: 480000, Net: 480000},
			{Key: 2, Gross: 180000, Base: 180000, Net: 180000},
		},
	}
	if !equalTotals(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// DH05: giảm cấp đơn 100.001 chia về ba dòng không lệch một đồng; phần lẻ vào dòng lớn nhất.
func TestCompute_DH05(t *testing.T) {
	got := mustCompute(t, pricing.Input{
		Lines: []pricing.Line{
			{Key: 1, Qty: 1, UnitPrice: 500000},
			{Key: 2, Qty: 1, UnitPrice: 180000},
			{Key: 3, Qty: 1, UnitPrice: 320000},
		},
		OrderDiscount: 100001,
		ShippingFee:   25000,
	})
	if a := allocated(got); !slices.Equal(a, []int64{50001, 18000, 32000}) {
		t.Fatalf("Allocated = %v, muốn [50001 18000 32000]", a)
	}
	var sumNet int64
	for _, l := range got.Lines {
		sumNet += l.Net
	}
	if sumNet+got.ShippingFee != got.GrandTotal || got.GrandTotal != 1000000-100001+25000 {
		t.Fatalf("ΣNet %d + ship %d, GrandTotal %d", sumNet, got.ShippingFee, got.GrandTotal)
	}
	if got.DiscountTotal != 100001 || got.OrderDiscount != 100001 {
		t.Fatalf("DiscountTotal %d, OrderDiscount %d", got.DiscountTotal, got.OrderDiscount)
	}
}

// Giảm cấp đơn và mã giảm giá cùng chia về dòng; DiscountTotal gồm cả ba loại.
func TestCompute_OrderAndCouponDiscount(t *testing.T) {
	got := mustCompute(t, pricing.Input{
		Lines: []pricing.Line{
			{Key: 7, Qty: 3, UnitPrice: 100000, LineDiscount: 10000},
			{Key: 9, Qty: 1, UnitPrice: 210000},
		},
		OrderDiscount: 50000, CouponDiscount: 50000, ShippingFee: 0,
	})
	// Base 290.000 / 210.000, giảm 100.000: 58.000 / 42.000 chia hết.
	if a := allocated(got); !slices.Equal(a, []int64{58000, 42000}) {
		t.Fatalf("Allocated = %v", a)
	}
	if got.DiscountTotal != 110000 || got.GrandTotal != 400000 || got.CouponDiscount != 50000 {
		t.Fatalf("%+v", got)
	}
	if got.Lines[0].Key != 7 || got.Lines[1].Key != 9 {
		t.Fatalf("Key không giữ thứ tự: %+v", got.Lines)
	}
}

func TestAllocate_TieGoesToFirst(t *testing.T) {
	if got := pricing.Allocate([]int64{300000, 300000}, 1); !slices.Equal(got, []int64{1, 0}) {
		t.Fatalf("got %v, muốn [1 0]", got)
	}
}

func TestAllocate_LargestBaseNotFirst(t *testing.T) {
	if got := pricing.Allocate([]int64{100, 300, 300}, 1); !slices.Equal(got, []int64{0, 1, 0}) {
		t.Fatalf("got %v, muốn [0 1 0]", got)
	}
}

// Phần lẻ lớn hơn chỗ còn của dòng lớn nhất: dòng lớn nhất nhận tới Base, phần còn lại sang dòng lớn kế tiếp
// (quyết định Q-T64-1), để luôn 0 ≤ Allocated ≤ Base.
func TestAllocate_RemainderCappedAtBase(t *testing.T) {
	if got := pricing.Allocate([]int64{1, 1, 1}, 2); !slices.Equal(got, []int64{1, 1, 0}) {
		t.Fatalf("got %v, muốn [1 1 0]", got)
	}
	if got := pricing.Allocate([]int64{1, 2, 1, 2}, 5); !slices.Equal(got, []int64{1, 2, 0, 2}) {
		t.Fatalf("got %v, muốn [1 2 0 2]", got)
	}
}

func TestAllocate_ZeroBase(t *testing.T) {
	if got := pricing.Allocate([]int64{0, 0, 0}, 0); !slices.Equal(got, []int64{0, 0, 0}) {
		t.Fatalf("got %v", got)
	}
	if got := pricing.Allocate(nil, 0); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

// Tích Base × giảm vượt int64 (≈ 2^105) vẫn chia đúng.
func TestAllocate_HugeProduct(t *testing.T) {
	half := pricing.MaxMoney / 2 // 2^52 − 1
	got := pricing.Allocate([]int64{half, half + 1}, pricing.MaxMoney-1)
	if got[0]+got[1] != pricing.MaxMoney-1 || got[0] > half || got[1] > half+1 {
		t.Fatalf("got %v", got)
	}
}

func TestCompute_AllZero(t *testing.T) {
	got := mustCompute(t, pricing.Input{Lines: []pricing.Line{{Key: 1, Qty: 2}, {Key: 2, Qty: 1}}})
	if a := allocated(got); !slices.Equal(a, []int64{0, 0}) || got.GrandTotal != 0 {
		t.Fatalf("%+v", got)
	}
	if _, err := pricing.Compute(pricing.Input{
		Lines: []pricing.Line{{Key: 1, Qty: 2}}, OrderDiscount: 1,
	}); !errors.Is(err, pricing.ErrOrderDiscountExceeds) {
		t.Fatalf("giảm 1 trên Σbase 0: err = %v", err)
	}
}

func TestCompute_Errors(t *testing.T) {
	line := pricing.Line{Key: 1, Qty: 1, UnitPrice: 100000}
	cases := []struct {
		name string
		in   pricing.Input
		want error
	}{
		{"tích tràn", pricing.Input{Lines: []pricing.Line{{Key: 1, Qty: 3, UnitPrice: 1 << 52}}}, pricing.ErrMoneyOverflow},
		{"tích vượt int64", pricing.Input{Lines: []pricing.Line{{Key: 1, Qty: 1 << 40, UnitPrice: 1 << 40}}}, pricing.ErrMoneyOverflow},
		{"tạm tính tràn", pricing.Input{Lines: []pricing.Line{
			{Key: 1, Qty: 1, UnitPrice: pricing.MaxMoney}, {Key: 2, Qty: 1, UnitPrice: 1},
		}}, pricing.ErrMoneyOverflow},
		{"đơn giá vượt", pricing.Input{Lines: []pricing.Line{{Key: 1, Qty: 0, UnitPrice: pricing.MaxMoney + 1}}}, pricing.ErrMoneyOverflow},
		{"ship vượt", pricing.Input{Lines: []pricing.Line{line}, ShippingFee: pricing.MaxMoney + 1}, pricing.ErrMoneyOverflow},
		{"tổng đơn tràn vì ship", pricing.Input{
			Lines: []pricing.Line{{Key: 1, Qty: 1, UnitPrice: pricing.MaxMoney}}, ShippingFee: 1,
		}, pricing.ErrMoneyOverflow},
		{"giảm đơn + mã tràn", pricing.Input{
			Lines: []pricing.Line{line}, OrderDiscount: pricing.MaxMoney, CouponDiscount: pricing.MaxMoney,
		}, pricing.ErrMoneyOverflow},
		{"giảm dòng > Gross", pricing.Input{
			Lines: []pricing.Line{{Key: 1, Qty: 2, UnitPrice: 1000, LineDiscount: 2001}},
		}, pricing.ErrLineDiscountExceeds},
		{"giảm đơn + mã > ΣBase", pricing.Input{
			Lines: []pricing.Line{{Key: 1, Qty: 1, UnitPrice: 1000, LineDiscount: 100}}, OrderDiscount: 800, CouponDiscount: 101,
		}, pricing.ErrOrderDiscountExceeds},
		{"số lượng âm", pricing.Input{Lines: []pricing.Line{{Key: 1, Qty: -1, UnitPrice: 1}}}, pricing.ErrNegative},
		{"đơn giá âm", pricing.Input{Lines: []pricing.Line{{Key: 1, Qty: 1, UnitPrice: -1}}}, pricing.ErrNegative},
		{"giảm dòng âm", pricing.Input{Lines: []pricing.Line{{Key: 1, Qty: 1, UnitPrice: 5, LineDiscount: -1}}}, pricing.ErrNegative},
		{"giảm đơn âm", pricing.Input{Lines: []pricing.Line{line}, OrderDiscount: -1}, pricing.ErrNegative},
		{"mã âm", pricing.Input{Lines: []pricing.Line{line}, CouponDiscount: -1}, pricing.ErrNegative},
		{"ship âm", pricing.Input{Lines: []pricing.Line{line}, ShippingFee: -1}, pricing.ErrNegative},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pricing.Compute(c.in)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, muốn %v", err, c.want)
			}
			if got.Lines != nil || got.GrandTotal != 0 {
				t.Fatalf("lỗi mà vẫn trả tổng: %+v", got)
			}
		})
	}
}

func TestManualDiscountBP(t *testing.T) {
	cases := []struct {
		t    pricing.Totals
		want int64
	}{
		{pricing.Totals{Subtotal: 1000000, LineDiscountTotal: 100000}, 1000},
		{pricing.Totals{Subtotal: 1000000, LineDiscountTotal: 60000, OrderDiscount: 40001}, 1001},
		// Mã giảm giá không tính vào giảm tay (Q3).
		{pricing.Totals{Subtotal: 1000000, OrderDiscount: 100000, CouponDiscount: 500000}, 1000},
		{pricing.Totals{Subtotal: 0}, 0},
		{pricing.Totals{Subtotal: 3, OrderDiscount: 1}, 3334},
		{pricing.Totals{Subtotal: pricing.MaxMoney, OrderDiscount: pricing.MaxMoney}, 10000},
		{pricing.Totals{Subtotal: pricing.MaxMoney, OrderDiscount: 1}, 1},
		// Totals không thể có từ Compute → luôn vượt ngưỡng duyệt.
		{pricing.Totals{Subtotal: 0, OrderDiscount: 1}, math.MaxInt64},
		{pricing.Totals{Subtotal: 1000, OrderDiscount: -1}, math.MaxInt64},
		{pricing.Totals{Subtotal: 1000, LineDiscountTotal: -1, OrderDiscount: 5}, math.MaxInt64},
		{pricing.Totals{Subtotal: -1}, math.MaxInt64},
		{pricing.Totals{Subtotal: 1, OrderDiscount: math.MaxInt64}, math.MaxInt64},
		{pricing.Totals{Subtotal: 9999, LineDiscountTotal: math.MaxInt64, OrderDiscount: math.MaxInt64}, math.MaxInt64},
		{pricing.Totals{Subtotal: 10001, LineDiscountTotal: math.MaxInt64, OrderDiscount: math.MaxInt64}, math.MaxInt64},
	}
	for _, c := range cases {
		if got := pricing.ManualDiscountBP(c.t); got != c.want {
			t.Errorf("ManualDiscountBP(%+v) = %d, muốn %d", c.t, got, c.want)
		}
	}
}

func equalTotals(a, b pricing.Totals) bool {
	return a.Subtotal == b.Subtotal && a.LineDiscountTotal == b.LineDiscountTotal &&
		a.OrderDiscount == b.OrderDiscount && a.CouponDiscount == b.CouponDiscount &&
		a.DiscountTotal == b.DiscountTotal && a.ShippingFee == b.ShippingFee && a.GrandTotal == b.GrandTotal &&
		slices.Equal(a.Lines, b.Lines)
}
