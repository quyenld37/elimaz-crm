package arch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/httpapi"
)

// m6Operations là bảng operation của kế hoạch M6, Task 225 (M6-02; hợp đồng viết một lần cho cả mốc): operationId,
// "METHOD đường", quyền. Dòng gộp của bảng kế hoạch (bốn operation lần chạy của tồn kho, bốn của xuất danh sách khách)
// tách thành từng operation.
func m6Operations() [][3]string {
	return [][3]string{
		{"getSalesDashboard", "GET /api/v1/reports/sales/dashboard", "report.sales.read"},
		{"getSalesOrdersReport", "GET /api/v1/reports/sales/orders", "report.sales.read"},
		{"listSalesOrdersDrilldown", "GET /api/v1/reports/sales/orders/drilldown", "report.sales.read"},
		{"getSalesCarriersReport", "GET /api/v1/reports/sales/carriers", "report.sales.read"},
		{"getSalesBestSellers", "GET /api/v1/reports/sales/best-sellers", "report.sales.read"},
		{"getSalesReportFilters", "GET /api/v1/reports/sales/filters", "report.sales.read"},
		{"getInventoryStock", "GET /api/v1/reports/inventory/stock", "report.inventory.read"},
		{"getInventoryMovements", "GET /api/v1/reports/inventory/movements", "report.inventory.read"},
		{"createSalesReportRun", "POST /api/v1/reports/sales/runs", "report.sales.read"},
		{"listSalesReportRuns", "GET /api/v1/reports/sales/runs", "report.sales.read"},
		{"getSalesReportRun", "GET /api/v1/reports/sales/runs/{id}", "report.sales.read"},
		{"getSalesReportRunFile", "GET /api/v1/reports/sales/runs/{id}/file", "report.sales.read"},
		{"createInventoryReportRun", "POST /api/v1/reports/inventory/runs", "report.inventory.read"},
		{"listInventoryReportRuns", "GET /api/v1/reports/inventory/runs", "report.inventory.read"},
		{"getInventoryReportRun", "GET /api/v1/reports/inventory/runs/{id}", "report.inventory.read"},
		{"getInventoryReportRunFile", "GET /api/v1/reports/inventory/runs/{id}/file", "report.inventory.read"},
		{"createCustomerExport", "POST /api/v1/reports/customers/exports", "customer.export"},
		{"listCustomerExports", "GET /api/v1/reports/customers/exports", "customer.export"},
		{"getCustomerExport", "GET /api/v1/reports/customers/exports/{id}", "customer.export"},
		{"getCustomerExportFile", "GET /api/v1/reports/customers/exports/{id}/file", "customer.export"},
	}
}

// Mọi operation M6 có trong Routes đúng đường, quyền, cờ ghi; route ghi bắt buộc Idempotency-Key (ADR-41, không
// exempt); mọi route dưới /api/v1/reports (trừ đường đêm mở, theo dõi của M7) là của bảng M6 (không route lạ, không
// route công khai).
func TestM6Contract_Operations(t *testing.T) {
	t.Parallel()
	ops := m6Operations()
	if len(ops) != 20 {
		t.Fatalf("bảng M6 có 20 operation, bảng test có %d", len(ops))
	}
	checkContractOps(t, "M6", ops)
	writes := 0
	for _, r := range httpapi.Routes {
		if !strings.Contains(r.Pattern, " /api/v1/reports") || strings.Contains(r.Pattern, " /api/v1/reports/launch-") ||
			strings.Contains(r.Pattern, " /api/v1/reports/watch") {
			continue // đường M7 (Task 260) kiểm ở TestM7Contract_Operations
		}
		if !slices.ContainsFunc(ops, func(w [3]string) bool { return w[0] == r.OperationID }) {
			t.Errorf("route %s (%s) dưới /api/v1/reports không có trong bảng M6", r.OperationID, r.Pattern)
		}
		if r.Permission == "" {
			t.Errorf("route %s không khai quyền", r.OperationID)
		}
		if r.Write {
			writes++
		}
	}
	if writes != 3 {
		t.Errorf("M6 có 3 route ghi (tạo lần chạy bán hàng, tồn kho, xuất khách), có %d", writes)
	}
}

// m6PendingList đọc testdata/m6_pending.txt: operationId M6 còn trả 501 (mỗi dòng một mã; # là chú thích).
func m6PendingList(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "m6_pending.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ops = append(ops, line)
	}
	return ops
}

// Danh sách tạm trả 501 (testdata/m6_pending.txt) chỉ chứa operation M6, không trùng dòng. Task hiện thực operation
// xóa dòng của mình; đối chiếu 501 với danh sách: TestM6Contract_StatusCodes (internal/httpapi).
func TestM6Contract_PendingList(t *testing.T) {
	t.Parallel()
	m6 := map[string]bool{}
	for _, w := range m6Operations() {
		m6[w[0]] = true
	}
	seen := map[string]bool{}
	for _, op := range m6PendingList(t) {
		switch {
		case !m6[op]:
			t.Errorf("m6_pending.txt có %q không phải operation M6", op)
		case seen[op]:
			t.Errorf("m6_pending.txt có %q hai lần", op)
		}
		seen[op] = true
	}
}
