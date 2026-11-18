package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// defaultJobsLimit: số việc mặc định của listJobs (openapi.yaml: default 50, tối đa 200).
const defaultJobsLimit = 50

// ListJobs (GET /api/v1/platform/jobs, platform.jobs.manage): việc dead, in_doubt, expired của tenant người gọi
// (lọc theo status nếu có), mới nhất trước; không có payload.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListJobs(ctx context.Context, params gen.ListJobsParams) (gen.ListJobsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var statuses []string
	if s, ok := params.Status.Get(); ok {
		statuses = []string{string(s)}
	}
	limit := int(params.Limit.Or(defaultJobsLimit))
	var jobs []queue.JobSummary
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		jobs, err = queue.ListAttention(ctx, tx, u.TenantID, statuses, limit)
		return err //nolint:wrapcheck // queue đã bọc
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("liệt kê việc: %w", err)
	}
	out := make(gen.ListJobsOKApplicationJSON, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, *jobOut(j))
	}
	return &out, nil
}

// RetryJob (POST /api/v1/platform/jobs/{id}/retry, platform.jobs.manage) đưa việc dead, in_doubt hoặc expired về
// pending. Mẫu chuẩn của một đường ghi: khóa chống trùng (middleware idempotent) → WriteTx (một transaction tenant:
// đổi dòng, RecordAudit, lưu kết quả) → trả kết quả; lỗi đổi sang HTTP ở errors.go.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RetryJob(ctx context.Context, params gen.RetryJobParams) (gen.RetryJobRes, error) {
	return h.changeJob(ctx, params.ID, "platform.job.retry", queue.RetryJob)
}

// AbandonJob (POST /api/v1/platform/jobs/{id}/abandon, platform.jobs.manage) bỏ việc dead hoặc in_doubt (→ expired).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AbandonJob(ctx context.Context, params gen.AbandonJobParams) (gen.AbandonJobRes, error) {
	return h.changeJob(ctx, params.ID, "platform.job.abandon", queue.AbandonJob)
}

type jobChange func(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (before, after queue.JobSummary, err error)

// changeJob chạy change trong WriteTx kèm một dòng audit. Việc không có (hay của tenant khác) và trạng thái không cho
// phép là câu trả lời không đổi gì: không lưu vào khóa chống trùng (final = false), gửi lại cùng khóa thì xét lại.
func (h *handler) changeJob(ctx context.Context, id uuid.UUID, action string, change jobChange) (*gen.Job, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var (
		out     *gen.Job
		refused error
	)
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		before, after, err := change(ctx, tx, u.TenantID, id)
		switch {
		case errors.Is(err, queue.ErrJobNotFound), errors.Is(err, queue.ErrJobState):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err
		}
		if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
			TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: action,
			EntityType: "platform.job", EntityID: id.String(),
			Before:    map[string]any{"status": before.Status, "attempts": before.Attempts},
			After:     map[string]any{"status": after.Status, "attempts": after.Attempts},
			RequestID: RequestID(ctx),
		}); err != nil {
			return 0, nil, false, fmt.Errorf("ghi audit %s: %w", action, err)
		}
		out = jobOut(after)
		return http.StatusOK, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("%s %s: %w", action, id, refused)
	}
	return out, nil
}

// jobOut là Job của hợp đồng: không payload, không kết quả.
func jobOut(j queue.JobSummary) *gen.Job {
	out := &gen.Job{ID: j.ID, Kind: j.Kind, Status: j.Status, Attempts: int32(min(j.Attempts, 1<<31-1)), CreatedAt: j.CreatedAt} //nolint:gosec // đã chặn
	if j.LastError != "" {
		out.LastError = gen.NewOptString(j.LastError)
	}
	return out
}
