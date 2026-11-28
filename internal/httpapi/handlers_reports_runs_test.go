package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/reportseed"
)

// runsHTTPEnv: kịch bản chín đơn; chu@ xem báo cáo bán hàng toàn shop, binh@ cũng vậy, ql@ thêm report.exports.manage,
// kho@ chỉ report.inventory.read.
type runsHTTPEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	srv    http.Handler
	keys   *cfaccesstest.AccessKeys
}

func newRunsHTTPEnv(t *testing.T) runsHTTPEnv {
	t.Helper()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	reportseed.NineOrders(reportseed.New(t, staff.Owner, tenant), "2026-11-17")
	for _, g := range []struct {
		email string
		perms []string
	}{
		{"chu@elimaz.test", []string{"report.sales.read"}},
		{"binh@elimaz.test", []string{"report.sales.read"}},
		{"ql@elimaz.test", []string{"report.sales.read", "report.exports.manage"}},
		{"kho@elimaz.test", []string{"report.inventory.read"}},
	} {
		staff.Grant(t, tenant, staff.User(t, tenant, g.email, "active"), g.perms, "all", "")
	}
	return runsHTTPEnv{t: t, staff: staff, tenant: tenant, srv: NewServer(testServerDeps(t, staff, keys, 0)), keys: keys}
}

func (e runsHTTPEnv) do(method, path, email, key, body string) *httptest.ResponseRecorder {
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

// drain chạy worker của lần chạy nền (kết quả đổi bằng ReportResultJSON như cmd/crm nối) tới hết việc.
func (e runsHTTPEnv) drain() {
	h := reporting.NewRunHandler(reporting.Deps{DB: e.staff.App, Result: ReportResultJSON})
	w := queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{Commands: commands.NewRunner(e.staff.App, []commands.Handler{h})})
	testkit.DrainDue(e.t, e.staff.Owner, w)
}

const ordersRunBody = `{"report":"orders","format":"json","params":{"from":"2026-11-17","to":"2026-11-17","basis":"placed","groupBy":"care_by"}}`

// post50 gửi 50 yêu cầu tạo lần chạy song song cùng Idempotency-Key; đòi mọi trả lời là 202 cùng thân hoặc 409, trả thân
// 202.
func (e runsHTTPEnv) post50() map[string]any {
	t := e.t
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() {
			recs[i] = e.do(http.MethodPost, "/api/v1/reports/sales/runs", "chu@elimaz.test", "run-50", ordersRunBody)
		})
	}
	wg.Wait()
	var bodies []string
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusAccepted:
			bodies = append(bodies, rec.Body.String())
		case http.StatusConflict:
		default:
			t.Fatalf("trả lời %d %s", rec.Code, rec.Body.String())
		}
	}
	if len(bodies) == 0 {
		t.Fatal("không có 202 nào")
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("202 khác thân:\n%s\n%s", b, bodies[0])
		}
	}
	var run map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &run); err != nil {
		t.Fatal(err)
	}
	return run
}

// decodeAny đọc thân JSON thành giá trị chung để so nội dung (không theo thứ tự khóa).
func decodeAny(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("thân không phải JSON: %v", err)
	}
	return v
}

// Tạo lần chạy qua HTTP: 50 yêu cầu song song cùng Idempotency-Key → 1 dòng, 1 lệnh, 1 audit; trả lời 202 cùng thân hoặc
// 409; worker chạy → GET lần chạy có result bằng đúng thân của GET /reports/sales/orders cùng tham số.
func TestReportRuns_HTTPCreateAndRun(t *testing.T) {
	t.Parallel()
	e := newRunsHTTPEnv(t)
	run := e.post50()
	params, _ := run["params"].(map[string]any)
	if run["status"] != "queued" || run["family"] != "sales" || run["report"] != "orders" || run["format"] != "json" ||
		params["groupBy"] != "care_by" || run["requestedByName"] == nil {
		t.Errorf("lần chạy = %v", run)
	}
	for _, c := range []struct {
		q    string
		want int
	}{
		{`SELECT count(*) FROM reporting.report_runs`, 1},
		{`SELECT count(*) FROM platform.jobs WHERE kind = 'command.report_run'`, 1},
		{`SELECT count(*) FROM identity.audit_logs WHERE action = 'report.run_requested'`, 1},
	} {
		if n := e.staff.CountIn(t, e.tenant, c.q); n != c.want {
			t.Errorf("%s = %d, mong %d", c.q, n, c.want)
		}
	}
	e.drain()
	id, _ := run["id"].(string)
	rec := e.do(http.MethodGet, "/api/v1/reports/sales/runs/"+id, "chu@elimaz.test", "", "")
	var got struct {
		Status string          `json:"status"`
		Result json.RawMessage `json:"result"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET lần chạy: %d %s", rec.Code, rec.Body.String())
	}
	live := e.do(http.MethodGet, "/api/v1/reports/sales/orders?from=2026-11-17&to=2026-11-17&basis=placed&groupBy=care_by",
		"chu@elimaz.test", "", "")
	if got.Status != "ready" || live.Code != http.StatusOK ||
		!reflect.DeepEqual(decodeAny(t, got.Result), decodeAny(t, live.Body.Bytes())) {
		t.Errorf("result = %s\nmong (GET) %s", got.Result, live.Body.String())
	}
}

// Xem qua HTTP: liệt kê thấy lần chạy (không kèm result); người khác 404 run_not_found; người có report.exports.manage
// 200; đường của họ khác 404.
func TestReportRuns_HTTPView(t *testing.T) {
	t.Parallel()
	e := newRunsHTTPEnv(t)
	created := e.do(http.MethodPost, "/api/v1/reports/sales/runs", "chu@elimaz.test", "view-1", ordersRunBody)
	if created.Code != http.StatusAccepted {
		t.Fatalf("tạo: %d %s", created.Code, created.Body.String())
	}
	id, _ := decodeJSON(t, created)["id"].(string)
	rec := e.do(http.MethodGet, "/api/v1/reports/sales/runs?limit=10", "chu@elimaz.test", "", "")
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK || len(page.Items) != 1 ||
		page.Items[0]["id"] != id || page.Items[0]["result"] != nil {
		t.Errorf("liệt kê: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []struct {
		name, path, email string
		status            int
	}{
		{"người tạo", "/api/v1/reports/sales/runs/" + id, "chu@elimaz.test", http.StatusOK},
		{"người khác", "/api/v1/reports/sales/runs/" + id, "binh@elimaz.test", http.StatusNotFound},
		{"người quản lý", "/api/v1/reports/sales/runs/" + id, "ql@elimaz.test", http.StatusOK},
		{"họ khác", "/api/v1/reports/inventory/runs/" + id, "kho@elimaz.test", http.StatusNotFound},
	} {
		rec := e.do(http.MethodGet, c.path, c.email, "", "")
		if rec.Code != c.status || c.status == http.StatusNotFound && decodeJSON(t, rec)["error"] != "run_not_found" {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
	}
}

// Lỗi khi tạo qua HTTP: xlsx không có report.export → 403 export_forbidden, không dòng, không lệnh; báo cáo của họ khác
// → 400 unknown_report; tham số lạ → 400 bad_params; người không có quyền đọc của họ → 403; quá 5 lần chờ → 429
// too_many_runs, người khác vẫn 202; con trỏ hỏng → 400 bad_params.
func TestReportRuns_HTTPErrors(t *testing.T) {
	t.Parallel()
	e := newRunsHTTPEnv(t)
	cases := []struct {
		name, path, email, body string
		status                  int
		code                    string
	}{
		{
			"xlsx", "/api/v1/reports/sales/runs", "chu@elimaz.test",
			`{"report":"orders","format":"xlsx","params":{"from":"2026-11-17","to":"2026-11-17"}}`, 403, "export_forbidden",
		},
		{"họ khác", "/api/v1/reports/inventory/runs", "kho@elimaz.test", ordersRunBody, 400, "unknown_report"},
		{
			"trường lạ", "/api/v1/reports/sales/runs", "chu@elimaz.test",
			`{"report":"orders","format":"json","params":{"from":"2026-11-17","to":"2026-11-17","x":1}}`, 400, "bad_params",
		},
		{"không quyền", "/api/v1/reports/sales/runs", "kho@elimaz.test", ordersRunBody, 403, "forbidden"},
	}
	for i, c := range cases {
		rec := e.do(http.MethodPost, c.path, c.email, "err-"+string(rune('a'+i)), c.body)
		if rec.Code != c.status || decodeJSON(t, rec)["error"] != c.code {
			t.Errorf("%s: %d %s, mong %d %s", c.name, rec.Code, rec.Body.String(), c.status, c.code)
		}
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM reporting.report_runs`); n != 0 {
		t.Fatalf("có %d lần chạy sau các yêu cầu bị từ chối", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = 'command.report_run'`); n != 0 {
		t.Fatalf("có %d lệnh sau các yêu cầu bị từ chối", n)
	}
	for i := range 5 {
		if rec := e.do(http.MethodPost, "/api/v1/reports/sales/runs", "chu@elimaz.test", "ok-"+string(rune('a'+i)), ordersRunBody); rec.Code != http.StatusAccepted {
			t.Fatalf("lần %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if rec := e.do(http.MethodPost, "/api/v1/reports/sales/runs", "chu@elimaz.test", "ok-f", ordersRunBody); rec.Code != http.StatusTooManyRequests ||
		decodeJSON(t, rec)["error"] != "too_many_runs" {
		t.Errorf("lần 6: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodPost, "/api/v1/reports/sales/runs", "binh@elimaz.test", "binh-1", ordersRunBody); rec.Code != http.StatusAccepted {
		t.Errorf("người khác: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodGet, "/api/v1/reports/sales/runs?cursor=xx", "chu@elimaz.test", "", ""); rec.Code != http.StatusBadRequest ||
		decodeJSON(t, rec)["error"] != "bad_params" {
		t.Errorf("con trỏ hỏng: %d %s", rec.Code, rec.Body.String())
	}
}
