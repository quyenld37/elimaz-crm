package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

func openOwnerDB(t *testing.T) *db.DB {
	t.Helper()
	_, ownerURL, _ := testkit.StartPostgres(t)
	d, err := db.Open(t.Context(), ownerURL, db.PoolOptions{MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Pool.Close)
	return d
}

func addJob(t *testing.T, owner *db.DB, tenant uuid.UUID, kind, mode, key, status string, attempts int) {
	t.Helper()
	lease, by := any(nil), any(nil)
	if status == "running" {
		lease, by = time.Now().Add(time.Minute), "w"
	}
	if _, err := owner.Pool.Exec(t.Context(), `INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key,
		on_lease_expired, max_attempts, status, attempts, lease_until, locked_by)
		VALUES ($1, $2, $3, $4, 'retry', 8, $5, $6, $7, $8)`, tenant, kind, mode, key, status, attempts, lease, by); err != nil {
		t.Fatal(err)
	}
}

func addPickup(t *testing.T, owner *db.DB, bucket time.Time, process string, hist []int) {
	t.Helper()
	if _, err := owner.Pool.Exec(t.Context(), `INSERT INTO platform.metrics_minute (bucket, process, name, label, count,
		sum, max, hist) VALUES ($1, $2, 'job_pickup_ms', 'inbound.selftest', 1, 1, 1, $3)`, bucket, process, hist); err != nil {
		t.Fatal(err)
	}
}

// readStats đếm đúng việc của lượt chạy (theo tiền tố eventId), lệnh gửi hơn một lần, cộng histogram nhận việc trong
// khung giờ, và đọc kích thước, dòng chết của platform.jobs.
func TestReadStats(t *testing.T) {
	owner := openOwnerDB(t)
	ctx := t.Context()
	tenant, err := tenancy.SeedTenant(ctx, owner, "Thử tải")
	if err != nil {
		t.Fatal(err)
	}
	for i, st := range []string{"done", "done", "done", "pending", "dead"} {
		addJob(t, owner, tenant, "inbound.selftest", "transactional", eventID("r1", time.Now(), 1, i), st, 1)
	}
	addJob(t, owner, tenant, "inbound.selftest", "transactional", eventID("r0", time.Now(), 1, 1), "done", 1) // lượt khác
	addJob(t, owner, tenant, "command.selftest", "command", commandKey("r1", 1), "done", 1)
	addJob(t, owner, tenant, "command.selftest", "command", commandKey("r1", 2), "done", 2) // gửi hai lần
	addJob(t, owner, tenant, "command.selftest", "command", commandKey("r1", 3), "in_doubt", 1)
	addJob(t, owner, tenant, "command.selftest", "command", commandKey("r1", 4), "running", 1)
	addJob(t, owner, tenant, "command.selftest", "command", commandKey("r0", 1), "done", 3) // lượt khác

	to := time.Now().UTC()
	from := to.Truncate(time.Minute).Add(-10 * time.Minute)
	h := func(vals ...int) []int {
		out := make([]int, len(metrics.HistBoundsMs)+1)
		copy(out, vals)
		return out
	}
	addPickup(t, owner, from, "worker-a", h(5, 1))
	addPickup(t, owner, from.Add(time.Minute), "worker-b", h(1, 0, 2))
	addPickup(t, owner, from.Add(-time.Minute), "worker-a", h(100)) // trước khung
	addPickup(t, owner, to.Add(time.Minute), "worker-a", h(100))    // sau khung
	if _, err := owner.Pool.Exec(ctx, `INSERT INTO platform.metrics_minute (bucket, process, name, label, count, sum,
		max) VALUES ($1, 'worker-a', 'job_ms', 'x', 1, 1, 1)`, from); err != nil { // chỉ số khác
		t.Fatal(err)
	}

	got, err := readStats(ctx, owner, "r1", from.Add(30*time.Second), to)
	if err != nil {
		t.Fatal(err)
	}
	if got.InboundDone != 3 || got.InboundOther != 2 {
		t.Fatalf("inbound done %d, khác %d; muốn 3, 2", got.InboundDone, got.InboundOther)
	}
	if got.CommandsTotal != 4 || got.CommandsDone != 2 || got.CommandsInDoubt != 1 || got.CommandsResent != 1 {
		t.Fatalf("lệnh = %+v", got)
	}
	if got.Dead < 1 {
		t.Fatalf("dead = %d, muốn ≥ 1", got.Dead)
	}
	if want := h(6, 1, 2); !equalInts(got.PickupHist, want) {
		t.Fatalf("histogram = %v, muốn %v", got.PickupHist, want)
	}
	if got.JobsBytes <= 0 || got.JobsDeadTuples < 0 || got.At.IsZero() {
		t.Fatalf("bảng jobs = %+v", got)
	}

	// Khung không có dòng nào: histogram toàn 0 (đủ ngăn), không lỗi.
	empty, err := readStats(ctx, owner, "khong-co", from.Add(-time.Hour), from.Add(-50*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.PickupHist) != len(metrics.HistBoundsMs)+1 || empty.InboundDone != 0 || empty.CommandsTotal != 0 {
		t.Fatalf("rỗng = %+v", empty)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// seedStaff dựng tenant thử, n nhân viên chủ shop và định tuyến selftest; chạy lại không thêm dòng.
func TestSeedStaff(t *testing.T) {
	owner := openOwnerDB(t)
	ctx := context.Background()
	var first uuid.UUID
	for range 2 {
		id, err := seedStaff(ctx, owner, 3, "loadtest-key")
		if err != nil {
			t.Fatal(err)
		}
		if first == uuid.Nil {
			first = id
		} else if id != first {
			t.Fatalf("lần hai trả tenant khác: %s, %s", id, first)
		}
	}
	var users, routes int
	if err := tenancy.WithTenant(ctx, owner, first, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM identity.users`).Scan(&users)
	}); err != nil {
		t.Fatal(err)
	}
	if err := owner.Pool.QueryRow(ctx, `SELECT count(*) FROM platform.tenant_routes WHERE tenant_id = $1`, first).Scan(&routes); err != nil {
		t.Fatal(err)
	}
	if users != 3 || routes != 4 {
		t.Fatalf("users %d, routes %d; muốn 3, 4", users, routes)
	}
}
