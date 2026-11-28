package httpapi

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
)

// m7LaunchOps là operation phần khởi động M7 (kế hoạch Task M7-15 = Task 260; bảng đầy đủ: tests/arch/m7contract_test.go).
func m7LaunchOps() []string {
	return []string{
		"getCatalogLaunchReadiness", "publishProducts", "unpublishProducts",
		"createOpeningStocktake", "listOpeningStocktakes", "getOpeningStocktake", "postOpeningStocktake",
		"checkOpeningStocktake", "incrementStocktakeLine", "createStocktakeCountImport", "getStocktakeCountImport",
		"createLegacyReturn", "listLegacyReturns", "getLegacyReturn", "listUnmatchedCarrierEvents",
		"getT2Report", "goLivePage", "pageBackToReadOnly",
		"getFallbackTemplate", "createFallbackImport", "getFallbackImport",
		"getLaunchReadiness", "createLaunchRun", "listLaunchRuns", "getLaunchRun", "startLaunchStep", "finishLaunchStep",
		"evaluateLaunchCheckpoint", "decideLaunchRun", "openSales", "getWatch", "closeWatch",
	}
}

// m7Body là thân hợp lệ tối thiểu của route ghi M7 có thân, để hợp đồng cho qua và handler trả 501.
func m7Body(operationID string) string {
	id := `"` + uuid.NewString() + `"`
	switch operationID {
	case "publishProducts":
		return `{"allReady":true}`
	case "unpublishProducts":
		return `{"productIds":[` + id + `]}`
	case "createOpeningStocktake":
		return `{"warehouseId":` + id + `}`
	case "incrementStocktakeLine":
		return `{"code":"SKU-1","by":1}`
	case "createStocktakeCountImport":
		return `{"fileId":` + id + `,"mode":"replace"}`
	case "createLegacyReturn":
		return `{"warehouseId":` + id + `,"reference":"VTP12345678","lines":[{"variantId":` + id + `,"qty":1,"condition":"sellable"}]}`
	case "goLivePage":
		return `{"ruleKinds":["greeting"],"oldToolAutomationsOff":true}`
	case "pageBackToReadOnly":
		return `{"reason":"thử"}`
	case "createFallbackImport":
		return `{"fileId":` + id + `}`
	case "createLaunchRun":
		return `{"mode":"drill","plannedStartAt":"2026-12-01T13:00:00Z","plannedWindowMinutes":240,"deciderUserId":` + id + `}`
	case "finishLaunchStep":
		return `{"outcome":"done"}`
	case "decideLaunchRun":
		return `{"decision":"go"}`
	case "closeWatch":
		return `{"note":"đủ 14 ngày"}`
	}
	return ""
}

// m7Path thay tham số đường: {no} → 1, {key} → A, còn lại uuid ngẫu nhiên; route đọc thêm tham số truy vấn bắt buộc
// (from, to của báo cáo T2; route không khai thì bỏ qua).
func m7Path(r Route) string {
	_, path, _ := strings.Cut(r.Pattern, " ")
	path = strings.NewReplacer("{no}", "1", "{key}", "A").Replace(path)
	path = regexp.MustCompile(`\{[^}]+\}`).ReplaceAllStringFunc(path, func(string) string { return uuid.NewString() })
	if !r.Write {
		path += "?from=2026-11-01&to=2026-11-07"
	}
	return path
}

// Mọi route phần khởi động M7 qua đúng auth và guard (kế hoạch Task M7-15 Step 1): không thẻ → 401; thẻ chỉ có
// audit.read → 403 forbidden; thẻ owner (route ghi kèm Idempotency-Key, thân hợp lệ) → operation còn trong
// m7_pending.txt trả 501 not_implemented. Operation đã làm (không còn trong danh sách) mà vẫn 501 thì đỏ.
func TestM7Contract_StatusCodes(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	staff.Grant(t, tenant, staff.User(t, tenant, "chu-m7@elimaz.test", "active"), rolePerms(t, "owner"), "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "nhatky-m7@elimaz.test", "active"), []string{"audit.read"}, "all", "")
	srv := testServer(t, staff, keys, 0)
	pending := pendingOps(t, "m7_pending.txt")
	ops := map[string]bool{}
	for _, op := range m7LaunchOps() {
		ops[op] = true
	}
	n, key := 0, 0
	for _, r := range Routes {
		if !ops[r.OperationID] {
			continue
		}
		n++
		path, body := m7Path(r), m7Body(r.OperationID)
		expectNoToken(t, srv, r, path, body)
		next := func() string { return contractKey(r, "m7", &key) }
		expect(t, r.OperationID+" không quyền", serveAs(srv, keys, r.Method, path, "nhatky-m7@elimaz.test", next(), body),
			http.StatusForbidden, "forbidden")
		rec := serveAs(srv, keys, r.Method, path, "chu-m7@elimaz.test", next(), body)
		switch {
		case pending[r.OperationID]:
			expect(t, r.OperationID+" owner (chưa làm)", rec, http.StatusNotImplemented, "not_implemented")
		case rec.Code == http.StatusNotImplemented:
			t.Errorf("%s trả 501 mà không còn trong m7_pending.txt", r.OperationID)
		}
	}
	if n != len(ops) {
		t.Errorf("thấy %d route M7 trong Routes, mong %d", n, len(ops))
	}
}
