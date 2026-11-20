package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng callbacks (kế hoạch M3, Task M3-19): đơn giao thất bại cần gọi lại.
// Khung của Task M3-04: mỗi operation trả 501 not_implemented
// (tests/arch/testdata/m3_pending.txt); task của vùng thay thân hàm trong tệp này, không sửa tệp của vùng khác.

// ListCallbackTasks (GET /api/v1/callback-tasks).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListCallbackTasks(_ context.Context, _ gen.ListCallbackTasksParams) (gen.ListCallbackTasksRes, error) {
	return nil, ht.ErrNotImplemented
}

// RecordCallbackOutcome (POST /api/v1/callback-tasks/{id}/outcomes).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) RecordCallbackOutcome(_ context.Context, _ *gen.RecordCallbackOutcomeRequest, _ gen.RecordCallbackOutcomeParams) (gen.RecordCallbackOutcomeRes, error) {
	return nil, ht.ErrNotImplemented
}

// AssignCallbackTask (POST /api/v1/callback-tasks/{id}/assign).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) AssignCallbackTask(_ context.Context, _ *gen.AssignCallbackTaskRequest, _ gen.AssignCallbackTaskParams) (gen.AssignCallbackTaskRes, error) {
	return nil, ht.ErrNotImplemented
}
