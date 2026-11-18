package queue_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// NewWorker điền mặc định cho tùy chọn rỗng và giữ nguyên tùy chọn đã đặt (mutation testing Task 23: các điều kiện
// mặc định của NewWorker còn sống). Không chạm database.
func TestNewWorkerOptionDefaults(t *testing.T) {
	o := queue.Options(queue.NewWorker(nil, nil, queue.WorkerOptions{}))
	if !strings.HasPrefix(o.WorkerID, "worker-") || len(o.WorkerID) <= len("worker-") {
		t.Errorf("WorkerID mặc định %q, muốn worker-<uuid>", o.WorkerID)
	}
	if o.Concurrency != 8 || o.Lease != 90*time.Second || o.Poll != 2*time.Second || o.Logger == nil {
		t.Errorf("mặc định sai: concurrency %d, lease %v, poll %v, logger %v", o.Concurrency, o.Lease, o.Poll, o.Logger)
	}
	neg := queue.Options(queue.NewWorker(nil, nil, queue.WorkerOptions{Concurrency: -1, Lease: -time.Second, Poll: -time.Second}))
	if neg.Concurrency != 8 || neg.Lease != 90*time.Second || neg.Poll != 2*time.Second {
		t.Errorf("giá trị âm phải lấy mặc định: %+v", neg)
	}
	l := slog.New(slog.DiscardHandler)
	set := queue.Options(queue.NewWorker(nil, nil, queue.WorkerOptions{
		WorkerID: "w-1", Concurrency: 3, Lease: 30 * time.Second, Poll: 500 * time.Millisecond, Logger: l,
	}))
	if set.WorkerID != "w-1" || set.Concurrency != 3 || set.Lease != 30*time.Second || set.Poll != 500*time.Millisecond || set.Logger != l {
		t.Errorf("tùy chọn đã đặt bị đổi: %+v", set)
	}
}
