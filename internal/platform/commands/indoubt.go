package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// ResolveInDoubt tra lại tối đa limit lệnh in_doubt của các handler có Looker (04 mục 5.3): Lookup (ngoài transaction,
// hạn Policy.Timeout của loại) nói done → done + DoneHook (result nil); not_sent → pending, chạy ngay; unknown hay
// Lookup lỗi → giữ in_doubt (người có quyền xem). Kết quả tra chỉ áp khi lệnh vẫn ở đúng lần in_doubt đã đọc
// (finished_at không đổi); lệnh đã đổi trong lúc tra thì bỏ qua. Lệnh của handler không có Looker không bị đụng tới.
// Trả số lệnh done, về hàng chờ và còn in_doubt; lỗi của từng lệnh được gộp, lệnh khác vẫn được tra.
func ResolveInDoubt(ctx context.Context, d *db.DB, hs []Handler, limit int) (done, requeued, still int, err error) {
	lookers := map[string]Handler{}
	kinds := []string{}
	for _, h := range hs {
		if _, ok := h.(Looker); ok {
			name := h.Kind().Name
			if _, dup := lookers[name]; !dup {
				kinds = append(kinds, name)
			}
			lookers[name] = h
		}
	}
	if len(kinds) == 0 || limit <= 0 {
		return 0, 0, 0, nil
	}
	rows, err := store.New(d.Pool).ListInDoubt(ctx, store.ListInDoubtParams{Kinds: kinds, Lim: int32(min(limit, 1<<31-1))})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("đọc lệnh in_doubt: %w", err)
	}
	var errs []error
	for _, r := range rows {
		h := lookers[r.Kind]
		j := queue.Job{ID: r.ID, Kind: r.Kind, Payload: r.Payload, Attempts: int(r.Attempts), CreatedAt: r.CreatedAt}
		if r.TenantID.Valid {
			t := uuid.UUID(r.TenantID.Bytes)
			j.TenantID = &t
		}
		changed, status, err := resolveOne(ctx, d, h, j, r.FinishedAt.Time)
		switch {
		case err != nil:
			errs = append(errs, err)
			still++
		case changed && status == StatusDone:
			done++
		case changed && status == StatusNotSent:
			requeued++
		default:
			still++
		}
	}
	return done, requeued, still, errors.Join(errs...)
}

// resolveOne tra một lệnh rồi áp kết quả; changed = false khi giữ in_doubt hoặc lệnh đã đổi trong lúc tra.
func resolveOne(ctx context.Context, d *db.DB, h Handler, j queue.Job, seen time.Time) (bool, string, error) {
	status, err := lookup(ctx, h, j)
	if err != nil {
		return false, StatusUnknown, err
	}
	var fn hookFn
	switch status {
	case StatusDone:
		fn = doneFn(h, j, nil)
	case StatusNotSent:
	default:
		return false, status, nil
	}
	err = runTx(ctx, d, j.TenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		return apply(ctx, tx, fn, status, func() (int64, error) {
			if status == StatusDone {
				return q.ResolveInDoubtDone(ctx, store.ResolveInDoubtDoneParams{ID: j.ID, SeenFinishedAt: seen})
			}
			return q.ResolveInDoubtRequeue(ctx, store.ResolveInDoubtRequeueParams{ID: j.ID, SeenFinishedAt: seen})
		})
	})
	switch {
	case errors.Is(err, errLostLease):
		return false, status, nil
	case err != nil:
		return false, status, fmt.Errorf("áp kết quả tra lệnh %s (%s): %w", j.ID, status, err)
	}
	return true, status, nil
}

// lookup gọi Looker với hạn Policy.Timeout của loại (mặc định của hàng đợi khi 0).
func lookup(ctx context.Context, h Handler, j queue.Job) (string, error) {
	timeout := h.Kind().Policy.Timeout
	if timeout <= 0 {
		timeout = queue.DefaultPolicy.Timeout
	}
	lctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	status, err := h.(Looker).Lookup(lctx, j)
	if err != nil {
		return StatusUnknown, fmt.Errorf("tra lệnh %s: %w", j.ID, err)
	}
	return status, nil
}
