package metrics_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/testkit"
)

const testProcess = "worker@test"

func openDB(t *testing.T) *db.DB {
	t.Helper()
	return testkit.OpenAppDB(t, db.PoolOptions{DefaultTx: db.TxWorker})
}

// newFlusher dựng Flusher của worker (không gửi nhịp sống ra ngoài) trên database thử mới.
func newFlusher(t *testing.T) (*db.DB, *metrics.Registry, *metrics.Flusher) {
	t.Helper()
	d := openDB(t)
	reg := metrics.NewRegistry()
	return d, reg, &metrics.Flusher{DB: d, Reg: reg, Process: testProcess, Role: config.RoleWorker}
}

type dbRow struct {
	count    int64
	sum, max float64
	hist     []int32
}

func readRow(t *testing.T, d *db.DB, name, label string) dbRow {
	t.Helper()
	var r dbRow
	err := d.Pool.QueryRow(t.Context(),
		"SELECT count, sum, max, hist FROM platform.metrics_minute WHERE process = $1 AND name = $2 AND label = $3",
		testProcess, name, label).Scan(&r.count, &r.sum, &r.max, &r.hist)
	if err != nil {
		t.Fatalf("đọc dòng %s/%q: %v", name, label, err)
	}
	return r
}

func countRows(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Pool.QueryRow(t.Context(), "SELECT count(*) FROM platform.metrics_minute").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func toInts(h []int32) []int {
	out := make([]int, len(h))
	for i, v := range h {
		out[i] = int(v)
	}
	return out
}

// minuteStart trả đầu một phút cố định cho các test cần bucket biết trước.
func minuteStart() time.Time { return time.Date(2026, 11, 18, 10, 30, 0, 0, time.UTC) }

// 95 giá trị 40 và 5 giá trị 900 → 1 dòng count = 100, max = 900, P95 = 50.
func TestFlushHistogram(t *testing.T) {
	d, reg, f := newFlusher(t)
	for range 95 {
		reg.Observe("job_ms", 40, "k")
	}
	for range 5 {
		reg.Observe("job_ms", 900, "k")
	}
	if err := f.Flush(t.Context(), minuteStart().Add(12*time.Second)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n := countRows(t, d); n != 1 {
		t.Fatalf("số dòng = %d, muốn 1", n)
	}
	r := readRow(t, d, "job_ms", "k")
	if r.count != 100 || r.max != 900 || r.sum != 95*40+5*900 {
		t.Fatalf("dòng sai: %+v", r)
	}
	if p := metrics.P95FromHist(toInts(r.hist)); p != 50 {
		t.Fatalf("P95 = %v, muốn 50 (hist %v)", p, r.hist)
	}
	var bucket time.Time
	if err := d.Pool.QueryRow(t.Context(), "SELECT bucket FROM platform.metrics_minute").Scan(&bucket); err != nil {
		t.Fatal(err)
	}
	if !bucket.Equal(minuteStart()) {
		t.Fatalf("bucket = %v, muốn đầu phút %v", bucket, minuteStart())
	}
}

// Hai lần Flush cùng phút cộng dồn; Gauge giữ giá trị cuối và lớn nhất.
func TestFlushAccumulatesWithinMinute(t *testing.T) {
	d, reg, f := newFlusher(t)

	reg.Observe("job_ms", 40, "k")
	reg.Inc("tx_retry", "40P01", 2)
	reg.Gauge("sse_conns", 7, "")
	if err := f.Flush(t.Context(), minuteStart().Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	reg.Observe("job_ms", 900, "k")
	reg.Inc("tx_retry", "40P01", 3)
	reg.Gauge("sse_conns", 2, "")
	if err := f.Flush(t.Context(), minuteStart().Add(50*time.Second)); err != nil {
		t.Fatal(err)
	}
	h := readRow(t, d, "job_ms", "k")
	if h.count != 2 || h.sum != 940 || h.max != 900 {
		t.Fatalf("histogram không cộng dồn: %+v", h)
	}
	if hist := toInts(h.hist); hist[0] != 1 || hist[6] != 1 {
		t.Fatalf("ngăn không cộng dồn: %v", hist)
	}
	if c := readRow(t, d, "tx_retry", "40P01"); c.count != 5 || c.hist != nil {
		t.Fatalf("counter không cộng dồn: %+v", c)
	}
	if g := readRow(t, d, "sse_conns", ""); g.sum != 2 || g.max != 7 {
		t.Fatalf("gauge phải giữ giá trị cuối 2, lớn nhất 7: %+v", g)
	}

	// Phút sau là dòng mới.
	reg.Inc("tx_retry", "40P01", 1)
	if err := f.Flush(t.Context(), minuteStart().Add(70*time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, d); n != 4 {
		t.Fatalf("số dòng = %d, muốn 4 (3 của phút đầu + 1 của phút sau)", n)
	}
}

// Flush lỗi (database đóng) giữ lô để ghi ở lần sau; không mất, không cộng hai lần.
func TestFlushKeepsBatchOnError(t *testing.T) {
	d, reg, f := newFlusher(t)
	reg.Inc("tx_retry", "40P01", 4)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.Flush(ctx, minuteStart()); err == nil {
		t.Fatal("Flush với ctx đã hủy phải lỗi")
	}
	if err := f.Flush(t.Context(), minuteStart().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var n int64
	var bucket time.Time
	if err := d.Pool.QueryRow(t.Context(),
		"SELECT count, bucket FROM platform.metrics_minute WHERE name = 'tx_retry'").Scan(&n, &bucket); err != nil {
		t.Fatal(err)
	}
	if n != 4 || !bucket.Equal(minuteStart()) {
		t.Fatalf("lô giữ lại phải ghi đúng một lần vào phút gốc: count %d, bucket %v", n, bucket)
	}
}

// Beat và Flush cập nhật process_heartbeats.at; HeartbeatAge đọc tuổi theo đồng hồ database.
func TestHeartbeat(t *testing.T) {
	d, _, f := newFlusher(t)

	if _, err := metrics.HeartbeatAge(t.Context(), d, testProcess); !errors.Is(err, metrics.ErrNoHeartbeat) {
		t.Fatalf("chưa có nhịp sống phải trả ErrNoHeartbeat, nhận %v", err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := f.Beat(t.Context(), old); err != nil {
		t.Fatalf("Beat: %v", err)
	}
	age, err := metrics.HeartbeatAge(t.Context(), d, testProcess)
	if err != nil || age < 110*time.Second || age > 130*time.Second {
		t.Fatalf("tuổi phải khoảng 2 phút: %v, %v", age, err)
	}
	if err := f.Flush(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	age, err = metrics.HeartbeatAge(t.Context(), d, testProcess)
	if err != nil || age > 5*time.Second {
		t.Fatalf("Flush phải cập nhật nhịp sống: %v, %v", age, err)
	}
	var role string
	if err := d.Pool.QueryRow(t.Context(),
		"SELECT role FROM platform.process_heartbeats WHERE process = $1", testProcess).Scan(&role); err != nil || role != "worker" {
		t.Fatalf("role = %q, %v", role, err)
	}
}

// GET /health/live qua Middleware sinh dòng http_ms nhãn "GET /health/live".
func TestMiddlewareRecordsRoutePattern(t *testing.T) {
	d := openDB(t)
	reg := metrics.NewRegistry()
	mux := http.NewServeMux()
	observability.NewHealth(d.Pool).Register(mux)
	srv := httptest.NewServer(metrics.Middleware(reg, mux))
	t.Cleanup(srv.Close)

	for _, path := range []string{"/health/live", "/khong-co/0901234567"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	f := &metrics.Flusher{DB: d, Reg: reg, Process: testProcess, Role: config.RoleAPI}
	if err := f.Flush(t.Context(), minuteStart()); err != nil {
		t.Fatal(err)
	}
	if r := readRow(t, d, "http_ms", "GET /health/live"); r.count != 1 {
		t.Fatalf("http_ms GET /health/live: %+v", r)
	}
	// Đường không khớp route không để lộ đường dẫn thật (có thể chứa SĐT) vào nhãn.
	if r := readRow(t, d, "http_ms", "GET unmatched"); r.count != 1 {
		t.Fatalf("http_ms GET unmatched: %+v", r)
	}
}

// Một lần Flush → một GET tới HEARTBEAT_URL.
func TestFlushPingsAfterWrite(t *testing.T) {
	d := openDB(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			hits.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	f := &metrics.Flusher{
		DB: d, Reg: metrics.NewRegistry(), Process: testProcess, Role: config.RoleWorker,
		Pinger: &metrics.Pinger{URL: srv.URL + "/ping/abc", Client: srv.Client()},
	}
	if err := f.Flush(t.Context(), minuteStart()); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("số GET = %d, muốn 1", n)
	}
}

// Máy nhận nhịp sống treo 10 s → Flush vẫn trả trong 6 s, không lỗi, đếm heartbeat_ping_failed.
func TestFlushPingHangDoesNotBlock(t *testing.T) {
	d := openDB(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(10 * time.Second):
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	reg := metrics.NewRegistry()
	f := &metrics.Flusher{
		DB: d, Reg: reg, Process: testProcess, Role: config.RoleWorker,
		Pinger: &metrics.Pinger{URL: srv.URL, Client: srv.Client()},
	}
	start := time.Now()
	if err := f.Flush(t.Context(), minuteStart()); err != nil {
		t.Fatalf("lỗi gửi nhịp sống không được làm Flush lỗi: %v", err)
	}
	if el := time.Since(start); el > 6*time.Second {
		t.Fatalf("Flush mất %v, muốn dưới 6 s", el)
	}
	if r := findRow(t, reg.Drain(), "heartbeat_ping_failed", ""); r.Count != 1 {
		t.Fatalf("heartbeat_ping_failed = %d, muốn 1", r.Count)
	}
}
