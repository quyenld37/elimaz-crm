package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	customerWriter = "ghi-khach@elimaz.test" // customer.read, customer.write
	customerReader = "xem-khach@elimaz.test" // chỉ customer.read
)

type customerAPIEnv struct {
	t             *testing.T
	staff         *testkit.Staff
	keys          *cfaccesstest.AccessKeys
	srv           http.Handler
	tenant, other uuid.UUID
	logs          fmt.Stringer
	key           int
}

func newCustomerAPIEnv(t *testing.T) *customerAPIEnv {
	t.Helper()
	e := &customerAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t)}
	// Danh mục mẫu mã T… (dữ liệu thật chưa nạp, chờ chủ shop OK): mã, cấp, tỉnh cha, tên, tên bỏ dấu.
	for _, u := range [][5]string{
		{"T01", "province", "", "Thành phố Thử Một", "thanh pho thu mot"},
		{"T02", "province", "", "Tỉnh Thử Hai", "tinh thu hai"},
		{"T01001", "ward", "T01", "Phường Bến Thử", "phuong ben thu"},
		{"T01002", "ward", "T01", "Phường Đa Kao", "phuong da kao"},
		{"T02001", "ward", "T02", "Phường Bến Hai", "phuong ben hai"},
	} {
		if _, err := e.staff.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units (code, level, parent_code, name,
			name_search, effective_from, source) VALUES ($1, $2, NULLIF($3, ''), $4, $5, DATE '2025-07-01', 'test')`,
			u[0], u[1], u[2], u[3], u[4]); err != nil {
			t.Fatal(err)
		}
	}
	e.tenant, e.other = e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, customerWriter, "active"), []string{"customer.read", "customer.write"}, "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, customerReader, "active"), []string{"customer.read"}, "all", "")
	logger, logs := testkit.CaptureLog(t)
	e.logs = logs
	authE := &authEnv{t: t, keys: e.keys}
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App, Logger: logger,
		Access: authE.verifier(), Realtime: &realtimetest.RecordingBus{},
	})
	return e
}

// send gửi yêu cầu của nhân viên email; yêu cầu ghi có khóa chống trùng mới.
func (e *customerAPIEnv) send(method, path, email, body string) *httptest.ResponseRecorder {
	key := ""
	if method != http.MethodGet {
		e.key++
		key = fmt.Sprintf("kh-%d-%s", e.key, uuid.NewString())
	}
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

func (e *customerAPIEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// TestCustomerWriteAPI: tạo, đọc, sửa khách, danh tính, địa chỉ qua HTTP; quyền; tenant khác 404 (PQ08); log và dấu
// vết không có dữ liệu cá nhân.
func TestCustomerWriteAPI(t *testing.T) {
	exp := testkit.CaptureSpans(t)
	e := newCustomerAPIEnv(t)
	id, code := e.checkCreate(t)
	e.checkGet(t, id, code)
	e.checkUpdate(t, id)
	e.checkAddresses(t, id)
	e.checkIdentities(t, id)

	// Log và dấu vết của cả bộ: không SĐT, không tên.
	text := e.logs.String() + testkit.SpanText(exp.GetSpans().Snapshots())
	for _, bad := range []string{"0912345678", "912345678", "Hồng Nhung", "Lê Lợi"} {
		if strings.Contains(text, bad) {
			t.Errorf("log hoặc dấu vết chứa %q", bad)
		}
	}
	testkit.AssertNoPersonalData(t, "log và span", text)
}

// checkCreate: 201, code KH…, danh tính +84…, name_search, sự kiện Seq 1, audit chỉ tên trường; SĐT đã có chủ → 409
// identity_taken kèm ownerId; chỉ xem → 403. Trả mã và code của khách.
func (e *customerAPIEnv) checkCreate(t *testing.T) (string, any) {
	t.Helper()
	rec := e.send(http.MethodPost, "/api/v1/customers", customerWriter, `{"fullName":"Nguyễn Thị Hồng Nhung","phone":"0912345678"}`)
	c := mustOK(t, "createCustomer", rec, http.StatusCreated)
	id := fmt.Sprint(c["id"])
	ids, _ := c["identities"].([]any)
	if !regexp.MustCompile(`^KH\d{6,}$`).MatchString(fmt.Sprint(c["code"])) || c["rootId"] != id || len(ids) != 1 ||
		ids[0].(map[string]any)["value"] != "+84912345678" || c["lifecycle"] != "lead" || c["version"] != float64(1) { //nolint:forcetypeassert // test
		t.Fatalf("khách tạo: %v", c)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND name_search = 'nguyen thi hong nhung'`, id); n != 1 {
		t.Errorf("name_search: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.created' AND aggregate_id = $1
		AND seq = 1`, id); n != 1 {
		t.Errorf("sự kiện customer.created: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs a WHERE action = 'customer.create' AND entity_id = $1
		AND changed_fields @> ARRAY['phone', 'full_name'] AND row_to_json(a)::text NOT LIKE '%912345678%'
		AND row_to_json(a)::text NOT LIKE '%Hồng Nhung%'`, id); n != 1 {
		t.Errorf("audit customer.create: %d", n)
	}

	rec = e.send(http.MethodPost, "/api/v1/customers", customerWriter, `{"fullName":"Khách khác","phone":"0912 345 678"}`)
	if b := decodeJSON(t, rec); rec.Code != http.StatusConflict || b["error"] != "identity_taken" || b["ownerId"] != id {
		t.Errorf("thân identity_taken: %s", rec.Body.String())
	}
	// Kiểm SĐT đầy đủ ở mọi dạng (gõ vào, đã chuẩn hóa, đảo ngược để tìm đuôi), không kiểm mẩu ngắn như "912": thân lỗi
	// mang ownerId và request_id là UUID ngẫu nhiên, chữ số hex có thể chứa "912" (Task 440).
	for _, bad := range []string{"912345678", "912 345 678", "87654321948"} {
		if strings.Contains(rec.Body.String(), bad) {
			t.Errorf("thân identity_taken mang SĐT (%q): %s", bad, rec.Body.String())
		}
	}
	expect(t, "chỉ xem không tạo được", e.send(http.MethodPost, "/api/v1/customers", customerReader, `{"fullName":"Lan"}`),
		http.StatusForbidden, "forbidden")
	return id, c["code"]
}

// checkGet: đọc theo gốc; không có, tenant khác (PQ08) → 404.
func (e *customerAPIEnv) checkGet(t *testing.T, id string, code any) {
	t.Helper()
	got := mustOK(t, "getCustomer", e.send(http.MethodGet, "/api/v1/customers/"+id, customerReader, ""), http.StatusOK)
	if got["code"] != code || len(got["memberIds"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Errorf("getCustomer: %v", got)
	}
	expect(t, "khách không có", e.send(http.MethodGet, "/api/v1/customers/"+uuid.NewString(), customerReader, ""),
		http.StatusNotFound, "not_found")
	var foreign uuid.UUID
	e.staff.Exec(t, e.other, func(tx pgx.Tx) error {
		var err error
		foreign, err = customer.Create(t.Context(), tx, "Khách tenant B")
		return err //nolint:wrapcheck // test
	})
	expect(t, "PQ08 khách của tenant B", e.send(http.MethodGet, "/api/v1/customers/"+foreign.String(), customerReader, ""),
		http.StatusNotFound, "not_found")
	expect(t, "PQ08 sửa khách của tenant B", e.send(http.MethodPatch, "/api/v1/customers/"+foreign.String(), customerWriter,
		`{"version":1,"fullName":"X"}`), http.StatusNotFound, "not_found")
}

// checkUpdate: version cũ → 409; hai PATCH song song cùng version → một 200, một 409.
func (e *customerAPIEnv) checkUpdate(t *testing.T, id string) {
	t.Helper()
	upd := mustOK(t, "updateCustomer", e.send(http.MethodPatch, "/api/v1/customers/"+id, customerWriter,
		`{"version":1,"fullName":"Nguyễn Thị Hồng Nhung","birthday":"1990-03-08"}`), http.StatusOK)
	if upd["version"] != float64(2) || upd["birthday"] != "1990-03-08" {
		t.Errorf("sau sửa: %v", upd)
	}
	expect(t, "version cũ", e.send(http.MethodPatch, "/api/v1/customers/"+id, customerWriter, `{"version":1,"fullName":"Lan"}`),
		http.StatusConflict, "version_conflict")
	recs := make([]*httptest.ResponseRecorder, 2)
	keys := []string{"song-1-" + uuid.NewString(), "song-2-" + uuid.NewString()}
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() {
			recs[i] = serveAs(e.srv, e.keys, http.MethodPatch, "/api/v1/customers/"+id, customerWriter, keys[i],
				fmt.Sprintf(`{"version":2,"fullName":"Tên %d"}`, i))
		})
	}
	wg.Wait()
	codes := map[int]int{}
	for _, r := range recs {
		codes[r.Code]++
	}
	if codes[http.StatusOK] != 1 || codes[http.StatusConflict] != 1 {
		t.Errorf("PATCH song song: %v (%s | %s)", codes, recs[0].Body.String(), recs[1].Body.String())
	}
}

// checkAddresses: xã không thuộc tỉnh → 422; địa chỉ mặc định thứ hai làm địa chỉ cũ hết mặc định; đặt lại mặc định; xóa.
func (e *customerAPIEnv) checkAddresses(t *testing.T, id string) {
	t.Helper()
	addrPath := "/api/v1/customers/" + id + "/addresses"
	addr := func(ward string, def bool) string {
		return fmt.Sprintf(`{"address":{"name":"Chị Nhung","phone":"0912345678","line":"12 Lê Lợi","provinceCode":"T01",`+
			`"wardCode":"%s"},"isDefault":%t}`, ward, def)
	}
	expect(t, "xã không thuộc tỉnh", e.send(http.MethodPost, addrPath, customerWriter, addr("T02001", false)),
		http.StatusUnprocessableEntity, "ward_not_in_province")
	mustOK(t, "địa chỉ 1", e.send(http.MethodPost, addrPath, customerWriter, addr("T01001", false)), http.StatusCreated)
	p := mustOK(t, "địa chỉ 2", e.send(http.MethodPost, addrPath, customerWriter, addr("T01002", true)), http.StatusCreated)
	addrs := p["addresses"].([]any) //nolint:forcetypeassert // test
	if len(addrs) != 2 {
		t.Fatalf("địa chỉ: %v", addrs)
	}
	var defaultWard, otherID string
	var otherVersion float64
	for _, a := range addrs {
		m := a.(map[string]any)                //nolint:forcetypeassert // test
		inner := m["address"].(map[string]any) //nolint:forcetypeassert // test
		if m["isDefault"] == true {
			defaultWard = fmt.Sprint(inner["wardCode"])
		} else {
			otherID, otherVersion = fmt.Sprint(m["id"]), m["version"].(float64) //nolint:forcetypeassert // test
		}
	}
	if defaultWard != "T01002" {
		t.Errorf("mặc định sau địa chỉ 2: %v", addrs)
	}
	p = mustOK(t, "đặt lại mặc định", e.send(http.MethodPatch, addrPath+"/"+otherID, customerWriter,
		fmt.Sprintf(`{"version":%d,"isDefault":true}`, int64(otherVersion))), http.StatusOK)
	for _, a := range p["addresses"].([]any) { //nolint:forcetypeassert // test
		m := a.(map[string]any) //nolint:forcetypeassert // test
		if (m["id"] == otherID) != (m["isDefault"] == true) {
			t.Errorf("mặc định sau sửa: %v", p["addresses"])
		}
	}
	p = mustOK(t, "xóa địa chỉ", e.send(http.MethodDelete, addrPath+"/"+otherID, customerWriter, ""), http.StatusOK)
	if len(p["addresses"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Errorf("sau xóa: %v", p["addresses"])
	}
}

// checkIdentities: thêm email (201), gỡ (200); thiếu quyền ghi → 403.
func (e *customerAPIEnv) checkIdentities(t *testing.T, id string) {
	t.Helper()
	p := mustOK(t, "thêm email", e.send(http.MethodPost, "/api/v1/customers/"+id+"/identities", customerWriter,
		`{"kind":"email","value":"Nhung@Example.vn"}`), http.StatusCreated)
	var emailID string
	for _, i := range p["identities"].([]any) { //nolint:forcetypeassert // test
		m := i.(map[string]any) //nolint:forcetypeassert // test
		if m["kind"] == "email" && m["value"] == "nhung@example.vn" {
			emailID = fmt.Sprint(m["id"])
		}
	}
	if emailID == "" {
		t.Fatalf("không thấy email: %v", p["identities"])
	}
	expect(t, "chỉ xem không gỡ được", e.send(http.MethodDelete, "/api/v1/customers/"+id+"/identities/"+emailID, customerReader, ""),
		http.StatusForbidden, "forbidden")
	p = mustOK(t, "gỡ email", e.send(http.MethodDelete, "/api/v1/customers/"+id+"/identities/"+emailID, customerWriter, ""), http.StatusOK)
	if len(p["identities"].([]any)) != 1 { //nolint:forcetypeassert // test
		t.Errorf("sau gỡ: %v", p["identities"])
	}
}
