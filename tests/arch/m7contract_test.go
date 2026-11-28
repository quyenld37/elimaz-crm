package arch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/httpapi"
)

// m7Operations là bảng operation phần khởi động của kế hoạch M7, Task 260 (M7-15; hợp đồng viết một lần cho Task
// 261–271): operationId, "METHOD đường", quyền. Ba operation đọc thêm so với bảng kế hoạch (listOpeningStocktakes,
// getOpeningStocktake: màn kiểm kho mở sổ cần "còn N dòng chưa đếm" từ máy chủ; getLegacyReturn: 409
// legacy_return_exists dẫn tới phiếu đã có) ghi ở mục Khác kế hoạch của docs/tasks/task-260.md.
func m7Operations() [][3]string {
	return [][3]string{
		{"getCatalogLaunchReadiness", "GET /api/v1/catalog/launch/readiness", "catalog.read"},
		{"publishProducts", "POST /api/v1/catalog/launch/publish", "catalog.launch.publish"},
		{"unpublishProducts", "POST /api/v1/catalog/launch/unpublish", "catalog.launch.publish"},
		{"createOpeningStocktake", "POST /api/v1/inventory/opening-stocktakes", "inventory.stocktake.opening"},
		{"listOpeningStocktakes", "GET /api/v1/inventory/opening-stocktakes", "inventory.read"},
		{"getOpeningStocktake", "GET /api/v1/inventory/opening-stocktakes/{id}", "inventory.read"},
		{"postOpeningStocktake", "POST /api/v1/inventory/opening-stocktakes/{id}/post", "inventory.stocktake.opening"},
		{"checkOpeningStocktake", "GET /api/v1/inventory/opening-stocktakes/{id}/check", "inventory.stocktake.opening"},
		{"incrementStocktakeLine", "POST /api/v1/inventory/stocktakes/{id}/increments", "inventory.stocktake"},
		{"createStocktakeCountImport", "POST /api/v1/inventory/stocktakes/{id}/count-imports", "inventory.stocktake"},
		{"getStocktakeCountImport", "GET /api/v1/inventory/count-imports/{id}", "inventory.stocktake"},
		{"createLegacyReturn", "POST /api/v1/inventory/legacy-returns", "inventory.legacy_return.create"},
		{"listLegacyReturns", "GET /api/v1/inventory/legacy-returns", "inventory.legacy_return.create"},
		{"getLegacyReturn", "GET /api/v1/inventory/legacy-returns/{id}", "inventory.legacy_return.create"},
		{"listUnmatchedCarrierEvents", "GET /api/v1/fulfillment/unmatched-events", "fulfillment.unmatched.read"},
		{"getT2Report", "GET /api/v1/channels/{id}/t2-report", "conversation.t2.read"},
		{"goLivePage", "POST /api/v1/channels/{id}/go-live", "channel.golive"},
		{"pageBackToReadOnly", "POST /api/v1/channels/{id}/back-to-read-only", "channel.golive"},
		{"getFallbackTemplate", "GET /api/v1/order-fallback/template", "order.fallback.import"},
		{"createFallbackImport", "POST /api/v1/order-fallback/imports", "order.fallback.import"},
		{"getFallbackImport", "GET /api/v1/order-fallback/imports/{id}", "order.fallback.import"},
		{"getLaunchReadiness", "GET /api/v1/reports/launch-readiness", "reporting.launch.read"},
		{"createLaunchRun", "POST /api/v1/reports/launch-runs", "reporting.launch.decide"},
		{"listLaunchRuns", "GET /api/v1/reports/launch-runs", "reporting.launch.read"},
		{"getLaunchRun", "GET /api/v1/reports/launch-runs/{id}", "reporting.launch.read"},
		{"startLaunchStep", "POST /api/v1/reports/launch-runs/{id}/steps/{no}/start", "reporting.launch.run"},
		{"finishLaunchStep", "POST /api/v1/reports/launch-runs/{id}/steps/{no}/finish", "reporting.launch.run"},
		{"evaluateLaunchCheckpoint", "POST /api/v1/reports/launch-runs/{id}/checkpoints/{key}/evaluate", "reporting.launch.run"},
		{"decideLaunchRun", "POST /api/v1/reports/launch-runs/{id}/decision", "reporting.launch.decide"},
		{"openSales", "POST /api/v1/reports/launch-runs/{id}/open-sales", "reporting.launch.decide"},
		{"getWatch", "GET /api/v1/reports/watch", "reporting.launch.read"},
		{"closeWatch", "POST /api/v1/reports/watch/close", "reporting.launch.decide"},
	}
}

// m7Prefixes: tiền tố đường chỉ của M7 (mọi route dưới đây phải có trong bảng M7).
func m7Prefixes() []string {
	return []string{
		"/api/v1/catalog/launch/", "/api/v1/inventory/opening-stocktakes", "/api/v1/inventory/count-imports/",
		"/api/v1/inventory/legacy-returns", "/api/v1/fulfillment/unmatched-events", "/api/v1/order-fallback/",
		"/api/v1/reports/launch-", "/api/v1/reports/watch",
	}
}

// Mọi operation M7 có trong Routes đúng đường, quyền, cờ ghi; route ghi bắt buộc Idempotency-Key (ADR-41, không
// exempt, không công khai); route dưới tiền tố của M7 đều thuộc bảng M7.
func TestM7Contract_Operations(t *testing.T) {
	t.Parallel()
	ops := m7Operations()
	if len(ops) != 32 {
		t.Fatalf("bảng M7 có 32 operation, bảng test có %d", len(ops))
	}
	checkContractOps(t, "M7", ops)
	writes := 0
	for _, r := range httpapi.Routes {
		inTable := slices.ContainsFunc(ops, func(w [3]string) bool { return w[0] == r.OperationID })
		_, path, _ := strings.Cut(r.Pattern, " ")
		if !inTable && slices.ContainsFunc(m7Prefixes(), func(p string) bool { return strings.HasPrefix(path, p) }) {
			t.Errorf("route %s (%s) dưới tiền tố M7 không có trong bảng M7", r.OperationID, r.Pattern)
		}
		if !inTable {
			continue
		}
		if r.Public || r.Authenticated || r.Auth != "access" {
			t.Errorf("route %s phải là route nhân viên có quyền (không công khai, không chỉ đăng nhập), có Auth %q", r.OperationID, r.Auth)
		}
		if r.Write {
			writes++
		}
	}
	if writes != 17 {
		t.Errorf("M7 có 17 route ghi, có %d", writes)
	}
}

// m7PendingList đọc testdata/m7_pending.txt: operationId M7 còn trả 501 (mỗi dòng một mã; # là chú thích).
func m7PendingList(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "m7_pending.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var ops []string
	for line := range strings.SplitSeq(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			ops = append(ops, line)
		}
	}
	return ops
}

// Danh sách tạm trả 501 (testdata/m7_pending.txt) chỉ chứa operation M7, không trùng dòng. Task hiện thực operation
// xóa dòng của mình; đối chiếu 501 với danh sách: TestM7Contract_StatusCodes (internal/httpapi).
func TestM7Contract_PendingList(t *testing.T) {
	t.Parallel()
	m7 := map[string]bool{}
	for _, w := range m7Operations() {
		m7[w[0]] = true
	}
	seen := map[string]bool{}
	for _, op := range m7PendingList(t) {
		switch {
		case !m7[op]:
			t.Errorf("m7_pending.txt có %q không phải operation M7", op)
		case seen[op]:
			t.Errorf("m7_pending.txt có %q hai lần", op)
		}
		seen[op] = true
	}
}

// Bảng thứ tự khóa sau khi M7 chèn (kế hoạch Task M7-15, C12, C17, R4): nhóm 6 có conversation.delivery_audits cuối
// nhóm; nhóm 7 có ord.fallback_imports → ord.fallback_import_rows cuối nhóm; nhóm 8 có
// fulfillment.unmatched_carrier_events cuối nhóm; nhóm 11 đúng dòng của M1 có thêm legacy_returns ngay sau
// stock_document_lines và count_imports ngay sau stocktake_lines (giữ inventory.suppliers); nhóm 12a có bốn bảng
// reporting của M7 cuối nhóm, chỉ một dòng 12a và không bảng reporting nào ở nhóm khác.
func TestM7Contract_LockOrder(t *testing.T) {
	t.Parallel()
	codes, tables := lockGroups(t)
	last := func(code string, want ...string) {
		got := tables[code]
		if len(got) < len(want) || !slices.Equal(got[len(got)-len(want):], want) {
			t.Errorf("nhóm %s: mong %v ở cuối nhóm, có %v", code, want, got)
		}
	}
	last("6", "conversation.delivery_audits")
	last("7", "ord.fallback_imports", "ord.fallback_import_rows")
	last("8", "fulfillment.unmatched_carrier_events")
	last("12a", "reporting.launch_runs", "reporting.launch_steps", "reporting.launch_checkpoints", "reporting.watch_days")
	inv := []string{
		"inventory.warehouses", "inventory.suppliers", "inventory.stock_documents", "inventory.stock_document_lines",
		"inventory.legacy_returns", "inventory.stocktakes", "inventory.stocktake_lines", "inventory.count_imports",
		"inventory.stock_balances", "inventory.stock_reservations", "inventory.flash_sale_units", "inventory.reconcile_runs",
	}
	if !slices.Equal(tables["11"], inv) {
		t.Errorf("nhóm 11: mong %v, có %v", inv, tables["11"])
	}
	if n := slices.Index(codes, "12a"); n < 0 || slices.Index(codes[n+1:], "12a") >= 0 {
		t.Errorf("mã nhóm 12a phải có đúng một dòng: %v", codes)
	}
	for code, ts := range tables {
		for _, tb := range ts {
			if strings.HasPrefix(tb, "reporting.") && code != "12a" {
				t.Errorf("bảng %s ở nhóm %s, mọi bảng reporting thuộc 12a", tb, code)
			}
		}
	}
}

// Dòng docs/views.md của hai view mới M7 (Task 261, 262 tạo; test viewsdoc bỏ qua tới khi migration tạo) đúng cột của
// kế hoạch. Dòng channel.channel_accounts_v không đổi ở đây (live_since nối ở Task 267 cùng 0709, R13).
func TestM7Contract_ViewsDoc(t *testing.T) {
	t.Parallel()
	doc := readViewsDoc(t, repoRoot(t))
	want := map[string]string{
		"catalog.product_readiness_v": "product_id, status, ready, reasons",
		"inventory.opening_status_v": "warehouse_id, stocktake_id, status, lines_expected, lines_counted, units_counted, " +
			"applied_at",
	}
	checkViewsDocCols(t, doc, want)
	if slices.Contains(doc.views["channel.channel_accounts_v"], "live_since") {
		t.Error("channel.channel_accounts_v: live_since chỉ nối ở Task 267 cùng migration 0709 (R13)")
	}
}
