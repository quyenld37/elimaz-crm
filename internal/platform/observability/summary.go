package observability

import (
	"context"
	"fmt"
	"math"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

const (
	// summaryWindow: lỗi và cảnh báo của tóm tắt sức khỏe tính trong chừng này.
	summaryWindow = 24 * time.Hour
	// summaryMaxErrors: số loại lỗi tối đa trong tóm tắt.
	summaryMaxErrors = 10
	// spoolWindow: gauge spool_files của tiến trình ingest không ghi trong chừng này thì bỏ (tiến trình đã dừng).
	spoolWindow = 10 * time.Minute
)

// HealthSummary là tình trạng của cả hệ thống cho GET /api/v1/platform/health. Không chứa payload của việc hay nội
// dung lỗi chưa lọc.
type HealthSummary struct {
	Processes []ProcessHealth
	Queue     QueueHealth
	Ingest    IngestHealth
	Backup    BackupHealth
	Errors    []ErrorSummary
	Alerts    []AlertSummary
}

// ProcessHealth là tuổi nhịp sống (giây) của một tiến trình.
type ProcessHealth struct {
	Role              string
	LastHeartbeatAgeS int64
}

// QueueHealth là tuổi việc chờ đến hạn cũ nhất (giây) và số việc pending, dead, in_doubt.
type QueueHealth struct {
	OldestPendingAgeS, Pending, Dead, InDoubt int64
}

// IngestHealth là tổng số tệp tạm chưa nạp của mọi tiến trình ingest (giá trị cuối của gauge spool_files).
type IngestHealth struct {
	SpoolFiles int64
}

// BackupHealth là tuổi (giờ) bản sao lưu base và lần diễn tập khôi phục gần nhất; nil khi chưa có.
type BackupHealth struct {
	LastBaseAgeH, LastDrillAgeH *int64
	LastDrillOk                 *bool
}

// ErrorSummary là một loại lỗi trong platform.error_events; Message đã qua ScrubMessage.
type ErrorSummary struct {
	Fingerprint, Name, Role, Message string
	Count                            int64
	FirstSeenAt, LastSeenAt          time.Time
}

// AlertSummary là một cảnh báo đã gửi.
type AlertSummary struct {
	Key, Severity string
	SentAt        time.Time
}

// Summary đọc tóm tắt sức khỏe tại now: nhịp sống mọi tiến trình, hàng đợi, tệp tạm của ingest, 10 loại lỗi nhiều
// lần nhất gặp trong 24 giờ. Sao lưu và cảnh báo đọc từ bảng của Task 19 (backup_runs, alert_events) khi có.
func Summary(ctx context.Context, d *db.DB, now time.Time) (HealthSummary, error) {
	q := store.New(d.Pool)
	s := HealthSummary{Processes: []ProcessHealth{}, Errors: []ErrorSummary{}, Alerts: []AlertSummary{}}

	beats, err := q.ListHeartbeatAges(ctx)
	if err != nil {
		return s, fmt.Errorf("đọc nhịp sống: %w", err)
	}
	for _, b := range beats {
		s.Processes = append(s.Processes, ProcessHealth{Role: b.Role, LastHeartbeatAgeS: floorNonNeg(b.AgeSeconds)})
	}

	counts, err := q.CountJobsByAttention(ctx)
	if err != nil {
		return s, fmt.Errorf("đếm việc: %w", err)
	}
	oldest, err := q.OldestPendingAgeSeconds(ctx)
	if err != nil {
		return s, fmt.Errorf("đọc tuổi việc chờ: %w", err)
	}
	s.Queue = QueueHealth{OldestPendingAgeS: floorNonNeg(oldest), Pending: counts.Pending, Dead: counts.Dead, InDoubt: counts.InDoubt}

	spools, err := q.ListLatestSpoolFiles(ctx, now.Add(-spoolWindow))
	if err != nil {
		return s, fmt.Errorf("đọc tệp tạm của ingest: %w", err)
	}
	for _, sp := range spools {
		s.Ingest.SpoolFiles += floorNonNeg(sp.Files)
	}

	errs, err := q.ListTopErrors(ctx, store.ListTopErrorsParams{Since: now.Add(-summaryWindow), MaxRows: summaryMaxErrors})
	if err != nil {
		return s, fmt.Errorf("đọc lỗi ứng dụng: %w", err)
	}
	for _, e := range errs {
		s.Errors = append(s.Errors, ErrorSummary{
			Fingerprint: e.Fingerprint, Name: e.Name, Role: e.Role, Message: e.Message, Count: e.Count,
			FirstSeenAt: e.FirstSeenAt.UTC(), LastSeenAt: e.LastSeenAt.UTC(),
		})
	}
	return s, nil
}

// floorNonNeg làm tròn xuống một số thực không âm thành int64.
func floorNonNeg(v float64) int64 { return int64(math.Max(0, math.Floor(v))) }
