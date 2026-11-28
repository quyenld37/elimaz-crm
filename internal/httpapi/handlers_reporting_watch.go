package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Theo dõi sát 14 ngày sau mở bán (Task M7-26 = 271, để sau).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetWatch (GET /api/v1/reports/watch).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetWatch(_ context.Context) (gen.GetWatchRes, error) {
	return nil, ht.ErrNotImplemented
}

// CloseWatch (POST /api/v1/reports/watch/close).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CloseWatch(_ context.Context, _ *gen.CloseWatchRequest, _ gen.CloseWatchParams) (gen.CloseWatchRes, error) {
	return nil, ht.ErrNotImplemented
}
