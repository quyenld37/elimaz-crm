package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Hộp chat: báo cáo chạy chỉ xem T2 (MO03, Task M7-21 = 266, để sau).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetT2Report (GET /api/v1/channels/{id}/t2-report).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetT2Report(_ context.Context, _ gen.GetT2ReportParams) (gen.GetT2ReportRes, error) {
	return nil, ht.ErrNotImplemented
}
