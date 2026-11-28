package httpapi

import (
	"context"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/reporting"
)

// Vùng báo cáo: xuất danh sách khách (PQ06, Task M6-15 = 238). Bốn operation dùng lại đường lần chạy nền của
// handlers_reports_runs.go với họ customers, báo cáo customers, luôn xlsx: tạo (WriteTx, chống trùng, audit
// customer.export_requested), xem, liệt kê (người tạo hoặc report.exports.manage), đường dẫn tải (audit
// customer.export_downloaded mỗi lần gọi). Quyền customer.export (toàn shop) kiểm ở guard theo x-permission và ở module.

// CreateCustomerExport (POST /api/v1/reports/customers/exports).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCustomerExport(ctx context.Context, req *gen.CreateCustomerExportRequest, _ gen.CreateCustomerExportParams) (gen.CreateCustomerExportRes, error) {
	return h.createReportRun(ctx, reporting.FamilyCustomers, &gen.CreateReportRunRequest{
		Report: gen.ReportKeyCustomers, Format: gen.CreateReportRunRequestFormatXlsx, Params: req.Params,
	})
}

// ListCustomerExports (GET /api/v1/reports/customers/exports).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCustomerExports(ctx context.Context, params gen.ListCustomerExportsParams) (gen.ListCustomerExportsRes, error) {
	return h.listReportRuns(ctx, reporting.FamilyCustomers, params.Cursor, params.Limit)
}

// GetCustomerExport (GET /api/v1/reports/customers/exports/{id}).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomerExport(ctx context.Context, params gen.GetCustomerExportParams) (gen.GetCustomerExportRes, error) {
	return h.getReportRun(ctx, reporting.FamilyCustomers, params.ID)
}

// GetCustomerExportFile (GET /api/v1/reports/customers/exports/{id}/file).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetCustomerExportFile(ctx context.Context, params gen.GetCustomerExportFileParams) (gen.GetCustomerExportFileRes, error) {
	return h.reportRunFile(ctx, reporting.FamilyCustomers, params.ID)
}
