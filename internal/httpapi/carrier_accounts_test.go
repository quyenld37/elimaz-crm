package httpapi

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/adapters/carrier/manual"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/tools/fakecarrier/server"
)

const (
	caManager = "quan-ly-hang@elimaz.test"
	caReader  = "xem-hang@elimaz.test"
	caNobody  = "chi-van-don@elimaz.test"
	caToken   = "tk-live-123"
	caSecret  = "carrier-accounts-api-fake-secret-0123456789"
)

type caEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *testkit.AccessKeys
	shop   *invtest.Shop
	srv    http.Handler
	worker *queue.Worker
	logs   *bytes.Buffer
	key    int
}

// newCAEnv: tenant A của newOrderAPIEnv (kho MAIN, người bán ELIMAZ), máy chủ hãng giả thật qua httptest, Service với {fake, self}, khóa
// k1, logger thật của ứng dụng ghi vào bộ đệm, worker chạy lệnh của fulfillment.
func newCAEnv(t *testing.T) *caEnv {
	t.Helper()
	oe := newOrderAPIEnv(t)
	e := &caEnv{t: t, staff: oe.staff, keys: oe.keys, shop: oe.shop}
	tenant := e.shop.Tenant
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, caManager, "active"), []string{
		"fulfillment.carrier_accounts.read", "fulfillment.carrier_accounts.manage", "fulfillment.settings.manage",
	}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, caReader, "active"), []string{"fulfillment.carrier_accounts.read"}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, caNobody, "active"), []string{"fulfillment.read"}, "all", "")
	fs := httptest.NewServer(server.New(server.Options{Secret: caSecret, Hang: time.Second}))
	t.Cleanup(fs.Close)
	reg, err := fulfillment.NewRegistry(fake.NewClient(fs.URL, caSecret, fs.Client()), manual.New(fulfillment.SelfCarrierKey))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := fieldcrypt.ParseKeyring(logging.Secret("k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	m := fulfillment.New(fulfillment.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Carriers: reg, Keys: ring, Bus: &testkit.RecordingBus{}})
	logger, buf := testkit.CaptureLog(t)
	e.logs = buf
	authE := &authEnv{t: t, keys: e.keys}
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App, Logger: logger,
		Access: authE.verifier(), Realtime: &testkit.RecordingBus{}, Fulfillment: m.Service,
	})
	e.worker = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		Logger:   logger,
		WorkerID: "test/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, m.CommandHandlers()),
	})
	return e
}

func (e *caEnv) send(method, path, email, body string) (int, map[string]any, string) {
	e.t.Helper()
	key := ""
	if method != http.MethodGet {
		e.key++
		key = fmt.Sprintf("ca-%d", e.key)
	}
	rec := serveAs(e.srv, e.keys, method, path, email, key, body)
	if rec.Body.Len() == 0 {
		return rec.Code, nil, ""
	}
	return rec.Code, decodeJSON(e.t, rec), rec.Body.String()
}

func (e *caEnv) run() {
	e.t.Helper()
	testkit.DrainDue(e.t, e.staff.Owner, e.worker)
}

func (e *caEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.shop.Tenant, q, args...)
}

func (e *caEnv) createBody(extra string) string {
	return fmt.Sprintf(`{"carrierKey":"fake","mode":"api","name":"Hãng giả","warehouseId":"%s"%s}`, e.shop.Warehouse, extra)
}

// VC01 qua HTTP: tạo tài khoản fake mode api với {"token":"tk-live-123"} → 201 draft, có webhookUrl; kiểm → 202, chạy
// worker → active; danh sách của người có quyền manage không có chuỗi khóa và có webhookUrl, của người chỉ có quyền
// read không có webhookUrl; audit tạo có changed_fields chứa credentials, after không có giá trị khóa; log, dấu vết,
// việc nền, audit của cả luồng không có chuỗi khóa. Không quyền manage → 403.
func TestCarrierAccountsAPI_VC01(t *testing.T) {
	spans := testkit.CaptureSpans(t)
	e := newCAEnv(t)
	code, acc, raw := e.send(http.MethodPost, "/api/v1/carrier-accounts", caManager, e.createBody(`,"credentials":{"token":"`+caToken+`"}`))
	if code != http.StatusCreated || acc["status"] != "draft" || acc["hasCredentials"] != true || acc["sellerId"] != e.shop.Seller.String() ||
		!strings.HasPrefix(fmt.Sprint(acc["webhookUrl"]), "/hooks/fake/") || strings.Contains(raw, caToken) {
		t.Fatalf("tạo: %d %s", code, raw)
	}
	id := fmt.Sprint(acc["id"])
	if code, _, raw := e.send(http.MethodPost, "/api/v1/carrier-accounts", caReader, e.createBody("")); code != http.StatusForbidden {
		t.Fatalf("quyền read tạo tài khoản: %d %s", code, raw)
	}
	if code, _, _ := e.send(http.MethodGet, "/api/v1/carrier-accounts", caNobody, ""); code != http.StatusForbidden {
		t.Fatalf("không quyền đọc tài khoản hãng: %d", code)
	}
	code, job, _ := e.send(http.MethodPost, "/api/v1/carrier-accounts/"+id+"/check", caManager, "")
	if code != http.StatusAccepted || job["jobId"] == nil {
		t.Fatalf("kiểm: %d %v", code, job)
	}
	e.run()
	code, page, raw := e.send(http.MethodGet, "/api/v1/carrier-accounts", caManager, "")
	items, _ := page["items"].([]any)
	if code != http.StatusOK || len(items) != 1 || strings.Contains(raw, caToken) {
		t.Fatalf("danh sách manage: %d %s", code, raw)
	}
	first, _ := items[0].(map[string]any)
	if first["status"] != "active" || first["lastCheckResult"] != "ok" || first["webhookUrl"] == nil {
		t.Fatalf("sau kiểm: %v", first)
	}
	_, page, raw = e.send(http.MethodGet, "/api/v1/carrier-accounts?status=active", caReader, "")
	items, _ = page["items"].([]any)
	if len(items) != 1 || strings.Contains(raw, "webhookUrl") || strings.Contains(raw, caToken) {
		t.Fatalf("danh sách quyền read: %s", raw)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.carrier_account.create'
		AND changed_fields @> ARRAY['credentials'] AND after->>'credentials' = 'set'`); n != 1 {
		t.Errorf("audit tạo có changed_fields credentials: %d dòng", n)
	}
	for _, q := range []string{
		`SELECT count(*) FROM identity.audit_logs a WHERE a::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM platform.jobs j WHERE j::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM platform.idempotency_keys k WHERE k::text LIKE '%' || $1 || '%'`,
	} {
		if n := e.count(q, caToken); n != 0 {
			t.Errorf("%s: %d dòng có chuỗi khóa", q, n)
		}
	}
	if strings.Contains(e.logs.String(), caToken) {
		t.Errorf("log có chuỗi khóa")
	}
	if text := testkit.SpanText(spans.GetSpans().Snapshots()); strings.Contains(text, caToken) {
		t.Errorf("dấu vết có chuỗi khóa")
	}
}

// Lỗi của đường ghi: hãng không có adapter → 422 capability_not_supported; self với api → 422; active cho tài khoản api
// chưa có khóa → 409 invalid_transition; hai tài khoản mặc định cùng kho cùng hãng → 409 already_exists; sửa với version
// cũ → 409 version_conflict; ghi khóa → checking và có lệnh kiểm; revoked → không đổi lại (409).
func TestCarrierAccountsAPI_Refusals(t *testing.T) {
	e := newCAEnv(t)
	post := func(body string) (int, map[string]any) {
		code, b, _ := e.send(http.MethodPost, "/api/v1/carrier-accounts", caManager, body)
		return code, b
	}
	if code, b := post(strings.Replace(e.createBody(""), `"fake"`, `"ghn"`, 1)); code != http.StatusUnprocessableEntity || b["error"] != "capability_not_supported" {
		t.Errorf("ghn: %d %v", code, b)
	}
	if code, b := post(strings.Replace(e.createBody(""), `"fake"`, `"self"`, 1)); code != http.StatusUnprocessableEntity || b["error"] != "capability_not_supported" {
		t.Errorf("self api: %d %v", code, b)
	}
	code, acc := post(e.createBody(`,"isDefaultForWarehouse":true`))
	if code != http.StatusCreated {
		t.Fatalf("tạo: %d %v", code, acc)
	}
	if code, b := post(e.createBody(`,"isDefaultForWarehouse":true`)); code != http.StatusConflict || b["error"] != "already_exists" {
		t.Errorf("mặc định thứ hai: %d %v", code, b)
	}
	e.lifecycle(fmt.Sprint(acc["id"]))
}

// lifecycle: active chưa có khóa → 409; sửa, version cũ → 409; ghi khóa → checking, có lệnh kiểm, audit không có khóa;
// revoked → không đổi lại; kiểm tài khoản lạ → 404.
func (e *caEnv) lifecycle(id string) {
	t := e.t
	t.Helper()
	status := "/api/v1/carrier-accounts/" + id + "/status"
	if code, b, _ := e.send(http.MethodPost, status, caManager, `{"status":"active"}`); code != http.StatusConflict || b["error"] != "invalid_transition" {
		t.Errorf("active chưa có khóa: %d %v", code, b)
	}
	if code, b, _ := e.send(http.MethodPatch, "/api/v1/carrier-accounts/"+id, caManager, `{"version":1,"name":"Tên mới"}`); code != http.StatusOK || b["version"] != float64(2) {
		t.Fatalf("sửa: %d %v", code, b)
	}
	if code, b, _ := e.send(http.MethodPatch, "/api/v1/carrier-accounts/"+id, caManager, `{"version":1,"name":"Tên khác"}`); code != http.StatusConflict || b["error"] != "version_conflict" {
		t.Errorf("version cũ: %d %v", code, b)
	}
	code, b, raw := e.send(http.MethodPut, "/api/v1/carrier-accounts/"+id+"/credentials", caManager, `{"credentials":{"token":"`+caToken+`"}}`)
	if code != http.StatusOK || b["status"] != "checking" || strings.Contains(raw, caToken) {
		t.Fatalf("ghi khóa: %d %s", code, raw)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.carrier_account.credentials'
		AND changed_fields @> ARRAY['credentials'] AND after::text NOT LIKE '%' || $1 || '%'`, caToken); n != 1 {
		t.Errorf("audit ghi khóa: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'carrier.check'`); n != 1 {
		t.Errorf("lệnh kiểm sau ghi khóa: %d", n)
	}
	if code, b, _ := e.send(http.MethodPost, status, caManager, `{"status":"revoked"}`); code != http.StatusOK || b["status"] != "revoked" {
		t.Fatalf("thu hồi: %d %v", code, b)
	}
	if code, b, _ := e.send(http.MethodPost, status, caManager, `{"status":"active"}`); code != http.StatusConflict || b["error"] != "invalid_transition" {
		t.Errorf("revoked → active: %d %v", code, b)
	}
	if code, _, _ := e.send(http.MethodPost, "/api/v1/carrier-accounts/"+uuid.NewString()+"/check", caManager, ""); code != http.StatusNotFound {
		t.Errorf("tài khoản lạ: %d", code)
	}
}

// setWarehousePickup: ghi → warehouses_v có địa chỉ mới, version 2, audit chỉ có tên trường; version cũ → 409; SĐT
// sai → 422 invalid_phone; không quyền fulfillment.settings.manage → 403.
func TestWarehousePickupAPI(t *testing.T) {
	e := newCAEnv(t)
	path := "/api/v1/inventory/warehouses/" + e.shop.Warehouse.String() + "/pickup"
	body := func(version int, phone string) string {
		return fmt.Sprintf(`{"version":%d,"name":"Kho MAIN","phone":%q,"line":"1 Hàng Bài","provinceCode":"01","wardCode":"00070"}`, version, phone)
	}
	if code, _, _ := e.send(http.MethodPut, path, caReader, body(1, "0901234567")); code != http.StatusForbidden {
		t.Fatalf("không quyền: %d", code)
	}
	code, b, _ := e.send(http.MethodPut, path, caManager, body(1, "0901 234 567"))
	if code != http.StatusOK || b["version"] != float64(2) || b["phone"] != "+84901234567" || b["line"] != "1 Hàng Bài" {
		t.Fatalf("ghi: %d %v", code, b)
	}
	if n := e.count(`SELECT count(*) FROM inventory.warehouses_v WHERE warehouse_id = $1 AND pickup_address->>'line' = '1 Hàng Bài'
		AND pickup_address->>'ward_code' = '00070' AND pickup_phone = '+84901234567'`, e.shop.Warehouse); n != 1 {
		t.Fatalf("warehouses_v chưa có địa chỉ mới")
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'inventory.warehouse.pickup'
		AND changed_fields @> ARRAY['line', 'name', 'phone', 'provinceCode', 'wardCode']
		AND after::text NOT LIKE '%Hàng Bài%' AND after::text NOT LIKE '%0901%' AND after::text NOT LIKE '%84901%'`); n != 1 {
		t.Errorf("audit chỉ ghi tên trường: %d", n)
	}
	if code, b, _ := e.send(http.MethodPut, path, caManager, body(1, "0901234567")); code != http.StatusConflict || b["error"] != "version_conflict" {
		t.Errorf("version cũ: %d %v", code, b)
	}
	if code, b, _ := e.send(http.MethodPut, path, caManager, body(2, "12345678")); code != http.StatusUnprocessableEntity || b["error"] != "invalid_phone" {
		t.Errorf("SĐT sai: %d %v", code, b)
	}
	if code, _, _ := e.send(http.MethodPut, "/api/v1/inventory/warehouses/"+uuid.NewString()+"/pickup", caManager, body(1, "0901234567")); code != http.StatusNotFound {
		t.Errorf("kho lạ: %d", code)
	}
}
