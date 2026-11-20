package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

const statementCSV = "ma_van_don,tien_cod,phi_van_chuyen,phi_khac,thuc_nhan\n" +
	"FK0001,630.000,32.000,0,598.000\nFK0002,0,25.000,0,-25.000\nNHANH9999,450000,30000,0,420000\n"

// uploadStatement tải body lên tenant của e (mục đích cod_statement) như kế toán và trả mã tệp.
func (e *m3Env) uploadStatement(name string, body []byte) uuid.UUID {
	e.t.Helper()
	var f files.File
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		f, _, err = files.CreateUpload(e.t.Context(), tx, e.files, files.UploadInput{
			TenantID: e.tenant, Purpose: cash.StatementPurposeKey, FileName: name, ContentType: "text/csv",
			SizeBytes: int64(len(body)),
		}, uuid.Nil)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("CreateUpload: %v", err)
	}
	if err := e.files.Put(e.t.Context(), files.BucketTmp, files.TmpKey(e.tenant, f.ID), bytes.NewReader(body),
		int64(len(body)), "text/csv"); err != nil {
		e.t.Fatal(err)
	}
	if _, err := files.CompleteUpload(e.t.Context(), e.staff.App, e.files, queue.NewQueue(), e.tenant, f.ID); err != nil {
		e.t.Fatalf("CompleteUpload: %v", err)
	}
	return f.ID
}

func importBody(account, file uuid.UUID, format string) string {
	if format != "" {
		return fmt.Sprintf(`{"carrierAccountId":%q,"fileId":%q,"format":%q}`, account, file, format)
	}
	return fmt.Sprintf(`{"carrierAccountId":%q,"fileId":%q}`, account, file)
}

// Bảng kê COD qua HTTP (Task M3-31, TI04): kế toán nhập → 202 imported kèm người nhập, audit cash.statement.import;
// cùng nội dung tải lên lại (tên khác) → 409 statement_duplicate kèm statementId của bảng kê cũ; tài khoản lạ → 404;
// định dạng lạ → 422 invalid_input; tệp chưa tải xong → 409 file_not_ready; Kho (không có cash.statements.import) →
// 403. Sau khi lệnh đọc tệp chạy: GET → parsed, 3 dòng, tổng pending; trang 2 dòng có nextCursor, con trỏ sai → 400;
// bảng kê lạ → 404; danh sách mới nhất trước.
func TestCodStatementsAPI(t *testing.T) {
	e := newM3Env(t)
	var account uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		account = shiptest.NewShop(t, tx, e.tenant).Account
		return nil
	})
	file := e.uploadStatement("bang-ke.csv", []byte(statementCSV))
	code, got := e.send(http.MethodPost, "/api/v1/cod-statements", m3Accounter, importBody(account, file, ""))
	imported, _ := got["importedBy"].(map[string]any)
	if code != http.StatusAccepted || got["status"] != "imported" || got["format"] != "elimaz_v1" ||
		got["carrierKey"] != shiptest.CarrierKey || imported["id"] == nil {
		t.Fatalf("nhập: %d %v", code, got)
	}
	id, _ := got["id"].(string)
	var audits int64
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM identity.audit_logs WHERE action = 'cash.statement.import'
			AND entity_id = $1`, id).Scan(&audits)
	})
	if audits != 1 {
		t.Errorf("%d dòng audit", audits)
	}

	again := e.uploadStatement("doi-ten.csv", []byte(statementCSV))
	if code, b := e.send(http.MethodPost, "/api/v1/cod-statements", m3Accounter, importBody(account, again, "")); code != http.StatusConflict ||
		b["error"] != "statement_duplicate" || b["statementId"] != id {
		t.Errorf("nhập lại: %d %v", code, b)
	}
	checkImportRefusals(t, e, account)
	checkStatementRead(t, e, id)
}

// checkImportRefusals: tài khoản lạ, định dạng lạ, tệp chưa tải xong, tệp không có, người không có quyền nhập.
func checkImportRefusals(t *testing.T, e *m3Env, account uuid.UUID) {
	t.Helper()
	other := e.uploadStatement("khac.csv", []byte("ma_van_don,tien_cod,phi_van_chuyen,thuc_nhan\nFK9,1,0,1\n"))
	var pending files.File
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		pending, _, err = files.CreateUpload(t.Context(), tx, e.files, files.UploadInput{
			TenantID: e.tenant, Purpose: cash.StatementPurposeKey, FileName: "cho.csv", ContentType: "text/csv", SizeBytes: 5,
		}, uuid.Nil)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, email, body, code string
		status                  int
	}{
		{"tài khoản lạ", m3Accounter, importBody(uuid.New(), other, ""), "not_found", http.StatusNotFound},
		{"định dạng lạ", m3Accounter, importBody(account, other, "khong_co"), "invalid_input", http.StatusUnprocessableEntity},
		{"tệp chưa tải xong", m3Accounter, importBody(account, pending.ID, ""), "file_not_ready", http.StatusConflict},
		{"tệp không có", m3Accounter, importBody(account, uuid.New(), ""), "not_found", http.StatusNotFound},
		{"Kho", m3Warehouse, importBody(account, other, ""), "forbidden", http.StatusForbidden},
	} {
		if code, b := e.send(http.MethodPost, "/api/v1/cod-statements", c.email, c.body); code != c.status || b["error"] != c.code {
			t.Errorf("%s: %d %v, mong %d %s", c.name, code, b, c.status, c.code)
		}
	}
}

// checkStatementRead chạy lệnh đọc tệp rồi đọc bảng kê id (3 dòng) và danh sách qua HTTP.
func checkStatementRead(t *testing.T, e *m3Env, id string) {
	t.Helper()
	wk := queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Poll: 50 * time.Millisecond,
		Commands: commands.NewRunner(e.staff.App, e.cash.CommandHandlers()),
	})
	e.staff.DrainJobs(t, wk)
	code, got := e.send(http.MethodGet, "/api/v1/cod-statements/"+id+"?limit=2", m3Accounter, "")
	head, _ := got["statement"].(map[string]any)
	lines, _ := got["lines"].([]any)
	totals, _ := got["totals"].([]any)
	if code != http.StatusOK || head["status"] != "parsed" || head["lineCount"] != float64(3) || len(lines) != 2 ||
		got["nextCursor"] != "3" || len(totals) != 1 {
		t.Fatalf("đọc bảng kê: %d %v", code, got)
	}
	first, _ := lines[0].(map[string]any)
	total, _ := totals[0].(map[string]any)
	if first["trackingCode"] != "FK0001" || first["codAmount"] != float64(630000) || first["netAmount"] != float64(598000) ||
		first["matchStatus"] != "pending" || total["count"] != float64(3) || total["codAmount"] != float64(1080000) {
		t.Errorf("dòng đầu %v, tổng %v", first, total)
	}
	if code, got := e.send(http.MethodGet, "/api/v1/cod-statements/"+id+"?limit=2&cursor=3", m3Accounter, ""); code != http.StatusOK {
		t.Errorf("trang 2: %d %v", code, got)
	} else if ls, _ := got["lines"].([]any); len(ls) != 1 || got["nextCursor"] != nil {
		t.Errorf("trang 2: %v", got)
	}
	if code, b := e.send(http.MethodGet, "/api/v1/cod-statements/"+id+"?cursor=x", m3Accounter, ""); code != http.StatusBadRequest || b["error"] != "invalid_input" {
		t.Errorf("con trỏ sai: %d %v", code, b)
	}
	if code, b := e.send(http.MethodGet, "/api/v1/cod-statements/"+uuid.NewString(), m3Accounter, ""); code != http.StatusNotFound || b["error"] != "not_found" {
		t.Errorf("bảng kê lạ: %d %v", code, b)
	}
	code, got = e.send(http.MethodGet, "/api/v1/cod-statements?status=parsed", m3Manager, "")
	items, _ := got["items"].([]any)
	if code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["id"] != id { //nolint:forcetypeassert // test
		t.Errorf("danh sách: %d %v", code, got)
	}
	if code, b := e.send(http.MethodGet, "/api/v1/cod-statements?cursor=x", m3Manager, ""); code != http.StatusBadRequest || b["error"] != "invalid_input" {
		t.Errorf("con trỏ danh sách sai: %d %v", code, b)
	}
}
