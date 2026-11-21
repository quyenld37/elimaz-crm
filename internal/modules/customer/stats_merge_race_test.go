package customer_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// afterRootTx bọc tx của consumer: ngay sau lần đọc gốc đầu tiên (GetRootOf) chạy hook một lần, như lần gộp của phiên
// khác vừa commit giữa câu đọc gốc và câu đọc nhóm (READ COMMITTED). Savepoint mở từ tx này cũng được bọc.
type afterRootTx struct {
	pgx.Tx
	once *sync.Once
	hook func()
}

//nolint:ireturn // hiện thực pgx.Tx
func (t afterRootTx) Begin(ctx context.Context) (pgx.Tx, error) {
	sp, err := t.Tx.Begin(ctx)
	if err != nil {
		return nil, err //nolint:wrapcheck // test
	}
	return afterRootTx{Tx: sp, once: t.once, hook: t.hook}, nil
}

//nolint:ireturn // pgx.Tx trả pgx.Row
func (t afterRootTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	r := t.Tx.QueryRow(ctx, sql, args...)
	if !strings.Contains(sql, "name: GetRootOf") {
		return r
	}
	return afterScanRow{Row: r, after: func() { t.once.Do(t.hook) }}
}

type afterScanRow struct {
	pgx.Row
	after func()
}

func (r afterScanRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	r.after()
	return err //nolint:wrapcheck // test
}

// Lỗi có sẵn (Task 103 phát hiện; gốc của TestStatsConcurrentWithMerge chập chờn): consumer đọc gốc của B (= B), rồi
// B bị gộp vào A và commit trước khi consumer đọc nhóm của B → nhóm rỗng → locked[0] panic. Đúng: đọc lại gốc (A) và
// tính lại cả nhóm A gồm B.
func TestStatsRootMergedBetweenReads(t *testing.T) {
	e := newStatsEnv(t)
	a, b := e.newCustomer("Khách A"), e.newCustomer("Khách B")
	e.delivered(a, 100000)
	e.delivered(b, 200000)
	e.feed(false)
	merge := func() {
		if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
			_, err := e.cw.MergeCustomers(t.Context(), tx, a, b, customer.MatchedByStaff, e.actor, e.now)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Errorf("gộp chen giữa: %v", err)
		}
	}
	e.app(func(tx pgx.Tx) error {
		return e.cw.RecomputeStats(t.Context(), afterRootTx{Tx: tx, once: &sync.Once{}, hook: merge}, b, e.now)
	})
	e.expect("A sau khi tính lại theo B vừa bị gộp", a, wantStats{2, 2, 0, 360000, 0})
	if r := e.rawStats(b); r != [4]int64{} {
		t.Errorf("B phải về 0: %v", r)
	}
}
