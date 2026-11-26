package channel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// AcquireSendToken lấy một lượt gửi của tài khoản id lúc now từ thùng token trong PostgreSQL (04 mục 5.3; mặc định 50
// lượt, nạp 20 lượt/giây), trong một transaction ngắn riêng (không giữ khóa trong transaction nghiệp vụ). Kênh đang
// chặn gửi (blocked_until > now) → ok = false, retryAfter tới mốc đó; hết lượt → ok = false, retryAfter = thời gian
// nạp đủ một lượt. Lấy cùng lúc từ nhiều tiến trình không bao giờ vượt số lượt có (một câu UPDATE có điều kiện).
// Không có tài khoản → ErrAccountNotFound. Thứ tự (ADR-46 nhóm 3): chỉ đọc channel_accounts, rồi ghi send_buckets.
func AcquireSendToken(ctx context.Context, d *db.DB, tenantID, id uuid.UUID, now time.Time) (ok bool, retryAfter time.Duration, err error) {
	now = now.UTC()
	err = tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
		ok, retryAfter = false, 0 // fn có thể chạy lại: gán lại mỗi lần
		q := store.New(tx)
		blocked, err := q.GetBlockedUntil(ctx, id)
		if err != nil {
			return notFound(err, "đọc mốc chặn gửi")
		}
		if blocked != nil && blocked.After(now) {
			retryAfter = blocked.Sub(now)
			return nil
		}
		key := store.EnsureBucketParams{TenantID: tenantID, ChannelAccountID: id, Now: now}
		if err := q.EnsureBucket(ctx, key); err != nil {
			return fmt.Errorf("tạo thùng lượt gửi: %w", err)
		}
		_, err = q.TakeToken(ctx, store.TakeTokenParams{Now: now, TenantID: tenantID, ChannelAccountID: id})
		switch {
		case err == nil:
			ok = true
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("lấy lượt gửi: %w", err)
		}
		p, err := q.PeekBucket(ctx, store.PeekBucketParams{Now: now, TenantID: tenantID, ChannelAccountID: id})
		if err != nil {
			return fmt.Errorf("đọc thùng lượt gửi: %w", err)
		}
		retryAfter = max(0, time.Duration(math.Ceil((1-p.Available)/p.RefillPerSec*float64(time.Second))))
		return nil
	})
	if err != nil {
		return false, 0, err //nolint:wrapcheck // lỗi của fn đã bọc, ErrAccountNotFound là sentinel
	}
	return ok, retryAfter, nil
}
