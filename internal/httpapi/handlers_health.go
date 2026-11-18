package httpapi

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// handler hiện thực gen.Handler. Operation chưa có handler trả ht.ErrNotImplemented (501) qua UnimplementedHandler;
// các task sau thêm từng operation. getPlatformHealth: cuối tệp này; healthDeep: Task 19. getMe: handlers_me.go;
// việc nền: handlers_jobs.go.
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

// opHealthDeep là operation tự kiểm thẻ HEALTH_TOKEN (công khai với Cloudflare Access, x-public trong hợp đồng).
const opHealthDeep = "healthDeep"

// HealthDeep: báo cáo kiểm sâu cho bộ kiểm từ ngoài (ADR-38); 200 khi đạt, 503 khi nhịp sống quá 90 s hoặc việc chờ
// quá 120 s. Thẻ đã kiểm ở healthToken. Chỉ tuổi và số đếm, không dữ liệu khách.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler): câu trả lời nhiều mã là interface
func (h *handler) HealthDeep(ctx context.Context) (gen.HealthDeepRes, error) {
	r, err := observability.DeepHealth(ctx, h.d.DB)
	if err != nil {
		return nil, err //nolint:wrapcheck // DeepHealth đã bọc; lỗi nội bộ đổi sang 500 ở writeHandlerError
	}
	body := gen.DeepHealth{
		Ok:                  r.OK,
		WorkerHeartbeatAgeS: optInt(r.WorkerHeartbeatAgeS),
		IngestHeartbeatAgeS: optInt(r.IngestHeartbeatAgeS),
		OldestPendingAgeS:   gen.NewOptInt64(r.OldestPendingAgeS),
		Dead:                gen.NewOptInt64(r.Dead),
		InDoubt:             gen.NewOptInt64(r.InDoubt),
		SpoolFiles:          gen.NewOptInt64(r.SpoolFiles),
		LastWalArchivedAgeS: optInt(r.LastWalArchivedAgeS),
		LastBackupAgeH:      optInt(r.LastBackupAgeH),
		LastFileSyncAgeM:    optInt(r.LastFileSyncAgeM),
	}
	if !r.OK {
		res := gen.HealthDeepServiceUnavailable(body)
		return &res, nil
	}
	res := gen.HealthDeepOK(body)
	return &res, nil
}

func optInt(v *int64) gen.OptInt64 {
	if v == nil {
		return gen.OptInt64{}
	}
	return gen.NewOptInt64(*v)
}

// healthToken cho qua khi Authorization là "Bearer <HEALTH_TOKEN>" (so hằng thời gian); thiếu, sai hay token cấu
// hình rỗng → 401 unauthenticated.
func healthToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeError(w, r, http.StatusUnauthorized, codeUnauthenticated)
			return
		}
		next.ServeHTTP(w, r)
	})
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
