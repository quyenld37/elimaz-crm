package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const auditPath = "/api/v1/identity/audit-logs"

// seedAudit ghi các dòng audit vào tenant bằng crm_app.
func (e *couponEnv) seedAudit(tenant uuid.UUID, entries ...identity.AuditEntry) {
	e.t.Helper()
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		for _, en := range entries {
			en.TenantID = tenant
			if err := identity.RecordAudit(e.t.Context(), tx, en); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

// [Task 457] GET /api/v1/identity/audit-logs: bán hàng (không có audit.read) → 403; quản lý: 120 dòng → 3 trang 50, 50, 20
// qua nextCursor, không trùng, không sót; lọc hành động, người làm, khoảng giờ; tên người làm (nhân viên); thân trả về
// không có giá trị before/after, IP (chỉ tên trường đổi); con trỏ hỏng, from ≥ to, entityId thiếu entityType → 400
// invalid_input; quản lý tenant khác: rỗng.
func TestAuditLogsAPI_List(t *testing.T) {
	t.Parallel()
	e := newCouponEnv(t)
	other := e.staff.Tenant(t, "B")
	otherManager := "quan-ly-b-audit@elimaz.test"
	e.staff.Grant(t, other, e.staff.User(t, other, otherManager, "active"), rolePerms(t, "manager"), "all", "")
	maker := e.staff.User(t, e.tenant, "nguoi-lam@elimaz.test", "active")
	base := time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)
	entries := make([]identity.AuditEntry, 0, 120)
	for i := range 120 {
		entries = append(entries, identity.AuditEntry{
			ActorType: "system", Action: "t.bulk", EntityType: "t", EntityID: fmt.Sprintf("b-%03d", i),
			At: base.Add(time.Duration(i) * time.Minute),
		})
	}
	entries = append(entries, identity.AuditEntry{
		ActorType: "staff", ActorID: maker.String(), Action: "order.update", EntityType: "order", EntityID: "o-1",
		Before: map[string]any{"phone": "0911222333"}, After: map[string]any{"phone": "0988777666"},
		IP: "203.0.113.9", At: base.Add(-time.Hour),
	})
	e.seedAudit(e.tenant, entries...)

	expect(t, "bán hàng đọc", e.do(http.MethodGet, auditPath, couponSale, "", ""), http.StatusForbidden, "forbidden")

	sizes, seen := e.auditPages(url.Values{"action": {"t.bulk"}}, couponManager)
	if fmt.Sprint(sizes) != "[50 50 20]" || len(seen) != 120 {
		t.Fatalf("trang: %v, %d dòng khác nhau", sizes, len(seen))
	}

	byActor := e.ok("người làm", e.do(http.MethodGet, auditPath+"?actorId="+maker.String(), couponManager, "", ""),
		http.StatusOK)
	items, _ := byActor["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("lọc người làm: %v", byActor)
	}
	row, _ := items[0].(map[string]any)
	if row["actorType"] != "staff" || row["actorName"] != "nguoi-lam@elimaz.test" || row["action"] != "order.update" ||
		row["entityType"] != "order" || row["entityId"] != "o-1" || fmt.Sprint(row["changedFields"]) != "[phone]" {
		t.Errorf("dòng nhân viên: %v", row)
	}
	assertNoBodyValues(t, byActor, "0911222333", "0988777666", "203.0.113.9", `"before"`, `"after"`, `"ip"`)

	span := url.Values{
		"from": {base.Add(10 * time.Minute).Format(time.RFC3339)},
		"to":   {base.Add(13 * time.Minute).Format(time.RFC3339)},
	}
	ranged := e.ok("khoảng giờ", e.do(http.MethodGet, auditPath+"?"+span.Encode(), couponManager, "", ""), http.StatusOK)
	if r, _ := ranged["items"].([]any); len(r) != 3 {
		t.Errorf("khoảng giờ [10, 13) phút: %v", ranged)
	}
	sys := e.ok("hệ thống", e.do(http.MethodGet, auditPath+"?entityType=t&entityId=b-005", couponManager, "", ""),
		http.StatusOK)
	if r, _ := sys["items"].([]any); len(r) != 1 {
		t.Errorf("lọc thực thể: %v", sys)
	} else if m, _ := r[0].(map[string]any); m["actorType"] != "system" || m["actorName"] != nil || m["actorId"] != nil {
		t.Errorf("dòng hệ thống: %v", m)
	}

	for name, query := range map[string]string{
		"con trỏ hỏng":              "?cursor=abc",
		"from bằng to":              "?" + url.Values{"from": {base.Format(time.RFC3339)}, "to": {base.Format(time.RFC3339)}}.Encode(),
		"entityId thiếu entityType": "?entityId=o-1",
	} {
		expect(t, name, e.do(http.MethodGet, auditPath+query, couponManager, "", ""), http.StatusBadRequest, "invalid_input")
	}

	empty := e.ok("tenant khác", e.do(http.MethodGet, auditPath+"?action=t.bulk", otherManager, "", ""), http.StatusOK)
	if r, _ := empty["items"].([]any); len(r) != 0 || empty["nextCursor"] != nil {
		t.Errorf("tenant khác thấy audit: %v", empty)
	}
}

// auditPages lật mọi trang của GET audit-logs với bộ lọc q (tối đa 5 trang), trả cỡ từng trang và các entityId đã gặp
// (trùng → lỗi test).
func (e *couponEnv) auditPages(q url.Values, email string) ([]int, map[string]bool) {
	e.t.Helper()
	seen := map[string]bool{}
	var sizes []int
	for range 5 {
		page := e.ok("trang", e.do(http.MethodGet, auditPath+"?"+q.Encode(), email, "", ""), http.StatusOK)
		items, _ := page["items"].([]any)
		sizes = append(sizes, len(items))
		for _, it := range items {
			m, _ := it.(map[string]any)
			id, _ := m["entityId"].(string)
			if seen[id] {
				e.t.Errorf("trùng %s", id)
			}
			seen[id] = true
		}
		next, ok := page["nextCursor"].(string)
		if !ok {
			break
		}
		q.Set("cursor", next)
	}
	return sizes, seen
}

// assertNoBodyValues: thân JSON body không chứa chuỗi nào trong values.
func assertNoBodyValues(t *testing.T, body map[string]any, values ...string) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if strings.Contains(string(raw), v) {
			t.Errorf("thân trả về có %s: %s", v, raw)
		}
	}
}
