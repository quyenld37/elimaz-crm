package e2e

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
)

// Vai trò worker của tệp chạy thật chạy hàng đợi (Task 11): một việc chưa có handler được ghi khi worker đang chạy
// → worker nhận (NOTIFY) và đặt dead "no handler"; SIGTERM → thoát mã 0 (Stop tắt êm).
func TestWorkerRoleRunsQueue(t *testing.T) {
	bin := buildBinary(t)
	_, ownerURL, appURL := testkit.StartPostgres(t)
	cmd, done, stdout, stderr := startRole(t, bin, roleEnv("worker", appURL, 0))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if err := waitHealthcheck(ctx, bin, roleEnv("worker", appURL, 0)); err != nil {
		t.Fatalf("worker chưa sẵn sàng: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	owner, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(context.Background()) }()
	var id string
	if err := owner.QueryRow(ctx, `INSERT INTO platform.jobs (kind, mode, dedupe_key, on_lease_expired, max_attempts)
		VALUES ('e2e.unknown', 'transactional', 'k', 'retry', 8) RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var status, lastError string
	for {
		if err := owner.QueryRow(ctx, "SELECT status, coalesce(last_error, '') FROM platform.jobs WHERE id = $1", id).
			Scan(&status, &lastError); err != nil {
			t.Fatalf("worker không xử lý việc: %v (status %q)\n%s", err, status, stdout.String())
		}
		if status != "pending" && status != "running" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "dead" || lastError != "no handler" {
		t.Fatalf("muốn dead/no handler, nhận %s/%s", status, lastError)
	}
	stopWorker(t, cmd, done, stdout, stderr)
}

// Vai trò worker chạy việc định kỳ (Task 13): khi khởi động, Ticker ghi hai lịch dọn vào platform.schedules và tạo
// việc dọn; worker có handler nên việc dọn thành done (không dead "no handler"); việc done quá 30 ngày bị xóa.
func TestWorkerRoleRunsSchedules(t *testing.T) {
	bin := buildBinary(t)
	_, ownerURL, appURL := testkit.StartPostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	owner, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(context.Background()) }()
	if _, err := owner.Exec(ctx, `INSERT INTO platform.tenants (name) VALUES ('a');
		INSERT INTO platform.jobs (kind, mode, dedupe_key, on_lease_expired, max_attempts, status, finished_at)
		VALUES ('e2e.old', 'transactional', 'k', 'retry', 8, 'done', now() - interval '31 days')`); err != nil {
		t.Fatal(err)
	}
	cmd, done, stdout, stderr := startRole(t, bin, roleEnv("worker", appURL, 0))
	for {
		var schedules, finished, old int
		if err := owner.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform.schedules WHERE name IN ('platform.cleanup', 'platform.cleanup_tenant')),
			(SELECT count(*) FROM platform.jobs WHERE kind LIKE 'task.platform.cleanup%' AND status = 'done'),
			(SELECT count(*) FROM platform.jobs WHERE kind = 'e2e.old')`).Scan(&schedules, &finished, &old); err != nil {
			t.Fatalf("worker không chạy lịch dọn: %v\n%s\n%s", err, stdout.String(), stderr.String())
		}
		if schedules == 2 && finished == 2 && old == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	stopWorker(t, cmd, done, stdout, stderr)
}

// stopWorker gửi SIGTERM, đòi worker thoát mã 0 trong 30 s và mọi dòng log là JSON.
func stopWorker(t *testing.T, cmd *exec.Cmd, done <-chan error, stdout, stderr *lineBuffer) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker thoát lỗi: %v\n%s\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("worker không thoát sau SIGTERM")
	}
	assertJSONLines(t, stdout.String())
}
