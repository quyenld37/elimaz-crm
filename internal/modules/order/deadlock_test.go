package order_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// cancelWithCoupon là bản hủy tạm của test (Task M2-21 làm thật) theo đúng thứ tự khóa ADR-46: khóa đơn (nhóm 7) →
// đổi trạng thái → promotion.Release (nhóm 10) → nhả giữ hàng của các dòng (nhóm 11).
func (e *orderEnv) cancelWithCoupon(o order.Order) error {
	return e.app(func(tx pgx.Tx) error {
		ctx := e.t.Context()
		if _, err := tx.Exec(ctx, `SELECT id FROM ord.orders WHERE id = $1 FOR UPDATE`, o.ID); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, `UPDATE ord.orders SET status = 'cancelled', cancelled_at = now(),
			version = version + 1 WHERE id = $1`, o.ID); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := promotion.Release(ctx, tx, o.ID, "order_cancelled"); err != nil {
			return err //nolint:wrapcheck // test
		}
		items := make([]uuid.UUID, 0, len(o.Items))
		for _, it := range o.Items {
			items = append(items, it.ID)
		}
		w := inventory.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
		return w.ReleaseHeldByItems(ctx, tx, items, "order_cancelled", inventory.Actor{
			Type: inventory.ActorStaff, ID: e.actor.UserID,
		})
	})
}

// Không deadlock: 200 lần lặp, mỗi lần chạy song song "tạo đơn dùng mã X với V1" và "hủy đơn cũ dùng mã X giữ V1"
// → không lần nào lỗi 40P01, pg_stat_database.deadlocks không tăng; used_count của X bằng số đơn đang dùng mã.
func TestCreateCoupon_NoDeadlockWithCancel(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	cid := e.coupon(sale10("XDEAD", 0))
	v1 := e.variant(250000, 1000)
	deadlocks := func() int {
		return e.staff.Count(t, `SELECT deadlocks::int FROM pg_stat_database WHERE datname = current_database()`)
	}
	before := deadlocks()
	in := func() order.CreateInput {
		x := e.m2Input(0, ln(v1, 1, 0))
		x.CouponCode = "XDEAD"
		return x
	}
	old := e.mustCreateM2(in()).Order
	for i := range 200 {
		var wg sync.WaitGroup
		start := make(chan struct{})
		var created order.CreateResult
		var createErr, cancelErr error
		wg.Go(func() { <-start; created, createErr = e.createM2(in()) })
		wg.Go(func() { <-start; cancelErr = e.cancelWithCoupon(old) })
		close(start)
		wg.Wait()
		for _, err := range []error{createErr, cancelErr} {
			if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == "40P01" {
				t.Fatalf("lần %d: deadlock: %v", i, err)
			}
			if err != nil {
				t.Fatalf("lần %d: %v", i, err)
			}
		}
		old = created.Order
	}
	if n := e.count(`SELECT used_count FROM promotion.coupons WHERE id = $1`, cid); n != 1 {
		t.Errorf("used_count = %d, mong 1 (chỉ đơn cuối chưa hủy)", n)
	}
	if b := e.balance(v1); b.Held != 1 {
		t.Errorf("giữ V1 = %d, mong 1", b.Held)
	}
	time.Sleep(1500 * time.Millisecond) // số liệu thống kê của backend đẩy lên tối đa mỗi giây
	if after := deadlocks(); after != before {
		t.Errorf("pg_stat_database.deadlocks %d → %d", before, after)
	}
}
