package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Sổ đêm mở: bảy bước, mốc đi/dừng, người quyết (MO04, MO05, Task M7-25 = 270, để sau).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// CreateLaunchRun (POST /api/v1/reports/launch-runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateLaunchRun(_ context.Context, _ *gen.CreateLaunchRunRequest, _ gen.CreateLaunchRunParams) (gen.CreateLaunchRunRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListLaunchRuns (GET /api/v1/reports/launch-runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListLaunchRuns(_ context.Context, _ gen.ListLaunchRunsParams) (gen.ListLaunchRunsRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetLaunchRun (GET /api/v1/reports/launch-runs/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetLaunchRun(_ context.Context, _ gen.GetLaunchRunParams) (gen.GetLaunchRunRes, error) {
	return nil, ht.ErrNotImplemented
}

// StartLaunchStep (POST /api/v1/reports/launch-runs/{id}/steps/{no}/start).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) StartLaunchStep(_ context.Context, _ gen.StartLaunchStepParams) (gen.StartLaunchStepRes, error) {
	return nil, ht.ErrNotImplemented
}

// FinishLaunchStep (POST /api/v1/reports/launch-runs/{id}/steps/{no}/finish).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) FinishLaunchStep(_ context.Context, _ *gen.FinishLaunchStepRequest, _ gen.FinishLaunchStepParams) (gen.FinishLaunchStepRes, error) {
	return nil, ht.ErrNotImplemented
}

// EvaluateLaunchCheckpoint (POST /api/v1/reports/launch-runs/{id}/checkpoints/{key}/evaluate).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) EvaluateLaunchCheckpoint(_ context.Context, _ gen.EvaluateLaunchCheckpointParams) (gen.EvaluateLaunchCheckpointRes, error) {
	return nil, ht.ErrNotImplemented
}

// DecideLaunchRun (POST /api/v1/reports/launch-runs/{id}/decision).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) DecideLaunchRun(_ context.Context, _ *gen.DecideLaunchRunRequest, _ gen.DecideLaunchRunParams) (gen.DecideLaunchRunRes, error) {
	return nil, ht.ErrNotImplemented
}

// OpenSales (POST /api/v1/reports/launch-runs/{id}/open-sales).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) OpenSales(_ context.Context, _ gen.OpenSalesParams) (gen.OpenSalesRes, error) {
	return nil, ht.ErrNotImplemented
}
