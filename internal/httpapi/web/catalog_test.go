package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

const testBase = "https://img.elimaz.test"

// countingCache bọc một ReadCache: đếm số lần load (đọc PostgreSQL), ghi nguồn của từng lần GetOrLoad và hạn sống
// gần nhất theo vùng.
type countingCache struct {
	cache.ReadCache

	mu      sync.Mutex
	loads   int
	sources []cache.Source
	ttls    map[cache.Space]time.Duration
}

func (c *countingCache) GetOrLoad(ctx context.Context, tenant uuid.UUID, space cache.Space, id string, ttl time.Duration,
	load func(context.Context) ([]byte, error),
) ([]byte, cache.Source, error) {
	v, src, err := c.ReadCache.GetOrLoad(ctx, tenant, space, id, ttl, func(ctx context.Context) ([]byte, error) {
		c.mu.Lock()
		c.loads++
		c.mu.Unlock()
		return load(ctx)
	})
	c.mu.Lock()
	c.sources = append(c.sources, src)
	if c.ttls == nil {
		c.ttls = map[cache.Space]time.Duration{}
	}
	c.ttls[space] = ttl
	c.mu.Unlock()
	return v, src, err //nolint:wrapcheck // bọc thử
}

func (c *countingCache) take() (int, []cache.Source) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, s := c.loads, c.sources
	c.loads, c.sources = 0, nil
	return n, s
}

// ttl trả hạn sống của lần GetOrLoad gần nhất trong space.
func (c *countingCache) ttl(space cache.Space) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ttls[space]
}

// env: tenant có người bán ELIMAZ, kho MAIN, Chủ; một khóa dịch vụ web.catalog.read và một khóa web.orders.read; máy
// chủ HTTP thật (httpapi.NewServer) với bộ kiểm khóa thật và bộ đệm rc.
type env struct {
	t        *testing.T
	staff    *testkit.Staff
	tenant   uuid.UUID
	seller   uuid.UUID
	owner    identity.AuthUser
	key      string // web.catalog.read
	otherKey string // web.orders.read
	rc       *countingCache
	srv      http.Handler
}

// valkeyCache chạy Valkey thật cho test và trả kết nối bộ đệm (đóng khi test xong).
func valkeyCache(t *testing.T) (*testkit.Valkey, *cache.Conn) {
	t.Helper()
	vk := testkit.StartValkey(t)
	conn, err := cache.Dial(t.Context(), vk.URL("crm_cache"), cache.DialOptions{Name: "cache"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	return vk, conn
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func newEnv(t *testing.T, conn *cache.Conn) *env {
	t.Helper()
	e := &env{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, e.tenant) })
	e.seller, _ = shoptest.SeedSellers(t, e.staff.Owner, e.tenant)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := inventory.EnsureDefaultWarehouse(t.Context(), tx, e.tenant, e.seller)
		return err //nolint:wrapcheck // test
	})
	id := e.staff.User(t, e.tenant, "chu@elimaz.test", "active")
	e.staff.Grant(t, e.tenant, id, []string{"identity.service_keys.manage", "catalog.manage"}, identity.ScopeAll, "")
	u, err := identity.LoadUser(t.Context(), e.staff.App, "chu@elimaz.test")
	if err != nil {
		t.Fatal(err)
	}
	e.owner = u
	e.key, e.otherKey = e.serviceKey("web.catalog.read"), e.serviceKey("web.orders.read")
	e.rc = &countingCache{ReadCache: cache.NewReadCache(conn, cache.ReadCacheOptions{})}
	e.srv = httpapi.NewServer(httpapi.Deps{
		Config: config.Config{FilesPublicBaseURL: testBase, ImagePresetVersion: 1},
		DB:     e.staff.App, Logger: quiet(),
		ServiceKeys: httpapi.NewServiceKeyVerifier(e.staff.App, nil, httpapi.ServiceKeyOptions{Logger: quiet()}),
		ReadCache:   e.rc,
	})
	return e
}

func (e *env) serviceKey(perm identity.Permission) string {
	e.t.Helper()
	var full string
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		_, full, err = identity.CreateServiceKey(e.t.Context(), tx, identity.CreateServiceKeyInput{
			Name: "Website " + string(perm), Permissions: []identity.Permission{perm}, Actor: e.owner,
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return full
}

// get gửi GET path với khóa key (rỗng: không gửi) và các cặp header thêm.
func (e *env) get(key, path string, hdr ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.send(httptest.NewRequestWithContext(e.t.Context(), http.MethodGet, path, nil), key, hdr...)
}

// send gửi req tới máy chủ thử với khóa key (rỗng: không gửi) và các cặp header thêm.
func (e *env) send(req *http.Request, key string, hdr ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	return rec
}

func (e *env) writer() catalog.Writer {
	return catalog.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
}

func (e *env) app(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn, db.TxAPI); err != nil {
		e.t.Fatalf("đọc, ghi thử bằng crm_app: %v", err)
	}
}

// product tạo mẫu code có 3 biến thể (S, M, L; L ngừng bán, có giá nhập) và bài viết article qua catalog.Writer.
func (e *env) product(code, article string) catalog.Product {
	e.t.Helper()
	var p catalog.Product
	in := catalog.ProductInput{
		SellerID: e.seller, Code: code, Name: "Đầm " + code, Description: "Mô tả ngắn", Status: catalog.StatusActive,
		Article: json.RawMessage(article),
	}
	for _, s := range []string{"S", "M", "L"} {
		cost := int64(150000)
		in.Variants = append(in.Variants, catalog.VariantInput{SKU: code + "-" + s, Color: "Đen", Size: s, Price: 350000, CostPrice: &cost})
	}
	e.app(func(tx pgx.Tx) error {
		var err error
		if p, err = e.writer().CreateProduct(e.t.Context(), tx, in, e.owner); err != nil {
			return err //nolint:wrapcheck // test
		}
		l := p.Variants[2]
		st := catalog.VariantDiscontinued
		p.Variants[2], err = e.writer().UpdateVariant(e.t.Context(), tx, e.seller, l.ID, l.Version, catalog.VariantPatch{Status: &st}, e.owner)
		return err //nolint:wrapcheck // test
	})
	return p
}

func (e *env) image() uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.app(func(tx pgx.Tx) error { id = filestest.Image(e.t, tx, e.tenant, "product_image"); return nil })
	return id
}

type page struct {
	Items      []map[string]any `json:"items"`
	NextCursor string           `json:"nextCursor"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) page {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("mong 200, có %d %s", rec.Code, rec.Body.String())
	}
	var p page
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// jsonKeys gom mọi tên trường của một giá trị JSON (đệ quy).
func jsonKeys(v any, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			out[k] = true
			jsonKeys(c, out)
		}
	case []any:
		for _, c := range x {
			jsonKeys(c, out)
		}
	}
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

var (
	urlAttr = regexp.MustCompile(`(?:src|srcset)="([^"]*)"`)
	v1URL   = regexp.MustCompile(`^` + regexp.QuoteMeta(testBase) + `/img/[0-9a-f]{64}/v1/`)
	etagRe  = regexp.MustCompile(`^"[0-9a-f]{64}"$`)
)

// [WB02], [WB09], R7, R12, A2c: mẫu có bài viết (heading, paragraph có <script>, list, 2 khối image: 1 tệp ready, 1
// tệp mất), 2 ảnh ready và 1 ảnh đang xử lý, 3 biến thể (1 ngừng bán) → articleHtml bằng đúng catalog.ArticleHTML của
// cột article, một <picture>, mọi src/srcset là FILES_PUBLIC_BASE_URL/img/…/v1/, <script> đã thoát; 2 ảnh theo vị trí;
// biến thể ngừng bán có status discontinued, sellable false; không có trường article thô, cost, import_price; chưa
// nhập cân nặng → không có weightGrams.
func TestWebCatalogProducts_Shape(t *testing.T) {
	e := newEnv(t, nil)
	ready, lost := e.image(), e.image()
	article := fmt.Sprintf(`[{"type":"heading","text":"Chất liệu","level":2},`+
		`{"type":"paragraph","text":"Lụa <script>alert(1)</script>"},{"type":"list","items":["Mềm","Mát"]},`+
		`{"type":"image","fileId":%q,"caption":"Mặt trước"},{"type":"image","fileId":%q,"caption":"Mặt sau"}]`, ready, lost)
	p := e.product("DAM01", article)
	e.app(func(tx pgx.Tx) error { filestest.MarkLost(t, tx, lost); return nil })
	img1, img2 := e.image(), e.image()
	var processing uuid.UUID
	e.app(func(tx pgx.Tx) error { processing = filestest.Uploaded(t, tx, e.tenant, "product_image"); return nil })
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for i, f := range []uuid.UUID{img2, processing, img1} {
			if _, err := tx.Exec(t.Context(), `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position)
				VALUES ($1, $2, $3, $4)`, e.tenant, p.ID, f, 3-i); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	var want string
	e.app(func(tx pgx.Tx) error {
		var raw json.RawMessage
		if err := tx.QueryRow(t.Context(), "SELECT article FROM catalog.products_v WHERE product_id = $1", p.ID).Scan(&raw); err != nil {
			return err //nolint:wrapcheck // test
		}
		var err error
		want, err = catalog.ArticleHTML(t.Context(), tx, raw, catalog.ImageView{BaseURL: testBase, Preset: 1})
		return err //nolint:wrapcheck // test
	})

	rec := e.get(e.key, "/api/v1/web/catalog/products?ids="+p.ID.String())
	pg := decode(t, rec)
	if len(pg.Items) != 1 || pg.NextCursor != "" {
		t.Fatalf("mong 1 mẫu, không có nextCursor: %s", rec.Body.String())
	}
	it := pg.Items[0]
	html, _ := it["articleHtml"].(string)
	if html != want || strings.Count(html, "<picture>") != 1 || strings.Contains(html, "<script>") ||
		!strings.Contains(html, "&lt;script&gt;") || strings.Contains(html, "Mặt sau") || !strings.Contains(html, "<li>Mát</li>") {
		t.Fatalf("articleHtml sai:\n%s\nmong:\n%s", html, want)
	}
	checkArticleURLs(t, html)
	checkImagesAndVariants(t, it)
	checkNoForbiddenKeys(t, rec.Body.Bytes())
}

// checkImagesAndVariants: 2 ảnh ở vị trí 1, 3, mỗi ảnh 12 biến thể; 3 biến thể, cái thứ ba ngừng bán.
func checkImagesAndVariants(t *testing.T, it map[string]any) {
	t.Helper()
	imgs, _ := it["images"].([]any)
	if len(imgs) != 2 {
		t.Fatalf("mong 2 ảnh ready, có %d: %v", len(imgs), imgs)
	}
	for i, wantPos := range []float64{1, 3} {
		vs, _ := obj(imgs[i])["variants"].([]any)
		if obj(imgs[i])["position"] != wantPos || len(vs) != 12 {
			t.Errorf("ảnh %d: vị trí %v, %d biến thể", i, obj(imgs[i])["position"], len(vs))
		}
	}
	vars, _ := it["variants"].([]any)
	if len(vars) != 3 {
		t.Fatalf("mong 3 biến thể: %v", vars)
	}
	if l := obj(vars[2]); l["status"] != "discontinued" || l["sellable"] != false {
		t.Errorf("[WB09] biến thể ngừng bán: %v", l)
	}
	if s := obj(vars[0]); s["sellable"] != true || s["price"] != float64(350000) || s["available"] != false {
		t.Errorf("biến thể đang bán, chưa nhập kho: %v", s)
	}
}

func checkArticleURLs(t *testing.T, html string) {
	t.Helper()
	attrs := urlAttr.FindAllStringSubmatch(html, -1)
	if len(attrs) == 0 {
		t.Fatalf("bài viết không có src/srcset: %s", html)
	}
	for _, m := range attrs {
		for u := range strings.SplitSeq(m[1], ",") {
			if f := strings.Fields(u); len(f) == 0 || !v1URL.MatchString(f[0]) {
				t.Errorf("đường dẫn ảnh bài viết %q không theo FILES_PUBLIC_BASE_URL/img/…/v1/", u)
			}
		}
	}
}

// checkNoForbiddenKeys: JSON không có bài viết thô, giá nhập, cân nặng chưa nhập.
func checkNoForbiddenKeys(t *testing.T, body []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	all := map[string]bool{}
	jsonKeys(v, all)
	for k := range all {
		low := strings.ToLower(k)
		if k == "article" || k == "weightGrams" || strings.Contains(low, "cost") || strings.Contains(low, "import") {
			t.Errorf("JSON có trường %q (bài viết thô, giá nhập, cân nặng chưa nhập)", k)
		}
	}
}

// QĐ30: đặt cân nặng 450 qua catalog.Writer → weightGrams 450 trên website; biến thể chưa nhập không có trường.
func TestWebCatalogProducts_Weight(t *testing.T) {
	e := newEnv(t, nil)
	p := e.product("DAM02", "[]")
	w := 450
	e.app(func(tx pgx.Tx) error {
		v := p.Variants[0]
		_, err := e.writer().UpdateVariant(t.Context(), tx, e.seller, v.ID, v.Version, catalog.VariantPatch{WeightGrams: &w}, e.owner)
		return err //nolint:wrapcheck // test
	})
	vs, _ := decode(t, e.get(e.key, "/api/v1/web/catalog/products?ids="+p.ID.String())).Items[0]["variants"].([]any)
	if g := obj(vs[0])["weightGrams"]; g != float64(450) {
		t.Fatalf("weightGrams: %v", g)
	}
	if _, ok := obj(vs[1])["weightGrams"]; ok {
		t.Fatal("biến thể chưa nhập cân nặng có weightGrams")
	}
}

func since(ts time.Time) string {
	return strings.ReplaceAll(ts.UTC().Format(time.RFC3339Nano), "+", "%2B")
}

// Khóa thiếu web.catalog.read → 403; không khóa, khóa sai, thẻ Access của nhân viên → 401; khóa đúng → 200 (cả ba route).
func TestWebCatalog_Auth(t *testing.T) {
	e := newEnv(t, nil)
	for _, path := range []string{
		"/api/v1/web/catalog/products", "/api/v1/web/catalog/changes?since=" + since(time.Now().Add(-time.Minute)),
		"/api/v1/web/catalog/taxonomy",
	} {
		for _, c := range []struct {
			name, key string
			hdr       []string
			want      int
		}{
			{"không khóa", "", nil, http.StatusUnauthorized},
			{"thẻ Access", "", []string{"Cf-Access-Jwt-Assertion", "the.access.thu"}, http.StatusUnauthorized},
			{"khóa sai", "ek_abcdefghijkl_sai", nil, http.StatusUnauthorized},
			{"thiếu quyền", e.otherKey, nil, http.StatusForbidden},
			{"đúng quyền", e.key, nil, http.StatusOK},
		} {
			if rec := e.get(c.key, path, c.hdr...); rec.Code != c.want {
				t.Errorf("%s %s: mong %d, có %d %s", path, c.name, c.want, rec.Code, rec.Body.String())
			}
		}
	}
}

// ids: 101 mã → 400; cursor cùng ids → 400; cursor sai → 400; limit 201 → 400; changes limit 1001, thiếu since → 400;
// 2 mã (kèm 1 mã lạ) → đúng 2 mẫu theo thứ tự gửi; theo trang limit 1 → nextCursor dẫn sang mẫu kế.
func TestWebCatalogProducts_Params(t *testing.T) {
	e := newEnv(t, nil)
	a, b := e.product("A1", "[]"), e.product("B1", "[]")
	many := make([]string, webcatalog.MaxIDs+1)
	for i := range many {
		many[i] = uuid.NewString()
	}
	for name, path := range map[string]string{
		"101 mã":        "/api/v1/web/catalog/products?ids=" + strings.Join(many, ","),
		"cursor và ids": "/api/v1/web/catalog/products?ids=" + a.ID.String() + "&cursor=" + b.ID.String(),
		"cursor sai":    "/api/v1/web/catalog/products?cursor=khong-phai-ma",
		"limit 201":     "/api/v1/web/catalog/products?limit=201",
		"changes 1001":  "/api/v1/web/catalog/changes?limit=1001&since=" + since(time.Now()),
		"changes thiếu": "/api/v1/web/catalog/changes",
	} {
		if rec := e.get(e.key, path); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: mong 400, có %d %s", name, rec.Code, rec.Body.String())
		}
	}
	pg := decode(t, e.get(e.key, "/api/v1/web/catalog/products?ids="+b.ID.String()+","+uuid.NewString()+","+a.ID.String()))
	var got []any
	for _, it := range pg.Items {
		got = append(got, it["code"])
	}
	if !slices.Equal(got, []any{"B1", "A1"}) {
		t.Fatalf("theo ids: %v", got)
	}
	p1 := decode(t, e.get(e.key, "/api/v1/web/catalog/products?limit=1"))
	p2 := decode(t, e.get(e.key, "/api/v1/web/catalog/products?limit=1&cursor="+p1.NextCursor))
	if len(p1.Items) != 1 || len(p2.Items) != 1 || p1.Items[0]["id"] == p2.Items[0]["id"] || p1.NextCursor == "" {
		t.Fatalf("trang: %v %v", p1, p2)
	}
}

// ETag: gửi lại If-None-Match bằng ETag → 304 thân rỗng; ETag khác → 200 cùng ETag (sản phẩm theo mã, theo trang,
// phân loại).
func TestWebCatalog_ETag(t *testing.T) {
	e := newEnv(t, nil)
	p := e.product("E1", "[]")
	for _, path := range []string{
		"/api/v1/web/catalog/products?ids=" + p.ID.String(), "/api/v1/web/catalog/products", "/api/v1/web/catalog/taxonomy",
	} {
		rec := e.get(e.key, path)
		tag := rec.Header().Get("ETag")
		if rec.Code != http.StatusOK || !etagRe.MatchString(tag) {
			t.Fatalf("%s: %d, ETag %q", path, rec.Code, tag)
		}
		if again := e.get(e.key, path, "If-None-Match", tag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Errorf("%s If-None-Match: mong 304 rỗng, có %d %q", path, again.Code, again.Body.String())
		}
		if other := e.get(e.key, path, "If-None-Match", `"khac"`); other.Code != http.StatusOK || other.Header().Get("ETag") != tag {
			t.Errorf("%s ETag khác: %d", path, other.Code)
		}
	}
}

// Valkey thật: lần hai đọc từ bộ đệm (không đọc PostgreSQL); Valkey dừng → vẫn 200 đúng dữ liệu, nguồn db_fallback.
func TestWebCatalog_CacheAndFallback(t *testing.T) {
	vk, conn := valkeyCache(t)
	e := newEnv(t, conn)
	p := e.product("C1", "[]")
	path := "/api/v1/web/catalog/products?ids=" + p.ID.String()
	first := e.get(e.key, path)
	if n, src := e.rc.take(); first.Code != http.StatusOK || n != 1 || !slices.Equal(src, []cache.Source{cache.SourceDB}) {
		t.Fatalf("lần đầu: %d, %d lần tải, nguồn %v", first.Code, n, src)
	}
	second := e.get(e.key, path)
	if n, src := e.rc.take(); n != 0 || !slices.Equal(src, []cache.Source{cache.SourceCache}) || second.Body.String() != first.Body.String() {
		t.Fatalf("lần hai: %d lần tải, nguồn %v", n, src)
	}
	tx1 := e.get(e.key, "/api/v1/web/catalog/taxonomy")
	e.rc.take()
	vk.Stop(t)
	down := e.get(e.key, path)
	if n, src := e.rc.take(); down.Code != http.StatusOK || n != 1 || !slices.Equal(src, []cache.Source{cache.SourceFallback}) ||
		down.Body.String() != first.Body.String() {
		t.Fatalf("Valkey dừng: %d, %d lần tải, nguồn %v, %s", down.Code, n, src, down.Body.String())
	}
	if t2 := e.get(e.key, "/api/v1/web/catalog/taxonomy"); t2.Code != http.StatusOK || t2.Body.String() != tx1.Body.String() {
		t.Fatalf("phân loại khi Valkey dừng: %d", t2.Code)
	}
	if pg := e.get(e.key, "/api/v1/web/catalog/products"); pg.Code != http.StatusOK {
		t.Fatalf("trang khi Valkey dừng: %d", pg.Code)
	}
}

// Luồng thay đổi qua HTTP: mẫu vừa tạo có trong changes, until dạng RFC 3339, hasMore false.
func TestWebCatalogChanges(t *testing.T) {
	e := newEnv(t, nil)
	p := e.product("H1", "[]")
	rec := e.get(e.key, "/api/v1/web/catalog/changes?since="+since(time.Now().Add(-time.Hour)))
	var out struct {
		ProductIDs []string `json:"productIds"`
		Until      string   `json:"until"`
		HasMore    bool     `json:"hasMore"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if !slices.Equal(out.ProductIDs, []string{p.ID.String()}) || out.HasMore {
		t.Fatalf("changes: %+v", out)
	}
	if _, err := time.Parse(time.RFC3339Nano, out.Until); err != nil {
		t.Fatalf("until %q: %v", out.Until, err)
	}
}
