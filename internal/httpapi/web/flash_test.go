package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/promotion/flashtest"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit"
)

// Điểm cuối mua flash sale của website (Task 203, M5-20): máy chủ HTTP thật, PostgreSQL thật, Valkey gate thật (container
// riêng của test), khóa dịch vụ thật. Ca [FS02], [FS03], [FS04], [FS05], [FS07], [FS10], [FS18], [HT27].

// flashOpts là tham số của một env flash: số suất, giới hạn mỗi khách, ngưỡng theo SĐT, tùy chọn cổng.
type flashOpts struct {
	slots, limit int
	phoneMax     int // lượt bấm mua mỗi SĐT / 10 s (cả khi Valkey ngừng)
	gate         cache.GateOptions
	traced       bool // pool của Deps có dấu vết otelpgx (ca span)
	extra        int  // số món thêm của đợt (mỗi món một biến thể mới, cùng số suất, giới hạn)
}

// flashEnv: orderEnv có khóa web.flash.buy, Valkey gate, cổng thật, đợt đã lên lịch một món; Deps.DB là pool đếm câu
// SQL (bộ kiểm khóa dịch vụ dùng pool riêng); đồng hồ nghiệp vụ là giờ mở + 1 phút.
type flashEnv struct {
	*orderEnv

	buyKey  string
	vk      *testkit.Valkey
	gate    cache.AdmissionGate
	reg     *metrics.Registry
	queries atomic.Int64
	sale    uuid.UUID
	item    uuid.UUID
	items   []uuid.UUID // mọi món của đợt (item là món đầu)
	start   time.Time
	shift   atomic.Int64 // tua đồng hồ nghiệp vụ (ns) so với giờ mở + 1 phút
	seq     atomic.Int64
}

type sqlCounter struct{ n *atomic.Int64 }

func (c sqlCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (sqlCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func newFlashEnv(t *testing.T, o flashOpts) *flashEnv {
	t.Helper()
	f := &flashEnv{orderEnv: newOrderEnv(t, nil), reg: metrics.NewRegistry()}
	f.buyKey = f.serviceKey("web.flash.buy")
	f.vk = testkit.StartValkey(t)
	conn, err := cache.Dial(t.Context(), f.vk.URL("crm_gate"), cache.DialOptions{Name: "gate", OpTimeout: valkeyTestOpTimeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	f.gate = cache.NewAdmissionGate(conn, o.gate, f.reg)
	ls := cache.DefaultLimits()
	ls.FlashCustomer.Max, ls.FlashCustomer.FallbackMax = o.phoneMax, o.phoneMax
	ls.FlashIP.Max, ls.FlashIP.FallbackMax = 10000, 10000
	cfg := f.staff.App.Pool.Config()
	cfg.MaxConns = 8
	if o.traced {
		cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	} else {
		cfg.ConnConfig.Tracer = sqlCounter{&f.queries}
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f.start = time.Now().UTC().Truncate(time.Second).Add(2 * time.Hour)
	f.schedule(o.slots, o.limit, o.extra)
	rl := cache.NewRateLimiter(conn, nil, f.reg)
	adm := httpapi.NewFlashAdmission(httpapi.FlashAdmissionOptions{
		Gate: f.gate, Limiter: rl, Customer: ls.FlashCustomer, IP: ls.FlashIP,
		Secret: []byte("khoa-hmac-thu-cua-test-du-32-ky-tu!!"), Metrics: f.reg,
	})
	f.serve(func(d *httpapi.Deps) {
		d.DB = &db.DB{Pool: pool}
		d.Now = f.now
		d.Admission = map[string]func(http.Handler) http.Handler{"flash": adm}
		d.FlashGate = f.gate
		// Giới hạn của khóa dịch vụ không phải đối tượng của ca này (mặc định trong tiến trình quá thấp cho 200 khách).
		d.RateLimiter = httpapi.NewWebKeyLimiter(rl, cache.Limit{Name: "web_key", Max: 100_000, Window: time.Minute, FallbackMax: 100_000})
		d.Metrics = f.reg
	})
	return f
}

// now là đồng hồ nghiệp vụ của máy chủ thử: giờ mở + 1 phút + shift.
func (f *flashEnv) now() time.Time {
	return f.start.Add(time.Minute + time.Duration(f.shift.Load()))
}

// schedule tạo biến thể (giá 350.000, tồn slots+5), đợt [start, start+2h) một món slots suất giá 199.000, giới hạn
// limit mỗi khách, giữ 15 phút (cộng extra món như vậy trên biến thể khác), rồi lên lịch đợt một giờ trước giờ mở.
func (f *flashEnv) schedule(slots, limit, extra int) {
	f.t.Helper()
	items := make([]flashtest.Item, 1+extra)
	for i := range items {
		v := f.product(fmt.Sprintf("FS%d%d", time.Now().UnixNano()%1_000_000, i), "[]").Variants[0].ID
		f.stock(v, int32(slots+5)) //nolint:gosec // test: số nhỏ
		items[i] = flashtest.Item{
			Variant: v, Price: 199000, Slots: slots, Limit: limit, HoldMinutes: 15, StartsAt: f.start,
			TenantID: f.tenant, UserID: f.owner.UserID,
		}
	}
	f.app(func(tx pgx.Tx) error {
		sl := flashtest.ScheduleItems(f.t, tx, items[0], items[1:]...)
		f.sale, f.item, f.items = sl.ID, sl.Item, sl.Items
		return nil
	})
}

func (f *flashEnv) path() string {
	return fmt.Sprintf("/api/v1/web/flash-sales/%s/items/%s/buy", f.sale, f.item)
}

// buyer là một khách bấm mua: SĐT, IP, mã ngoài, khóa chống trùng (= mã ngoài, quy ước của website).
type buyer struct{ phone, ip, ref string }

func (f *flashEnv) buyer(n int) buyer {
	return buyer{
		phone: phone(n), ip: fmt.Sprintf("10.%d.%d.%d", n/65536%256, n/256%256, n%256),
		ref: fmt.Sprintf("FS-%d-%s", f.seq.Add(1), uuid.NewString()[:8]),
	}
}

func (b buyer) body() string {
	return fmt.Sprintf(`{"externalRef":%q,"phone":%q,"qty":1,"clientIp":%q}`, b.ref, b.phone, b.ip)
}

// flashReply là một câu trả lời của điểm cuối.
type flashReply struct {
	status     int
	result     string
	code       string
	errCode    string
	retryAfter string
	replayed   string
	raw        string
}

func parseReply(status int, h http.Header, body []byte) flashReply {
	var b struct {
		Result string `json:"result"`
		Code   string `json:"code"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(body, &b)
	return flashReply{
		status: status, result: b.Result, code: b.Code, errCode: b.Error, retryAfter: h.Get("Retry-After"),
		replayed: h.Get("Idempotent-Replayed"), raw: string(body),
	}
}

// send gửi một lượt trong tiến trình (ServeHTTP) với X-Attempt attempt.
func (f *flashEnv) send(b buyer, attempt int) flashReply {
	f.t.Helper()
	return f.sendBody(b.ref, b.body(), attempt)
}

func (f *flashEnv) sendBody(key, body string, attempt int) flashReply {
	f.t.Helper()
	req := httptest.NewRequestWithContext(f.t.Context(), http.MethodPost, f.path(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := f.orderEnv.send(req, f.buyKey, "Idempotency-Key", key, "X-Attempt", strconv.Itoa(attempt))
	return parseReply(rec.Code, rec.Header(), rec.Body.Bytes())
}

// postFlash gửi một lượt qua HTTP thật tới base.
func (f *flashEnv) postFlash(base string, b buyer, attempt int) (flashReply, error) {
	req, err := http.NewRequestWithContext(f.t.Context(), http.MethodPost, base+f.path(), strings.NewReader(b.body()))
	if err != nil {
		return flashReply{}, err //nolint:wrapcheck // test
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.buyKey)
	req.Header.Set("Idempotency-Key", b.ref)
	req.Header.Set("X-Attempt", strconv.Itoa(attempt))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return flashReply{}, err //nolint:wrapcheck // test
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	return parseReply(resp.StatusCode, resp.Header, raw), err
}

// final: câu trả lời không phải "thử lại" (503, 409 request_in_progress).
func (r flashReply) final() bool {
	return r.status != http.StatusServiceUnavailable && (r.status != http.StatusConflict || r.errCode != "request_in_progress")
}

// website giả: gửi lượt của b, gặp 503 hay 409 đang chạy thì chờ chốc lát rồi gửi lại cùng khóa với X-Attempt tăng
// (tối đa 10); trả câu trả lời cuối. onReply (nếu có) được gọi sau mỗi câu trả lời.
func (f *flashEnv) website(base string, b buyer, onReply func(flashReply)) flashReply {
	for attempt := 1; ; attempt++ {
		r, err := f.postFlash(base, b, min(attempt, 10))
		if err != nil {
			f.t.Errorf("gửi lượt mua: %v", err)
			return r
		}
		if onReply != nil {
			onReply(r)
		}
		if r.final() || attempt > 2000 {
			return r
		}
		if r.status == http.StatusServiceUnavailable && r.retryAfter == "" {
			f.t.Errorf("503 không có Retry-After: %s", r.raw)
		}
		time.Sleep(time.Duration(5+attempt%20) * time.Millisecond)
	}
}

// crowd chạy n khách (buyer(0..n-1)) đồng thời qua HTTP thật; trả câu trả lời cuối của từng khách.
func (f *flashEnv) crowd(n int, onReply func(flashReply)) []flashReply {
	f.t.Helper()
	srv := httptest.NewServer(f.srv)
	f.t.Cleanup(srv.Close)
	out := make([]flashReply, n)
	var wg sync.WaitGroup
	for i := range n {
		b := f.buyer(i)
		wg.Go(func() { out[i] = f.website(srv.URL, b, onReply) })
	}
	wg.Wait()
	return out
}

func tally(rs []flashReply) map[string]int {
	m := map[string]int{}
	for _, r := range rs {
		if r.status == http.StatusOK {
			m[r.result]++
			continue
		}
		m[strconv.Itoa(r.status)+" "+r.errCode]++
	}
	return m
}

// checkInvariants kiểm bất biến của lượt mua (M5-18) cho món của f: đơn vị held/sold = dòng đơn flash = tổng claims
// = bought; không khách nào vượt limit; mỗi mã ngoài tối đa một đơn.
func (f *flashEnv) checkInvariants(bought, limit int) {
	f.t.Helper()
	q := func(sql string) int { return f.staff.CountIn(f.t, f.tenant, sql, f.item) }
	units := q(`SELECT count(*) FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1 AND status IN ('held', 'sold')`)
	lines := q(`SELECT coalesce(sum(quantity), 0) FROM ord.order_items WHERE flash_sale_item_id = $1`)
	claims := q(`SELECT coalesce(sum(quantity), 0) FROM promotion.flash_sale_claims WHERE flash_sale_item_id = $1`)
	if units != bought || lines != bought || claims != bought {
		f.t.Fatalf("bought %d, đơn vị held/sold %d, dòng đơn %d, claims %d", bought, units, lines, claims)
	}
	if n := f.staff.CountIn(f.t, f.tenant, `SELECT count(*) FROM promotion.flash_sale_claims
		WHERE flash_sale_item_id = $1 AND quantity > $2`, f.item, limit); n != 0 {
		f.t.Fatalf("%d khách vượt giới hạn %d", n, limit)
	}
	if n := f.staff.CountIn(f.t, f.tenant, `SELECT count(*) FROM (SELECT external_ref FROM ord.orders
		WHERE external_ref IS NOT NULL GROUP BY external_ref HAVING count(*) > 1) AS d`); n != 0 {
		f.t.Fatalf("%d mã ngoài có hơn một đơn", n)
	}
}

// [FS03], [FS02] 30 suất, 200 khách HTTP đồng thời (mỗi khách một SĐT), website giả tự gửi lại cùng khóa khi 503 →
// đúng 30 bought, còn lại sold_out, không 500; bất biến của M5-18 đạt.
func TestWebFlashBuy_ThirtySlotsTwoHundredBuyers(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 30, limit: 1, phoneMax: 10000})
	got := tally(f.crowd(200, nil))
	if got["bought"] != 30 || got["sold_out"] != 170 {
		t.Fatalf("kết quả %v, muốn 30 bought, 170 sold_out", got)
	}
	f.checkInvariants(30, 1)
}

// [FS07] Sau khi hết suất: lượt X-Attempt 1 trả sold_out mà không chạm database (0 câu SQL, không dòng khóa chống
// trùng). Khóa đã bought gửi lại với X-Attempt 2 khi cờ đang bật → đúng kết quả bought đã lưu, Idempotent-Replayed.
func TestWebFlashBuy_SoldOutFlagSkipsDatabase(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 1, limit: 1, phoneMax: 100})
	first := f.buyer(1)
	won := f.send(first, 1)
	if won.status != http.StatusOK || won.result != "bought" || won.code == "" {
		t.Fatalf("lượt đầu: %+v", won)
	}
	if r := f.send(f.buyer(2), 1); r.result != "sold_out" {
		t.Fatalf("lượt hai (database): %+v", r)
	}
	if soldOut, known := f.gate.SoldOut(t.Context(), f.tenant, f.item); !soldOut || !known {
		t.Fatalf("sold_out không dựng cờ: soldOut=%v known=%v", soldOut, known)
	}
	late := f.buyer(3)
	f.queries.Store(0)
	r := f.send(late, 1)
	if r.status != http.StatusOK || strings.TrimSpace(r.raw) != `{"result":"sold_out"}` {
		t.Fatalf("lượt sau cờ: %+v", r)
	}
	if n := f.queries.Load(); n != 0 {
		t.Fatalf("lượt sau cờ chạy %d câu SQL, muốn 0", n)
	}
	if n := f.storedKeys(late.ref); n != 0 {
		t.Fatalf("lượt sau cờ để lại %d dòng khóa chống trùng", n)
	}
	again := f.send(first, 2)
	if again.status != http.StatusOK || again.raw != won.raw || again.replayed != "true" {
		t.Fatalf("gửi lại khóa đã bought với X-Attempt 2: %+v, muốn %s (Idempotent-Replayed)", again, won.raw)
	}
	if n := f.ordersOf(first.ref); n != 1 {
		t.Fatalf("%d đơn cho mã ngoài đã mua, muốn 1", n)
	}
}

// [FS05] Một SĐT gửi 100 lượt song song (giới hạn mỗi khách 1, mỗi lượt một khóa và mã ngoài) → đúng 1 bought, không
// lượt nào mua lần hai; mọi lượt còn lại là 429 too_fast hoặc over_limit.
func TestWebFlashBuy_OnePhoneHundredParallel(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 30, limit: 1, phoneMax: 10})
	srv := httptest.NewServer(f.srv)
	t.Cleanup(srv.Close)
	out := make([]flashReply, 100)
	var wg sync.WaitGroup
	for i := range out {
		b := f.buyer(7)
		b.ip = fmt.Sprintf("10.9.%d.%d", i/256, i%256)
		wg.Go(func() { out[i] = f.website(srv.URL, b, nil) })
	}
	wg.Wait()
	got := tally(out)
	if got["bought"] != 1 || got["bought"]+got["over_limit"]+got["429 too_fast"] != 100 {
		t.Fatalf("kết quả %v, muốn 1 bought, còn lại 429 hay over_limit", got)
	}
	if got["429 too_fast"] < 50 {
		t.Fatalf("chỉ %d lượt 429 (ngưỡng SĐT 10/10 s), muốn phần lớn", got["429 too_fast"])
	}
	f.checkInvariants(1, 1)
}

// [FS04] FLASH_ADMIT_PER_SECOND = 5 (đồng hồ cổng đứng yên trong một giây): 50 lượt đồng thời → tối đa 5 dòng khóa
// chống trùng được tạo trong giây đó, còn lại 503 có Retry-After; website gửi lại cùng khóa sang giây sau → mỗi khóa
// đúng một kết quả, 50 đơn, không đơn trùng.
func TestWebFlashBuy_AdmitPerSecond(t *testing.T) {
	t.Parallel()
	var sec atomic.Int64
	base := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return base.Add(time.Duration(sec.Load()) * time.Second) }
	f := newFlashEnv(t, flashOpts{
		slots: 50, limit: 1, phoneMax: 1000,
		gate: cache.GateOptions{PerSecond: 5, GlobalPerSecond: 1000, Now: clock},
	})
	buyers := make([]buyer, 50)
	for i := range buyers {
		buyers[i] = f.buyer(100 + i)
	}
	pending := buyers
	for round := 1; len(pending) > 0; round++ {
		if round > 40 {
			t.Fatalf("còn %d khóa chưa có kết quả sau 40 giây giả", len(pending))
		}
		replies := make([]flashReply, len(pending))
		var wg sync.WaitGroup
		for i, b := range pending {
			wg.Go(func() { replies[i] = f.send(b, min(round, 10)) })
		}
		wg.Wait()
		next, admitted := sortRound(t, round, pending, replies)
		if keys := f.staff.CountIn(t, f.tenant, `SELECT count(*) FROM platform.idempotency_keys`); keys > 5*round {
			t.Fatalf("vòng %d: %d dòng khóa chống trùng, muốn ≤ %d", round, keys, 5*round)
		}
		if admitted > 5 {
			t.Fatalf("vòng %d: %d lượt qua cổng trong một giây, muốn ≤ 5", round, admitted)
		}
		pending = next
		sec.Add(1)
	}
	for _, b := range buyers {
		if n := f.ordersOf(b.ref); n != 1 {
			t.Fatalf("mã %s có %d đơn, muốn 1", b.ref, n)
		}
	}
	f.checkInvariants(50, 1)
}

// sortRound chia câu trả lời của một vòng: 503 retry_later (phải có Retry-After) hay 409 đang chạy → gửi lại vòng
// sau; 200 bought → đã qua cổng; câu khác làm test đỏ.
func sortRound(t *testing.T, round int, pending []buyer, replies []flashReply) (next []buyer, admitted int) {
	t.Helper()
	for i, r := range replies {
		switch {
		case r.status == http.StatusServiceUnavailable && r.errCode == "retry_later":
			if r.retryAfter == "" {
				t.Fatalf("503 không có Retry-After: %+v", r)
			}
			next = append(next, pending[i])
		case r.status == http.StatusConflict && r.errCode == "request_in_progress":
			next = append(next, pending[i])
		case r.status == http.StatusOK && r.result == "bought":
			admitted++
		default:
			t.Fatalf("vòng %d: câu trả lời lạ %+v", round, r)
		}
	}
	return next, admitted
}

// [FS18] Thiếu SĐT → 400 phone_required, không giữ khóa chống trùng, không đơn.
func TestWebFlashBuy_PhoneRequired(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 3, limit: 1, phoneMax: 100})
	r := f.sendBody("FS-NOPHONE", `{"externalRef":"FS-NOPHONE","qty":1,"clientIp":"203.0.113.9"}`, 1)
	if r.status != http.StatusBadRequest || r.errCode != "phone_required" {
		t.Fatalf("thiếu SĐT: %+v", r)
	}
	if f.storedKeys("FS-NOPHONE") != 0 || f.ordersOf("FS-NOPHONE") != 0 {
		t.Fatal("thiếu SĐT mà vẫn giữ khóa hay tạo đơn")
	}
}

// [FS10] Dừng Valkey gate giữa lúc 200 khách mua → vẫn đúng 30 bought, không 500; cổng đếm gate_degraded > 0.
func TestWebFlashBuy_ValkeyStopped(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 30, limit: 1, phoneMax: 10000})
	var replies, bad atomic.Int64
	reached := make(chan struct{})
	var once sync.Once
	done := make(chan map[string]int)
	go func() {
		done <- tally(f.crowd(200, func(r flashReply) {
			if r.status >= http.StatusInternalServerError && r.status != http.StatusServiceUnavailable {
				bad.Add(1)
			}
			if replies.Add(1) >= 40 {
				once.Do(func() { close(reached) })
			}
		}))
	}()
	<-reached
	f.vk.Stop(t) // goroutine của test: Stop có thể gọi t.Fatalf
	got := <-done
	if bad.Load() != 0 {
		t.Fatalf("%d câu trả lời 5xx (không phải 503)", bad.Load())
	}
	if got["bought"] != 30 || got["sold_out"] != 170 {
		t.Fatalf("kết quả %v, muốn 30 bought, 170 sold_out", got)
	}
	f.checkInvariants(30, 1)
	if n := counterOf(f.reg, "gate_degraded"); n == 0 {
		t.Fatal("gate_degraded = 0 sau khi Valkey dừng")
	}
}

// [HT27] FLUSHALL Valkey sau khi hết suất (cờ đã dựng): không bán vượt; lượt X-Attempt 1 kế tiếp không còn cờ nên
// xuống database (có câu SQL) và vẫn trả sold_out, rồi dựng lại cờ.
func TestWebFlashBuy_FlushAll(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t, flashOpts{slots: 5, limit: 1, phoneMax: 10000})
	got := tally(f.crowd(20, nil))
	if got["bought"] != 5 || got["sold_out"] != 15 {
		t.Fatalf("trước FLUSHALL: %v", got)
	}
	f.vk.FlushAll(t)
	if _, known := f.gate.SoldOut(t.Context(), f.tenant, f.item); known {
		t.Fatal("cờ còn sau FLUSHALL")
	}
	f.queries.Store(0)
	if r := f.send(f.buyer(500), 1); r.result != "sold_out" {
		t.Fatalf("lượt sau FLUSHALL: %+v", r)
	}
	if f.queries.Load() == 0 {
		t.Fatal("lượt sau FLUSHALL không xuống database")
	}
	if soldOut, known := f.gate.SoldOut(t.Context(), f.tenant, f.item); !soldOut || !known {
		t.Fatal("cờ không được dựng lại sau lượt sold_out")
	}
	f.checkInvariants(5, 1)
}

// Dấu vết của một lượt mua có đoạn của cổng (flash.admission), câu SQL của khóa chống trùng và transaction mua
// (flash.buy); không thuộc tính, tên, sự kiện nào chứa SĐT hay IP.
func TestWebFlashBuy_TraceHasNoPhoneOrIP(t *testing.T) {
	exp := testkit.CaptureSpans(t)
	f := newFlashEnv(t, flashOpts{slots: 2, limit: 1, phoneMax: 100, traced: true})
	b := buyer{phone: "0907654321", ip: "203.0.113.77", ref: "FS-TRACE-1"}
	if r := f.send(b, 1); r.result != "bought" {
		t.Fatalf("lượt mua: %+v", r)
	}
	names := map[string]bool{}
	idem := false
	for _, s := range exp.GetSpans() {
		names[s.Name] = true
		raw, _ := json.Marshal(struct {
			Name  string
			Attrs any
			Ev    any
		}{s.Name, s.Attributes, s.Events})
		if strings.Contains(string(raw), "idempotency_keys") {
			idem = true
		}
		for _, secret := range []string{"0907654321", "907654321", "203.0.113.77"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("span %s chứa %q", s.Name, secret)
			}
		}
	}
	if !names["flash.admission"] || !names["flash.buy"] || !idem {
		t.Fatalf("thiếu đoạn: admission=%v buy=%v khóa chống trùng=%v (%v)", names["flash.admission"], names["flash.buy"],
			idem, names)
	}
}

// counterOf đọc tổng counter name (mọi nhãn) của reg.
func counterOf(reg *metrics.Registry, name string) int64 {
	var n int64
	for _, r := range reg.Drain() {
		if r.Name == name {
			n += r.Count
		}
	}
	return n
}
