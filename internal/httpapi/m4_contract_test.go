package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
)

// pendingOps đọc một danh sách chờ 501 trong tests/arch/testdata (mỗi dòng một operationId; # là chú thích).
func pendingOps(t *testing.T, file string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "arch", "testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for line := range strings.SplitSeq(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out[line] = true
		}
	}
	return out
}

// m4Permission: quyền chỉ M4 khai (kế hoạch M4, Task 143).
func m4Permission(p string) bool {
	return p == "channel.manage" || p == "live.operate" || strings.HasPrefix(p, "conversation.")
}

// Mọi operation M4 (quyền của M4, hoặc còn trong m4_pending.txt như setCustomerDoNotContact) qua đúng auth và guard
// (kế hoạch Task 143 Step 1, ADR-18): không thẻ → 401 unauthenticated; thẻ hợp lệ không có quyền của route → 403
// forbidden; thẻ owner → operation còn trong m4_pending.txt trả 501 not_implemented (route ghi gửi kèm khóa chống
// trùng, Origin đúng, thân {}: route ghi có thân bắt buộc được phép 400 vì kiểm hợp đồng chạy trước handler). Operation
// đã làm (không còn trong danh sách) mà vẫn 501 thì đỏ ở TestM3Routes_PendingList.
func TestM4Contract_StatusCodes(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	staff.Grant(t, tenant, staff.User(t, tenant, "chu-m4@elimaz.test", "active"), rolePerms(t, "owner"), "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "kho-m4@elimaz.test", "active"), []string{"catalog.read"}, "all", "")
	srv := testServer(t, staff, keys, 0)
	pending := pendingOps(t, "m4_pending.txt")
	param := regexp.MustCompile(`\{[^}]+\}`)
	n, key := 0, 0
	for _, r := range Routes {
		if !m4Permission(r.Permission) && !pending[r.OperationID] {
			continue
		}
		n++
		_, path, _ := strings.Cut(r.Pattern, " ")
		path = param.ReplaceAllString(path, uuid.NewString())
		path = strings.Replace(path, "/automation-rules/"+path[strings.LastIndex(path, "/")+1:], "/automation-rules/greeting", 1)
		body := ""
		if r.Write {
			body = `{}`
		}
		req := httptest.NewRequestWithContext(t.Context(), r.Method, path, strings.NewReader(body))
		if r.Write {
			req.Header.Set("Origin", testOrigin)
			req.Header.Set("Content-Type", "application/json")
		}
		noToken := httptest.NewRecorder()
		srv.ServeHTTP(noToken, req)
		expect(t, r.OperationID+" không thẻ", noToken, http.StatusUnauthorized, "unauthenticated")
		next := func() string {
			if !r.Write {
				return ""
			}
			key++
			return fmt.Sprintf("m4-%d", key)
		}
		expect(t, r.OperationID+" không quyền", serveAs(srv, keys, r.Method, path, "kho-m4@elimaz.test", next(), body),
			http.StatusForbidden, "forbidden")
		if !pending[r.OperationID] {
			continue
		}
		rec := serveAs(srv, keys, r.Method, path, "chu-m4@elimaz.test", next(), body)
		if rec.Code != http.StatusNotImplemented && (!r.Write || rec.Code != http.StatusBadRequest) {
			t.Errorf("%s (owner, chưa làm): mong 501 not_implemented, có %d %s", r.OperationID, rec.Code, rec.Body.String())
		}
	}
	if n < len(pending) {
		t.Errorf("chỉ thấy %d operation M4 trong Routes, danh sách chờ có %d", n, len(pending))
	}
}
