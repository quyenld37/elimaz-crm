package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Đơn: bảng tính đơn dự phòng (Task M7-23 = 268, để sau).
// Khung của Task 260 (M7-15): mỗi operation trả 501 not_implemented (tests/arch/testdata/m7_pending.txt); task của
// vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// GetFallbackTemplate (GET /api/v1/order-fallback/template).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetFallbackTemplate(_ context.Context) (gen.GetFallbackTemplateRes, error) {
	return nil, ht.ErrNotImplemented
}

// CreateFallbackImport (POST /api/v1/order-fallback/imports).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateFallbackImport(_ context.Context, _ *gen.CreateFallbackImportRequest, _ gen.CreateFallbackImportParams) (gen.CreateFallbackImportRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetFallbackImport (GET /api/v1/order-fallback/imports/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetFallbackImport(_ context.Context, _ gen.GetFallbackImportParams) (gen.GetFallbackImportRes, error) {
	return nil, ht.ErrNotImplemented
}
