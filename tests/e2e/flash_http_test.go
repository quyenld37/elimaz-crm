package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion/flashtest"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// flashE2E là phần lượt mua flash sale dựng trên stack: đợt 30 suất, khóa dịch vụ web.flash.buy, Valkey gate riêng,
// máy chủ api có cổng nhận vào như cmd/crm.
type flashE2E struct {
	s     *stack
	sale  flashtest.Sale
	key   string
	vk    *testkit.Valkey
	reg   *metrics.Registry
	api   *httptest.Server
	start time.Time
}

// Lượt mua flash sale của website qua HTTP thật (Task 203, M5-20; [FS02], [FS03], [FS10]): 30 suất, 200 khách đồng
// thời, Valkey gate dừng giữa chừng → đúng 30 bought, 170 sold_out, không 5xx ngoài 503, gate_degraded > 0; dấu vết có
// đoạn flash.admission, flash.buy và không chứa SĐT, IP.
func TestFlashBuyHTTP_ValkeyDownAndTrace(t *testing.T) {
	// Không t.Parallel (Task 484): newStack đặt tracer provider của cả tiến trình.
	f := newFlashE2E(t)
	counts, server5xx := f.crowdWithValkeyStop(200, 40)
	if counts["200 bought"] != 30 || counts["200 sold_out"] != 170 || server5xx != 0 {
		t.Fatalf("kết quả %v, 5xx %d; muốn 30 bought, 170 sold_out, không 5xx", counts, server5xx)
	}
	if n := f.s.count(`SELECT count(*) FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1
		AND status IN ('held', 'sold')`, f.sale.Item); n != 30 {
		t.Fatalf("%d đơn vị suất đã lấy, muốn 30", n)
	}
	if n := counterSum(f.reg, "gate_degraded"); n == 0 {
		t.Fatal("gate_degraded = 0 sau khi Valkey dừng")
	}
	f.checkSpans()
}

func newFlashE2E(t *testing.T) *flashE2E {
	t.Helper()
	f := &flashE2E{s: newStack(t, stackOptions{}), start: time.Now().UTC().Truncate(time.Second).Add(2 * time.Hour)}
	ctx := t.Context()
	owner, err := identity.LoadUser(ctx, f.s.app, stackOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, f.s.owner, f.s.tenant, func(tx pgx.Tx) error {
		if err := order.EnsureDefaults(ctx, tx, f.s.tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		f.sale = flashtest.Schedule(t, tx, flashtest.Item{
			Variant: f.s.shop.Variant(t, tx, f.s.shop.Seller, 350000, 35), Price: 199000, Slots: 30, Limit: 1,
			HoldMinutes: 15, StartsAt: f.start, TenantID: f.s.tenant, UserID: owner.UserID,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, f.s.app, f.s.tenant, func(tx pgx.Tx) error {
		_, f.key, err = identity.CreateServiceKey(ctx, tx, identity.CreateServiceKeyInput{
			Name: "Website e2e", Permissions: []identity.Permission{"web.flash.buy"}, Actor: owner,
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	f.vk = testkit.StartValkey(t)
	conn, err := cache.Dial(ctx, f.vk.URL("crm_gate"), cache.DialOptions{Name: "gate", OpTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	f.reg = metrics.NewRegistry()
	f.api = httptest.NewServer(httpapi.NewServer(f.deps(conn)))
	t.Cleanup(f.api.Close)
	return f
}

// deps là Deps của api như cmd/crm (cổng trên Valkey gate conn, ngưỡng QĐ23; giới hạn khóa dịch vụ nới cho 200 khách).
func (f *flashE2E) deps(conn *cache.Conn) httpapi.Deps {
	gate := cache.NewAdmissionGate(conn, cache.GateOptions{}, f.reg)
	rl := cache.NewRateLimiter(conn, nil, f.reg)
	ls := cache.DefaultLimits()
	logger := slog.New(slog.DiscardHandler)
	return httpapi.Deps{
		Config: config.Config{ConsoleOrigins: []string{stackOrigin}}, DB: f.s.app, Logger: logger, Metrics: f.reg,
		ServiceKeys: httpapi.NewServiceKeyVerifier(f.s.app, nil, httpapi.ServiceKeyOptions{Logger: logger}),
		RateLimiter: httpapi.NewWebKeyLimiter(rl, cache.Limit{Name: "web_key", Max: 100_000, Window: time.Minute, FallbackMax: 100_000}),
		Admission: map[string]func(http.Handler) http.Handler{"flash": httpapi.NewFlashAdmission(httpapi.FlashAdmissionOptions{
			Gate: gate, Limiter: rl, Customer: ls.FlashCustomer, IP: ls.FlashIP, Metrics: f.reg,
		})},
		FlashGate: gate,
		Now:       func() time.Time { return f.start.Add(time.Minute) },
	}
}

// crowdWithValkeyStop chạy n khách đồng thời (website giả tự gửi lại cùng khóa khi 503, 409); sau stopAfter câu trả lời
// thì dừng Valkey (từ goroutine của test). Trả số câu trả lời cuối theo "status kết quả" và số 5xx không phải 503.
func (f *flashE2E) crowdWithValkeyStop(n, stopAfter int) (map[string]int, int64) {
	var replies, server5xx atomic.Int64
	reached := make(chan struct{})
	var once sync.Once
	results := make([]string, n)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			results[i] = f.website(i, func(status int) {
				if replies.Add(1) >= int64(stopAfter) {
					once.Do(func() { close(reached) })
				}
				if status >= http.StatusInternalServerError && status != http.StatusServiceUnavailable {
					server5xx.Add(1)
				}
			})
		})
	}
	<-reached
	f.vk.Stop(f.s.t)
	wg.Wait()
	counts := map[string]int{}
	for _, r := range results {
		counts[r]++
	}
	return counts, server5xx.Load()
}

// website gửi lượt mua của khách i tới khi có câu trả lời cuối; onReply nhận status của mỗi lần.
func (f *flashE2E) website(i int, onReply func(int)) string {
	phone := fmt.Sprintf("09%08d", 30000000+i)
	ip := fmt.Sprintf("10.20.%d.%d", i/256, i%256)
	ref := fmt.Sprintf("E2E-FS-%d", i)
	for attempt := 1; attempt <= 2000; attempt++ {
		status, result, err := f.post(ref, phone, ip, min(attempt, 10))
		if err != nil {
			f.s.t.Errorf("gửi lượt mua: %v", err)
			return "lỗi"
		}
		onReply(status)
		if status != http.StatusServiceUnavailable && status != http.StatusConflict {
			return strconv.Itoa(status) + " " + result
		}
		time.Sleep(time.Duration(5+attempt%20) * time.Millisecond)
	}
	return "hết lượt"
}

// post gửi một lượt mua; trả status và result (hay mã lỗi).
func (f *flashE2E) post(ref, phone, ip string, attempt int) (int, string, error) {
	path := fmt.Sprintf("%s/api/v1/web/flash-sales/%s/items/%s/buy", f.api.URL, f.sale.ID, f.sale.Item)
	body := fmt.Sprintf(`{"externalRef":%q,"phone":%q,"qty":1,"clientIp":%q}`, ref, phone, ip)
	req, err := http.NewRequestWithContext(f.s.t.Context(), http.MethodPost, path, strings.NewReader(body))
	if err != nil {
		return 0, "", err //nolint:wrapcheck // test
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.key)
	req.Header.Set("Idempotency-Key", ref)
	req.Header.Set("X-Attempt", strconv.Itoa(attempt))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, "", err //nolint:wrapcheck // test
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	var b struct{ Result, Error string }
	_ = json.Unmarshal(raw, &b)
	return resp.StatusCode, b.Result + b.Error, err //nolint:wrapcheck // test
}

// checkSpans: có đoạn flash.admission và flash.buy; không span nào chứa SĐT hay IP của khách thử.
func (f *flashE2E) checkSpans() {
	t := f.s.t
	names := map[string]bool{}
	for _, sp := range f.s.spansSnapshot() {
		names[sp.Name()] = true
		raw, _ := json.Marshal(struct {
			N string
			A any
			E any
		}{sp.Name(), sp.Attributes(), sp.Events()})
		for _, secret := range []string{"0930000001", "930000001", `10.20.0.1"`} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("span %s chứa %q", sp.Name(), secret)
			}
		}
	}
	if !names["flash.admission"] || !names["flash.buy"] {
		t.Fatalf("thiếu đoạn flash.admission / flash.buy: %v", names)
	}
}

// counterSum đọc tổng counter name (mọi nhãn) của reg.
func counterSum(reg *metrics.Registry, name string) int64 {
	var n int64
	for _, r := range reg.Drain() {
		if r.Name == name {
			n += r.Count
		}
	}
	return n
}
