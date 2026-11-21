package e2e

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Worker chết ngay sau khi bên ngoài đã nhận lệnh (Review Focus 1): lệnh command.selftest, bên ngoài nhận rồi treo
// (hang_after_send); worker một nhận việc rồi bị bỏ (không Stop), giữ chỗ đặt về quá khứ; worker hai RunOnce 3 lần
// → in_doubt, bên ngoài vẫn chỉ nhận 1 lần. Tra lại (Lookup nói done) → done, vẫn 1 lần; worker một tỉnh lại muộn
// không ghi đè.
func TestWorkerCrashDoesNotSendTwice(t *testing.T) {
	staff := testkit.OpenStaff(t)
	tenant := staff.Tenant(t, "A")
	ext := &testkit.FakeExternal{Mode: testkit.FakeHangAfterSend}
	cmd := selftest.NewCommand(ext)
	hs := []commands.Handler{cmd}
	ctx := t.Context()

	var id uuid.UUID
	if err := tenancy.WithTenant(ctx, staff.App, tenant, func(tx pgx.Tx) error {
		var err error
		id, _, err = queue.NewQueue().Enqueue(ctx, tx, selftest.CommandKind(), queue.NewJob{TenantID: &tenant, DedupeKey: "crash-1"})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	status := func() string {
		var s string
		if err := staff.Owner.Pool.QueryRow(context.Background(), "SELECT status FROM platform.jobs WHERE id = $1", id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	w1 := queue.NewWorker(staff.App, nil, queue.WorkerOptions{WorkerID: "w1", Commands: commands.NewRunner(staff.App, hs)})
	ctx1, abandon := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	testkit.MakeDueSoon(t, staff.Owner) // lệnh vừa ghi có thể lệch hạn vì đồng hồ PostgreSQL lùi (Task 451)
	wg.Go(func() { _, _ = w1.RunOnce(ctx1, 1) })
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			abandon()
			wg.Wait()
		}
	}
	t.Cleanup(stop)
	waitFor(t, 5*time.Second, "bên ngoài nhận lệnh", func() bool { return len(ext.Calls()) == 1 })
	if s := status(); s != "running" {
		t.Fatalf("lệnh đang gửi phải running, nhận %s", s)
	}
	if _, err := staff.Owner.Pool.Exec(ctx, "UPDATE platform.jobs SET lease_until = now() - interval '1 second' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}

	w2 := queue.NewWorker(staff.App, nil, queue.WorkerOptions{WorkerID: "w2", Commands: commands.NewRunner(staff.App, hs)})
	for range 3 {
		if _, err := w2.RunOnce(ctx, 10); err != nil {
			t.Fatal(err)
		}
	}
	if s, calls := status(), ext.Calls(); s != queue.StatusInDoubt || len(calls) != 1 {
		t.Fatalf("sau worker chết: %s, %d lần gửi; muốn in_doubt, 1", s, len(calls))
	}

	done, requeued, still, err := commands.ResolveInDoubt(ctx, staff.App, hs, 10)
	if err != nil || done != 1 || requeued != 0 || still != 0 {
		t.Fatalf("ResolveInDoubt = %d %d %d %v, muốn 1 0 0", done, requeued, still, err)
	}
	if s, calls := status(), ext.Calls(); s != commands.StatusDone || len(calls) != 1 {
		t.Fatalf("sau tra lại: %s, %d lần gửi; muốn done, 1", s, len(calls))
	}
	stop() // worker một "tỉnh lại": Execute trả lỗi, câu ghi có điều kiện locked_by = w1 chạm 0 dòng
	if s, calls := status(), ext.Calls(); s != commands.StatusDone || len(calls) != 1 {
		t.Fatalf("worker một tỉnh lại ghi đè: %s, %d lần gửi", s, len(calls))
	}
}
