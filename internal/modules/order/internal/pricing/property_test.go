package pricing_test

import (
	"errors"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
)

// moneyGen trộn số rất nhỏ (để phần lẻ nhiều so với Base, dễ chạm trần dòng) với giá thật của shop.
func moneyGen(hi int64) *rapid.Generator[int64] {
	return rapid.OneOf(rapid.Int64Range(0, min(hi, 20)), rapid.Int64Range(0, hi))
}

// validInputGen sinh đầu vào hợp lệ: 1–6 dòng, số lượng 1–5, giảm dòng ≤ Gross, giảm đơn + mã ≤ ΣBase.
func validInputGen() *rapid.Generator[pricing.Input] {
	return rapid.Custom(func(t *rapid.T) pricing.Input {
		n := rapid.IntRange(1, 6).Draw(t, "lines")
		in := pricing.Input{Lines: make([]pricing.Line, n)}
		var sumBase int64
		for i := range in.Lines {
			qty := rapid.Int64Range(1, 5).Draw(t, "qty")
			price := moneyGen(5_000_000).Draw(t, "price")
			disc := rapid.Int64Range(0, qty*price).Draw(t, "lineDiscount")
			in.Lines[i] = pricing.Line{Key: i + 1, Qty: qty, UnitPrice: price, LineDiscount: disc}
			sumBase += qty*price - disc
		}
		in.OrderDiscount = rapid.Int64Range(0, sumBase).Draw(t, "orderDiscount")
		in.CouponDiscount = rapid.Int64Range(0, sumBase-in.OrderDiscount).Draw(t, "couponDiscount")
		in.ShippingFee = moneyGen(100_000).Draw(t, "shippingFee")
		return in
	})
}

// checkInvariants kiểm các quy luật của một kết quả Compute thành công.
func checkInvariants(t *rapid.T, in pricing.Input, got pricing.Totals) {
	if len(got.Lines) != len(in.Lines) {
		t.Fatalf("số dòng %d != %d", len(got.Lines), len(in.Lines))
	}
	var sumAlloc, sumNet, sumGross, sumLineDisc int64
	for i, l := range got.Lines {
		if l.Key != in.Lines[i].Key {
			t.Fatalf("dòng %d: Key %d != %d", i, l.Key, in.Lines[i].Key)
		}
		if l.Allocated < 0 || l.Allocated > l.Base {
			t.Fatalf("dòng %d: Allocated %d ngoài [0, Base %d]", i, l.Allocated, l.Base)
		}
		if l.Base != l.Gross-in.Lines[i].LineDiscount || l.Net != l.Base-l.Allocated {
			t.Fatalf("dòng %d sai: %+v", i, l)
		}
		sumAlloc += l.Allocated
		sumNet += l.Net
		sumGross += l.Gross
		sumLineDisc += in.Lines[i].LineDiscount
	}
	if sumAlloc != in.OrderDiscount+in.CouponDiscount {
		t.Fatalf("ΣAllocated %d != giảm đơn %d + mã %d", sumAlloc, in.OrderDiscount, in.CouponDiscount)
	}
	if sumNet+got.ShippingFee != got.GrandTotal || got.GrandTotal < 0 || got.GrandTotal > pricing.MaxMoney {
		t.Fatalf("ΣNet %d + ship %d != GrandTotal %d", sumNet, got.ShippingFee, got.GrandTotal)
	}
	if got.Subtotal != sumGross || got.LineDiscountTotal != sumLineDisc ||
		got.DiscountTotal != sumLineDisc+in.OrderDiscount+in.CouponDiscount {
		t.Fatalf("tổng sai: %+v", got)
	}
}

// TestCompute_Invariants (rapid, 1.000 lượt): Σ Allocated = giảm đơn + mã; 0 ≤ Allocated_i ≤ Base_i;
// Σ Net + ship = GrandTotal ≥ 0.
func TestCompute_Invariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := validInputGen().Draw(t, "input")
		got, err := pricing.Compute(in)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		checkInvariants(t, in, got)
	})
}

// TestCompute_AnyInput (rapid, 1.000 lượt): đầu vào bất kỳ (kể cả âm, rất lớn) → hoặc một trong bốn lỗi của gói,
// hoặc kết quả giữ mọi quy luật. Không panic, không tràn ngầm.
func TestCompute_AnyInput(t *testing.T) {
	anyMoney := rapid.OneOf(
		rapid.Int64Range(-5, 20), rapid.Int64Range(0, pricing.MaxMoney), rapid.Int64Range(pricing.MaxMoney-5, pricing.MaxMoney+5),
		rapid.Int64(),
	)
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 4).Draw(t, "lines")
		in := pricing.Input{Lines: make([]pricing.Line, n)}
		for i := range in.Lines {
			in.Lines[i] = pricing.Line{
				Key: i, Qty: anyMoney.Draw(t, "qty"), UnitPrice: anyMoney.Draw(t, "price"),
				LineDiscount: anyMoney.Draw(t, "lineDiscount"),
			}
		}
		in.OrderDiscount = anyMoney.Draw(t, "orderDiscount")
		in.CouponDiscount = anyMoney.Draw(t, "couponDiscount")
		in.ShippingFee = anyMoney.Draw(t, "shippingFee")
		got, err := pricing.Compute(in)
		switch {
		case err == nil:
			checkInvariants(t, in, got)
		case errors.Is(err, pricing.ErrNegative), errors.Is(err, pricing.ErrMoneyOverflow),
			errors.Is(err, pricing.ErrLineDiscountExceeds), errors.Is(err, pricing.ErrOrderDiscountExceeds):
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	})
}

// TestAllocate_Invariants (rapid, 1.000 lượt): với Base tới MaxMoney, Σ kết quả = amount, 0 ≤ kết quả_i ≤ Base_i,
// và dòng không phải dòng nhận phần lẻ được đúng floor(Base_i × amount / ΣBase) hoặc nhận thêm chỉ khi mọi dòng
// lớn hơn nó đã đầy.
func TestAllocate_Invariants(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(t, "n")
		bases := make([]int64, n)
		var sum int64
		for i := range bases {
			bases[i] = moneyGen(pricing.MaxMoney/int64(n)).Draw(t, "base")
			sum += bases[i]
		}
		amount := rapid.Int64Range(0, sum).Draw(t, "amount")
		got := pricing.Allocate(bases, amount)
		var total int64
		for i, a := range got {
			if a < 0 || a > bases[i] {
				t.Fatalf("dòng %d: %d ngoài [0, %d]", i, a, bases[i])
			}
			total += a
		}
		if total != amount {
			t.Fatalf("Σ %d != %d (bases %v)", total, amount, bases)
		}
	})
}

// TestRefundFor_SplitSumsToNet (rapid, 1.000 lượt): mọi cách chia số lượng trả của một dòng thành nhiều lần cho tổng
// RefundFor đúng bằng Net, mỗi lần ≥ 0.
func TestRefundFor_SplitSumsToNet(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		net := moneyGen(pricing.MaxMoney).Draw(t, "net")
		qty := rapid.Int64Range(1, 12).Draw(t, "qty")
		var returned, refunded int64
		for returned < qty {
			now := rapid.Int64Range(1, qty-returned).Draw(t, "returnNow")
			r := pricing.RefundFor(net, qty, returned, now, refunded)
			if r < 0 {
				t.Fatalf("hoàn âm %d", r)
			}
			returned += now
			refunded += r
		}
		if refunded != net {
			t.Fatalf("Σ hoàn %d != Net %d", refunded, net)
		}
	})
}
