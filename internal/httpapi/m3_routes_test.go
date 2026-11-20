package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	m3Owner     = "chu-m3@elimaz.test"
	m3Reader    = "chi-xem-van-don@elimaz.test"
	m3Manager   = "quan-ly-m3@elimaz.test"
	m3Warehouse = "kho-m3@elimaz.test"
	m3Accounter = "ke-toan-m3@elimaz.test"
)

type m3Env struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *testkit.AccessKeys
	srv    http.Handler
	tenant uuid.UUID
	key    int
	// files, cash: kho tệp và module tiền mà server dùng (Deps.Files, Deps.Cash; bảng kê COD, Task M3-31).
	files *files.MemStore
	cash  *cash.Module
}

func newM3Env(t *testing.T) *m3Env {
	t.Helper()
	cash.RegisterFilePurposes()
	fulfillment.RegisterFilePurposes()
	e := &m3Env{t: t, staff: testkit.OpenStaff(t), keys: testkit.AccessKeyServer(t)}
	e.tenant = e.staff.Tenant(t, "A")
	for email, role := range map[string]string{m3Owner: "owner", m3Manager: "manager", m3Warehouse: "warehouse", m3Accounter: "accountant"} {
		e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, email, "active"), rolePerms(t, role), "all", "")
	}
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, m3Reader, "active"), []string{"fulfillment.read"}, "all", "")
	authE := &authEnv{t: t, keys: e.keys}
	e.files = testkit.MemFileStore(t)
	e.cash = cash.New(cash.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Files: e.files})
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App, Logger: discardLogger(t),
		Access: authE.verifier(), Realtime: &testkit.RecordingBus{}, Files: e.files, Cash: e.cash.Service,
	})
	return e
}

func (e *m3Env) send(method, path, email, body string) (int, map[string]any) {
	e.t.Helper()
	key := ""
	if method != http.MethodGet {
		e.key++
		key = fmt.Sprintf("m3-%d", e.key)
	}
	rec := serveAs(e.srv, e.keys, method, path, email, key, body)
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	return rec.Code, decodeJSON(e.t, rec)
}

// Thiếu quyền vẫn 403 trước handler (guard kiểm quyền trước): user chỉ có fulfillment.read gọi báo phí
// (fulfillment.quote, Task M3-04). Không phụ thuộc operation đã làm hay chưa.
func TestM3Routes_Forbidden(t *testing.T) {
	e := newM3Env(t)
	path := "/api/v1/orders/" + uuid.NewString() + "/shipping-quotes"
	if code, b := e.send(http.MethodPost, path, m3Reader, `{}`); code != http.StatusForbidden || b["error"] != "forbidden" {
		t.Errorf("chỉ fulfillment.read: mong 403, có %d %v", code, b)
	}
}

// m3Pending đọc tests/arch/testdata/m3_pending.txt: operationId của M3 được tạm trả 501 (mỗi dòng một mã; # là chú
// thích). Task của operation xóa dòng khi làm xong; Task M3-48 đòi tệp rỗng.
func m3Pending(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "arch", "testdata", "m3_pending.txt"))
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

// Mọi route (trừ công khai) gọi bằng thẻ owner, mã đường là uuid ngẫu nhiên, thân {}: trả 501 not_implemented thì
// operation phải nằm trong m3_pending.txt; operation trong danh sách chỉ được trả 501, hoặc 400 khi thân {} không qua
// kiểm hợp đồng trước handler (thân rỗng không tới được handler). Trả mã khác (2xx, 404, 409, 422) là đã làm: xóa
// dòng khỏi danh sách. Không dựa vào một operation cụ thể nào (bẫy Task 427).
func TestM3Routes_PendingList(t *testing.T) {
	e := newM3Env(t)
	pending := m3Pending(t)
	param := regexp.MustCompile(`\{[^}]+\}`)
	stub := 0
	for _, r := range Routes {
		if r.Public || r.OperationID == "stream" {
			continue
		}
		_, path, _ := strings.Cut(r.Pattern, " ")
		path = param.ReplaceAllString(path, uuid.NewString())
		body := ""
		if r.Method != http.MethodGet {
			body = `{}`
		}
		key := ""
		if r.Method != http.MethodGet {
			e.key++
			key = fmt.Sprintf("m3-%d", e.key)
		}
		rec := serveAs(e.srv, e.keys, r.Method, path, m3Owner, key, body)
		code, b := rec.Code, rec.Body.String()
		switch {
		case code == http.StatusNotImplemented && !pending[r.OperationID]:
			t.Errorf("%s trả 501 mà không có trong m3_pending.txt", r.OperationID)
		case code == http.StatusNotImplemented:
			stub++
		case pending[r.OperationID] && code != http.StatusBadRequest:
			t.Errorf("%s có trong m3_pending.txt mà trả %d %s: đã làm thì xóa dòng", r.OperationID, code, b)
		}
	}
	t.Logf("%d operation trả 501, danh sách chờ có %d dòng", stub, len(pending))
}

func fulfillmentSettingsBody(version int64, highRate int, shipping int) string {
	return fmt.Sprintf(`{"version":%d,"stuckHours":{"awaiting_pickup":48,"shipping":%d,"delivery_failed":24,"returning":168,`+
		`"cancel_requested":24},"callbackDeadlineHours":24,"pollMinutes":{"awaiting_pickup":120,"shipping":60,`+
		`"delivery_failed":30,"returning":360,"cancel_requested":15},"defaultWeightGramsPerItem":300,"quoteValidMinutes":30,`+
		`"returnRisk":{"watchMinReturned":1,"highMinReturned":2,"highRateBp":%d},"codOverdueDays":14,`+
		`"agedDays":{"held":20,"inTransit":180}}`, version, shipping, highRate)
}

// Cài đặt vận chuyển (Task M3-04): đọc mặc định (version 0); HighRateBP = 0 → 422; lưu hợp lệ → audit có
// changed_fields; version cũ → 409 version_conflict; Kho đọc được, không sửa được (fulfillment.settings.manage).
func TestFulfillmentSettingsAPI(t *testing.T) {
	e := newM3Env(t)
	code, got := e.send(http.MethodGet, "/api/v1/fulfillment-settings", m3Warehouse, "")
	stuck, _ := got["stuckHours"].(map[string]any)
	risk, _ := got["returnRisk"].(map[string]any)
	if code != http.StatusOK || got["version"] != float64(0) || stuck["shipping"] != float64(72) || risk["highRateBp"] != float64(3000) {
		t.Fatalf("mặc định: %d %v", code, got)
	}
	if code, b := e.send(http.MethodPut, "/api/v1/fulfillment-settings", m3Manager, fulfillmentSettingsBody(0, 0, 72)); code != http.StatusUnprocessableEntity || b["error"] != "setting_out_of_range" {
		t.Fatalf("highRateBp 0: mong 422 setting_out_of_range, có %d %v", code, b)
	}
	if code, b := e.send(http.MethodPut, "/api/v1/fulfillment-settings", m3Warehouse, fulfillmentSettingsBody(0, 3000, 96)); code != http.StatusForbidden {
		t.Fatalf("Kho sửa cài đặt: mong 403, có %d %v", code, b)
	}
	code, got = e.send(http.MethodPut, "/api/v1/fulfillment-settings", m3Manager, fulfillmentSettingsBody(0, 3000, 96))
	stuck, _ = got["stuckHours"].(map[string]any)
	if code != http.StatusOK || got["version"] != float64(1) || stuck["shipping"] != float64(96) {
		t.Fatalf("lưu hợp lệ: %d %v", code, got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.settings.update'
		AND entity_type = 'fulfillment.settings' AND changed_fields @> ARRAY['stuckHours']
		AND NOT changed_fields && ARRAY['returnRisk', 'pollMinutes']`); n != 1 {
		t.Errorf("audit của lần lưu (changed_fields chỉ stuckHours): %d dòng", n)
	}
	if code, b := e.send(http.MethodPut, "/api/v1/fulfillment-settings", m3Manager, fulfillmentSettingsBody(0, 3000, 80)); code != http.StatusConflict || b["error"] != "version_conflict" {
		t.Fatalf("version cũ: mong 409 version_conflict, có %d %v", code, b)
	}
	if code, b := e.send(http.MethodPut, "/api/v1/fulfillment-settings", m3Manager, strings.Replace(fulfillmentSettingsBody(1, 3000, 80), `"returning":168,`, `"lost":5,"returning":168,`, 1)); code != http.StatusUnprocessableEntity {
		t.Fatalf("trạng thái lạ: mong 422, có %d %v", code, b)
	}
	if code, b := e.send(http.MethodPut, "/api/v1/fulfillment-settings", m3Manager, fulfillmentSettingsBody(-1, 3000, 80)); code != http.StatusBadRequest {
		t.Fatalf("version âm: mong 400, có %d %v", code, b)
	}
	if _, got = e.send(http.MethodGet, "/api/v1/fulfillment-settings", m3Owner, ""); got["version"] != float64(1) {
		t.Fatalf("lần từ chối không được đổi cài đặt: %v", got)
	}
}

// Mục đích cod_statement (Task M3-04): Kế toán (vai trò mẫu, có files.upload từ M3) tạo phiên tải bảng kê .xlsx 3 MiB được, 11 MiB → 422 file_too_large;
// carrier_label không nhận tải lên.
func TestCodStatementUploadPurpose(t *testing.T) {
	e := newM3Env(t)
	upload := func(purpose, name, ct string, size int) (int, map[string]any) {
		return e.send(http.MethodPost, "/api/v1/files/uploads", m3Accounter,
			fmt.Sprintf(`{"purpose":%q,"fileName":%q,"contentType":%q,"sizeBytes":%d}`, purpose, name, ct, size))
	}
	if code, b := upload("cod_statement", "bk.xlsx", xlsxContentType, 3<<20); code != http.StatusCreated && code != http.StatusOK {
		t.Errorf("bảng kê 3 MiB: mong được, có %d %v", code, b)
	}
	if code, b := upload("cod_statement", "bk.csv", "text/csv", 1<<20); code != http.StatusCreated && code != http.StatusOK {
		t.Errorf("bảng kê csv: mong được, có %d %v", code, b)
	}
	if code, b := upload("cod_statement", "bk.xlsx", xlsxContentType, 11<<20); code != http.StatusUnprocessableEntity || b["error"] != "file_too_large" {
		t.Errorf("bảng kê 11 MiB: mong 422 file_too_large, có %d %v", code, b)
	}
	if code, b := upload("carrier_label", "tem.pdf", "application/pdf", 1<<20); code != http.StatusUnprocessableEntity {
		t.Errorf("nhãn hãng không nhận tải lên: mong 422, có %d %v", code, b)
	}
}
