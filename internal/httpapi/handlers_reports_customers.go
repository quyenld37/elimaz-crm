package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng báo cáo: xuất danh sách khách (PQ06, Task M6-15 = 238).
// Khung của Task 225 (M6-02): mỗi operation trả 501 not_implemented (tests/arch/testdata/m6_pending.txt); task
// của vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// CreateCustomerExport (POST /api/v1/reports/customers/exports).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateCustomerExport(_ context.Context, _ *gen.CreateCustomerExportRequest, _ gen.CreateCustomerExportParams) (gen.CreateCustomerExportRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListCustomerExports (GET /api/v1/reports/customers/exports).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListCustomerExports(_ context.Context, _ gen.ListCustomerExportsParams) (gen.ListCustomerExportsRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetCustomerExport (GET /api/v1/reports/customers/exports/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetCustomerExport(_ context.Context, _ gen.GetCustomerExportParams) (gen.GetCustomerExportRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetCustomerExportFile (GET /api/v1/reports/customers/exports/{id}/file).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetCustomerExportFile(_ context.Context, _ gen.GetCustomerExportFileParams) (gen.GetCustomerExportFileRes, error) {
	return nil, ht.ErrNotImplemented
}
