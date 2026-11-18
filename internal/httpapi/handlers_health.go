package httpapi

import (
	"context"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// handler hiện thực gen.Handler. Operation chưa có handler trả ht.ErrNotImplemented (501) qua UnimplementedHandler;
// các task sau thêm từng operation (Task 12 việc nền, Task 20 health). getMe: handlers_me.go.
type handler struct {
	gen.UnimplementedHandler

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
