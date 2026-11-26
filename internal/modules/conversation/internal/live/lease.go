package live

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
)

// Khóa thuê của luồng đọc comment live (Q3, 04 mục 6): đúng một worker giữ một phiên; worker chết thì khóa hết hạn sau
// LeaseTTL và bộ quét (mỗi ScanEvery) của worker khác nhận lại, nên luồng mở lại trong LeaseTTL + ScanEvery = 15 s
// (LV10); tắt êm nhả khóa ngay. Luồng mở lại hỏi bù từ last_comment_at − Overlap.
const (
	LeaseTTL   = 10 * time.Second
	RenewEvery = 3 * time.Second
	ScanEvery  = 5 * time.Second
	Overlap    = 5 * time.Second
	// claimBatch là số phiên tối đa một lần quét nhận cho một tenant.
	claimBatch = 10
)

// ErrLeaseLost là lỗi: worker không còn giữ khóa của phiên (worker khác đã nhận) — luồng phải dừng.
var ErrLeaseLost = errors.New("live: đã mất khóa thuê của phiên")

// MaxReopenDelay là thời gian lâu nhất từ lúc worker giữ phiên chết tới lúc worker khác nhận lại: khóa hết hạn sau ttl,
// bộ quét gặp nó trong một chu kỳ scan.
func MaxReopenDelay(ttl, scan time.Duration) time.Duration { return ttl + scan }

func intervalOf(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

// claim nhận tối đa claimBatch phiên live hết khóa cho owner, hạn ttl (theo đồng hồ database).
func claim(ctx context.Context, tx pgx.Tx, owner string, ttl time.Duration) ([]Session, error) {
	rows, err := store.New(tx).ClaimLiveSessions(ctx, store.ClaimLiveSessionsParams{Owner: owner, Ttl: intervalOf(ttl), Lim: claimBatch})
	if err != nil {
		return nil, fmt.Errorf("nhận phiên live: %w", err)
	}
	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, sessionFrom(r))
	}
	return out, nil
}

// renew gia hạn khóa của owner trên phiên id; held = false khi đã mất khóa; status là trạng thái phiên.
func renew(ctx context.Context, tx pgx.Tx, id uuid.UUID, owner string, ttl time.Duration) (status string, held bool, err error) {
	status, err = store.New(tx).RenewLiveLease(ctx, store.RenewLiveLeaseParams{Ttl: intervalOf(ttl), ID: id, Owner: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("gia hạn khóa phiên live: %w", err)
	}
	return status, true, nil
}

// release nhả khóa của owner trên phiên id (khóa đã thuộc worker khác thì không làm gì).
func release(ctx context.Context, tx pgx.Tx, id uuid.UUID, owner string) error {
	if err := store.New(tx).ReleaseLiveLease(ctx, store.ReleaseLiveLeaseParams{ID: id, Owner: owner}); err != nil {
		return fmt.Errorf("nhả khóa phiên live: %w", err)
	}
	return nil
}

// record ghi một lô comment của phiên id: last (nếu có) đẩy mốc comment mới nhất, n việc mới cộng vào received_count;
// owner không còn giữ khóa → ErrLeaseLost (bên gọi lùi cả lô).
func record(ctx context.Context, tx pgx.Tx, id uuid.UUID, owner string, last *time.Time, n int) error {
	p := store.RecordLiveCommentsParams{ID: id, Owner: owner, N: int32(n)} //nolint:gosec // một lô vài trăm comment
	if last != nil {
		p.LastCommentAt = tsOf(*last)
	}
	rows, err := store.New(tx).RecordLiveComments(ctx, p)
	if err != nil {
		return fmt.Errorf("ghi lô comment live: %w", err)
	}
	if rows == 0 {
		return ErrLeaseLost
	}
	return nil
}
