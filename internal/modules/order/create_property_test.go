package order_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order"
)

// TestCreate_ConcurrentReservations (rapid): 2–6 người tạo đơn (Writer.Create) đồng thời, tập dòng (có giảm dòng) và
// tồn ngẫu nhiên → với mỗi biến thể, held bằng tổng số lượng các đơn tạo được; không lúc nào held > on_hand − damaged −
// in_transit; đơn bị từ chối chỉ vì thiếu hàng; mọi đơn tạo được đã qua trigger tổng tiền lúc commit và có
// grand_total = Σ net + phí.
func TestCreate_ConcurrentReservations(t *testing.T) {
	e := newM2Env(t)
	rapid.Check(t, func(rt *rapid.T) {
		vs := make([]uuid.UUID, rapid.IntRange(1, 3).Draw(rt, "variants"))
		for i := range vs {
			vs[i] = e.variant(rapid.Int64Range(1, 300000).Draw(rt, "price"), rapid.Int32Range(0, 5).Draw(rt, "stock"))
		}
		inputs := make([]order.CreateInput, rapid.IntRange(2, 6).Draw(rt, "creators"))
		for i := range inputs {
			lines := rapid.SliceOfNDistinct(rapid.SampledFrom(vs), 1, len(vs), func(v uuid.UUID) uuid.UUID { return v }).Draw(rt, "lines")
			in := e.m2Input(rapid.Int64Range(0, 50000).Draw(rt, "fee"))
			for _, v := range lines {
				in.Lines = append(in.Lines, ln(v, rapid.Int32Range(1, 3).Draw(rt, "qty"), rapid.Int64Range(0, 1).Draw(rt, "discount")))
			}
			inputs[i] = in
		}
		res := make([]order.CreateResult, len(inputs))
		errs := make([]error, len(inputs))
		var wg sync.WaitGroup
		for i, in := range inputs {
			wg.Go(func() { res[i], errs[i] = e.createM2(in) })
		}
		wg.Wait()
		if msg := e.checkReservations(vs, inputs, res, errs); msg != "" {
			rt.Fatal(msg)
		}
	})
}

// checkReservations kiểm quy luật của TestCreate_ConcurrentReservations; trả mô tả vi phạm đầu tiên.
func (e *orderEnv) checkReservations(vs []uuid.UUID, inputs []order.CreateInput, res []order.CreateResult,
	errs []error,
) string {
	used := map[uuid.UUID]int32{}
	for i, err := range errs {
		if err != nil {
			if !errors.Is(err, order.ErrOutOfStock) {
				return fmt.Sprintf("lỗi không mong đợi: %v", err)
			}
			continue
		}
		var net int64
		for _, it := range res[i].Order.Items {
			net += it.NetAmount
		}
		if o := res[i].Order; o.GrandTotal != net+o.ShippingFee || o.CODAmount != o.GrandTotal {
			return fmt.Sprintf("đơn %s: tổng %d, Σ net %d, phí %d, COD %d", o.Code, o.GrandTotal, net, o.ShippingFee, o.CODAmount)
		}
		for _, l := range inputs[i].Lines {
			used[l.VariantID] += l.Quantity
		}
	}
	for _, v := range vs {
		b := e.balance(v)
		if b.Held != used[v] || b.Available < 0 {
			return fmt.Sprintf("biến thể %s: đơn giữ %d, held %d, có thể bán %d", v, used[v], b.Held, b.Available)
		}
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_balances
		WHERE held > on_hand - damaged - in_transit`); n != 0 {
		return fmt.Sprintf("%d số dư có held > on_hand − damaged − in_transit", n)
	}
	return ""
}
