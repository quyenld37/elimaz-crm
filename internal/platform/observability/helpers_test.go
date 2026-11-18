package observability_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
)

// env là một database thử đã migrate: d mở bằng crm_app (pool của worker), ownerURL, adminURL cho câu dựng dữ liệu.
type env struct {
	d                *db.DB
	adminURL, appURL string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	adminURL, _, appURL := testkit.StartPostgres(t)
	d, err := db.Open(t.Context(), appURL, db.PoolOptions{DefaultTx: db.TxWorker})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(d.Pool.Close)
	return &env{d: d, adminURL: adminURL, appURL: appURL}
}

// exec chạy câu bằng crm_app.
func (e *env) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := e.d.Pool.Exec(t.Context(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func connect(t *testing.T, u string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(t.Context(), u)
	if err != nil {
		t.Fatalf("nối %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func (e *env) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.d.Pool.QueryRow(t.Context(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// job chèn một việc với trạng thái, run_at và finished_at cho trước; trả mã việc.
func (e *env) job(t *testing.T, kind, status, payload string, runAt time.Time, finishedAt *time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.d.Pool.QueryRow(t.Context(), `
		INSERT INTO platform.jobs (kind, mode, payload, dedupe_key, on_lease_expired, max_attempts, status, run_at, finished_at)
		VALUES ($1, 'transactional', $2::jsonb, gen_random_uuid()::text, 'retry', 8, $3, $4, $5) RETURNING id`,
		kind, payload, status, runAt, finishedAt).Scan(&id)
	if err != nil {
		t.Fatalf("chèn việc: %v", err)
	}
	return id
}

// metric chèn một dòng metrics_minute (sum = value, max = value, count = 1).
func (e *env) metric(t *testing.T, bucket time.Time, process, name, label string, value float64) {
	t.Helper()
	e.exec(t, `INSERT INTO platform.metrics_minute (bucket, process, name, label, count, sum, max)
		VALUES ($1, $2, $3, $4, 1, $5, $5)`, bucket.UTC().Truncate(time.Minute), process, name, label, value)
}

func (e *env) heartbeat(t *testing.T, process, role string, at time.Time) {
	t.Helper()
	e.exec(t, `INSERT INTO platform.process_heartbeats (process, role, at) VALUES ($1, $2, $3)
		ON CONFLICT (process) DO UPDATE SET at = EXCLUDED.at`, process, role, at)
}

func (e *env) alertSent(t *testing.T, key string, ago time.Duration) {
	t.Helper()
	e.exec(t, `INSERT INTO platform.alert_events (key, severity, text, sent_at)
		VALUES ($1, 'normal', 'thử', now() - make_interval(secs => $2))`, key, ago.Seconds())
}

type sent struct{ key, severity, text string }

// fakeAlerter ghi lại mọi lần Notify.
type fakeAlerter struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeAlerter) Notify(_ context.Context, key, severity, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{key, severity, text})
	return nil
}

func (f *fakeAlerter) find(key string) (sent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.sent, func(s sent) bool { return s.key == key })
	if i < 0 {
		return sent{}, false
	}
	return f.sent[i], true
}

// tgReq là một lần gọi sendMessage tới Telegram giả.
type tgReq struct {
	path, chatID, text string
}

type fakeTelegram struct {
	mu     sync.Mutex
	reqs   []tgReq
	status int
	body   string
}

func (f *fakeTelegram) calls() []tgReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

func (f *fakeTelegram) set(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.body = status, body
}

// newTelegram dựng Telegram giả và http.Client chuyển mọi yêu cầu (api.telegram.org) tới nó.
func newTelegram(t *testing.T) (*fakeTelegram, *http.Client) {
	t.Helper()
	f := &fakeTelegram{status: http.StatusOK, body: `{"ok":true,"result":{}}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ChatID string `json:"chat_id"`
			Text   string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		f.mu.Lock()
		f.reqs = append(f.reqs, tgReq{path: r.URL.Path, chatID: m.ChatID, text: m.Text})
		status, body := f.status, f.body
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return f, &http.Client{Transport: redirect{base: base}}
}

type redirect struct{ base *url.URL }

func (r redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme, out.URL.Host, out.Host = r.base.Scheme, r.base.Host, ""
	return http.DefaultTransport.RoundTrip(out) //nolint:wrapcheck // test
}

// failTransport làm test đỏ khi có yêu cầu mạng nào.
type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.t.Errorf("không được gọi mạng: %s", req.URL.Host)
	return nil, http.ErrUseLastResponse
}
