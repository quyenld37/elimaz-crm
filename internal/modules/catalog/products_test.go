package catalog_test

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/barcode"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain nâng số lượt rapid lên 500 (kế hoạch Task M1-27: thuộc tính của bộ dựng HTML 500 lượt), trừ khi người chạy
// đã đặt -rapid.checks hay RAPID_CHECKS.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(500)
	os.Exit(m.Run())
}

type catalogEnv struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	seller uuid.UUID // ELIMAZ (người bán mặc định)
	// partner là người bán thứ hai PARTNER1 (ADR-29, KH14).
	partner uuid.UUID
	actor   identity.AuthUser
	bus     *testkit.RecordingBus
}

func newCatalogEnv(t *testing.T) *catalogEnv {
	t.Helper()
	e := &catalogEnv{t: t, staff: testkit.OpenStaff(t), bus: &testkit.RecordingBus{}}
	e.tenant = e.staff.Tenant(t, "A")
	e.seller, e.partner = testkit.SeedSellers(t, e.staff.Owner, e.tenant)
	uid := e.staff.User(t, e.tenant, "chu@elimaz.test", "active")
	e.actor = identity.AuthUser{
		UserID: uid, TenantID: e.tenant, Email: "chu@elimaz.test",
		Grants: []identity.Grant{{Permission: "catalog.manage", Scope: identity.Scope{Type: identity.ScopeAll}}},
	}
	return e
}

func (e *catalogEnv) ensureSeller(tenant uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, tenant, func(tx pgx.Tx) error {
		var err error
		id, err = catalog.EnsureDefaultSeller(e.t.Context(), tx, tenant)
		return err //nolint:wrapcheck // test
	})
	return id
}

func (e *catalogEnv) writer() catalog.Writer {
	return catalog.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: e.bus, RequestID: "req-test"}
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant của e.
func (e *catalogEnv) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

func (e *catalogEnv) create(in catalog.ProductInput) (catalog.Product, error) {
	var p catalog.Product
	err := e.app(func(tx pgx.Tx) error {
		var err error
		p, err = e.writer().CreateProduct(e.t.Context(), tx, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return p, err
}

func (e *catalogEnv) get(id uuid.UUID) (catalog.Product, error) {
	var p catalog.Product
	err := e.app(func(tx pgx.Tx) error {
		var err error
		p, err = catalog.GetProduct(e.t.Context(), tx, e.seller, id)
		return err //nolint:wrapcheck // test
	})
	return p, err
}

func (e *catalogEnv) count(q string, args ...any) int {
	e.t.Helper()
	// Chủ bảng trong ngữ cảnh tenant: crm_app không đọc được audit_logs, domain_events.
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

func ptr[T any](v T) *T { return &v }

// dam01: mẫu DAM01 "Đầm lụa xòe", 2 màu × 3 size, giá 350000, giá nhập 180000.
func dam01(seller uuid.UUID) catalog.ProductInput {
	in := catalog.ProductInput{SellerID: seller, Code: "dam01", Name: "Đầm lụa xòe", Description: "Đầm lụa dáng xòe"}
	for _, c := range []struct{ name, code string }{{"Đỏ", "DO"}, {"Đen", "DEN"}} {
		for _, s := range []string{"S", "M", "L"} {
			in.Variants = append(in.Variants, catalog.VariantInput{
				SKU: "DAM01-" + c.code + "-" + s, Color: c.name, Size: s, Price: 350000, CostPrice: ptr(int64(180000)),
			})
		}
	}
	return in
}

func TestCreateProduct_SixVariants(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if p.Code != "DAM01" || p.Status != catalog.ProductStatus("draft") || p.Version != 1 || len(p.Variants) != 6 {
		t.Fatalf("mẫu tạo sai: %+v", p)
	}
	got, err := e.get(p.ID)
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if got.Name != "Đầm lụa xòe" || got.Description != "Đầm lụa dáng xòe" || len(got.Variants) != 6 || got.SellerID != e.seller {
		t.Fatalf("GetProduct trả sai: %+v", got)
	}
	for _, v := range got.Variants {
		if v.Price != 350000 || v.CostPrice == nil || *v.CostPrice != 180000 || v.Status != "active" || v.Version != 1 {
			t.Errorf("biến thể sai: %+v", v)
		}
	}
	if n := e.count("SELECT count(*) FROM catalog.products WHERE id = $1 AND name_search = 'dam lua xoe'", p.ID); n != 1 {
		t.Errorf("name_search phải là \"dam lua xoe\"")
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1 AND seq = 1", p.ID.String()); n != 1 {
		t.Errorf("mong 1 sự kiện product.updated seq 1, có %d", n)
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.product.create' AND entity_id = $1", p.ID.String()); n != 1 {
		t.Errorf("mong 1 dòng audit tạo mẫu, có %d", n)
	}
	if sig := e.bus.Signals(); len(sig) != 1 || sig[0].Topic != "catalog.product" || sig[0].Requires != "catalog.read" ||
		sig[0].Entity != (realtime.Entity{Type: "product", ID: p.ID.String()}) {
		t.Errorf("báo hiệu sai: %+v", sig)
	}
}

func TestCreateProduct_DuplicateSKU(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	add := func(v catalog.VariantInput) error {
		return e.app(func(tx pgx.Tx) error {
			cur, err := catalog.GetProduct(t.Context(), tx, e.seller, p.ID)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			_, err = e.writer().AddVariant(t.Context(), tx, e.seller, p.ID, cur.Version, v, e.actor)
			return err //nolint:wrapcheck // test
		})
	}
	if err := add(catalog.VariantInput{SKU: "dam01-do-m", Color: "Xanh", Size: "XL", Price: 1}); !errors.Is(err, catalog.ErrSKUTaken) {
		t.Errorf("SKU đã có (khác hoa thường): mong ErrSKUTaken, có %v", err)
	}
	if err := add(catalog.VariantInput{SKU: "DAM01-DO-M2", Color: "Đỏ", Size: "M", Price: 1}); !errors.Is(err, catalog.ErrDuplicateOption) {
		t.Errorf("cùng (màu, size): mong ErrDuplicateOption, có %v", err)
	}
	dupInOne := dam01(e.seller)
	dupInOne.Code = "DAM02"
	for i := range dupInOne.Variants {
		dupInOne.Variants[i].SKU = "X" + dupInOne.Variants[i].SKU
	}
	dupInOne.Variants[1].SKU = dupInOne.Variants[0].SKU
	if _, err := e.create(dupInOne); !errors.Is(err, catalog.ErrSKUTaken) {
		t.Errorf("hai biến thể cùng SKU trong một lần tạo: mong ErrSKUTaken, có %v", err)
	}
	again := dam01(e.seller)
	again.Variants = again.Variants[:1]
	again.Variants[0].SKU = "OTHER-1"
	if _, err := e.create(again); !errors.Is(err, catalog.ErrCodeTaken) {
		t.Errorf("mã mẫu trùng: mong ErrCodeTaken, có %v", err)
	}
	withBarcode := catalog.VariantInput{SKU: "DAM01-TRANG-M", Color: "Trắng", Size: "M", Price: 1, Barcode: p.Variants[0].Barcode}
	if err := add(withBarcode); !errors.Is(err, catalog.ErrBarcodeTaken) {
		t.Errorf("barcode đã có: mong ErrBarcodeTaken, có %v", err)
	}

	// Người bán thứ hai (ADR-29) dùng cùng mã mẫu và SKU → được.
	if _, err := e.create(dam01(e.partner)); err != nil {
		t.Errorf("người bán thứ hai cùng SKU phải được: %v", err)
	}
}

func TestCreateProduct_GeneratedBarcodes(t *testing.T) {
	e := newCatalogEnv(t)
	in := dam01(e.seller)
	in.Variants = in.Variants[:3]
	p, err := e.create(in)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, v := range p.Variants {
		if !barcode.ValidEAN13(v.Barcode) || v.Barcode[0] != '2' || seen[v.Barcode] {
			t.Errorf("barcode tự sinh sai hoặc trùng: %q", v.Barcode)
		}
		seen[v.Barcode] = true
	}

	const n = 100
	codes := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			one := catalog.ProductInput{
				SellerID: e.seller, Code: "C" + uuid.NewString()[:8], Name: "Mẫu",
				Variants: []catalog.VariantInput{{SKU: "S" + uuid.NewString()[:12], Color: "Đỏ", Size: "M", Price: 1}},
			}
			var p catalog.Product
			p, errs[i] = e.create(one)
			if errs[i] == nil {
				codes[i] = p.Variants[0].Barcode
			}
		})
	}
	wg.Wait()
	for i, c := range codes {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if seen[c] || !barcode.ValidEAN13(c) {
			t.Fatalf("barcode %q trùng hoặc sai", c)
		}
		seen[c] = true
	}
}

func TestCreateProduct_InvalidPrice(t *testing.T) {
	e := newCatalogEnv(t)
	for _, price := range []int64{-1, 9007199254740992} {
		in := dam01(e.seller)
		in.Variants[0].Price = price
		if _, err := e.create(in); !errors.Is(err, catalog.ErrInvalidPrice) {
			t.Errorf("giá %d: mong ErrInvalidPrice, có %v", price, err)
		}
		in = dam01(e.seller)
		in.Variants[0].CostPrice = ptr(price)
		if _, err := e.create(in); !errors.Is(err, catalog.ErrInvalidPrice) {
			t.Errorf("giá nhập %d: mong ErrInvalidPrice, có %v", price, err)
		}
	}
	bad := map[string]func(*catalog.ProductInput){
		"tên rỗng":        func(in *catalog.ProductInput) { in.Name = "  " },
		"mã sai mẫu":      func(in *catalog.ProductInput) { in.Code = "đầm 01" },
		"màu rỗng":        func(in *catalog.ProductInput) { in.Variants[0].Color = "" },
		"SKU sai mẫu":     func(in *catalog.ProductInput) { in.Variants[0].SKU = "-ABC" },
		"barcode sai":     func(in *catalog.ProductInput) { in.Variants[0].Barcode = "ab" },
		"màu hex sai":     func(in *catalog.ProductInput) { in.Variants[0].ColorHex = "red" },
		"trạng thái lạ":   func(in *catalog.ProductInput) { in.Status = "deleted" },
		"mô tả quá dài":   func(in *catalog.ProductInput) { in.Description = string(make([]rune, 1001)) },
		"không người bán": func(in *catalog.ProductInput) { in.SellerID = uuid.Nil },
	}
	for name, mutate := range bad {
		in := dam01(e.seller)
		mutate(&in)
		if _, err := e.create(in); !errors.Is(err, catalog.ErrInvalidInput) {
			t.Errorf("%s: mong ErrInvalidInput, có %v", name, err)
		}
	}
	in := dam01(e.seller)
	in.Variants[0].ColorHex = "#ff0000"
	p, err := e.create(in)
	if err != nil || p.Variants[0].ColorHex != "#FF0000" {
		t.Errorf("màu hex chuẩn hóa viết hoa: %+v, %v", p.Variants, err)
	}
}

func TestUpdateProduct_Version(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	update := func(version int64, patch catalog.ProductPatch) (catalog.Product, error) {
		var out catalog.Product
		err := e.app(func(tx pgx.Tx) error {
			var err error
			out, err = e.writer().UpdateProduct(t.Context(), tx, e.seller, p.ID, version, patch, e.actor)
			return err //nolint:wrapcheck // test
		})
		return out, err
	}
	got, err := update(1, catalog.ProductPatch{Name: ptr("Đầm Lụa Xòe Mới"), Status: ptr(catalog.ProductStatus("active"))})
	if err != nil || got.Version != 2 || got.Name != "Đầm Lụa Xòe Mới" || got.Status != "active" {
		t.Fatalf("UpdateProduct: %+v, %v", got, err)
	}
	if n := e.count("SELECT count(*) FROM catalog.products WHERE id = $1 AND name_search = 'dam lua xoe moi'", p.ID); n != 1 {
		t.Errorf("name_search phải đổi theo tên")
	}
	if _, err := update(1, catalog.ProductPatch{Description: ptr("cũ")}); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Errorf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	if _, err := e.writerUpdateMissing(); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("mẫu không có: mong ErrNotFound, có %v", err)
	}

	const n = 2
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { _, errs[i] = update(2, catalog.ProductPatch{Description: ptr("song song")}) })
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, catalog.ErrVersionConflict):
			conflict++
		default:
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Errorf("hai người sửa cùng version: mong 1 thành công 1 xung đột, có %d và %d", ok, conflict)
	}
	if c := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1", p.ID.String()); c != 3 {
		t.Errorf("mong 3 product.updated (tạo, sửa, sửa song song thắng), có %d", c)
	}
}

func (e *catalogEnv) writerUpdateMissing() (catalog.Product, error) {
	var out catalog.Product
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.writer().UpdateProduct(e.t.Context(), tx, e.seller, uuid.New(), 1, catalog.ProductPatch{}, e.actor)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func TestArticle_RejectsUnknownBlock(t *testing.T) {
	e := newCatalogEnv(t)
	in := dam01(e.seller)
	in.Article = json.RawMessage(`[
		{"type":"heading","text":"Chất liệu","level":2},
		{"type":"paragraph","text":"<script>alert(1)</script>"},
		{"type":"list","items":["a","b"]}]`)
	p, err := e.create(in)
	if err != nil {
		t.Fatalf("CreateProduct có bài viết: %v", err)
	}
	got, err := e.get(p.ID)
	if err != nil || len(got.Article) != 3 || got.Article[1].Text != "<script>alert(1)</script>" || got.Article[0].Level != 2 {
		t.Fatalf("đọc lại bài viết sai: %+v, %v", got.Article, err)
	}
	for name, raw := range map[string]string{
		"khối html": `[{"type":"html","text":"<b>x</b>"}]`,
		"201 khối":  "[" + repeatJSON(`{"type":"paragraph","text":"x"}`, 201) + "]",
	} {
		in := dam01(e.seller)
		in.Code = "ART2"
		in.Article = json.RawMessage(raw)
		if _, err := e.create(in); !errors.Is(err, catalog.ErrInvalidArticle) {
			t.Errorf("%s: mong ErrInvalidArticle, có %v", name, err)
		}
	}
	_, err = e.writerUpdateArticle(p.ID, `[{"type":"html"}]`)
	if !errors.Is(err, catalog.ErrInvalidArticle) {
		t.Errorf("UpdateProduct khối html: mong ErrInvalidArticle, có %v", err)
	}
}

// B-02 (review độc lập M1): bài viết lưu được (≤ 200 KB ở dạng JSON gọn) phải đọc lại, sửa được dù PostgreSQL trả jsonb
// dạng văn bản có dấu cách sau mỗi ':' và ',' (dài hơn bản đã lưu).
func TestArticle_NearLimitReadsBack(t *testing.T) {
	e := newCatalogEnv(t)
	items := `"` + strings.Repeat("x", 16) + `"`
	block := `{"type":"list","items":[` + repeatJSON(items, 50) + `]}`
	raw := "[" + repeatJSON(block, 200) + "]"
	in := dam01(e.seller)
	in.Article = json.RawMessage(raw)
	p, err := e.create(in)
	if err != nil {
		t.Fatalf("lưu bài viết %d byte (dưới trần): %v", len(raw), err)
	}
	got, err := e.get(p.ID)
	if err != nil || len(got.Article) != 200 {
		t.Fatalf("đọc lại mẫu có bài viết sát trần: %d khối, %v", len(got.Article), err)
	}
	if _, err := e.writerUpdateArticle(p.ID, raw); err != nil {
		t.Fatalf("sửa mẫu có bài viết sát trần: %v", err)
	}
}

func (e *catalogEnv) writerUpdateArticle(id uuid.UUID, raw string) (catalog.Product, error) {
	var out catalog.Product
	err := e.app(func(tx pgx.Tx) error {
		cur, err := catalog.GetProduct(e.t.Context(), tx, e.seller, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		a := json.RawMessage(raw)
		out, err = e.writer().UpdateProduct(e.t.Context(), tx, e.seller, id, cur.Version, catalog.ProductPatch{Article: &a}, e.actor)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func repeatJSON(s string, n int) string {
	out := make([]byte, 0, (len(s)+1)*n)
	for i := range n {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, s...)
	}
	return string(out)
}

// PQ08: tenant B không đọc được mẫu của A; view catalog.variants_v ngoài ngữ cảnh tenant → 0 dòng.
func TestCatalog_TenantIsolation(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	b := e.staff.Tenant(t, "B")
	sellerB := e.ensureSeller(b)
	err = tenancy.WithTenant(t.Context(), e.staff.App, b, func(tx pgx.Tx) error {
		_, err := catalog.GetProduct(t.Context(), tx, sellerB, p.ID)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("tenant B đọc mẫu của A: mong ErrNotFound, có %v", err)
	}
	if _, err := e.getWithSeller(sellerB, p.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("người bán khác đọc mẫu: mong ErrNotFound, có %v", err)
	}
	var n int
	if err := e.staff.App.Pool.QueryRow(t.Context(), "SELECT count(*) FROM catalog.variants_v").Scan(&n); err != nil || n != 0 {
		t.Errorf("variants_v ngoài ngữ cảnh: mong 0 dòng, có %d, %v", n, err)
	}
	if c := e.count("SELECT count(*) FROM catalog.variants_v WHERE product_id = $1 AND sellable = false", p.ID); c != 6 {
		t.Errorf("mẫu draft: 6 biến thể không bán được trong variants_v, có %d", c)
	}
}

func (e *catalogEnv) getWithSeller(seller, id uuid.UUID) (catalog.Product, error) {
	var p catalog.Product
	err := e.app(func(tx pgx.Tx) error {
		var err error
		p, err = catalog.GetProduct(e.t.Context(), tx, seller, id)
		return err //nolint:wrapcheck // test
	})
	return p, err
}
