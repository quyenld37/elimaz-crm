package inventory_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// deadlocks đọc pg_stat_database.deadlocks của database thử (chủ bảng, ngoài transaction nghiệp vụ).
func (e *invEnv) deadlocks() int64 {
	e.t.Helper()
	var n int64
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(),
		"SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()").Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// both chạy f và g song song, chờ cả hai, trả lỗi gộp.
func both(f, g func() error) error {
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Go(func() { errs[0] = f() })
	wg.Go(func() { errs[1] = g() })
	wg.Wait()
	return errors.Join(errs...)
}

// Khóa chéo: 200 lượt, mỗi lượt hai transaction song song giữ {A, B} và {B, A} (thứ tự truyền vào ngược nhau) →
// không deadlock. Transaction mở thẳng trên pool (không qua RetryTx) để lỗi 40P01 không bị chạy lại che mất; mỗi
// transaction giữ khóa thêm một chút trước khi commit để hai bên chắc chắn chồng nhau.
func TestReserve_NoDeadlockOnCrossOrder(t *testing.T) {
	e := newInvEnv(t)
	a, b := e.stock(1000), e.stock(1000)
	before := e.deadlocks()
	reserve := func(first, second inventory.ReserveLine) error {
		ctx := t.Context()
		tx, err := e.staff.App.Pool.Begin(ctx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", e.tenant.String()); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := e.writer().Reserve(ctx, tx, []inventory.ReserveLine{first, second}, sysActor()); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, "SELECT pg_sleep(0.001)"); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.Commit(ctx)
	}
	for round := range 200 {
		if err := both(
			func() error { return reserve(e.line(a, 1), e.line(b, 1)) },
			func() error { return reserve(e.line(b, 1), e.line(a, 1)) },
		); err != nil {
			t.Fatalf("lượt %d: %v", round, err)
		}
	}
	want(t, e.balance(a), inventory.Balance{OnHand: 1000, Held: 400, Available: 600})
	want(t, e.balance(b), inventory.Balance{OnHand: 1000, Held: 400, Available: 600})
	// Số đếm thống kê được đẩy lên khi backend rảnh: chờ tối đa 2 s cho chắc.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := e.staff.Owner.Pool.Exec(t.Context(), "SELECT pg_stat_clear_snapshot()"); err != nil {
			t.Fatal(err)
		}
		if got := e.deadlocks(); got != before {
			t.Fatalf("pg_stat_database.deadlocks tăng từ %d lên %d", before, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Nhả và đi giao chéo thứ tự: hai transaction cùng chạm hai dòng số dư theo hai danh sách ngược nhau.
func TestCore_NoDeadlockOnCrossOrder(t *testing.T) {
	e := newInvEnv(t)
	a, b := e.stock(1000), e.stock(1000)
	for round := range 50 {
		ids1, err := e.reserve(sysActor(), e.line(a, 1), e.line(b, 1))
		if err != nil {
			t.Fatal(err)
		}
		ids2, err := e.reserve(sysActor(), e.line(b, 1), e.line(a, 1))
		if err != nil {
			t.Fatal(err)
		}
		if err := both(
			func() error { return e.release([]uuid.UUID{ids1[1], ids1[0]}, "x") },
			func() error { return e.startTransit(ids2, sysActor()) },
		); err != nil {
			t.Fatalf("lượt %d: %v", round, err)
		}
	}
	want(t, e.balance(a), inventory.Balance{OnHand: 1000, InTransit: 50, Available: 950})
}
