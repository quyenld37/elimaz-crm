package order_test

import (
	"errors"
	"sync"
	"testing"

	"elimaz.com.vn/crm/internal/modules/order"
)

// DH08/AT05: V2 còn 1; hai nhân viên tạo đơn V2 × 1 cùng lúc, lặp 20 lần (mỗi lần biến thể mới, tồn 1) → mỗi lần đúng
// một đơn, một OutOfStockError (V2, có thể bán 0); held của V2 luôn 1.
func TestCreate_LastUnitRace(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	for range 20 {
		v := e.variant(180000, 1)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range errs {
			wg.Go(func() { _, errs[i] = e.createM2(e.m2Input(0, ln(v, 1, 0))) })
		}
		wg.Wait()
		ok, short := 0, 0
		for _, err := range errs {
			var oos *order.OutOfStockError
			switch {
			case err == nil:
				ok++
			case errors.As(err, &oos) && oos.VariantID == v && oos.Available == 0:
				short++
			default:
				t.Fatalf("lỗi lạ: %v", err)
			}
		}
		if ok != 1 || short != 1 {
			t.Fatalf("thành công %d, hết hàng %d; mong 1 và 1", ok, short)
		}
		if b := e.balance(v); b.Held != 1 || b.Available != 0 {
			t.Fatalf("số dư: %+v", b)
		}
	}
}
