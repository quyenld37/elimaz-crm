package order_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/order"
)

// editSwap là đầu vào sửa đơn o: bỏ dòng có sẵn ngoài shared, thêm to ×1, đổi số lượng dòng shared thành q (đổi số lượng
// giữ cùng lúc với nhả và giữ mới).
func editSwap(t *testing.T, o order.Order, to, shared uuid.UUID, q int32) order.EditInput {
	t.Helper()
	return order.EditInput{
		Version: o.Version, Lines: editLines(editKeep(itemOf(t, o, shared), q, 0), editAdd(to, 1, 0)),
	}
}

// Không deadlock (kế hoạch M2-20, ADR-46): 200 lần, hai lần sửa song song trên hai đơn — một bên bỏ V1 thêm V3, bên
// kia bỏ V3 thêm V1 (lần sau đảo lại), cả hai cùng đổi số lượng dòng V2 → không lần nào lỗi 40P01,
// pg_stat_database.deadlocks không tăng; tổng giữ mỗi biến thể bằng số lượng trên hai đơn.
func TestEdit_NoDeadlock(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v1, v2, v3 := e.variant(1000, 1000), e.variant(1000, 1000), e.variant(1000, 1000)
	deadlocks := func() int {
		return e.staff.Count(t, `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`)
	}
	before := deadlocks()
	a := e.mustCreateM2(e.m2Input(0, ln(v2, 1, 0), ln(v1, 1, 0))).Order
	b := e.mustCreateM2(e.m2Input(0, ln(v2, 1, 0), ln(v3, 1, 0))).Order
	aHas, bHas := v1, v3
	for i := range 200 {
		q := int32(1 + i%2)
		inA, inB := editSwap(t, a, bHas, v2, q), editSwap(t, b, aHas, v2, q)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var resA, resB order.EditResult
		var errA, errB error
		wg.Go(func() { <-start; resA, errA = e.editOrder(a.ID, inA, e.actor) })
		wg.Go(func() { <-start; resB, errB = e.editOrder(b.ID, inB, e.actor) })
		close(start)
		wg.Wait()
		if err := errors.Join(errA, errB); err != nil {
			t.Fatalf("lần %d (40P01 là deadlock): %v", i, err)
		}
		a, b, aHas, bHas = resA.Order, resB.Order, bHas, aHas
	}
	last := int32(2) // lần cuối i = 199: q = 2
	if h := e.balance(v2).Held; h != 2*last {
		t.Errorf("giữ V2 = %d, mong %d", h, 2*last)
	}
	if h1, h3 := e.balance(v1).Held, e.balance(v3).Held; h1 != 1 || h3 != 1 {
		t.Errorf("giữ V1 = %d, V3 = %d, mong 1, 1", h1, h3)
	}
	time.Sleep(1500 * time.Millisecond) // số liệu thống kê của backend đẩy lên tối đa mỗi giây
	if after := deadlocks(); after != before {
		t.Errorf("pg_stat_database.deadlocks %d → %d", before, after)
	}
}

// Sửa đơn đổi mã khóa hai mã theo cùng thứ tự với lịch hủy đơn nháp và hủy đơn (ADR-46 nhóm 10: theo id tăng dần):
// 200 lần, song song: đơn X đổi mã A → B, đơn Y đổi B → A (lần sau đảo lại), ExpireDrafts hai đơn nháp quá hạn mang A
// và B, hủy một đơn mang A → không lần nào lỗi, pg_stat_database.deadlocks không tăng; cuối cùng mỗi mã còn đúng một
// lượt (của X hoặc Y), mọi đơn nháp và đơn hủy đã trả lượt.
func TestEdit_CouponSwapNoDeadlock(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	codeA, codeB := "SWAPA", "SWAPB"
	ca, cb := e.coupon(sale10(codeA, 0)), e.coupon(sale10(codeB, 0))
	v := e.variant(250000, 5000)
	deadlocks := func() int {
		return e.staff.Count(t, `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`)
	}
	withCoupon := func(code string) order.Order {
		in := e.m2Input(0, ln(v, 1, 0))
		in.CouponCode = code
		return e.mustCreateM2(in).Order
	}
	before := deadlocks()
	x, y := withCoupon(codeA), withCoupon(codeB)
	xHas, yHas := codeA, codeB
	at := e.now().Add(25 * time.Hour)
	for i := range 200 {
		e.draftWith(v, 1, codeA)
		e.draftWith(v, 1, codeB)
		z := withCoupon(codeA)
		inX := order.EditInput{Version: x.Version, CouponCode: &yHas}
		inY := order.EditInput{Version: y.Version, CouponCode: &xHas}
		var wg sync.WaitGroup
		start := make(chan struct{})
		var resX, resY order.EditResult
		var errX, errY, errExp, errCancel error
		var n int
		wg.Go(func() { <-start; resX, errX = e.editOrder(x.ID, inX, e.actor) })
		wg.Go(func() { <-start; resY, errY = e.editOrder(y.ID, inY, e.actor) })
		wg.Go(func() { <-start; n, errExp = e.expire(at, 200) })
		wg.Go(func() { <-start; _, errCancel = e.cancel(z) })
		close(start)
		wg.Wait()
		if err := errors.Join(errX, errY, errExp, errCancel); err != nil || n != 2 {
			t.Fatalf("lần %d (40P01 là deadlock): hết hạn %d đơn; %v", i, n, err)
		}
		x, y, xHas, yHas = resX.Order, resY.Order, yHas, xHas
	}
	if a, b := e.usedCount(ca), e.usedCount(cb); a != 1 || b != 1 {
		t.Errorf("used_count A = %d, B = %d; mong 1, 1", a, b)
	}
	if n := e.count(`SELECT count(*)::int FROM promotion.coupon_redemptions WHERE status = 'active'`); n != 2 {
		t.Errorf("lượt active = %d, mong 2 (của X và Y)", n)
	}
	time.Sleep(1500 * time.Millisecond) // số liệu thống kê của backend đẩy lên tối đa mỗi giây
	if after := deadlocks(); after != before {
		t.Errorf("pg_stat_database.deadlocks %d → %d", before, after)
	}
}
