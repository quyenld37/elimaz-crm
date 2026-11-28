package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// GET /api/v1/reports/launch-readiness (Task 269): dạng JSON đúng hợp đồng trên shop rỗng (15 mục, đếm fail/warn đúng
// với các mục, bốn mục chưa có nguồn là warn "chưa có nguồn"), không lộ tên Page, người; thiếu quyền → 403.
func TestLaunchReadiness_API(t *testing.T) {
	t.Parallel()
	staff := testkit.OpenStaff(t)
	tenant := staff.Tenant(t, "A")
	keys := cfaccesstest.AccessKeyServer(t)
	seed := reportseed.New(t, staff.Owner, tenant)
	seed.Page("BÍ-MẬT-PAGE")
	staff.Grant(t, tenant, staff.User(t, tenant, "chu@elimaz.test", "active"), []string{"reporting.launch.read"}, "all", "")
	staff.Grant(t, tenant, staff.User(t, tenant, "nv@elimaz.test", "active"), []string{"report.sales.read"}, "all", "")
	srv := testServer(t, staff, keys, 0)
	const path = "/api/v1/reports/launch-readiness"

	rec := serveAs(srv, keys, http.MethodGet, path, "chu@elimaz.test", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("mã %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			Key, Group, Status, Value, Target string
		} `json:"items"`
		Fail        int    `json:"fail"`
		Warn        int    `json:"warn"`
		EvaluatedAt string `json:"evaluatedAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("thân %s: %v", rec.Body.String(), err)
	}
	if len(body.Items) != 15 || body.EvaluatedAt == "" {
		t.Fatalf("%d mục, evaluatedAt %q: %s", len(body.Items), body.EvaluatedAt, rec.Body.String())
	}
	fail, warn := 0, 0
	for _, it := range body.Items {
		switch it.Status {
		case "fail":
			fail++
		case "warn":
			warn++
		case "ok":
		default:
			t.Errorf("mục %s: status lạ %q", it.Key, it.Status)
		}
		if strings.HasSuffix(it.Key, ".t2_pass") && (it.Status != "warn" || it.Value != "chưa có nguồn") {
			t.Errorf("t2_pass: %+v", it)
		}
	}
	if body.Fail != fail || body.Warn != warn || fail == 0 {
		t.Errorf("fail %d/%d, warn %d/%d", body.Fail, fail, body.Warn, warn)
	}
	if strings.Contains(rec.Body.String(), "BÍ-MẬT") || strings.Contains(rec.Body.String(), "@") {
		t.Errorf("thân lộ tên hay email: %s", rec.Body.String())
	}

	expect(t, "thiếu quyền", serveAs(srv, keys, http.MethodGet, path, "nv@elimaz.test", "", ""), http.StatusForbidden, "forbidden")
}
