package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	invManager = "ql-kho@elimaz.test"   // mọi quyền kho, phạm vi all
	invPostW1  = "ghiso-w1@elimaz.test" // đọc, soạn, ghi sổ chỉ ở kho w1 (MAIN)
	invCreator = "soan@elimaz.test"     // đọc, soạn ở mọi kho; không ghi sổ
	invOtherB  = "kho-b@elimaz.test"    // tenant B, mọi quyền kho
	testPhone  = "0911222333"
)

type invAPIEnv struct {
	t        *testing.T
	staff    *testkit.Staff
	keys     *cfaccesstest.AccessKeys
	srv      http.Handler
	logs     *lockedBuffer
	tenant   uuid.UUID
	w1, w2   uuid.UUID
	seller   uuid.UUID
	product  uuid.UUID
	nVariant int
}

func newInvAPIEnv(t *testing.T) *invAPIEnv {
	t.Helper()
	e := &invAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t), logs: &lockedBuffer{buf: &bytes.Buffer{}}}
	e.tenant = e.staff.Tenant(t, "A")
	b := e.staff.Tenant(t, "B")
	e.seller, e.w1 = seedSellerWarehouse(t, e.staff, e.tenant)
	seedSellerWarehouse(t, e.staff, b)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, 'W2', 'Kho phụ') RETURNING id`, e.tenant, e.seller).Scan(&e.w2); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(t.Context(), `INSERT INTO catalog.products (tenant_id, seller_id, status, code, name, name_search)
			VALUES ($1, $2, 'active', 'QK01', 'Quần kho', 'quan kho') RETURNING id`, e.tenant, e.seller).Scan(&e.product)
	})
	all := []string{"inventory.read", "inventory.documents.create", "inventory.documents.post", "inventory.suppliers.manage"}
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, invManager, "active"), all, "all", "")
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, invPostW1, "active"),
		[]string{"inventory.read", "inventory.documents.create", "inventory.documents.post"}, "warehouse", e.w1.String())
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, invCreator, "active"),
		[]string{"inventory.read", "inventory.documents.create"}, "all", "")
	e.staff.Grant(t, b, e.staff.User(t, b, invOtherB, "active"), all, "all", "")
	authE := &authEnv{t: t, keys: e.keys}
	e.srv = NewServer(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App,
		Logger: slog.New(slog.NewJSONHandler(e.logs, nil)), Access: authE.verifier(),
	})
	return e
}

// seedSellerWarehouse tạo người bán mặc định và kho MAIN của tenant (chủ bảng).
func seedSellerWarehouse(t *testing.T, staff *testkit.Staff, tenant uuid.UUID) (seller, warehouse uuid.UUID) {
	t.Helper()
	staff.Exec(t, tenant, func(tx pgx.Tx) (err error) {
		if seller, err = catalog.EnsureDefaultSeller(t.Context(), tx, tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		warehouse, err = inventory.EnsureDefaultWarehouse(t.Context(), tx, tenant, seller)
		return err //nolint:wrapcheck // test
	})
	return seller, warehouse
}

func (e *invAPIEnv) do(method, path, email, key, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

// variant tạo một biến thể của mẫu thử, nạp onHand (> 0) ở kho wh.
func (e *invAPIEnv) variant(wh uuid.UUID, onHand int32) uuid.UUID {
	e.t.Helper()
	e.nVariant++
	var v uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(e.t.Context(), `INSERT INTO catalog.variants
			(tenant_id, seller_id, product_id, sku, barcode, color, size, price)
			VALUES ($1, $2, $3, $4, $5, 'Đỏ', $6, 100000) RETURNING id`, e.tenant, e.seller, e.product,
			fmt.Sprintf("QK01-%d", e.nVariant), fmt.Sprintf("BC%06d", e.nVariant), fmt.Sprintf("S%d", e.nVariant)).Scan(&v); err != nil {
			return err //nolint:wrapcheck // test
		}
		if onHand > 0 {
			invtest.SeedStock(e.t, tx, v, wh, onHand)
		}
		return nil
	})
	return v
}

func (e *invAPIEnv) onHand(v, wh uuid.UUID) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant,
		"SELECT coalesce(sum(on_hand), 0)::int FROM inventory.stock_balances WHERE variant_id = $1 AND warehouse_id = $2", v, wh)
}

func mustOK(t *testing.T, name string, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("%s: mã %d, mong %d (%s)", name, rec.Code, status, rec.Body.String())
	}
	return decodeJSON(t, rec)
}

// draft tạo phiếu nháp ở kho wh và đặt dòng; trả (id, version).
func (e *invAPIEnv) draft(email, kind string, wh uuid.UUID, supplier, lines string) (string, int) {
	e.t.Helper()
	body := fmt.Sprintf(`{"warehouseId":%q,"kind":%q`, wh, kind)
	if supplier != "" {
		body += fmt.Sprintf(`,"supplierId":%q`, supplier)
	}
	d := mustOK(e.t, "createStockDocument", e.do(http.MethodPost, "/api/v1/inventory/documents", email, "d-"+uuid.NewString(), body+"}"),
		http.StatusCreated)
	id := d["id"].(string) //nolint:forcetypeassert // test
	d = mustOK(e.t, "replaceStockDocumentLines", e.do(http.MethodPut, "/api/v1/inventory/documents/"+id+"/lines", email,
		"l-"+uuid.NewString(), fmt.Sprintf(`{"version":%v,"lines":%s}`, d["version"], lines)), http.StatusOK)
	return id, int(d["version"].(float64)) //nolint:forcetypeassert // test
}

func (e *invAPIEnv) post(id string, version int, email, key string) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, "/api/v1/inventory/documents/"+id+"/post", email, key, fmt.Sprintf(`{"version":%d}`, version))
}

// KH03, C5: nhà cung cấp và phiếu nhập qua API; lỗi trùng tên, sai loại, đã lưu trữ; SĐT không vào log.
func TestInventoryAPI_SupplierAndImport(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	s := mustOK(t, "createSupplier", e.do(http.MethodPost, "/api/v1/inventory/suppliers", invManager, "s1",
		`{"name":"Xưởng Hoa","phone":"`+testPhone+`","note":"Lụa"}`), http.StatusCreated)
	sid := s["id"].(string) //nolint:forcetypeassert // test
	if s["phone"] != testPhone || s["status"] != "active" || s["version"] != float64(1) {
		t.Fatalf("NCC tạo sai: %v", s)
	}
	expect(t, "trùng tên", e.do(http.MethodPost, "/api/v1/inventory/suppliers", invManager, "s2", `{"name":"Xưởng Hoa"}`),
		http.StatusConflict, "supplier_name_taken")
	expect(t, "SĐT sai", e.do(http.MethodPost, "/api/v1/inventory/suppliers", invManager, "s3", `{"name":"B","phone":"12ab"}`),
		http.StatusBadRequest, "invalid_input")
	expect(t, "không có inventory.suppliers.manage", e.do(http.MethodPost, "/api/v1/inventory/suppliers", invCreator, "s4",
		`{"name":"C"}`), http.StatusForbidden, "forbidden")

	expect(t, "phiếu xuất có NCC", e.do(http.MethodPost, "/api/v1/inventory/documents", invManager, "x1",
		fmt.Sprintf(`{"warehouseId":%q,"kind":"export","supplierId":%q}`, e.w1, sid)), http.StatusUnprocessableEntity,
		"supplier_not_for_kind")

	v := e.variant(e.w1, 0)
	id, ver := e.draft(invManager, "import", e.w1, sid,
		fmt.Sprintf(`[{"variantId":%q,"quantity":10,"unitCost":180000}]`, v))
	d := mustOK(t, "postStockDocument", e.post(id, ver, invManager, "p1"), http.StatusOK)
	if d["status"] != "posted" || d["supplierName"] != "Xưởng Hoa" || e.onHand(v, e.w1) != 10 {
		t.Fatalf("phiếu nhập sau ghi sổ: %v, on_hand %d", d, e.onHand(v, e.w1))
	}
	lines := d["lines"].([]any)                                                                           //nolint:forcetypeassert // test
	if l := lines[0].(map[string]any); l["unitCost"] != float64(180000) || l["quantity"] != float64(10) { //nolint:forcetypeassert // test
		t.Fatalf("dòng phiếu: %v", l)
	}

	// Lưu trữ NCC → không dùng được cho phiếu mới; phiếu cũ vẫn đọc ra tên.
	a := mustOK(t, "updateSupplier", e.do(http.MethodPatch, "/api/v1/inventory/suppliers/"+sid, invManager, "u1",
		`{"version":1,"status":"archived"}`), http.StatusOK)
	if a["status"] != "archived" || a["version"] != float64(2) {
		t.Fatalf("lưu trữ: %v", a)
	}
	expect(t, "version cũ", e.do(http.MethodPatch, "/api/v1/inventory/suppliers/"+sid, invManager, "u2",
		`{"version":1,"note":"x"}`), http.StatusConflict, "version_conflict")
	expect(t, "NCC đã lưu trữ", e.do(http.MethodPost, "/api/v1/inventory/documents", invManager, "x2",
		fmt.Sprintf(`{"warehouseId":%q,"kind":"import","supplierId":%q}`, e.w1, sid)), http.StatusUnprocessableEntity,
		"supplier_archived")
	got := mustOK(t, "getStockDocument", e.do(http.MethodGet, "/api/v1/inventory/documents/"+id, invCreator, "", ""), http.StatusOK)
	if got["supplierName"] != "Xưởng Hoa" {
		t.Fatalf("phiếu cũ mất tên NCC: %v", got)
	}

	rec := e.do(http.MethodGet, "/api/v1/inventory/suppliers?q=xuong&includeArchived=true", invCreator, "", "")
	var list []map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list) != 1 {
		t.Fatalf("listSuppliers gồm lưu trữ: %d %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodGet, "/api/v1/inventory/suppliers?q=xuong", invCreator, "", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("listSuppliers không gồm lưu trữ: %d %s", rec.Code, rec.Body.String())
	}
	expect(t, "tenant B sửa NCC của A", e.do(http.MethodPatch, "/api/v1/inventory/suppliers/"+sid, invOtherB, "u3",
		`{"version":2,"note":"x"}`), http.StatusNotFound, "not_found")
	expect(t, "tenant B đọc phiếu của A", e.do(http.MethodGet, "/api/v1/inventory/documents/"+id, invOtherB, "", ""),
		http.StatusNotFound, "not_found")

	if strings.Contains(e.logs.text(), testPhone) {
		t.Fatalf("log lộ SĐT nhà cung cấp: %s", e.logs.text())
	}
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.audit_logs WHERE action LIKE 'inventory.%' AND (before::text LIKE $1 OR after::text LIKE $1)",
		"%"+testPhone+"%"); n != 0 {
		t.Fatalf("audit chứa SĐT: %d dòng", n)
	}
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM identity.audit_logs WHERE action = 'inventory.document.post'"); n != 1 {
		t.Fatalf("audit ghi sổ: mong 1, có %d", n)
	}
}

// KH10 qua API: insufficient_stock 409 kèm variantId, available; số dư không đổi.
func TestInventoryAPI_InsufficientBody(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	v := e.variant(e.w1, 5)
	// Giữ 3 cái bằng một đơn nhập tay thật (giữ hàng phải thuộc dòng đơn có thật, khóa ngoại của Task M1-30).
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		seller, err := catalog.DefaultSeller(t.Context(), tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		c, err := customer.Create(t.Context(), tx, "Khách thử")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}.CreateManual(t.Context(), tx,
			order.CreateManualInput{
				IdempotencyKey: uuid.New(), SellerID: seller, CustomerID: c, WarehouseID: e.w1,
				Items:       []order.ItemInput{{VariantID: v, Qty: 3}},
				ShipAddress: order.ShipAddress{Name: "Khách thử", Phone: "0901234567", Line: "1 Lê Lợi"},
			}, order.Actor{TenantID: e.tenant})
		return err //nolint:wrapcheck // test
	})
	id, ver := e.draft(invManager, "export", e.w1, "", fmt.Sprintf(`[{"variantId":%q,"quantity":3}]`, v))
	rec := e.post(id, ver, invManager, "p1")
	var body map[string]any
	if rec.Code != http.StatusConflict || json.Unmarshal(rec.Body.Bytes(), &body) != nil ||
		body["error"] != "insufficient_stock" || body["variantId"] != v.String() || body["available"] != float64(2) ||
		body["request_id"] == nil || len(body) != 4 {
		t.Fatalf("mong 409 insufficient_stock kèm variantId, available 2: %d %s", rec.Code, rec.Body.String())
	}
	if e.onHand(v, e.w1) != 5 {
		t.Fatal("số dư đổi sau lỗi thiếu hàng")
	}
	expect(t, "version cũ", e.post(id, ver-1, invManager, "p2"), http.StatusConflict, "version_conflict")
	expect(t, "lines thiếu quyền", e.do(http.MethodPut, "/api/v1/inventory/documents/"+id+"/lines", invOtherB, "l1",
		`{"version":1,"lines":[]}`), http.StatusNotFound, "not_found")
}

// Ghi sổ: 50 lần cùng khóa → áp một lần; hai khóa khác nhau song song → một 200, một 409 document_not_draft.
func TestInventoryAPI_PostIdempotent(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	v := e.variant(e.w1, 100)
	id, ver := e.draft(invManager, "export", e.w1, "", fmt.Sprintf(`[{"variantId":%q,"quantity":1}]`, v))
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() { recs[i] = e.post(id, ver, invManager, "k-50") })
	}
	wg.Wait()
	ok := 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			if c := errCode(t, rec); c != "request_in_progress" {
				t.Errorf("409 phải request_in_progress, có %q", c)
			}
		default:
			t.Errorf("mã lạ %d %s", rec.Code, rec.Body.String())
		}
	}
	if ok == 0 || e.onHand(v, e.w1) != 99 {
		t.Fatalf("cùng khóa: %d lần 200, on_hand %d (mong 99)", ok, e.onHand(v, e.w1))
	}
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1", id); n != 1 {
		t.Fatalf("sổ: mong 1 dòng, có %d", n)
	}

	id2, ver2 := e.draft(invManager, "export", e.w1, "", fmt.Sprintf(`[{"variantId":%q,"quantity":1}]`, v))
	oneWinsOneConflict(t, "document_not_draft", func(i int) *httptest.ResponseRecorder {
		return e.post(id2, ver2, invManager, fmt.Sprintf("k2-%d", i))
	})
	if n := e.staff.CountIn(t, e.tenant, "SELECT count(*) FROM inventory.stock_ledger WHERE doc_id = $1", id2); n != 1 {
		t.Fatalf("sổ phiếu 2: mong 1 dòng, có %d", n)
	}
}

// Quyền theo kho: ghi sổ phiếu kho khác → 403; chỉ có quyền soạn → 403 ở post; đọc, liệt kê theo kho được giao.
func TestInventoryAPI_WarehouseScope(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	v1, v2 := e.variant(e.w1, 10), e.variant(e.w2, 10)
	d1, ver1 := e.draft(invManager, "export", e.w1, "", fmt.Sprintf(`[{"variantId":%q,"quantity":1}]`, v1))
	d2, ver2 := e.draft(invManager, "export", e.w2, "", fmt.Sprintf(`[{"variantId":%q,"quantity":1}]`, v2))

	expect(t, "ghi sổ kho w2 bằng quyền w1", e.post(d2, ver2, invPostW1, "a1"), http.StatusForbidden, "forbidden")
	expect(t, "chỉ có quyền soạn", e.post(d1, ver1, invCreator, "a2"), http.StatusForbidden, "forbidden")
	expect(t, "soạn ở kho w2 bằng quyền w1", e.do(http.MethodPost, "/api/v1/inventory/documents", invPostW1, "a3",
		fmt.Sprintf(`{"warehouseId":%q,"kind":"export"}`, e.w2)), http.StatusForbidden, "forbidden")
	expect(t, "sửa dòng kho w2 bằng quyền w1", e.do(http.MethodPut, "/api/v1/inventory/documents/"+d2+"/lines", invPostW1,
		"a4", fmt.Sprintf(`{"version":%d,"lines":[]}`, ver2)), http.StatusForbidden, "forbidden")
	expect(t, "hủy kho w2 bằng quyền w1", e.do(http.MethodPost, "/api/v1/inventory/documents/"+d2+"/cancel", invPostW1,
		"a5", fmt.Sprintf(`{"version":%d}`, ver2)), http.StatusForbidden, "forbidden")
	expect(t, "đọc phiếu kho w2 bằng quyền w1", e.do(http.MethodGet, "/api/v1/inventory/documents/"+d2, invPostW1, "", ""),
		http.StatusForbidden, "forbidden")
	expect(t, "phiếu điều chỉnh cần quyền ghi sổ", e.do(http.MethodPost, "/api/v1/inventory/documents", invCreator, "a6",
		fmt.Sprintf(`{"warehouseId":%q,"kind":"adjust"}`, e.w1)), http.StatusForbidden, "forbidden")
	expect(t, "liệt kê kho w2 bằng quyền w1", e.do(http.MethodGet, "/api/v1/inventory/documents?warehouseId="+e.w2.String(),
		invPostW1, "", ""), http.StatusForbidden, "forbidden")
	if e.onHand(v2, e.w2) != 10 {
		t.Fatal("phiếu kho w2 bị ghi sổ")
	}

	mustOK(t, "ghi sổ kho w1 bằng quyền w1", e.post(d1, ver1, invPostW1, "a7"), http.StatusOK)
	lst := mustOK(t, "listStockDocuments w1", e.do(http.MethodGet, "/api/v1/inventory/documents", invPostW1, "", ""), http.StatusOK)
	items := lst["items"].([]any)                                 //nolint:forcetypeassert // test
	if len(items) != 1 || items[0].(map[string]any)["id"] != d1 { //nolint:forcetypeassert // test
		t.Fatalf("người kho w1 chỉ thấy phiếu w1: %v", lst)
	}
	lst = mustOK(t, "listStockDocuments all", e.do(http.MethodGet, "/api/v1/inventory/documents?status=draft&limit=1",
		invManager, "", ""), http.StatusOK)
	if items := lst["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != d2 { //nolint:forcetypeassert // test
		t.Fatalf("lọc draft: %v", lst)
	}
	mustOK(t, "hủy kho w2", e.do(http.MethodPost, "/api/v1/inventory/documents/"+d2+"/cancel", invManager, "a8",
		fmt.Sprintf(`{"version":%d}`, ver2)), http.StatusOK)
	expect(t, "ghi sổ phiếu đã hủy", e.post(d2, ver2+1, invManager, "a9"), http.StatusConflict, "document_not_draft")
}

func (b *lockedBuffer) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Danh sách kho (Task 51): chỉ kho người gọi có inventory.read; quyền toàn shop thấy mọi kho, MAIN trước; tenant khác
// không thấy kho của A; không có inventory.read → 403.
func TestListWarehousesAPI(t *testing.T) {
	t.Parallel()
	e := newInvAPIEnv(t)
	e.staff.Grant(t, e.tenant, e.staff.User(t, e.tenant, listCatalogOnly, "active"), []string{"catalog.read"}, "all", "")
	names := func(email string) []string {
		t.Helper()
		var out []string
		for _, w := range mustOKArray(t, "listWarehouses "+email, e.do(http.MethodGet, "/api/v1/inventory/warehouses", email, "", "")) {
			m := w.(map[string]any) //nolint:forcetypeassert // test
			out = append(out, fmt.Sprintf("%s/%s/%s", m["code"], m["name"], m["status"]))
		}
		return out
	}
	if got := names(invManager); !slices.Equal(got, []string{"MAIN/Kho chính/active", "W2/Kho phụ/active"}) {
		t.Fatalf("toàn shop: %v", got)
	}
	if got := names(invPostW1); !slices.Equal(got, []string{"MAIN/Kho chính/active"}) {
		t.Fatalf("phạm vi w1 thấy kho khác: %v", got)
	}
	if got := names(invOtherB); len(got) != 1 || got[0] != "MAIN/Kho chính/active" {
		t.Fatalf("tenant B: %v", got)
	}
	expect(t, "không có inventory.read", e.do(http.MethodGet, "/api/v1/inventory/warehouses", listCatalogOnly, "", ""),
		http.StatusForbidden, "forbidden")
}
