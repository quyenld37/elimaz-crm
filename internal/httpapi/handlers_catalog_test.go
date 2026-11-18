package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/testkit"
)

const (
	catalogManager = "ql@elimaz.test"
	catalogViewer  = "xem@elimaz.test"
	catalogOtherB  = "b@elimaz.test"
)

type catalogAPIEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	keys   *testkit.AccessKeys
	srv    http.Handler
	tenant uuid.UUID
}

func newCatalogAPIEnv(t *testing.T) *catalogAPIEnv {
	t.Helper()
	e := &catalogAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: testkit.AccessKeyServer(t)}
	e.tenant = e.staff.Tenant(t, "A")
	b := e.staff.Tenant(t, "B")
	for _, tid := range []uuid.UUID{e.tenant, b} {
		e.staff.Exec(t, tid, func(tx pgx.Tx) error {
			_, err := catalog.EnsureDefaultSeller(t.Context(), tx, tid)
			return err //nolint:wrapcheck // test
		})
	}
	m := e.staff.User(t, e.tenant, catalogManager, "active")
	e.staff.Grant(t, e.tenant, m, []string{"catalog.manage", "catalog.read"}, "all", "")
	v := e.staff.User(t, e.tenant, catalogViewer, "active")
	e.staff.Grant(t, e.tenant, v, []string{"catalog.read"}, "all", "")
	ub := e.staff.User(t, b, catalogOtherB, "active")
	e.staff.Grant(t, b, ub, []string{"catalog.manage", "catalog.read"}, "all", "")
	e.srv = testServer(t, e.staff, e.keys, 0)
	return e
}

func (e *catalogAPIEnv) do(method, path, email, key, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("đọc JSON: %v (%s)", err, rec.Body.String())
	}
	return m
}

const dam01Body = `{"code":"DAM01","name":"Đầm lụa xòe","description":"Đầm lụa","article":[
	{"type":"heading","text":"Chất liệu","level":2},{"type":"paragraph","text":"Lụa <b>mềm</b>\nmát"}],
	"variants":[
	{"sku":"DAM01-DO-S","color":"Đỏ","size":"S","price":350000,"costPrice":180000},
	{"sku":"DAM01-DO-M","color":"Đỏ","size":"M","price":350000},
	{"sku":"DAM01-DEN-S","color":"Đen","size":"S","price":350000,"colorHex":"#000000"}]}`

// createDam01 tạo mẫu DAM01 bằng Quản lý (khóa c1) và trả thân 201.
func (e *catalogAPIEnv) createDam01() map[string]any {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/v1/catalog/products", catalogManager, "c1", dam01Body)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("createProduct: %d %s", rec.Code, rec.Body.String())
	}
	return decodeJSON(e.t, rec)
}

func TestCatalogAPI_CreateGetUpdate(t *testing.T) {
	e := newCatalogAPIEnv(t)
	p := e.createDam01()
	id := p["id"].(string)                                                                       //nolint:forcetypeassert // test
	if p["status"] != "draft" || len(p["variants"].([]any)) != 3 || p["version"] != float64(1) { //nolint:forcetypeassert // test
		t.Fatalf("mẫu tạo sai: %v", p)
	}
	if _, ok := p["sellerId"]; ok {
		t.Errorf("API không hiện người bán: %v", p)
	}
	// Gửi lại cùng khóa → cùng kết quả, không tạo thêm.
	again := e.do(http.MethodPost, "/api/v1/catalog/products", catalogManager, "c1", dam01Body)
	if again.Code != http.StatusCreated || decodeJSON(t, again)["id"] != id {
		t.Errorf("gửi lại cùng khóa: %d %s", again.Code, again.Body.String())
	}

	got := e.do(http.MethodGet, "/api/v1/catalog/products/"+id, catalogViewer, "", "")
	if got.Code != http.StatusOK {
		t.Fatalf("getProduct: %d %s", got.Code, got.Body.String())
	}
	g := decodeJSON(t, got)
	blocks, err := catalog.DecodeArticle(mustJSON(t, g["article"]))
	if err != nil || len(blocks) != 2 {
		t.Fatalf("article trả về: %v, %v", g["article"], err)
	}
	want, err := catalog.RenderArticleHTML(blocks)
	if err != nil || g["articleHtml"] != want || !strings.Contains(want, "Lụa &lt;b&gt;mềm&lt;/b&gt;<br>mát") {
		t.Errorf("articleHtml = %q, mong %q (%v)", g["articleHtml"], want, err)
	}

	expect(t, "Xem không có quyền sửa", e.do(http.MethodPatch, "/api/v1/catalog/products/"+id, catalogViewer, "u0",
		`{"version":1,"name":"X"}`), http.StatusForbidden, "forbidden")
	upd := e.do(http.MethodPatch, "/api/v1/catalog/products/"+id, catalogManager, "u1", `{"version":1,"status":"active","article":[]}`)
	if upd.Code != http.StatusOK || decodeJSON(t, upd)["articleHtml"] != "" || decodeJSON(t, upd)["version"] != float64(2) {
		t.Fatalf("updateProduct: %d %s", upd.Code, upd.Body.String())
	}
	expect(t, "version cũ", e.do(http.MethodPatch, "/api/v1/catalog/products/"+id, catalogManager, "u2", `{"version":1,"name":"X"}`),
		http.StatusConflict, "version_conflict")
	expect(t, "khối html", e.do(http.MethodPatch, "/api/v1/catalog/products/"+id, catalogManager, "u3",
		`{"version":2,"article":[{"type":"html","text":"<b>x</b>"}]}`), http.StatusUnprocessableEntity, "invalid_article")
	expect(t, "tenant B đọc mẫu của A", e.do(http.MethodGet, "/api/v1/catalog/products/"+id, catalogOtherB, "", ""),
		http.StatusNotFound, "not_found")
	expect(t, "mẫu không có", e.do(http.MethodGet, "/api/v1/catalog/products/"+uuid.NewString(), catalogViewer, "", ""),
		http.StatusNotFound, "not_found")
}

func TestCatalogAPI_VariantErrors(t *testing.T) {
	e := newCatalogAPIEnv(t)
	p := e.createDam01()
	id := p["id"].(string)                //nolint:forcetypeassert // test
	variants := p["variants"].([]any)     //nolint:forcetypeassert // test
	first := variants[0].(map[string]any) //nolint:forcetypeassert // test
	path := "/api/v1/catalog/products/" + id + "/variants"

	expect(t, "SKU trùng", e.do(http.MethodPost, path, catalogManager, "v1",
		`{"productVersion":1,"sku":"DAM01-DO-M","color":"Xanh","size":"XL","price":1}`), http.StatusConflict, "sku_taken")
	expect(t, "cùng màu size", e.do(http.MethodPost, path, catalogManager, "v2",
		`{"productVersion":1,"sku":"DAM01-DO-M2","color":"Đỏ","size":"M","price":1}`), http.StatusConflict, "duplicate_option")
	expect(t, "barcode trùng", e.do(http.MethodPost, path, catalogManager, "v3",
		`{"productVersion":1,"sku":"DAM01-X","color":"Xanh","size":"M","price":1,"barcode":"`+first["barcode"].(string)+`"}`), //nolint:forcetypeassert // test
		http.StatusConflict, "barcode_taken")
	expect(t, "giá âm (ogen chặn minimum)", e.do(http.MethodPost, path, catalogManager, "v4",
		`{"productVersion":1,"sku":"DAM01-Y","color":"Xanh","size":"M","price":-1}`), http.StatusBadRequest, "bad_request")
	expect(t, "giá vượt maximum", e.do(http.MethodPost, path, catalogManager, "v5",
		`{"productVersion":1,"sku":"DAM01-Y","color":"Xanh","size":"M","price":9007199254740992}`), http.StatusBadRequest, "bad_request")
	added := e.do(http.MethodPost, path, catalogManager, "v6", `{"productVersion":1,"sku":"dam01-xanh-m","color":"Xanh","size":"M","price":9007199254740991}`)
	if added.Code != http.StatusCreated {
		t.Fatalf("addVariant: %d %s", added.Code, added.Body.String())
	}
	a := decodeJSON(t, added)
	if a["sku"] != "DAM01-XANH-M" || a["price"] != float64(9007199254740991) || len(a["barcode"].(string)) != 13 { //nolint:forcetypeassert // test
		t.Errorf("biến thể thêm sai: %v", a)
	}

	vpath := "/api/v1/catalog/variants/" + first["id"].(string) //nolint:forcetypeassert // test
	upd := e.do(http.MethodPatch, vpath, catalogManager, "p1", `{"version":1,"price":390000,"costPrice":null}`)
	if upd.Code != http.StatusOK {
		t.Fatalf("updateVariant: %d %s", upd.Code, upd.Body.String())
	}
	u := decodeJSON(t, upd)
	if u["price"] != float64(390000) || u["version"] != float64(2) {
		t.Errorf("biến thể sau sửa: %v", u)
	}
	if _, ok := u["costPrice"]; ok {
		t.Errorf("costPrice null phải bỏ giá nhập: %v", u)
	}
	expect(t, "version biến thể cũ", e.do(http.MethodPatch, vpath, catalogManager, "p2", `{"version":1,"price":1}`),
		http.StatusConflict, "version_conflict")
	expect(t, "biến thể không có", e.do(http.MethodPatch, "/api/v1/catalog/variants/"+uuid.NewString(), catalogManager, "p3",
		`{"version":1,"price":1}`), http.StatusNotFound, "not_found")
	var audits int
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.variant.update'
			AND before->>'price' = '350000' AND after->>'price' = '390000' AND request_id IS NOT NULL`).Scan(&audits)
	})
	if audits != 1 {
		t.Errorf("mong 1 dòng audit đổi giá có request_id, có %d", audits)
	}
	expect(t, "mã mẫu trùng", e.do(http.MethodPost, "/api/v1/catalog/products", catalogManager, "c2",
		strings.Replace(dam01Body, `"DAM01-`, `"Z-`, 3)), http.StatusConflict, "code_taken")
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
