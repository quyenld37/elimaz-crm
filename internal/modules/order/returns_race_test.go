package order_test

import (
	"errors"
	"sync"
	"testing"

	"elimaz.com.vn/crm/internal/modules/order"
)

// Đồng thời (TD07, WAT28): hai phiếu trả cho đơn vị cuối của một dòng cùng lúc → đúng một thành công, một
// ErrReturnQtyExceeds; lặp 20 lần, mỗi lần một đơn mới.
func TestCreateReturn_LastUnitRace(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(99999, 100)
	for i := range 20 {
		o := e.deliveredOrder(ln(v, 1, 0))
		in := custReturn(rl(o.Items[0].ID, 1))
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for g := range errs {
			wg.Go(func() { _, errs[g] = e.createReturn(o.ID, in) })
		}
		wg.Wait()
		ok, exceeded := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, order.ErrReturnQtyExceeds):
				exceeded++
			default:
				t.Fatalf("lần %d: lỗi lạ %v", i, err)
			}
		}
		if ok != 1 || exceeded != 1 {
			t.Fatalf("lần %d: %d thành công, %d vượt số (%v)", i, ok, exceeded, errs)
		}
		if n := e.count(`SELECT coalesce(sum(ri.quantity), 0)::int FROM ord.return_items ri
			JOIN ord.returns r ON r.id = ri.return_id WHERE r.order_id = $1`, o.ID); n != 1 {
			t.Fatalf("lần %d: tổng số trả %d", i, n)
		}
	}
}

// Hai đường song song chạm cùng đơn thay thế (ADR-46): hủy phiếu đổi (khóa đơn gốc → đơn thay thế → phiếu → lượt mã →
// tồn) và hủy thẳng đơn thay thế (khóa đơn thay thế → tồn) → không deadlock; đơn thay thế và phiếu đều cancelled (hủy
// đơn thắng trước thì hủy phiếu chỉ hủy phiếu; hủy phiếu thắng trước thì hủy đơn gặp version mới), hàng nhả đúng một
// lần.
func TestCancelReturn_ExchangeRaceNoDeadlock(t *testing.T) {
	e := newM2Env(t)
	a := e.variant(250000, 100)
	al := e.variant(250000, 100)
	for i := range 10 {
		o := e.deliveredOrder(ln(a, 1, 0))
		repl := e.m2Input(0, ln(al, 1, 0))
		r := e.mustReturn(o.ID, order.CreateReturnInput{
			Kind: order.ReturnExchange, Lines: []order.ReturnLineInput{rl(o.Items[0].ID, 1)}, Replacement: &repl,
		})
		n := e.get(*r.ReplacementOrderID)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, errs[0] = e.cancelReturn(r) })
		wg.Go(func() { _, errs[1] = e.cancel(n) })
		wg.Wait()
		for _, err := range errs {
			if err != nil && !errors.Is(err, order.ErrVersionConflict) && !errors.Is(err, order.ErrInvalidTransition) {
				t.Fatalf("lần %d: lỗi lạ %v", i, err)
			}
		}
		if g := e.get(n.ID); g.Status != order.StatusCancelled || e.getReturn(r.ID).Status != order.ReturnCancelled {
			t.Fatalf("lần %d: đơn thay thế %s, phiếu %s (%v)", i, g.Status, e.getReturn(r.ID).Status, errs)
		}
	}
	if b := e.balance(al); b.Held != 0 || b.Available != 100 {
		t.Errorf("A-L sau 10 lần: %+v", b)
	}
}
