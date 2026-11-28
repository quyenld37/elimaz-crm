package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Đếm cộng dồn và nhập số đếm bằng Excel qua API (kế hoạch M7, Task 263 = M7-18).

const countXlsx = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// grantOtherStocktake cho người của tenant B quyền đếm ở mọi kho của B (kiểm tenant khác → 404, không 403).
func (e *invAPIEnv) grantOtherStocktake() {
	e.t.Helper()
	var b, user uuid.UUID
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(), `SELECT tenant_id FROM platform.tenant_routes
		WHERE kind = 'staff_email' AND external_key = $1`, invOtherB).Scan(&b); err != nil {
		e.t.Fatal(err)
	}
	e.staff.Exec(e.t, b, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id FROM identity.users WHERE email = $1`, invOtherB).Scan(&user)
	})
	e.staff.Grant(e.t, b, user, []string{"inventory.stocktake"}, "all", "")
}

// withFiles dựng lại máy chủ của e với kho tệp trong bộ nhớ.
func (e *invAPIEnv) withFiles() *files.MemStore {
	e.t.Helper()
	inventory.RegisterCountImportPurpose()
	store := filestest.MemFileStore(e.t)
	authE := &authEnv{t: e.t, keys: e.keys}
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App, Files: store,
		Logger: slog.New(slog.NewJSONHandler(e.logs, nil)), Access: authE.verifier(),
	})
	return store
}

func (e *invAPIEnv) increment(email, id, key, body string) map[string]any {
	e.t.Helper()
	return decodeJSON(e.t, e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/increments", email, key, body))
}

// uploadCount tải tệp .xlsx với dòng tiêu đề "Mã", "Số đếm" và rows vào kho tệp như nhân viên; trả mã tệp.
func (e *invAPIEnv) uploadCount(store *files.MemStore, rows [][]string) uuid.UUID {
	e.t.Helper()
	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{"Mã", "Số đếm"})
	for i, r := range rows {
		_ = f.SetSheetRow("Sheet1", fmt.Sprintf("A%d", i+2), &[]any{r[0], r[1]})
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		e.t.Fatal(err)
	}
	_ = f.Close()
	body := buf.Bytes()
	var file files.File
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		var err error
		file, _, err = files.CreateUpload(e.t.Context(), tx, store, files.UploadInput{
			TenantID: e.tenant, Purpose: inventory.CountImportPurposeKey, FileName: "dem.xlsx", ContentType: countXlsx,
			SizeBytes: int64(len(body)),
		}, uuid.New())
		return err //nolint:wrapcheck // test
	})
	if err := store.Put(e.t.Context(), files.BucketTmp, files.TmpKey(e.tenant, file.ID), bytes.NewReader(body),
		int64(len(body)), countXlsx); err != nil {
		e.t.Fatal(err)
	}
	if _, err := files.CompleteUpload(e.t.Context(), e.staff.App, store, queue.NewQueue(), e.tenant, file.ID); err != nil {
		e.t.Fatal(err)
	}
	return file.ID
}

// 3 người × 30 lần +1 song song qua API (mỗi lần một khóa) → 90; quét barcode; mã lạ 422 unknown_code; trừ quá 422
// negative_count (số giữ nguyên); quyền chỉ kho w1 → 403 ở w2; sau khi đăng 409 stocktake_not_open; tenant khác 404;
// phiên kiểm thường 400; mỗi lần cộng một dòng audit.
func TestCountAPI_Increment(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	e.grantOpening()
	e.grantStocktake()
	e.grantOtherStocktake()
	v := e.variant(e.w1, 0)
	v2 := e.variant(e.w2, 0)
	o := mustOK(t, "mở w1", e.createOpening(openAll, e.w1, "o1"), http.StatusCreated)
	id := o["stocktakeId"].(string) //nolint:forcetypeassert // test
	w2 := mustOK(t, "mở w2", e.createOpening(openAll, e.w2, "o2"), http.StatusCreated)

	var wg sync.WaitGroup
	for p := range 3 {
		wg.Go(func() {
			for i := range 30 {
				rec := e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/increments", openW1,
					fmt.Sprintf("inc-%d-%d", p, i), `{"code":"qk01-1","by":1}`)
				if rec.Code != http.StatusOK {
					t.Errorf("cộng song song: %d %s", rec.Code, rec.Body.String())
				}
			}
		})
	}
	wg.Wait()
	if r := e.increment(openW1, id, "b1", `{"code":"BC000001","by":2}`); r["counted"] != float64(92) || r["variantId"] != v.String() {
		t.Fatalf("quét barcode: %v", r)
	}
	expect(t, "mã lạ", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/increments", openW1, "u1",
		`{"code":"KHONGCO","by":1}`), http.StatusUnprocessableEntity, "unknown_code")
	expect(t, "trừ quá", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/increments", openW1, "n1",
		`{"code":"QK01-1","by":-93}`), http.StatusUnprocessableEntity, "negative_count")
	expect(t, "w2 bằng quyền w1", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+w2["stocktakeId"].(string)+"/increments", //nolint:forcetypeassert // test
		openW1, "f1", `{"code":"QK01-2","by":1}`), http.StatusForbidden, "forbidden")
	expect(t, "tenant B", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/increments", invOtherB, "x1",
		`{"code":"QK01-1","by":1}`), http.StatusNotFound, "not_found")
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.audit_logs WHERE action = 'inventory.stocktake.increment'"); n != 91 {
		t.Fatalf("audit cộng: %d dòng, mong 91", n)
	}
	mustOK(t, "đếm v2 ở w1", e.countStk(openAll, id, v2, `{"counted":0}`, "c2"), http.StatusOK)
	mustOK(t, "đăng w1", e.postOpening(openAll, id, "p1"), http.StatusOK)
	expect(t, "sau khi đăng", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/increments", openW1, "a1",
		`{"code":"QK01-1","by":1}`), http.StatusConflict, "stocktake_not_open")
	if e.onHand(v, e.w1) != 92 {
		t.Fatalf("tồn mở sổ: %d, mong 92", e.onHand(v, e.w1))
	}
	reg := mustOK(t, "phiên thường w1", e.startStk(stkApply, e.w1, "s1"), http.StatusCreated)
	expect(t, "phiên thường", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+reg["id"].(string)+"/increments", stkApply, //nolint:forcetypeassert // test
		"r1", `{"code":"QK01-1","by":1}`), http.StatusBadRequest, "invalid_input")
}

// Tạo bản nhập → 202 queued; việc nền áp → đọc lại done, lỗi từng dòng không nội dung ô; replace lần hai → cùng bản;
// add lần hai → 409 import_already_applied; tệp không có → 404; đọc bản nhập bằng quyền chỉ w1 khi phiên ở w2 → 403.
func TestCountAPI_Import(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	e.grantOpening()
	e.grantOtherStocktake()
	store := e.withFiles()
	v := e.variant(e.w2, 0)
	o := mustOK(t, "mở w2", e.createOpening(openAll, e.w2, "o1"), http.StatusCreated)
	id := o["stocktakeId"].(string) //nolint:forcetypeassert // test
	fid := e.uploadCount(store, [][]string{{"QK01-1", "3"}, {"BIMAT-LA", "1"}, {"BC000001", "4"}})
	create := func(mode, key string) map[string]any {
		return decodeJSON(t, e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/count-imports", openAll, key,
			fmt.Sprintf(`{"fileId":%q,"mode":%q}`, fid, mode)))
	}
	rec := e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/count-imports", openAll, "i1",
		fmt.Sprintf(`{"fileId":%q,"mode":"replace"}`, fid))
	imp := mustOK(t, "tạo bản nhập", rec, http.StatusAccepted)
	if imp["status"] != "queued" || imp["mode"] != "replace" || imp["stocktakeId"] != id {
		t.Fatalf("bản nhập: %v", imp)
	}
	impID := imp["id"].(string) //nolint:forcetypeassert // test
	wk := queue.NewWorker(e.staff.App, inventory.CountImportJobs(inventory.CountImportDeps{Now: time.Now}),
		queue.WorkerOptions{WorkerID: "w-" + uuid.NewString()})
	var got map[string]any
	for range 100 { // chốt chống treo
		if _, err := wk.RunOnce(t.Context(), 10); err != nil {
			t.Fatal(err)
		}
		got = mustOK(t, "đọc bản nhập", e.do(http.MethodGet, "/api/v1/inventory/count-imports/"+impID, openAll, "", ""), http.StatusOK)
		if got["status"] == "done" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, _ := json.Marshal(got)
	if got["status"] != "done" || got["appliedRows"] != float64(2) || !strings.Contains(string(raw), `"errorRows":[{"code":"unknown_code","row":3}]`) ||
		strings.Contains(string(raw), "BIMAT") || got["finishedAt"] == nil {
		t.Fatalf("bản nhập xong: %s", raw)
	}
	if n := e.staff.CountIn(t, e.tenant, "SELECT counted FROM inventory.stocktake_lines WHERE stocktake_id = $1 AND variant_id = $2", id, v); n != 7 {
		t.Fatalf("số đếm: %d, mong 7", n)
	}
	if again := create("replace", "i2"); again["id"] != impID {
		t.Fatalf("replace lần hai: %v", again)
	}
	if first := create("add", "i3"); first["status"] != "queued" {
		t.Fatalf("add lần một: %v", first)
	}
	expect(t, "add lần hai", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/count-imports", openAll, "i4",
		fmt.Sprintf(`{"fileId":%q,"mode":"add"}`, fid)), http.StatusConflict, "import_already_applied")
	expect(t, "tệp không có", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/count-imports", openAll, "i5",
		fmt.Sprintf(`{"fileId":%q,"mode":"add"}`, uuid.New())), http.StatusNotFound, "not_found")
	expect(t, "tạo bằng quyền w1", e.do(http.MethodPost, "/api/v1/inventory/stocktakes/"+id+"/count-imports", openW1, "i6",
		fmt.Sprintf(`{"fileId":%q,"mode":"replace"}`, fid)), http.StatusForbidden, "forbidden")
	expect(t, "đọc bằng quyền w1", e.do(http.MethodGet, "/api/v1/inventory/count-imports/"+impID, openW1, "", ""),
		http.StatusForbidden, "forbidden")
	expect(t, "tenant B", e.do(http.MethodGet, "/api/v1/inventory/count-imports/"+impID, invOtherB, "", ""),
		http.StatusNotFound, "not_found")
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.audit_logs WHERE action = 'inventory.count_import.create'"); n != 2 {
		t.Fatalf("audit tạo bản nhập: %d, mong 2", n)
	}
	if strings.Contains(e.logs.text(), "BIMAT") {
		t.Fatal("log chứa nội dung ô")
	}
}

// ambiguous_code kèm candidates (mỗi biến thể trùng mã: variantId, sellerId, sku).
func TestCountAPI_AmbiguousBody(t *testing.T) {
	t.Parallel()
	a, b := uuid.New(), uuid.New()
	body, status, ok := inventoryDetail(fmt.Errorf("cộng: %w", &inventory.AmbiguousCodeError{Candidates: []inventory.CodeCandidate{
		{VariantID: a, SellerID: b, SKU: "TRUNG-1"}, {VariantID: b, SellerID: a, SKU: "TRUNG-1"},
	}}))
	raw, _ := json.Marshal(body)
	want := fmt.Sprintf(`{"error":"ambiguous_code","request_id":"","candidates":[{"variantId":%q,"sellerId":%q,"sku":"TRUNG-1"},{"variantId":%q,"sellerId":%q,"sku":"TRUNG-1"}]}`, a, b, b, a)
	if !ok || status != http.StatusUnprocessableEntity || string(raw) != want {
		t.Fatalf("thân lỗi: %d %s", status, raw)
	}
}
