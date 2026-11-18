package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// recordTimeout giới hạn mỗi lần ghi kết quả (trên ctx tách khỏi việc hủy của Timeout và Stop).
const recordTimeout = 5 * time.Second

// errLostLease: câu đổi trạng thái có điều kiện chạm 0 dòng (lệnh đã bị quét sang in_doubt hay đổi trạng thái khác);
// transaction lùi, không ghi gì.
var errLostLease = errors.New("commands: mất giữ chỗ")

// Runner chạy lệnh ra ngoài cho queue.Worker (hiện thực queue.CommandRunner; nối bằng WorkerOptions.Commands).
type Runner struct {
	d        *db.DB
	handlers map[string]Handler
	kinds    []queue.Kind
	// recordTimeout: hạn của mỗi lần ghi kết quả, tính từ lúc bắt đầu ghi (test đổi được).
	recordTimeout time.Duration
}

// NewRunner tạo Runner cho hs (mỗi Kind().Name một handler; trùng tên thì cái sau thắng).
func NewRunner(d *db.DB, hs []Handler) *Runner {
	r := &Runner{d: d, handlers: make(map[string]Handler, len(hs)), recordTimeout: recordTimeout}
	for _, h := range hs {
		r.handlers[h.Kind().Name] = h
	}
	for _, h := range r.handlers {
		r.kinds = append(r.kinds, h.Kind())
	}
	return r
}

// Kinds là các loại lệnh Runner chạy được.
func (r *Runner) Kinds() []queue.Kind { return r.kinds }

// TerminalHook trả hook của loại kind khi handler hiện thực queue.TerminalHook, ngược lại nil.
func (r *Runner) TerminalHook(kind string) queue.TerminalHook { //nolint:ireturn // queue.CommandRunner trả interface
	if hook, ok := r.handlers[kind].(queue.TerminalHook); ok {
		return hook
	}
	return nil
}

// Run chạy lệnh j (đang running, đã commit, do workerID giữ chỗ): gọi Execute ngoài transaction rồi ghi kết quả trong
// một transaction có tenant của lệnh, mọi câu có điều kiện running + locked_by = workerID:
//   - done → done (kèm Result) + DoneHook; ghi done lỗi → in_doubt (bên ngoài đã nhận, không gửi lại);
//   - not_sent → AutoRetryNotSent, còn lượt và chưa quá expires_at: pending sau RetryAfter hoặc queue.NextDelay;
//     ngược lại dead + OnTerminal;
//   - rejected → dead + OnTerminal;
//   - unknown, trạng thái lạ, Execute trả lỗi, quá Timeout, Stop hủy giữa chừng → in_doubt + OnTerminal.
//
// ctx đã hủy trước khi gọi Execute (Stop quá grace) → chưa gửi gì: trả về hàng chờ, không tính lượt.
func (r *Runner) Run(ctx context.Context, j queue.Job, workerID string) (string, error) {
	h, ok := r.handlers[j.Kind]
	if !ok { // Worker chỉ giao loại có trong Kinds
		return "", fmt.Errorf("commands: không có handler cho %s", j.Kind)
	}
	if ctx.Err() != nil {
		rctx, cancel := r.recordCtx(ctx)
		defer cancel()
		return r.record(rctx, j, "pending", nil, func(q *store.Queries) (int64, error) {
			return q.ReleaseJob(rctx, store.ReleaseJobParams{ID: j.ID, LockedBy: workerID})
		})
	}
	out, err := h.Execute(ctx, j)
	// Hạn ghi tính từ đây (sau Execute), trên ctx tách khỏi việc hủy của Timeout và Stop.
	rctx, cancel := r.recordCtx(ctx)
	defer cancel()
	switch {
	case err != nil:
		return r.inDoubt(rctx, j, workerID, queue.ErrorText(err))
	case out.Status == StatusDone:
		status, err := r.record(rctx, j, StatusDone, doneFn(h, j, out.Result), func(q *store.Queries) (int64, error) {
			return q.CompleteJob(rctx, store.CompleteJobParams{Result: out.Result, ID: j.ID, LockedBy: workerID})
		})
		if err != nil {
			dctx, dcancel := r.recordCtx(ctx) // hạn mới: lần ghi done có thể đã dùng hết hạn cũ
			defer dcancel()
			return r.inDoubt(dctx, j, workerID, queue.ErrorText(fmt.Errorf("ghi done: %w", err)))
		}
		return status, nil
	case out.Status == StatusNotSent:
		return r.notSent(rctx, h, j, workerID, out)
	case out.Status == StatusRejected:
		return r.dead(rctx, j, workerID, "rejected: "+out.Reason)
	}
	return r.inDoubt(rctx, j, workerID, "unknown: "+out.Status+" "+out.Reason)
}

// recordCtx là ctx của một lần ghi kết quả: giữ giá trị (span) của ctx, không theo việc hủy của nó, hạn recordTimeout
// tính từ lúc gọi.
func (r *Runner) recordCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), r.recordTimeout)
}

// notSent: hẹn gửi lại khi được phép, ngược lại dead (cùng một transaction: thử hẹn lại trước, 0 dòng thì dead).
func (r *Runner) notSent(ctx context.Context, h Handler, j queue.Job, workerID string, out Outcome) (string, error) {
	msg := queue.ErrorText(errors.New("not_sent: " + out.Reason))
	delay := out.RetryAfter
	if delay <= 0 {
		delay = queue.NextDelay(j.Attempts, rand.Float64)
	}
	onDead := r.terminalFn(j, queue.StatusDead)
	status := ""
	err := r.inTx(ctx, j.TenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		if h.AutoRetryNotSent() {
			n, err := q.RescheduleNotSent(ctx, store.RescheduleNotSentParams{
				DelayMs: delay.Milliseconds(), LastError: msg, ID: j.ID, LockedBy: workerID,
			})
			if err != nil {
				return fmt.Errorf("hẹn gửi lại: %w", err)
			}
			if n == 1 {
				status = "pending"
				return nil
			}
		}
		status = queue.StatusDead
		return apply(ctx, tx, onDead, status, func() (int64, error) {
			return q.KillJob(ctx, store.KillJobParams{LastError: msg, ID: j.ID, LockedBy: workerID})
		})
	})
	return result(status, err)
}

func (r *Runner) dead(ctx context.Context, j queue.Job, workerID, msg string) (string, error) {
	msg = queue.ErrorText(errors.New(msg))
	return r.record(ctx, j, queue.StatusDead, r.terminalFn(j, queue.StatusDead), func(q *store.Queries) (int64, error) {
		return q.KillJob(ctx, store.KillJobParams{LastError: msg, ID: j.ID, LockedBy: workerID})
	})
}

func (r *Runner) inDoubt(ctx context.Context, j queue.Job, workerID, msg string) (string, error) {
	msg = queue.ErrorText(errors.New(msg))
	return r.record(ctx, j, queue.StatusInDoubt, r.terminalFn(j, queue.StatusInDoubt), func(q *store.Queries) (int64, error) {
		return q.MarkInDoubt(ctx, store.MarkInDoubtParams{LastError: msg, ID: j.ID, LockedBy: workerID})
	})
}

// hookFn chạy trong transaction ghi kết quả, trước câu đổi trạng thái (ADR-46: dòng nghiệp vụ trước, jobs cuối).
type hookFn func(ctx context.Context, tx pgx.Tx) error

func (r *Runner) terminalFn(j queue.Job, status string) hookFn {
	hook := r.TerminalHook(j.Kind)
	if hook == nil {
		return nil
	}
	return func(ctx context.Context, tx pgx.Tx) error { return hook.OnTerminal(ctx, j, status, tx) }
}

func doneFn(h Handler, j queue.Job, res json.RawMessage) hookFn {
	dh, ok := h.(DoneHook)
	if !ok {
		return nil
	}
	return func(ctx context.Context, tx pgx.Tx) error { return dh.OnDone(ctx, j, res, tx) }
}

// record chạy fn (nếu có) rồi upd trong một transaction ngữ cảnh tenant của lệnh; upd chạm 0 dòng → lùi cả hai,
// trả trạng thái rỗng (mất giữ chỗ, không phải lỗi).
func (r *Runner) record(ctx context.Context, j queue.Job, status string, fn hookFn, upd func(*store.Queries) (int64, error)) (string, error) {
	err := r.inTx(ctx, j.TenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		return apply(ctx, tx, fn, status, func() (int64, error) { return upd(q) })
	})
	return result(status, err)
}

// apply chạy fn (nếu có) rồi upd trong tx; upd 0 dòng → errLostLease.
func apply(ctx context.Context, tx pgx.Tx, fn hookFn, status string, upd func() (int64, error)) error {
	if fn != nil {
		if err := fn(ctx, tx); err != nil {
			return fmt.Errorf("hook %s: %w", status, err)
		}
	}
	n, err := upd()
	switch {
	case err != nil:
		return fmt.Errorf("ghi %s: %w", status, err)
	case n == 0:
		return errLostLease
	}
	return nil
}

func result(status string, err error) (string, error) {
	switch {
	case errors.Is(err, errLostLease):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("ghi kết quả lệnh (%s): %w", status, err)
	}
	return status, nil
}

// inTx chạy fn trong ngữ cảnh tenant của lệnh, hoặc ngoài ngữ cảnh khi lệnh không có tenant.
func (r *Runner) inTx(ctx context.Context, tenant *uuid.UUID, fn func(pgx.Tx) error) error {
	return runTx(ctx, r.d, tenant, fn)
}

func runTx(ctx context.Context, d *db.DB, tenant *uuid.UUID, fn func(pgx.Tx) error) error {
	if tenant != nil {
		return tenancy.WithTenant(ctx, d, *tenant, fn) //nolint:wrapcheck // bên gọi bọc
	}
	return d.RetryTx(ctx, db.TxOptions{}, fn) //nolint:wrapcheck // bên gọi bọc
}
