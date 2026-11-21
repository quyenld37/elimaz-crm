package observability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

const (
	// summaryWindow: lỗi và cảnh báo của tóm tắt sức khỏe tính trong chừng này.
	summaryWindow = 24 * time.Hour
	// summaryMaxErrors: số loại lỗi tối đa trong tóm tắt.
	summaryMaxErrors = 10
	// summaryMaxAlerts: số cảnh báo tối đa trong tóm tắt (mới nhất trước; hợp đồng không giới hạn, chặn khi bão cảnh báo).
	summaryMaxAlerts = 200
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
// lần nhất gặp trong 24 giờ, sao lưu (backup_runs) và cảnh báo đã gửi trong 24 giờ (alert_events, trừ tin tổng kết).
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
	if err := backupAndAlerts(ctx, q, now, &s); err != nil {
		return s, err
	}
	return s, nil
}

// backupAndAlerts điền s.Backup (tuổi giờ của base thành công gần nhất; tuổi và ok của restore_drill gần nhất, kể cả
// khi hỏng) và s.Alerts. Trường sao lưu để nil khi chưa có lần nào. Tuổi sao lưu tính theo đồng hồ PostgreSQL
// (scripts/backup ghi mốc bằng now()), không so với now (Task 445).
func backupAndAlerts(ctx context.Context, q *store.Queries, now time.Time, s *HealthSummary) error {
	ageH := func(ageS float64) *int64 {
		h := floorNonNeg(ageS / 3600)
		return &h
	}
	base, err := q.LastBackupAgeSeconds(ctx, "base")
	switch {
	case err == nil:
		s.Backup.LastBaseAgeH = ageH(base)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("đọc lần sao lưu: %w", err)
	}
	drill, err := q.LastBackupRun(ctx, "restore_drill")
	switch {
	case err == nil:
		s.Backup.LastDrillAgeH = ageH(drill.AgeS)
		s.Backup.LastDrillOk = &drill.Ok
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("đọc lần diễn tập khôi phục: %w", err)
	}
	alerts, err := q.ListAlertsSince(ctx, store.ListAlertsSinceParams{Since: now.Add(-summaryWindow), MaxRows: summaryMaxAlerts})
	if err != nil {
		return fmt.Errorf("đọc cảnh báo đã gửi: %w", err)
	}
	for _, a := range alerts {
		s.Alerts = append(s.Alerts, AlertSummary{Key: a.Key, Severity: a.Severity, SentAt: a.SentAt.UTC()})
	}
	return nil
}

// floorNonNeg làm tròn xuống một số thực không âm thành int64.
func floorNonNeg(v float64) int64 { return int64(math.Max(0, math.Floor(v))) }
