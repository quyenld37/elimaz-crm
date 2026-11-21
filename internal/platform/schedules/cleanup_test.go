package schedules_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// oldJob chèn một việc ở trạng thái status, kết thúc cách đây age (finished_at NULL khi status là pending).
func (e *env) oldJob(t *testing.T, status string, age time.Duration) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.Owner.Pool.QueryRow(t.Context(), `INSERT INTO platform.jobs (kind, mode, dedupe_key, on_lease_expired, max_attempts,
		status, finished_at, created_at) VALUES ('t.old', 'transactional', gen_random_uuid()::text, 'retry', 8, $1,
		CASE WHEN $1 = 'pending' THEN NULL ELSE now() - $2::interval END, now() - $2::interval) RETURNING id`,
		status, age.String()).Scan(&id)
	if err != nil {
		t.Fatalf("chèn việc: %v", err)
	}
	return id
}

// idemKey chèn khóa chống trùng của tenant (bảng ép RLS, kể cả với chủ bảng: ghi và đọc trong WithTenant).
func (e *env) idemKey(t *testing.T, tenant uuid.UUID, key string, expiresIn time.Duration) {
	t.Helper()
	e.inTenant(t, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO platform.idempotency_keys (tenant_id, actor, route, key, request_hash,
			status, response_status, expires_at) VALUES ($1, 'u', 'r', $2, '\x00', 'done', 200, now() + $3::interval)`,
			tenant, key, expiresIn.String())
		return err //nolint:wrapcheck // test
	})
}

func (e *env) idemKeys(t *testing.T, tenant uuid.UUID, key string) int {
	t.Helper()
	var n int
	e.inTenant(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT count(*) FROM platform.idempotency_keys WHERE key = $1", key).Scan(&n)
	})
	return n
}

func (e *env) inTenant(t *testing.T, tenant uuid.UUID, fn func(pgx.Tx) error) {
	t.Helper()
	if err := tenancy.WithTenant(t.Context(), e.App, tenant, fn); err != nil {
		t.Fatalf("ghi, đọc trong tenant: %v", err)
	}
}

func (e *env) metric(t *testing.T, age time.Duration, name string) {
	t.Helper()
	e.exec(t, `INSERT INTO platform.metrics_minute (bucket, process, name, count, sum, max)
		VALUES (date_trunc('minute', now() - $1::interval), 'p', $2, 1, 1, 1)`, age.String(), name)
}

// tickCleanup định nghĩa hai lịch dọn rồi Tick ở now (ghi lịch, tạo việc dọn).
func (e *env) tickCleanup(t *testing.T, c *schedules.Cleanup) {
	t.Helper()
	r := schedules.NewRegistry()
	if err := c.Define(r); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ticker(r).Tick(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

// runCleanup định nghĩa hai lịch dọn, Tick ở now, rồi cho worker chạy hết việc.
func (e *env) runCleanup(t *testing.T, c *schedules.Cleanup) {
	t.Helper()
	e.tickCleanup(t, c)
	w := queue.NewWorker(e.App, c.Handlers(), queue.WorkerOptions{WorkerID: "w", Concurrency: 4})
	testkit.DrainDue(t, e.Owner, w)
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind LIKE 'task.platform.cleanup%' AND status <> 'done'"); n != 0 {
		t.Fatalf("%d việc dọn chưa done", n)
	}
}

const day = 24 * time.Hour

// Dọn với hạn mặc định: việc done 8 ngày bị xóa, 6 ngày còn, dead 60 ngày còn, expired 8 ngày bị xóa, pending cũ còn
// (việc giữ 7 ngày, Task 394); chỉ số quá 30 ngày bị xóa, 29 ngày còn; khóa chống trùng quá hạn bị xóa ở mọi tenant,
// còn hạn giữ nguyên.
func TestCleanupRemovesOnlyOldRows(t *testing.T) {
	e := newEnv(t)
	a, b := e.Tenant(t, "a"), e.Tenant(t, "b")
	done8 := e.oldJob(t, "done", 8*day)
	done6 := e.oldJob(t, "done", 6*day)
	dead60 := e.oldJob(t, "dead", 60*day)
	inDoubt60 := e.oldJob(t, "in_doubt", 60*day)
	exp8 := e.oldJob(t, "expired", 8*day)
	pend60 := e.oldJob(t, "pending", 60*day)
	e.metric(t, 31*day, "old")
	e.metric(t, 29*day, "new")
	for _, id := range []uuid.UUID{a, b} {
		e.idemKey(t, id, "gone", -time.Minute)
		e.idemKey(t, id, "kept", time.Hour)
	}
	e.runCleanup(t, schedules.NewCleanup(schedules.CleanupOptions{}))

	for id, want := range map[uuid.UUID]int{done8: 0, done6: 1, dead60: 1, inDoubt60: 1, exp8: 0, pend60: 1} {
		if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE id = $1", id); n != want {
			t.Fatalf("việc %s: còn %d, muốn %d", id, n, want)
		}
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.metrics_minute WHERE name = 'old'"); n != 0 {
		t.Fatal("chỉ số 31 ngày phải bị xóa")
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.metrics_minute WHERE name = 'new'"); n != 1 {
		t.Fatal("chỉ số 29 ngày phải còn")
	}
	for _, id := range []uuid.UUID{a, b} {
		if n := e.idemKeys(t, id, "gone"); n != 0 {
			t.Fatalf("tenant %s: khóa quá hạn còn %d", id, n)
		}
		if n := e.idemKeys(t, id, "kept"); n != 1 {
			t.Fatalf("tenant %s: khóa còn hạn còn %d, muốn 1", id, n)
		}
	}
}

// Hạn giữ việc xong cấu hình được: 2 ngày → việc done 3 ngày bị xóa, 1 ngày còn; chỉ số 29 ngày vẫn còn (hạn riêng).
func TestCleanupJobRetentionConfigurable(t *testing.T) {
	e := newEnv(t)
	done3 := e.oldJob(t, "done", 3*day)
	done1 := e.oldJob(t, "done", 1*day)
	e.metric(t, 29*day, "new")
	e.runCleanup(t, schedules.NewCleanup(schedules.CleanupOptions{JobRetention: 2 * day}))
	for id, want := range map[uuid.UUID]int{done3: 0, done1: 1} {
		if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE id = $1", id); n != want {
			t.Fatalf("việc %s: còn %d, muốn %d", id, n, want)
		}
	}
	if n := e.Count(t, "SELECT count(*) FROM platform.metrics_minute WHERE name = 'new'"); n != 1 {
		t.Fatal("chỉ số 29 ngày phải còn")
	}
}

// Hai lịch dọn chạy mỗi 5 phút (Task 394): mỗi kỳ tối đa 5.000 việc, mỗi giờ tới 60.000 việc xong.
func TestCleanupEveryFiveMinutes(t *testing.T) {
	e := newEnv(t)
	e.tickCleanup(t, schedules.NewCleanup(schedules.CleanupOptions{}))
	if n := e.Count(t, "SELECT count(*) FROM platform.schedules WHERE name LIKE 'platform.cleanup%' AND every_seconds = 300"); n != 2 {
		t.Fatalf("%d lịch dọn có chu kỳ 300 s, muốn 2", n)
	}
}

// Câu dọn việc xong dùng index (jobs_finished, migration 0008), không quét bảng jobs khi bảng có 100.000 việc xong mới
// và 5.000 việc quá hạn: transaction dọn không giữ lâu.
func TestCleanupJobsPlanUsesIndex(t *testing.T) {
	// Dựng một lần làm database mẫu, bằng chủ bảng như e.exec.
	e := &env{testkit.OpenStaffSeeded(t, testkit.Seed{Name: "schedules-cleanup-plan", SQL: []string{
		"SET ROLE crm_owner",
		`INSERT INTO platform.jobs (kind, mode, dedupe_key, on_lease_expired, max_attempts, status, finished_at)
		SELECT 't.recent', 'transactional', g::text, 'retry', 8, 'done', now() - g * interval '1 s'
		FROM generate_series(1, 100000) AS g`,
		`INSERT INTO platform.jobs (kind, mode, dedupe_key, on_lease_expired, max_attempts, status, finished_at)
		SELECT 't.old', 'transactional', g::text, 'retry', 8, 'done', now() - interval '40 days'
		FROM generate_series(1, 5000) AS g`,
		"ANALYZE platform.jobs",
	}})}
	// Cùng câu với DeleteFinishedJobs (queries/platform/schedules.sql), chạy bằng crm_app như lịch dọn.
	rows, err := e.App.Pool.Query(t.Context(), `EXPLAIN (COSTS OFF) DELETE FROM platform.jobs WHERE id = ANY (ARRAY(
		SELECT j.id FROM platform.jobs AS j WHERE j.status IN ('done', 'expired') AND j.finished_at < $1::timestamptz
		ORDER BY j.finished_at LIMIT $2 FOR UPDATE SKIP LOCKED))`, time.Now().Add(-schedules.DefaultJobRetention), 5000)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(lines, "\n")
	if strings.Contains(plan, "Seq Scan") || !strings.Contains(plan, "jobs_finished") || !strings.Contains(plan, "jobs_pkey") {
		t.Fatalf("câu dọn phải dùng index jobs_finished, xóa theo jobs_pkey, không quét bảng:\n%s", plan)
	}
}

// Mỗi lần dọn tối đa 5.000 dòng mỗi bảng.
func TestCleanupBatchLimit(t *testing.T) {
	e := newEnv(t)
	e.exec(t, `INSERT INTO platform.jobs (kind, mode, dedupe_key, on_lease_expired, max_attempts, status, finished_at)
		SELECT 't.old', 'transactional', g::text, 'retry', 8, 'done', now() - interval '40 days' FROM generate_series(1, 5003) AS g`)
	e.runCleanup(t, schedules.NewCleanup(schedules.CleanupOptions{}))
	if n := e.Count(t, "SELECT count(*) FROM platform.jobs WHERE kind = 't.old'"); n != 3 {
		t.Fatalf("sau một lần còn %d việc cũ, muốn 3", n)
	}
}

// Hàm dọn đăng ký thêm chạy trong transaction của việc dọn; hàm theo tenant chạy một lần cho mỗi tenant, trong ngữ
// cảnh tenant đó.
func TestRegisteredCleanupsRun(t *testing.T) {
	e := newEnv(t)
	a, b := e.Tenant(t, "a"), e.Tenant(t, "b")
	c := schedules.NewCleanup(schedules.CleanupOptions{})
	var (
		mu      sync.Mutex
		global  int
		tenants = map[string]int{}
	)
	c.RegisterCleanup(func(_ context.Context, tx pgx.Tx, now time.Time) (int64, error) {
		if time.Since(now) > time.Minute || tx == nil {
			t.Errorf("now %v, tx %v", now, tx)
		}
		mu.Lock()
		global++
		mu.Unlock()
		return 0, nil
	})
	c.RegisterTenantCleanup(func(ctx context.Context, tx pgx.Tx, _ time.Time) (int64, error) {
		var cur string
		if err := tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&cur); err != nil {
			return 0, err //nolint:wrapcheck // test
		}
		mu.Lock()
		tenants[cur]++
		mu.Unlock()
		return 0, nil
	})
	e.runCleanup(t, c)
	mu.Lock()
	defer mu.Unlock()
	if global != 1 || len(tenants) != 2 || tenants[a.String()] != 1 || tenants[b.String()] != 1 {
		t.Fatalf("global %d, tenants %v", global, tenants)
	}
}
