package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Báo cáo sẵn sàng dữ liệu trước đêm mở (Task M7-24 = 269).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetLaunchReadiness (GET /api/v1/reports/launch-readiness).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetLaunchReadiness(_ context.Context) (gen.GetLaunchReadinessRes, error) {
	return nil, ht.ErrNotImplemented
}
