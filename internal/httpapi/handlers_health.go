package httpapi

import (
	"context"
	"fmt"
	"time"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// handler hiện thực gen.Handler. Operation chưa có handler trả ht.ErrNotImplemented (501) qua UnimplementedHandler;
// các task sau thêm từng operation. getPlatformHealth: cuối tệp này. getMe: handlers_me.go; việc nền: handlers_jobs.go.
type handler struct {
	gen.UnimplementedHandler

	d      Deps
	health *observability.Health
}

// HealthLive: tiến trình còn sống.
func (h *handler) HealthLive(context.Context) (*gen.HealthStatus, error) {
	return &gen.HealthStatus{Status: gen.HealthStatusStatusOk}, nil
}

// HealthReady: 200 khi database trả lời trong 1 s, ngược lại 503; không nêu lỗi gốc.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler): câu trả lời nhiều mã là interface
func (h *handler) HealthReady(ctx context.Context) (gen.HealthReadyRes, error) {
	if err := h.health.Check(ctx); err != nil {
		return &gen.HealthReadyServiceUnavailable{
			Status: gen.HealthStatusStatusUnavailable,
			Checks: gen.NewOptHealthStatusChecks(gen.HealthStatusChecks{"db": "down"}),
		}, nil
	}
	return &gen.HealthReadyOK{Status: gen.HealthStatusStatusOk}, nil
}

// GetPlatformHealth: tóm tắt sức khỏe của cả hệ thống (platform.health.read, guard đã kiểm). Không có payload của việc
// hay nội dung lỗi chưa lọc.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetPlatformHealth(ctx context.Context) (gen.GetPlatformHealthRes, error) {
	s, err := observability.Summary(ctx, h.d.DB, time.Now())
	if err != nil {
		return nil, fmt.Errorf("tóm tắt sức khỏe: %w", err)
	}
	out := &gen.PlatformHealth{
		Processes: make([]gen.ProcessHealth, 0, len(s.Processes)),
		Queue: gen.QueueHealth{
			OldestPendingAgeS: s.Queue.OldestPendingAgeS, Pending: s.Queue.Pending, Dead: s.Queue.Dead, InDoubt: s.Queue.InDoubt,
		},
		Ingest: gen.IngestHealth{SpoolFiles: s.Ingest.SpoolFiles},
		Errors: make([]gen.ErrorSummary, 0, len(s.Errors)),
		Alerts: make([]gen.AlertSummary, 0, len(s.Alerts)),
	}
	for _, p := range s.Processes {
		out.Processes = append(out.Processes, gen.ProcessHealth{Role: p.Role, LastHeartbeatAgeS: p.LastHeartbeatAgeS})
	}
	if s.Backup.LastBaseAgeH != nil {
		out.Backup.LastBaseAgeH = gen.NewOptInt64(*s.Backup.LastBaseAgeH)
	}
	if s.Backup.LastDrillAgeH != nil {
		out.Backup.LastDrillAgeH = gen.NewOptInt64(*s.Backup.LastDrillAgeH)
	}
	if s.Backup.LastDrillOk != nil {
		out.Backup.LastDrillOk = gen.NewOptBool(*s.Backup.LastDrillOk)
	}
	for _, e := range s.Errors {
		out.Errors = append(out.Errors, gen.ErrorSummary{
			Fingerprint: e.Fingerprint, Name: e.Name, Role: e.Role, Message: e.Message, Count: e.Count,
			FirstSeenAt: e.FirstSeenAt, LastSeenAt: e.LastSeenAt,
		})
	}
	for _, a := range s.Alerts {
		out.Alerts = append(out.Alerts, gen.AlertSummary{Key: a.Key, Severity: gen.AlertSummarySeverity(a.Severity), SentAt: a.SentAt})
	}
	return out, nil
}
