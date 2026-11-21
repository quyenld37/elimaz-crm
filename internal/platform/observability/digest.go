package observability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

// digestHour: tin tổng kết gửi từ 08:00 giờ Việt Nam (01:00 UTC; spec 04 mục 8 "mỗi sáng 8 giờ").
const digestHour = 8

// vietnam là giờ Việt Nam (UTC+7, không đổi giờ mùa hè); không cần tzdata trong image.
//
//nolint:gochecknoglobals // múi giờ cố định, chỉ đọc
var vietnam = time.FixedZone("ICT", 7*3600)

// DigestDue báo đã tới giờ gửi tin tổng kết của ngày (giờ Việt Nam) chứa now.
func DigestDue(now time.Time) bool { return now.In(vietnam).Hour() >= digestHour }

// SendDailyDigest gửi một tin normal, key "digest:<ngày giờ Việt Nam>": số cảnh báo 24 giờ qua theo key, số việc
// dead và in_doubt đang mở, tuổi bản sao lưu đầy đủ thành công cuối. Ngày đó đã có tin (alert_events có key) thì
// không gửi nữa.
func SendDailyDigest(ctx context.Context, d *db.DB, a Alerter, now time.Time) error {
	key := "digest:" + now.In(vietnam).Format(time.DateOnly)
	q := store.New(d.Pool)
	sent, err := q.AlertKeyExists(ctx, key)
	if err != nil {
		return fmt.Errorf("đọc tin tổng kết đã gửi: %w", err)
	}
	if sent {
		return nil
	}
	counts, err := q.AlertCountsSince(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return fmt.Errorf("đếm cảnh báo 24 giờ: %w", err)
	}
	open, err := q.OpenAttentionCounts(ctx)
	if err != nil {
		return fmt.Errorf("đếm việc dead, in_doubt: %w", err)
	}
	backup := "chưa có"
	// Tuổi theo đồng hồ PostgreSQL (scripts/backup ghi mốc bằng now()), không so với now của tiến trình (Task 445).
	ageS, err := q.LastBackupAgeSeconds(ctx, "base")
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("đọc lần sao lưu: %w", err)
	default:
		backup = fmt.Sprintf("%d giờ", int(math.Round(ageS/3600)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Tổng kết 24 giờ (%s)\n", now.In(vietnam).Format(time.DateOnly))
	if len(counts) == 0 {
		b.WriteString("Cảnh báo: không có\n")
	} else {
		b.WriteString("Cảnh báo:\n")
		for _, c := range counts {
			fmt.Fprintf(&b, "- %s: %d\n", c.Key, c.N)
		}
	}
	fmt.Fprintf(&b, "Việc đang mở: dead: %d, in_doubt: %d\n", open.Dead, open.InDoubt)
	fmt.Fprintf(&b, "Bản sao lưu đầy đủ cuối: sao lưu: %s", backup)
	if err := a.Notify(ctx, key, SeverityNormal, b.String()); err != nil {
		return fmt.Errorf("gửi tin tổng kết: %w", err)
	}
	return nil
}
