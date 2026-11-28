package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
)

// expectNoToken: gọi route r ở path không thẻ (route ghi kèm Origin đúng, thân body) → 401 unauthenticated.
func expectNoToken(t *testing.T, srv http.Handler, r Route, path, body string) {
	t.Helper()
	headers := map[string]string{}
	if r.Write {
		headers = map[string]string{"Origin": testOrigin, "Content-Type": "application/json"}
	}
	req := httptest.NewRequestWithContext(t.Context(), r.Method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	expect(t, r.OperationID+" không thẻ", rec, http.StatusUnauthorized, "unauthenticated")
}

// contractKey trả khóa chống trùng mới "<prefix>-<n>" cho route ghi (tăng *n), rỗng cho route đọc.
func contractKey(r Route, prefix string, n *int) string {
	if !r.Write {
		return ""
	}
	*n++
	return fmt.Sprintf("%s-%d", prefix, *n)
}

// m6Body là thân hợp lệ tối thiểu của ba route ghi M6 (kế hoạch Task M6-02), để hợp đồng cho qua và handler trả 501.
func m6Body(operationID string) string {
	switch operationID {
	case "createSalesReportRun":
		return `{"report":"orders","format":"json","params":{"from":"2026-11-01","to":"2026-11-02"}}`
	case "createInventoryReportRun":
		return `{"report":"products","format":"xlsx","params":{}}`
	case "createCustomerExport":
		return `{"params":{}}`
	}
	return ""
}

// m7ReportPath: đường dưới /api/v1/reports của phần khởi động M7 (Task 260; kiểm ở TestM7Contract_StatusCodes).
func m7ReportPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/reports/launch-") || strings.HasPrefix(path, "/api/v1/reports/watch")
}

// Mọi route M6 (dưới /api/v1/reports, trừ đường M7) qua đúng auth và guard (kế hoạch Task M6-02 Step 1): không thẻ → 401; thẻ có
// audit.read mà không có quyền báo cáo → 403 forbidden; thẻ owner (route ghi kèm Idempotency-Key) → operation còn
// trong m6_pending.txt trả 501 not_implemented. Operation đã làm (không còn trong danh sách) mà vẫn 501 thì đỏ.
func TestM6Contract_StatusCodes(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	staff.Grant(t, tenant, staff.User(t, tenant, "chu-m6@elimaz.test", "active"), rolePerms(t, "owner"), "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "nhatky-m6@elimaz.test", "active"), []string{"audit.read"}, "all", "")
	srv := testServer(t, staff, keys, 0)
	pending := pendingOps(t, "m6_pending.txt")
	param := regexp.MustCompile(`\{[^}]+\}`)
	n, key := 0, 0
	for _, r := range Routes {
		_, path, _ := strings.Cut(r.Pattern, " ")
		if !strings.HasPrefix(path, "/api/v1/reports/") || m7ReportPath(path) {
			continue
		}
		n++
		path = param.ReplaceAllString(path, uuid.NewString())
		body := m6Body(r.OperationID)
		if !r.Write {
			// Tham số bắt buộc của các GET báo cáo (from, to, metric); route không khai thì bỏ qua.
			path += "?from=2026-11-01&to=2026-11-02&metric=placed"
		}
		expectNoToken(t, srv, r, path, body)
		next := func() string { return contractKey(r, "m6", &key) }
		expect(t, r.OperationID+" không quyền báo cáo", serveAs(srv, keys, r.Method, path, "nhatky-m6@elimaz.test", next(), body),
			http.StatusForbidden, "forbidden")
		rec := serveAs(srv, keys, r.Method, path, "chu-m6@elimaz.test", next(), body)
		switch {
		case pending[r.OperationID]:
			expect(t, r.OperationID+" owner (chưa làm)", rec, http.StatusNotImplemented, "not_implemented")
		case rec.Code == http.StatusNotImplemented:
			t.Errorf("%s trả 501 mà không còn trong m6_pending.txt", r.OperationID)
		}
	}
	if n != 20 {
		t.Errorf("thấy %d route M6 trong Routes, mong 20", n)
	}
}
