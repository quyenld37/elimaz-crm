package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
	"elimaz.com.vn/crm/internal/wiring/webcache"
)

// hangGuard là chốt chống treo khi chờ một sự kiện; không phải tiêu chí đúng/sai về thời gian.
const hangGuard = 30 * time.Second

// valkeyTestOpTimeout là hạn mỗi lệnh Valkey của test (Task 498, như Task 496 ở các gói khác): hạn 50 ms của mã chạy thật
// hết khi máy chạy test tải nặng, lệnh rơi về PostgreSQL (nguồn không phải bộ đệm) hay lần xóa khóa lỗi → test đỏ oan.
const valkeyTestOpTimeout = 2 * time.Second

// sourceCache ghi nguồn của lần GetOrLoad gần nhất.
type sourceCache struct {
	cache.ReadCache

	mu   sync.Mutex
	last cache.Source
}

func (s *sourceCache) GetOrLoad(ctx context.Context, tenant uuid.UUID, space cache.Space, id string, ttl time.Duration,
	load func(context.Context) ([]byte, error),
) ([]byte, cache.Source, error) {
	v, src, err := s.ReadCache.GetOrLoad(ctx, tenant, space, id, ttl, load)
	s.mu.Lock()
	s.last = src
	s.mu.Unlock()
	return v, src, err //nolint:wrapcheck // bọc thử
}

func (s *sourceCache) source() cache.Source {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// runHub chạy hub tới khi test xong và chờ lượt resync của lần nghe đầu (để lượt đó không làm mới bộ đệm giữa chừng
// của test: người đăng ký sau đó không nhận nó).
func runHub(t *testing.T, hub *realtime.PGHub) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	listening := make(chan struct{})
	var once sync.Once
	stopProbe := hub.SubscribeSystem(realtime.TopicSystemResync, func(realtime.Signal) { once.Do(func() { close(listening) }) })
	wg.Go(func() { hub.Run(ctx) })
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	select {
	case <-listening:
	case <-time.After(hangGuard):
		t.Fatal("hub không nghe được")
	}
	stopProbe()
}

// webShop là dữ liệu của test bộ đệm: tenant, người bán ELIMAZ, Chủ, một mẫu 1 biến thể 350.000 đ, khóa web.catalog.read.
type webShop struct {
	tenant, seller uuid.UUID
	owner          identity.AuthUser
	product        catalog.Product
	key            string
}

// seedWebShop dựng webShop; mẫu tạo bằng w (sự kiện vào sổ của w).
func seedWebShop(t *testing.T, staff *testkit.Staff, w catalog.Writer) webShop {
	t.Helper()
	tenant := staff.Tenant(t, "A")
	staff.Exec(t, tenant, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, tenant) })
	seller, _ := shoptest.SeedSellers(t, staff.Owner, tenant)
	uid := staff.User(t, tenant, "chu@elimaz.test", "active")
	staff.Grant(t, tenant, uid, []string{"identity.service_keys.manage", "catalog.manage"}, identity.ScopeAll, "")
	owner, err := identity.LoadUser(t.Context(), staff.App, "chu@elimaz.test")
	if err != nil {
		t.Fatal(err)
	}

	var out webShop
	if err := tenancy.WithTenant(t.Context(), staff.App, tenant, func(tx pgx.Tx) error {
		var err error
		out.product, err = w.CreateProduct(t.Context(), tx, catalog.ProductInput{
			SellerID: seller, Code: "DAM01", Name: "Đầm", Status: catalog.StatusActive,
			Variants: []catalog.VariantInput{{SKU: "DAM01-S", Color: "Đen", Size: "S", Price: 350000}},
		}, owner)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, out.key, err = identity.CreateServiceKey(t.Context(), tx, identity.CreateServiceKeyInput{
			Name: "Website", Permissions: []identity.Permission{"web.catalog.read"}, Actor: owner,
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	out.tenant, out.seller, out.owner = tenant, seller, owner
	return out
}

// crossRole nối như vai trò api và worker (newWebCache, newModules) trên Valkey và PostgreSQL thật: worker chạy việc
// của sổ sự kiện (consumer webcache), hub nghe, bộ đệm + bộ xóa khóa, máy chủ HTTP; shop đã tạo và việc của lần tạo
// đã chạy trước khi api nghe (báo hiệu của chúng không tới ai). rec khác nil: bộ đệm và Invalidator dựng như
// newWebCache nhưng mọi lần xóa khóa đi qua rec.
type crossRole struct {
	t      *testing.T
	staff  *testkit.Staff
	hub    *realtime.PGHub
	mods   modules
	worker *queue.Worker
	shop   webShop
	rc     *sourceCache
	srv    http.Handler
}

func newCrossRole(t *testing.T, rec *invalidations) *crossRole {
	t.Helper()
	vk := testkit.StartValkey(t)
	c := &crossRole{t: t, staff: testkit.OpenStaff(t)}
	quiet := slog.New(slog.DiscardHandler)
	c.hub = realtime.NewPGHub(realtime.PGHubOptions{URL: c.staff.App.Pool.Config().ConnString(), Logger: quiet})
	var err error
	if c.mods, err = newModules(config.Config{Role: config.RoleWorker}, c.staff.App, c.hub, nil, nil); err != nil {
		t.Fatal(err)
	}
	c.worker = queue.NewWorker(c.staff.App, c.mods.Events.JobHandlers(), queue.WorkerOptions{WorkerID: "test/" + uuid.NewString(), Logger: quiet})
	c.shop = seedWebShop(t, c.staff, catalog.Writer{Events: c.mods.Events, Queue: queue.NewQueue(), Realtime: c.hub})
	c.staff.DrainJobs(t, c.worker)
	runHub(t, c.hub)
	if rec == nil {
		wc, err := newWebCache(t.Context(), config.Config{ValkeyCacheURL: vk.URL("crm_cache")}, c.staff.App, c.hub, nil, quiet,
			valkeyTestOpTimeout)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(wc.stop)
		c.rc = &sourceCache{ReadCache: wc.rc}
	} else {
		// Như newWebCache, nhưng Invalidator xóa khóa qua rec (ghi lại từng lần xóa).
		conn, err := cache.Dial(t.Context(), vk.URL("crm_cache"), cache.DialOptions{Name: "cache", OpTimeout: valkeyTestOpTimeout})
		if err != nil {
			t.Fatal(err)
		}
		rec.ReadCache = cache.NewReadCache(conn, cache.ReadCacheOptions{MaxValueBytes: webCacheMaxValue})
		inv := webcache.NewInvalidator(c.hub, rec, c.staff.App, quiet)
		inv.Start()
		t.Cleanup(func() { inv.Stop(); conn.Close() })
		c.rc = &sourceCache{ReadCache: rec}
	}
	c.srv = httpapi.NewServer(httpapi.Deps{
		Config: config.Config{FilesPublicBaseURL: "https://img.elimaz.test", ImagePresetVersion: 1}, DB: c.staff.App,
		Logger: quiet, ServiceKeys: httpapi.NewServiceKeyVerifier(c.staff.App, nil, httpapi.ServiceKeyOptions{Logger: quiet}),
		ReadCache: c.rc, Events: c.mods.Events,
	})
	return c
}

// get gửi GET path bằng khóa web.catalog.read của shop.
func (c *crossRole) get(path string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequestWithContext(c.t.Context(), http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+c.shop.key)
	rec := httptest.NewRecorder()
	c.srv.ServeHTTP(rec, req)
	return rec
}

// [WB02], [FS13]: nối như vai trò api và worker (newWebCache, newModules): mẫu đã nằm trong bộ đệm Valkey; đổi giá
// biến thể bằng catalog.Writer (sự kiện price.changed, product.updated) → trước khi worker chạy, đọc vẫn ra giá cũ từ
// bộ đệm; worker chạy consumer webcache → Invalidator ở api xóa khóa → lần đọc sau ra giá mới (chờ theo sự kiện).
func TestWebCache_PriceChangedRefreshesProduct(t *testing.T) {
	c := newCrossRole(t, nil)
	staff, hub, worker, shop, rc := c.staff, c.hub, c.worker, c.shop, c.rc
	w := catalog.Writer{Events: c.mods.Events, Queue: queue.NewQueue(), Realtime: hub}
	seller, owner, p := shop.seller, shop.owner, shop.product
	tenant := shop.tenant
	price := func() float64 {
		t.Helper()
		rec := c.get("/api/v1/web/catalog/products?ids=" + p.ID.String())
		var out struct {
			Items []struct {
				Variants []struct {
					Price float64 `json:"price"`
				} `json:"variants"`
			} `json:"items"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Items) != 1 {
			t.Fatalf("đọc mẫu: %d %s", rec.Code, rec.Body.String())
		}
		return out.Items[0].Variants[0].Price
	}
	// Lần đầu tải từ PostgreSQL và ghi vào bộ đệm; đọc tới khi ra từ bộ đệm.
	deadline := time.Now().Add(hangGuard)
	for price() != 350000 || rc.source() != cache.SourceCache {
		if time.Now().After(deadline) {
			t.Fatalf("bộ đệm không ổn định, nguồn %s", rc.source())
		}
	}

	if err := tenancy.WithTenant(t.Context(), staff.App, tenant, func(tx pgx.Tx) error {
		v := p.Variants[0]
		_, err := w.UpdateVariant(t.Context(), tx, seller, v.ID, v.Version, catalog.VariantPatch{Price: new(int64(390000))}, owner)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if got := price(); got != 350000 || rc.source() != cache.SourceCache {
		t.Fatalf("trước khi worker chạy: mong giá cũ từ bộ đệm, có %v (%s)", got, rc.source())
	}
	start := time.Now()
	staff.DrainJobs(t, worker)
	for price() != 390000 {
		if time.Since(start) > hangGuard {
			t.Fatal("giá mới không tới website sau khi worker chạy consumer webcache")
		}
	}
	t.Logf("giá mới tới website sau %s kể từ lúc worker bắt đầu", time.Since(start))
}

// invalidations bọc ReadCache và gửi vào ch mỗi khóa (vùng/mã) mà Invalidate đã xóa xong.
type invalidations struct {
	cache.ReadCache

	ch chan string
}

func (r *invalidations) Invalidate(ctx context.Context, tenant uuid.UUID, space cache.Space, ids ...string) error {
	if err := r.ReadCache.Invalidate(ctx, tenant, space, ids...); err != nil {
		return err //nolint:wrapcheck // bọc thử
	}
	for _, id := range ids {
		select {
		case r.ch <- string(space) + "/" + id:
		default:
		}
	}
	return nil
}

// availability đọc còn/hết của biến thể v qua HTTP.
func (c *crossRole) availability(v uuid.UUID) (available, low bool) {
	c.t.Helper()
	rec := c.get("/api/v1/web/availability?variantIds=" + v.String())
	var out struct {
		Items []struct{ Available, Low bool } `json:"items"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Items) != 1 {
		c.t.Fatalf("đọc còn/hết: %d %s", rec.Code, rec.Body.String())
	}
	return out.Items[0].Available, out.Items[0].Low
}

// inTenant chạy fn bằng crm_app trong ngữ cảnh tenant của shop.
func (c *crossRole) inTenant(fn func(tx pgx.Tx) error) {
	c.t.Helper()
	if err := tenancy.WithTenant(c.t.Context(), c.staff.App, c.shop.tenant, fn); err != nil {
		c.t.Fatal(err)
	}
}

// [WB02], [FS07]: còn/hết đã nằm trong bộ đệm Valkey (tồn 2: còn, sắp hết); phiếu xuất 2 của M1 (sự kiện stock.changed,
// cùng đường với giữ hàng của đơn) → worker chạy consumer webcache → Invalidator ở api xóa khóa của biến thể (chờ
// đúng lần xóa đó, theo sự kiện) → lần đọc ngay sau ra hết hàng. Hạn sống 5 s là lưới an toàn riêng
// (TestWebAvailability_TTLWithoutWorker); test này không qua được nhờ hết hạn vì đòi lần xóa của Invalidator.
func TestWebCache_StockChangedRefreshesAvailability(t *testing.T) {
	rec := &invalidations{ch: make(chan string, 64)}
	c := newCrossRole(t, rec)
	v := c.shop.product.Variants[0].ID
	var wh uuid.UUID
	c.staff.Exec(t, c.shop.tenant, func(tx pgx.Tx) error { // kho do chủ bảng tạo (như newEnv của web)
		var err error
		wh, err = inventory.EnsureDefaultWarehouse(t.Context(), tx, c.shop.tenant, c.shop.seller)
		return err //nolint:wrapcheck // test
	})
	c.inTenant(func(tx pgx.Tx) error { invtest.SeedStock(t, tx, v, wh, 2); return nil })
	// Lần đầu tải từ PostgreSQL và ghi vào bộ đệm; đọc tới khi ra từ bộ đệm.
	deadline := time.Now().Add(hangGuard)
	for a, low := c.availability(v); c.rc.source() != cache.SourceCache || !a || !low; a, low = c.availability(v) {
		if !a || !low || time.Now().After(deadline) {
			t.Fatalf("tồn 2: còn %v, sắp hết %v, nguồn %s", a, low, c.rc.source())
		}
	}
	w := inventory.Writer{Events: c.mods.Events, Queue: queue.NewQueue(), Realtime: c.hub}
	c.inTenant(func(tx pgx.Tx) error { invtest.Export(t, tx, w, wh, v, 2); return nil })
	start := time.Now()
	c.staff.DrainJobs(t, c.worker)
	want := string(cache.SpaceAvailability) + "/" + v.String()
	for got := ""; got != want; {
		select {
		case got = <-rec.ch:
		case <-time.After(hangGuard):
			t.Fatal("Invalidator không xóa khóa còn/hết của biến thể sau khi worker chạy consumer webcache")
		}
	}
	if a, low := c.availability(v); a || low {
		t.Fatalf("ngay sau khi xóa khóa: mong hết hàng, có còn %v, sắp hết %v", a, low)
	}
	t.Logf("hết hàng tới website sau %s kể từ lúc worker bắt đầu", time.Since(start))
}
