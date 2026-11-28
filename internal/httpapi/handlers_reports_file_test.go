package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// fileHTTPEnv: kịch bản chín đơn, kho tệp bộ nhớ; chu@ và binh@ xem bán hàng và xuất tệp, ql@ thêm
// report.exports.manage, kho@ xem tồn và xuất tệp.
type fileHTTPEnv struct {
	runsHTTPEnv
	users map[string]uuid.UUID
}

func newFileHTTPEnv(t *testing.T) fileHTTPEnv {
	t.Helper()
	reporting.RegisterFilePurposes()
	staff, keys, mem := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t), filestest.MemFileStore(t)
	tenant := staff.Tenant(t, "A")
	reportseed.NineOrders(reportseed.New(t, staff.Owner, tenant), "2026-11-17")
	users := map[string]uuid.UUID{}
	for _, g := range []struct {
		email string
		perms []string
	}{
		{"chu@elimaz.test", []string{"report.sales.read"}},
		{"binh@elimaz.test", []string{"report.sales.read", "report.export"}},
		{"ql@elimaz.test", []string{"report.sales.read", "report.export", "report.exports.manage"}},
		{"kho@elimaz.test", []string{"report.inventory.read", "report.export"}},
	} {
		users[g.email] = staff.User(t, tenant, g.email, "active")
		staff.Grant(t, tenant, users[g.email], g.perms, "all", "")
	}
	staff.Grant(t, tenant, users["chu@elimaz.test"], []string{"report.export"}, "all", "") // vai riêng: gỡ được
	deps := testServerDeps(t, staff, keys, 0)
	deps.Files = mem
	return fileHTTPEnv{
		runsHTTPEnv: runsHTTPEnv{t: t, staff: staff, tenant: tenant, srv: NewServer(deps), keys: keys, files: mem},
		users:       users,
	}
}

// create tạo lần chạy họ family bằng email, trả mã.
func (e fileHTTPEnv) create(family, email, body string) string {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/v1/reports/"+family+"/runs", email, uuid.NewString(), body)
	if rec.Code != http.StatusAccepted {
		e.t.Fatalf("tạo lần chạy: %d %s", rec.Code, rec.Body.String())
	}
	var run struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		e.t.Fatal(err)
	}
	return run.ID
}

func (e fileHTTPEnv) file(family, id, email string) (int, string, map[string]any) {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/api/v1/reports/"+family+"/runs/"+id+"/file", email, "", "")
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	code, _ := body["error"].(string)
	return rec.Code, code, body
}

func (e fileHTTPEnv) downloads(id string, actor uuid.UUID) int {
	return e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'report.file_downloaded'
		AND entity_id = $1 AND actor_id = $2`, id, actor.String())
}

const xlsxOrdersBody = `{"report":"orders","format":"xlsx","params":{"from":"2026-11-17","to":"2026-11-17","groupBy":"care_by"}}`

// GET …/file: người tạo → 200, url ký hạn 5 phút (exp của kho bộ nhớ), expiresAt = bây giờ + 5 phút, tên tải về
// bao-cao-don_<kỳ>.xlsx, mỗi lần gọi một dòng audit report.file_downloaded; người khác, họ khác → 404;
// report.exports.manage → 200, audit mang mã người đó; họ tồn (danh sách sản phẩm) → 200.
func TestReportRunFile_HTTPLink(t *testing.T) {
	t.Parallel()
	e := newFileHTTPEnv(t)
	chu := e.users["chu@elimaz.test"]
	id := e.create("sales", "chu@elimaz.test", xlsxOrdersBody)
	e.drain()
	before := time.Now()
	code, _, body := e.file("sales", id, "chu@elimaz.test")
	if code != http.StatusOK {
		t.Fatalf("người tạo: %d %v", code, body)
	}
	checkLink(t, body, before, time.Now())
	if n := e.downloads(id, chu); n != 1 {
		t.Errorf("audit sau lần 1 = %d", n)
	}
	if code, _, _ := e.file("sales", id, "chu@elimaz.test"); code != http.StatusOK || e.downloads(id, chu) != 2 {
		t.Errorf("lần 2: %d, audit %d", code, e.downloads(id, chu))
	}
	for _, c := range []struct{ family, email string }{{"sales", "binh@elimaz.test"}, {"inventory", "kho@elimaz.test"}} {
		if code, ec, _ := e.file(c.family, id, c.email); code != http.StatusNotFound || ec != "run_not_found" {
			t.Errorf("%s/%s: %d %s", c.family, c.email, code, ec)
		}
	}
	if code, _, _ := e.file("sales", id, "ql@elimaz.test"); code != http.StatusOK || e.downloads(id, e.users["ql@elimaz.test"]) != 1 {
		t.Errorf("quản lý: %d, audit %d", code, e.downloads(id, e.users["ql@elimaz.test"]))
	}
	kho := e.create("inventory", "kho@elimaz.test", `{"report":"products","format":"xlsx","params":{}}`)
	e.drain()
	if code, _, body := e.file("inventory", kho, "kho@elimaz.test"); code != http.StatusOK {
		t.Errorf("danh sách sản phẩm: %d %v", code, body)
	}
}

// checkLink: url ký hạn 5 phút tính từ khoảng [before, after], tên tải về theo kỳ, expiresAt cùng hạn.
func checkLink(t *testing.T, body map[string]any, before, after time.Time) {
	t.Helper()
	raw, _ := body["url"].(string)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	exp, _ := strconv.ParseInt(u.Query().Get("exp"), 10, 64)
	if got := time.Unix(exp, 0); got.Before(before.Add(5*time.Minute).Truncate(time.Second)) || got.After(after.Add(5*time.Minute)) {
		t.Errorf("hạn chữ ký %v", got)
	}
	if cd := u.Query().Get("cd"); !strings.Contains(cd, "bao-cao-don_2026-11-17_2026-11-17.xlsx") {
		t.Errorf("tên tải về %q", cd)
	}
	s, _ := body["expiresAt"].(string)
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || at.Before(before.Add(5*time.Minute).Add(-time.Second)) || at.After(after.Add(5*time.Minute)) {
		t.Errorf("expiresAt = %v %v", body["expiresAt"], err)
	}
}

// GET …/file lỗi: chưa chạy → 409 run_not_ready; json → 400 bad_params; người tạo bị gỡ report.export → 403
// export_forbidden; quá hạn → 410 run_expired.
func TestReportRunFile_HTTPErrors(t *testing.T) {
	t.Parallel()
	e := newFileHTTPEnv(t)
	id := e.create("sales", "chu@elimaz.test", xlsxOrdersBody)
	if code, ec, _ := e.file("sales", id, "chu@elimaz.test"); code != http.StatusConflict || ec != "run_not_ready" {
		t.Errorf("chưa chạy: %d %s", code, ec)
	}
	jsonID := e.create("sales", "chu@elimaz.test", ordersRunBody)
	e.drain()
	if code, ec, _ := e.file("sales", jsonID, "chu@elimaz.test"); code != http.StatusBadRequest || ec != "bad_params" {
		t.Errorf("json: %d %s", code, ec)
	}
	e.ownerSQL(`DELETE FROM identity.user_role_grants g USING identity.role_permissions p
		WHERE g.user_id = $1 AND p.role_id = g.role_id AND p.permission = 'report.export'`, e.users["chu@elimaz.test"])
	if code, ec, _ := e.file("sales", id, "chu@elimaz.test"); code != http.StatusForbidden || ec != "export_forbidden" {
		t.Errorf("bị gỡ report.export: %d %s", code, ec)
	}
	e.ownerSQL(`UPDATE reporting.report_runs SET expires_at = now() - interval '1 second' WHERE id = $1`, id)
	if code, ec, _ := e.file("sales", id, "ql@elimaz.test"); code != http.StatusGone || ec != "run_expired" {
		t.Errorf("quá hạn: %d %s", code, ec)
	}
}

// ownerSQL chạy một câu trong ngữ cảnh tenant của env.
func (e fileHTTPEnv) ownerSQL(q string, args ...any) {
	e.t.Helper()
	tenantSQL(e.t, e.staff, e.tenant, q, args...)
}
