package httpapi

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/xlsx"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	customerImporter = "nhap-khach@elimaz.test" // customer.import, files.upload
	customerSale     = "sale-khach@elimaz.test" // vai trò sale mặc định
)

type customerImportEnv struct {
	t             *testing.T
	staff         *testkit.Staff
	keys          *cfaccesstest.AccessKeys
	store         *files.MemStore
	srv           http.Handler
	tenant, other uuid.UUID
	importer      uuid.UUID
	key           int
}

func newCustomerImportEnv(t *testing.T) *customerImportEnv {
	t.Helper()
	customer.RegisterFilePurposes()
	e := &customerImportEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), store: filestest.MemFileStore(t)}
	e.tenant, e.other = e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	e.importer = e.staff.User(t, e.tenant, customerImporter, "active")
	e.staff.Grant(t, e.tenant, e.importer, []string{"customer.import", "customer.read", "files.upload"}, "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, customerSale, "active"), rolePerms(t, "sale"), "all", "")
	authE := &authEnv{t: t, keys: e.keys}
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App, Logger: discardLogger(t),
		Access: authE.verifier(), Realtime: &realtimetest.RecordingBus{}, Files: e.store,
	})
	return e
}

func (e *customerImportEnv) send(method, path, email, body string) *httptest.ResponseRecorder {
	key := ""
	if method != http.MethodGet {
		e.key++
		key = fmt.Sprintf("nk-%d-%s", e.key, uuid.NewString())
	}
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

// sheet ghi tệp nhập n dòng hợp lệ (SĐT 0914 + 6 chữ số) bằng xlsx.Write.
func (e *customerImportEnv) sheet(n int) []byte {
	e.t.Helper()
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{fmt.Sprintf("Khách %d", i), fmt.Sprintf("0914%06d", i)}
	}
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, []xlsx.Sheet{{Name: "Khách", Header: customer.ImportHeader(), Rows: rows}}); err != nil {
		e.t.Fatal(err)
	}
	return buf.Bytes()
}

// upload tải body lên tenant (CreateUpload, ghi kho, CompleteUpload) với mục đích purpose.
func (e *customerImportEnv) upload(tenant uuid.UUID, purpose, contentType string, body []byte) uuid.UUID {
	e.t.Helper()
	var f files.File
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		var err error
		f, _, err = files.CreateUpload(e.t.Context(), tx, e.store, files.UploadInput{
			TenantID: tenant, Purpose: purpose, FileName: "khach.xlsx", ContentType: contentType, SizeBytes: int64(len(body)),
		}, e.importer)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	if err := e.store.Put(e.t.Context(), files.BucketTmp, files.TmpKey(tenant, f.ID), bytes.NewReader(body),
		int64(len(body)), contentType); err != nil {
		e.t.Fatal(err)
	}
	if purpose == customer.ImportPurposeKey {
		if _, err := files.CompleteUpload(e.t.Context(), e.staff.App, e.store, queue.NewQueue(), tenant, f.ID); err != nil {
			e.t.Fatal(err)
		}
	}
	return f.ID
}

// TestCustomerImportAPI: mẫu tệp; bắt đầu nhập → 202 importId, chạy lệnh → done với số đếm; 20.001 dòng → 422
// import_too_large (không tạo lần nhập); tệp ảnh, tệp tenant khác → 422 import_file_invalid; createUpload 6 MiB với
// mục đích customer_import → 422 file_too_large; vai trò sale → 403 ở cả ba route.
func TestCustomerImportAPI(t *testing.T) {
	t.Parallel()
	e := newCustomerImportEnv(t)

	tpl := e.send(http.MethodGet, "/api/v1/customer-imports/template", customerImporter, "")
	if tpl.Code != http.StatusOK || !strings.Contains(tpl.Header().Get("Content-Disposition"), ".xlsx") ||
		tpl.Header().Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("mẫu tệp: %d %v", tpl.Code, tpl.Header())
	}
	if _, _, err := xlsx.Read(bytes.NewReader(tpl.Body.Bytes()), int64(tpl.Body.Len()), xlsx.Limits{}, customer.ImportHeader()); err != nil {
		t.Errorf("mẫu đọc lại: %v", err)
	}

	fileID := e.upload(e.tenant, customer.ImportPurposeKey, xlsxContentType, e.sheet(3))
	rec := e.send(http.MethodPost, "/api/v1/customer-imports", customerImporter, fmt.Sprintf(`{"fileId":%q}`, fileID))
	out := mustOK(t, "startCustomerImport", rec, http.StatusAccepted)
	importID := fmt.Sprint(out["importId"])
	got := mustOK(t, "getCustomerImport", e.send(http.MethodGet, "/api/v1/customer-imports/"+importID, customerImporter, ""), http.StatusOK)
	if got["status"] != "pending" || got["id"] != importID {
		t.Fatalf("lần nhập mới: %v", got)
	}
	e.runImporter()
	got = mustOK(t, "getCustomerImport", e.send(http.MethodGet, "/api/v1/customer-imports/"+importID, customerImporter, ""), http.StatusOK)
	if got["status"] != "done" || got["totalRows"] != float64(3) || got["okRows"] != float64(3) ||
		got["skippedRows"] != float64(0) || got["errorRows"] != float64(0) || got["finishedAt"] == nil {
		t.Fatalf("sau khi chạy: %v", got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers`); n != 3 {
		t.Errorf("số khách: %d", n)
	}
	expect(t, "lần nhập không có", e.send(http.MethodGet, "/api/v1/customer-imports/"+uuid.NewString(), customerImporter, ""),
		http.StatusNotFound, "not_found")

	big := e.upload(e.tenant, customer.ImportPurposeKey, xlsxContentType, e.sheet(20_001))
	expect(t, "20.001 dòng", e.send(http.MethodPost, "/api/v1/customer-imports", customerImporter, fmt.Sprintf(`{"fileId":%q}`, big)),
		http.StatusUnprocessableEntity, "import_too_large")
	img := e.upload(e.tenant, "product_image", "image/jpeg", []byte("\xff\xd8\xff"))
	expect(t, "tệp ảnh sản phẩm", e.send(http.MethodPost, "/api/v1/customer-imports", customerImporter, fmt.Sprintf(`{"fileId":%q}`, img)),
		http.StatusUnprocessableEntity, "import_file_invalid")
	other := e.upload(e.other, customer.ImportPurposeKey, xlsxContentType, e.sheet(1))
	expect(t, "tệp tenant khác", e.send(http.MethodPost, "/api/v1/customer-imports", customerImporter, fmt.Sprintf(`{"fileId":%q}`, other)),
		http.StatusUnprocessableEntity, "import_file_invalid")
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customer_imports`); n != 1 {
		t.Errorf("tệp bị từ chối không được tạo lần nhập: %d", n)
	}
	otherImport := e.startIn(e.other, other)
	expect(t, "lần nhập của tenant khác", e.send(http.MethodGet, "/api/v1/customer-imports/"+otherImport.String(), customerImporter, ""),
		http.StatusNotFound, "not_found")

	expect(t, "tệp 6 MiB", e.send(http.MethodPost, "/api/v1/files/uploads", customerImporter,
		fmt.Sprintf(`{"purpose":"customer_import","fileName":"k.xlsx","contentType":%q,"sizeBytes":%d}`, xlsxContentType, 6<<20)),
		http.StatusUnprocessableEntity, "file_too_large")

	expect(t, "sale tải mẫu", e.send(http.MethodGet, "/api/v1/customer-imports/template", customerSale, ""), http.StatusForbidden, "forbidden")
	expect(t, "sale bắt đầu nhập", e.send(http.MethodPost, "/api/v1/customer-imports", customerSale, fmt.Sprintf(`{"fileId":%q}`, fileID)),
		http.StatusForbidden, "forbidden")
	expect(t, "sale xem lần nhập", e.send(http.MethodGet, "/api/v1/customer-imports/"+importID, customerSale, ""), http.StatusForbidden, "forbidden")
}

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// startIn tạo lần nhập trong tenant cho tệp fileID bằng module (không qua HTTP).
func (e *customerImportEnv) startIn(tenant, fileID uuid.UUID) uuid.UUID {
	e.t.Helper()
	w := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	var imp customer.Import
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		var err error
		imp, err = w.StartImport(e.t.Context(), tx, fileID, customer.Actor{Type: customer.ActorStaff, ID: e.importer})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return imp.ID
}

// runImporter chạy lệnh customer.import tới khi hết việc đến hạn.
func (e *customerImportEnv) runImporter() {
	e.t.Helper()
	w := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	imp := customer.NewImporter(customer.ImporterOptions{DB: e.staff.App, Store: e.store, Writer: w})
	wk := queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Poll: 50 * time.Millisecond,
		Commands: commands.NewRunner(e.staff.App, []commands.Handler{imp}),
	})
	testkit.DrainDue(e.t, e.staff.Owner, wk)
}

func discardLogger(t *testing.T) *slog.Logger {
	t.Helper()
	l, _ := testkit.CaptureLog(t)
	return l
}
