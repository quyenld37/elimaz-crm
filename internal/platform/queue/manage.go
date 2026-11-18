package queue

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// Quản lý việc cần người xử lý (Task 12; spec 04 mục 5.1: màn "Sự kiện lỗi" xem, chạy lại hoặc bỏ). platform.jobs
// không RLS (spec 04 mục 11), nên mọi hàm ở đây nhận tenant của người gọi và lọc tường minh theo nó; tx phải là
// transaction tenancy.WithTenant của tenant đó (cùng transaction với audit và khóa chống trùng).

// Lỗi của phần quản lý việc.
var (
	// ErrJobNotFound: không có việc mang mã đó trong tenant của người gọi (việc của tenant khác cũng vậy).
	ErrJobNotFound = errors.New("queue: không có việc")
	// ErrJobState: việc không ở trạng thái cho phép thao tác (chạy lại: dead, in_doubt, expired; bỏ: dead, in_doubt).
	ErrJobState = errors.New("queue: trạng thái việc không cho phép thao tác này")
)

// AttentionStatuses là các trạng thái cần người xử lý (mặc định của ListAttention).
func AttentionStatuses() []string { return []string{StatusDead, StatusInDoubt, StatusExpired} }

// JobSummary là thông tin một việc cho người xem: không có payload, kết quả hay dấu vết.
type JobSummary struct {
	ID        uuid.UUID
	Kind      string
	Status    string
	Attempts  int
	LastError string
	CreatedAt time.Time
}

// ListAttention trả tối đa limit việc của tenant có trạng thái thuộc statuses (rỗng: AttentionStatuses), mới nhất trước.
func ListAttention(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, statuses []string, limit int) ([]JobSummary, error) {
	if len(statuses) == 0 {
		statuses = AttentionStatuses()
	}
	rows, err := store.New(tx).ListAttentionJobs(ctx, store.ListAttentionJobsParams{
		TenantID: db.NullUUID(&tenant), Statuses: statuses, Lim: int32(min(max(limit, 0), maxInt32)),
	})
	if err != nil {
		return nil, fmt.Errorf("liệt kê việc cần xử lý: %w", err)
	}
	out := make([]JobSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, summary(r.ID, r.Kind, r.Status, r.Attempts, r.LastError, r.CreatedAt))
	}
	return out, nil
}

// RetryJob đưa việc dead, in_doubt hoặc expired của tenant về pending, chạy ngay, attempts = 0, bỏ hạn sống (người
// quyết chạy lại, kể cả lệnh in_doubt: spec 04 mục 5.3 "nhân viên quyết"). Trả trạng thái trước và sau.
func RetryJob(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (before, after JobSummary, err error) {
	return changeJob(ctx, tx, tenant, id, []string{StatusDead, StatusInDoubt, StatusExpired},
		func(q *store.Queries) (JobSummary, error) {
			r, err := q.RequeueTenantJob(ctx, store.RequeueTenantJobParams{ID: id, TenantID: db.NullUUID(&tenant)})
			return summary(r.ID, r.Kind, r.Status, r.Attempts, r.LastError, r.CreatedAt), err
		})
}

// AbandonJob bỏ việc dead hoặc in_doubt của tenant (→ expired). Việc đang chạy hay còn chờ không bỏ được ở đây: lệnh
// đang chạy có thể đang ra ngoài, việc còn chờ chưa qua OnTerminal.
func AbandonJob(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID) (before, after JobSummary, err error) {
	return changeJob(ctx, tx, tenant, id, []string{StatusDead, StatusInDoubt},
		func(q *store.Queries) (JobSummary, error) {
			r, err := q.AbandonTenantJob(ctx, store.AbandonTenantJobParams{ID: id, TenantID: db.NullUUID(&tenant)})
			return summary(r.ID, r.Kind, r.Status, r.Attempts, r.LastError, r.CreatedAt), err
		})
}

// changeJob khóa dòng việc của tenant (ADR-46: jobs trước idempotency_keys), kiểm trạng thái thuộc allowed rồi chạy upd.
func changeJob(ctx context.Context, tx pgx.Tx, tenant, id uuid.UUID, allowed []string,
	upd func(*store.Queries) (JobSummary, error),
) (before, after JobSummary, err error) {
	q := store.New(tx)
	r, err := q.LockTenantJob(ctx, store.LockTenantJobParams{ID: id, TenantID: db.NullUUID(&tenant)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return before, after, ErrJobNotFound
	case err != nil:
		return before, after, fmt.Errorf("khóa việc %s: %w", id, err)
	}
	before = summary(r.ID, r.Kind, r.Status, r.Attempts, r.LastError, r.CreatedAt)
	if !slices.Contains(allowed, before.Status) {
		return before, after, fmt.Errorf("%w: %s", ErrJobState, before.Status)
	}
	if after, err = upd(q); err != nil {
		return before, after, fmt.Errorf("đổi việc %s: %w", id, err)
	}
	return before, after, nil
}

func summary(id uuid.UUID, kind, status string, attempts int32, lastError pgtype.Text, created time.Time) JobSummary {
	return JobSummary{ID: id, Kind: kind, Status: status, Attempts: int(attempts), LastError: lastError.String, CreatedAt: created}
}
