package httpapi

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// Vùng báo cáo: lần chạy nền và tệp xuất (BC04, BC05, Task M6-12 = 235, M6-14 = 237).
// Khung của Task 225 (M6-02): mỗi operation trả 501 not_implemented (tests/arch/testdata/m6_pending.txt); task
// của vùng thay thân hàm trong tệp này và xóa dòng của mình khỏi danh sách, không sửa tệp của vùng khác.

// CreateSalesReportRun (POST /api/v1/reports/sales/runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateSalesReportRun(_ context.Context, _ *gen.CreateReportRunRequest, _ gen.CreateSalesReportRunParams) (gen.CreateSalesReportRunRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListSalesReportRuns (GET /api/v1/reports/sales/runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListSalesReportRuns(_ context.Context, _ gen.ListSalesReportRunsParams) (gen.ListSalesReportRunsRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetSalesReportRun (GET /api/v1/reports/sales/runs/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetSalesReportRun(_ context.Context, _ gen.GetSalesReportRunParams) (gen.GetSalesReportRunRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetSalesReportRunFile (GET /api/v1/reports/sales/runs/{id}/file).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetSalesReportRunFile(_ context.Context, _ gen.GetSalesReportRunFileParams) (gen.GetSalesReportRunFileRes, error) {
	return nil, ht.ErrNotImplemented
}

// CreateInventoryReportRun (POST /api/v1/reports/inventory/runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) CreateInventoryReportRun(_ context.Context, _ *gen.CreateReportRunRequest, _ gen.CreateInventoryReportRunParams) (gen.CreateInventoryReportRunRes, error) {
	return nil, ht.ErrNotImplemented
}

// ListInventoryReportRuns (GET /api/v1/reports/inventory/runs).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) ListInventoryReportRuns(_ context.Context, _ gen.ListInventoryReportRunsParams) (gen.ListInventoryReportRunsRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetInventoryReportRun (GET /api/v1/reports/inventory/runs/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetInventoryReportRun(_ context.Context, _ gen.GetInventoryReportRunParams) (gen.GetInventoryReportRunRes, error) {
	return nil, ht.ErrNotImplemented
}

// GetInventoryReportRunFile (GET /api/v1/reports/inventory/runs/{id}/file).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (*handler) GetInventoryReportRunFile(_ context.Context, _ gen.GetInventoryReportRunFileParams) (gen.GetInventoryReportRunFileRes, error) {
	return nil, ht.ErrNotImplemented
}
