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
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

// hangGuard là chốt chống treo khi chờ một sự kiện; không phải tiêu chí đúng/sai về thời gian.
const hangGuard = 30 * time.Second

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

// [WB02], [FS13]: nối như vai trò api và worker (newWebCache, newModules): mẫu đã nằm trong bộ đệm Valkey; đổi giá
// biến thể bằng catalog.Writer (sự kiện price.changed, product.updated) → trước khi worker chạy, đọc vẫn ra giá cũ từ
// bộ đệm; worker chạy consumer webcache → Invalidator ở api xóa khóa → lần đọc sau ra giá mới (chờ theo sự kiện).
func TestWebCache_PriceChangedRefreshesProduct(t *testing.T) {
	vk := testkit.StartValkey(t)
	staff := testkit.OpenStaff(t)
	appURL := staff.App.Pool.Config().ConnString()
	quiet := slog.New(slog.DiscardHandler)
	hub := realtime.NewPGHub(realtime.PGHubOptions{URL: appURL, Logger: quiet})
	mods, err := newModules(config.Config{Role: config.RoleWorker}, staff.App, hub, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	worker := queue.NewWorker(staff.App, mods.Events.JobHandlers(), queue.WorkerOptions{WorkerID: "test/" + uuid.NewString(), Logger: quiet})
	w := catalog.Writer{Events: mods.Events, Queue: queue.NewQueue(), Realtime: hub}
	shop := seedWebShop(t, staff, w)
	seller, owner, p, key := shop.seller, shop.owner, shop.product, shop.key
	tenant := shop.tenant
	staff.DrainJobs(t, worker) // việc của lần tạo chạy trước khi api nghe (báo hiệu của chúng không tới ai)

	// Vai trò api: hub nghe, rồi bộ đệm + bộ xóa khóa, máy chủ HTTP.
	runHub(t, hub)
	wc, err := newWebCache(t.Context(), config.Config{ValkeyCacheURL: vk.URL("crm_cache")}, staff.App, hub, nil, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wc.stop)
	rc := &sourceCache{ReadCache: wc.rc}
	srv := httpapi.NewServer(httpapi.Deps{
		Config: config.Config{FilesPublicBaseURL: "https://img.elimaz.test", ImagePresetVersion: 1}, DB: staff.App,
		Logger: quiet, ServiceKeys: httpapi.NewServiceKeyVerifier(staff.App, nil, httpapi.ServiceKeyOptions{Logger: quiet}),
		ReadCache: rc, Events: mods.Events,
	})
	price := func() float64 {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/web/catalog/products?ids="+p.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
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
