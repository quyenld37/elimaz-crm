package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit"
)

// Cổng nhận vào của lượt mua flash sale (Task 203, M5-20): thứ tự kiểm, câu trả lời của từng lớp, vé được trả.

// fakeGate là AdmissionGate thử: soldOut/known cố định, admit quyết định từng lượt, đếm vé đang giữ.
type fakeGate struct {
	mu            sync.Mutex
	soldOut       bool
	known         bool
	admit         bool
	soldOutCalls  int
	admitCalls    int
	setStateCalls int
}

func (g *fakeGate) Admit(context.Context, uuid.UUID, uuid.UUID) *cache.Admission {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.admitCalls++
	if !g.admit {
		return &cache.Admission{Reason: cache.ReasonBusy, RetryAfter: 1500 * time.Millisecond}
	}
	return &cache.Admission{Admitted: true}
}

func (g *fakeGate) SoldOut(context.Context, uuid.UUID, uuid.UUID) (soldOut, known bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.soldOutCalls++
	return g.soldOut, g.known
}

func (g *fakeGate) SetState(context.Context, uuid.UUID, uuid.UUID, int, bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.setStateCalls++
	return nil
}

func (*fakeGate) ClearState(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (g *fakeGate) Remaining(context.Context, uuid.UUID, []uuid.UUID) map[uuid.UUID]int {
	return map[uuid.UUID]int{}
}

// admitEnv: cổng thử trên bộ đếm trong tiến trình (SĐT 2 lượt, IP 3 lượt / 10 s), handler kế tiếp ghi lại thân đã
// nhận và trả status của nó.
type admitEnv struct {
	gate   *fakeGate
	h      http.Handler
	reg    *metrics.Registry
	calls  atomic.Int64
	body   atomic.Value // string: thân handler kế tiếp đọc được
	status int
}

const admitPath = "/api/v1/web/flash-sales/%s/items/%s/buy"

func newAdmitEnv(t *testing.T) *admitEnv {
	t.Helper()
	e := &admitEnv{gate: &fakeGate{admit: true}, reg: metrics.NewRegistry(), status: http.StatusOK}
	mw := NewFlashAdmission(FlashAdmissionOptions{
		Gate:     e.gate,
		Limiter:  cache.NewRateLimiter(nil, nil, nil),
		Customer: cache.Limit{Name: "flash_customer", Max: 2, Window: 10 * time.Second, FallbackMax: 2},
		IP:       cache.Limit{Name: "flash_ip", Max: 3, Window: 10 * time.Second, FallbackMax: 3},
		Secret:   []byte("khoa-hmac-thu-cua-test-du-32-ky-tu!!"),
		Metrics:  e.reg,
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		e.body.Store(string(b))
		w.WriteHeader(e.status)
	})
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/web/flash-sales/{saleId}/items/{itemId}/buy", mw(next))
	e.h = requestID(mux)
	return e
}

// post gửi thân body với X-Attempt attempt (rỗng: không gửi) của tenant thử.
func (e *admitEnv) post(t *testing.T, body, attempt string) *httptest.ResponseRecorder {
	t.Helper()
	path := strings.Replace(strings.Replace(admitPath, "%s", uuid.NewString(), 1), "%s", testItemID, 1)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	if attempt != "" {
		req.Header.Set("X-Attempt", attempt)
	}
	ctx := context.WithValue(req.Context(), authUserKey{}, identity.AuthUser{
		TenantID: uuid.MustParse(testTenantID), Kind: identity.KindService, ServiceKeyID: uuid.New(),
	})
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// Tenant và món thử của cổng (hằng chuỗi; gochecknoglobals cấm biến cấp gói).
const (
	testTenantID = "11111111-1111-4111-8111-111111111111"
	testItemID   = "22222222-2222-4222-8222-222222222222"
)

func flashBody(phone, ip string) string {
	return `{"externalRef":"FS-1","phone":"` + phone + `","qty":1,"clientIp":"` + ip + `"}`
}

// [FS18] Thiếu, rỗng hay sai SĐT → 400 phone_required; IP thiếu, sai → 400 bad_request; thân không phải JSON → 400
// bad_request; X-Attempt ngoài 1–10 hay không phải số → 400 bad_request. Không lượt nào tới cổng hay handler.
func TestFlashAdmission_BadInput(t *testing.T) {
	t.Parallel()
	e := newAdmitEnv(t)
	for _, c := range []struct {
		name, body, attempt, code string
	}{
		{"thiếu SĐT", `{"externalRef":"FS-1","qty":1,"clientIp":"203.0.113.7"}`, "", codePhoneRequired},
		{"SĐT rỗng", flashBody("", "203.0.113.7"), "", codePhoneRequired},
		{"SĐT sai", flashBody("12ab", "203.0.113.7"), "", codePhoneRequired},
		{"thiếu IP", `{"externalRef":"FS-1","phone":"0901234567","qty":1}`, "", codeBadRequest},
		{"IP sai", flashBody("0901234567", "999.1.1.1"), "", codeBadRequest},
		{"không phải JSON", `phone=0901234567`, "", codeBadRequest},
		{"X-Attempt 0", flashBody("0901234567", "203.0.113.7"), "0", codeBadRequest},
		{"X-Attempt 11", flashBody("0901234567", "203.0.113.7"), "11", codeBadRequest},
		{"X-Attempt chữ", flashBody("0901234567", "203.0.113.7"), "hai", codeBadRequest},
	} {
		rec := e.post(t, c.body, c.attempt)
		if rec.Code != http.StatusBadRequest || errCode(t, rec) != c.code {
			t.Errorf("%s: %d %s, muốn 400 %s", c.name, rec.Code, rec.Body.String(), c.code)
		}
	}
	if e.calls.Load() != 0 || e.gate.admitCalls != 0 || e.gate.soldOutCalls != 0 {
		t.Fatalf("đầu vào sai mà tới cổng/handler: handler %d, Admit %d, SoldOut %d",
			e.calls.Load(), e.gate.admitCalls, e.gate.soldOutCalls)
	}
}

// [FS05] Giới hạn theo SĐT (2) rồi theo IP (3): lượt vượt → 429 too_fast có Retry-After, không tới cổng điều tiết.
// SĐT viết hai dạng (0… và +84…) là một khách. Thân handler nhận được nguyên vẹn.
func TestFlashAdmission_RateLimits(t *testing.T) {
	t.Parallel()
	e := newAdmitEnv(t)
	body := flashBody("0901234567", "203.0.113.7")
	for i := range 2 {
		if rec := e.post(t, body, ""); rec.Code != http.StatusOK {
			t.Fatalf("lượt %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if got, _ := e.body.Load().(string); got != body {
		t.Fatalf("handler nhận thân %q, muốn %q", got, body)
	}
	rec := e.post(t, flashBody("+84901234567", "198.51.100.1"), "")
	if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != codeTooFast || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("SĐT lượt 3: %d %s Retry-After=%q", rec.Code, rec.Body.String(), rec.Header().Get("Retry-After"))
	}
	// IP 203.0.113.7 đã 2 lượt; SĐT khác cùng IP: lượt 3 qua, lượt 4 bị chặn theo IP.
	if rec := e.post(t, flashBody("0901000002", "203.0.113.7"), ""); rec.Code != http.StatusOK {
		t.Fatalf("IP lượt 3: %d", rec.Code)
	}
	rec = e.post(t, flashBody("0901000003", "203.0.113.7"), "")
	if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != codeTooFast {
		t.Fatalf("IP lượt 4: %d %s", rec.Code, rec.Body.String())
	}
	// IPv6 cùng /64 là một IP.
	for i := range 3 {
		if rec := e.post(t, flashBody("09010001"+string(rune('1'+i))+"0", "2001:db8:1:2::"+string(rune('1'+i))), ""); rec.Code != http.StatusOK {
			t.Fatalf("IPv6 lượt %d: %d", i+1, rec.Code)
		}
	}
	if rec := e.post(t, flashBody("0901000150", "2001:db8:1:2:ffff::9"), ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("IPv6 cùng /64 lượt 4: %d", rec.Code)
	}
	if e.gate.admitCalls != int(e.calls.Load()) {
		t.Fatalf("Admit %d lần, handler %d lần", e.gate.admitCalls, e.calls.Load())
	}
}

// Task 525: khóa giới hạn theo từng khách, không chung — 200 khách khác SĐT, khác IP (như e2e flash: 093…, 10.20.x.y)
// mỗi người dùng đủ 2 lượt của mình đều qua; lượt thứ 3 của mỗi người bị 429 too_fast mà không làm khách khác bị chặn.
func TestFlashAdmission_LimitsPerSubject(t *testing.T) {
	t.Parallel()
	e := newAdmitEnv(t)
	const n = 200
	body := func(i int) string {
		return flashBody(fmt.Sprintf("09%08d", 30000000+i), fmt.Sprintf("10.20.%d.%d", i/256, i%256))
	}
	for round := range 2 {
		for i := range n {
			if rec := e.post(t, body(i), ""); rec.Code != http.StatusOK {
				t.Fatalf("khách %d lượt %d: %d %s", i, round+1, rec.Code, rec.Body.String())
			}
		}
	}
	for i := range n {
		rec := e.post(t, body(i), "")
		if rec.Code != http.StatusTooManyRequests || errCode(t, rec) != codeTooFast {
			t.Fatalf("khách %d lượt 3: %d %s, muốn 429 too_fast", i, rec.Code, rec.Body.String())
		}
	}
	if got := e.calls.Load(); got != 2*n {
		t.Fatalf("handler chạy %d lần, muốn %d", got, 2*n)
	}
}

// [FS07] Cờ hết suất biết chắc: X-Attempt vắng hay 1 → 200 {"result":"sold_out"}, không tới cổng điều tiết hay
// handler, đếm flash_gate_short_circuit; X-Attempt 2 bỏ qua cờ nhưng vẫn qua Admit (QĐ21). Cờ "không biết" → đi tiếp.
func TestFlashAdmission_SoldOutShortCircuit(t *testing.T) {
	t.Parallel()
	e := newAdmitEnv(t)
	e.gate.soldOut, e.gate.known = true, true
	for i, attempt := range []string{"", "1"} {
		rec := e.post(t, flashBody("0901000"+string(rune('1'+i))+"00", "203.0.113.1"+string(rune('1'+i))), attempt)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"result":"sold_out"}` {
			t.Fatalf("X-Attempt %q: %d %s", attempt, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type %q", ct)
		}
	}
	if e.calls.Load() != 0 || e.gate.admitCalls != 0 {
		t.Fatalf("cờ hết suất mà vẫn tới Admit (%d) hay handler (%d)", e.gate.admitCalls, e.calls.Load())
	}
	rec := e.post(t, flashBody("0901000300", "203.0.113.30"), "2")
	if rec.Code != http.StatusOK || e.calls.Load() != 1 || e.gate.admitCalls != 1 {
		t.Fatalf("X-Attempt 2: %d, handler %d, Admit %d", rec.Code, e.calls.Load(), e.gate.admitCalls)
	}
	e.gate.known = false
	if rec := e.post(t, flashBody("0901000400", "203.0.113.40"), "1"); rec.Code != http.StatusOK || e.calls.Load() != 2 {
		t.Fatalf("cờ không biết: %d, handler %d", rec.Code, e.calls.Load())
	}
	if n := counter(e.reg, "flash_gate_short_circuit"); n != 2 {
		t.Fatalf("flash_gate_short_circuit = %d, muốn 2", n)
	}
}

// [FS04] Cổng điều tiết từ chối → 503 retry_later, Retry-After làm tròn lên (1,5 s → 2), handler không chạy. 503 do
// handler trả mà không đặt Retry-After (suất đang bị khóa) được thêm Retry-After: 1.
func TestFlashAdmission_GateBusyAndRetryAfter(t *testing.T) {
	t.Parallel()
	e := newAdmitEnv(t)
	e.gate.admit = false
	rec := e.post(t, flashBody("0901234567", "203.0.113.7"), "")
	if rec.Code != http.StatusServiceUnavailable || errCode(t, rec) != codeRetryLater || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("cổng từ chối: %d %s Retry-After=%q", rec.Code, rec.Body.String(), rec.Header().Get("Retry-After"))
	}
	if e.calls.Load() != 0 {
		t.Fatal("cổng từ chối mà handler vẫn chạy")
	}
	e.gate.admit = true
	e.status = http.StatusServiceUnavailable
	rec = e.post(t, flashBody("0901000002", "203.0.113.8"), "")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("503 của handler: %d Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// Vé của cổng thật (cache.NewAdmissionGate, không Valkey, MaxInflight 1) được trả sau khi handler xong: hai lượt nối
// tiếp đều vào; lượt thứ hai trong lúc lượt đầu đang chạy bị 503 (đầy chỗ đang xử lý).
func TestFlashAdmission_ReleasesTicket(t *testing.T) {
	t.Parallel()
	gate := cache.NewAdmissionGate(nil, cache.GateOptions{MaxInflight: 1, PerSecond: 1000, GlobalPerSecond: 1000}, nil)
	inside, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	mw := NewFlashAdmission(FlashAdmissionOptions{
		Gate: gate, Limiter: cache.NewRateLimiter(nil, nil, nil),
		Customer: cache.DefaultLimits().FlashCustomer, IP: cache.DefaultLimits().FlashIP,
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if first.CompareAndSwap(false, true) {
			close(inside)
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/web/flash-sales/{saleId}/items/{itemId}/buy", h)
	e := &admitEnv{h: requestID(mux)}
	done := make(chan int)
	go func() { done <- e.post(t, flashBody("0901000001", "203.0.113.1"), "").Code }()
	<-inside
	if rec := e.post(t, flashBody("0901000002", "203.0.113.2"), ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("lượt hai khi lượt đầu còn chạy: %d, muốn 503", rec.Code)
	}
	close(release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("lượt đầu: %d", code)
	}
	if rec := e.post(t, flashBody("0901000003", "203.0.113.3"), ""); rec.Code != http.StatusOK {
		t.Fatalf("lượt sau khi vé được trả: %d", rec.Code)
	}
}

// Span của cổng (flash.admission) có kết quả nhưng không có SĐT hay IP ở thuộc tính, tên hay sự kiện nào.
func TestFlashAdmission_SpanHasNoPersonalData(t *testing.T) {
	exp := testkit.CaptureSpans(t)
	e := newAdmitEnv(t)
	e.post(t, flashBody("0907654321", "203.0.113.77"), "")
	e.gate.soldOut, e.gate.known = true, true
	e.post(t, flashBody("0907654322", "203.0.113.78"), "")
	spans := exp.GetSpans()
	found := 0
	for _, s := range spans {
		if s.Name == "flash.admission" {
			found++
		}
		raw, _ := json.Marshal(struct {
			Name  string
			Attrs any
			Ev    any
		}{s.Name, s.Attributes, s.Events})
		for _, secret := range []string{"0907654321", "907654321", "203.0.113.77", "0907654322", "203.0.113.78"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("span %s chứa %q: %s", s.Name, secret, raw)
			}
		}
	}
	if found != 2 {
		t.Fatalf("%d span flash.admission, muốn 2", found)
	}
}

// counter đọc tổng counter name (mọi nhãn) của reg.
func counter(reg *metrics.Registry, name string) int64 {
	var n int64
	for _, r := range reg.Drain() {
		if r.Name == name {
			n += r.Count
		}
	}
	return n
}

// WriteTxWith chạy fn với đúng tùy chọn transaction đã cho (TxFlashBuy: lock_timeout 200 ms) và lưu kết quả như
// WriteTx; WriteTx giữ tùy chọn TxAPI.
func TestWriteTxWith_UsesTxOptions(t *testing.T) {
	t.Parallel()
	e := newIdemEnv(t)
	authE := &authEnv{t: t, keys: e.keys}
	d := prepare(Deps{
		Config: config.Config{ConsoleOrigins: []string{testOrigin}}, DB: e.staff.App,
		Logger: slog.New(slog.DiscardHandler), Access: authE.verifier(),
	})
	probe := func(flash bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, _ := authUser(r.Context())
			fn := func(tx pgx.Tx) (int, any, bool, error) { //nolint:contextcheck // fn của WriteTx (kế hoạch) không nhận ctx; dùng ctx của yêu cầu
				var lt string
				if err := tx.QueryRow(r.Context(), `SHOW lock_timeout`).Scan(&lt); err != nil {
					return 0, nil, false, err //nolint:wrapcheck // test
				}
				return http.StatusOK, map[string]string{"lock_timeout": lt}, true, nil
			}
			var err error
			if flash {
				err = WriteTxWith(r.Context(), d, u.TenantID, db.TxFlashBuy, fn)
			} else {
				err = WriteTx(r.Context(), d, u.TenantID, fn)
			}
			if err != nil {
				writeHandlerError(d, w, r, err)
			}
		})
	}
	for _, p := range []struct {
		pattern string
		flash   bool
	}{{"POST /api/v1/_probe/flashtx", true}, {"POST /api/v1/_probe/apitx", false}} {
		e.mux.Handle(p.pattern, chain(d, Route{
			Method: "POST", Pattern: p.pattern, OperationID: "probeTx" + p.pattern[len(p.pattern)-7:],
			Permission: "platform.jobs.manage", Write: true, IdempotencyMode: "required",
		}, probe(p.flash)))
	}
	for _, c := range []struct{ path, want string }{
		{"/api/v1/_probe/flashtx", db.TxFlashBuy.LockTimeout.String()}, // 200ms
		{"/api/v1/_probe/apitx", db.TxAPI.LockTimeout.String()},
	} {
		key := uuid.NewString()
		rec := e.post(c.path, userA, key, `{}`)
		var got map[string]string
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got["lock_timeout"] != c.want {
			t.Fatalf("%s: %d %s, muốn lock_timeout %s", c.path, rec.Code, rec.Body.String(), c.want)
		}
		if again := e.post(c.path, userA, key, `{}`); again.Header().Get(headerReplayed) != "true" {
			t.Fatalf("%s: gửi lại cùng khóa không phát lại kết quả đã lưu", c.path)
		}
	}
}
