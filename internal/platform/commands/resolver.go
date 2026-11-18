package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Lịch tra lại lệnh in_doubt (kế hoạch M0, Task 13 Interfaces: "Lịch platform.resolve_in_doubt (30 s) gọi
// commands.ResolveInDoubt"; nối ở lượt 2 Task 14).
const (
	// ResolveInDoubtSchedule là tên lịch (schedules.Schedule.Name).
	ResolveInDoubtSchedule = "platform.resolve_in_doubt"
	// ResolveInDoubtEvery là chu kỳ của lịch, cũng là hạn sống của một việc tra lại (kỳ cũ chưa chạy thì expired).
	ResolveInDoubtEvery = 30 * time.Second

	resolveBatch   = 50
	resolveTimeout = 25 * time.Second
	resolveFailed  = "lookup_failed"
)

// NewInDoubtResolver trả lệnh chạy ResolveInDoubt cho hs (tối đa 50 lệnh mỗi lần), để lịch ResolveInDoubtSchedule ghi
// và Runner chạy: ngoài transaction vì Looker gọi bên ngoài (handler transactional của queue không được gọi mạng).
// Chạy lại an toàn (chỉ tra, kết quả chỉ áp cho đúng lần in_doubt đã đọc) nên giữ chỗ hết hạn thì chạy lại, và tra
// lỗi thì not_sent (tự chạy lại trong hạn 30 s, quá hạn thì expired). Kết quả done: {"done","requeued","still"}.
// Đăng ký cùng hs vào NewRunner; hs không chứa chính nó.
func NewInDoubtResolver(d *db.DB, hs []Handler) Handler { //nolint:ireturn // Runner nhận Handler
	return inDoubtResolver{d: d, hs: hs}
}

type inDoubtResolver struct {
	d  *db.DB
	hs []Handler
}

func (inDoubtResolver) Kind() queue.Kind {
	return queue.Kind{Name: "task." + ResolveInDoubtSchedule, Policy: queue.Policy{
		Mode: queue.ModeCommand, MaxAttempts: 3, OnLeaseExpired: queue.LeaseRetry,
		ExpiresAfter: ResolveInDoubtEvery, Timeout: resolveTimeout,
	}}
}

func (inDoubtResolver) AutoRetryNotSent() bool { return true }

// Execute tra lại; lỗi của bất kỳ lần tra nào → not_sent với lý do cố định (chuỗi lỗi của bên ngoài có thể mang dữ
// liệu cá nhân nên không chép vào last_error).
func (r inDoubtResolver) Execute(ctx context.Context, _ queue.Job) (Outcome, error) {
	done, requeued, still, err := ResolveInDoubt(ctx, r.d, r.hs, resolveBatch)
	if err != nil {
		return Outcome{Status: StatusNotSent, Reason: resolveFailed}, nil
	}
	res, err := json.Marshal(map[string]int{"done": done, "requeued": requeued, "still": still})
	if err != nil {
		return Outcome{}, fmt.Errorf("mã hóa kết quả tra lại: %w", err)
	}
	return Outcome{Status: StatusDone, Result: res}, nil
}
