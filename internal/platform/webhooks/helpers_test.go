package webhooks_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/selftest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

const secret = "0123456789abcdef0123456789abcdef"

// deadURL trỏ tới cổng không có PostgreSQL: mọi transaction lỗi ngay (database "giả luôn lỗi").
const deadURL = "postgres://crm_app:x@127.0.0.1:1/crm?sslmode=disable&connect_timeout=1"

func sign(body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// openDB mở database thử bằng crm_app với giới hạn của vai trò ingest.
func openDB(t *testing.T) *db.DB {
	t.Helper()
	return testkit.OpenAppDB(t, db.PoolOptions{DefaultTx: db.TxIngest})
}

// openDeadDB mở pool tới cổng không có database (pool mở lười nên không lỗi lúc mở).
func openDeadDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), deadURL, db.PoolOptions{DefaultTx: db.TxIngest})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Pool.Close)
	return d
}

type rig struct {
	srv   *httptest.Server
	in    *webhooks.Ingest
	spool *webhooks.Spool
	reg   *metrics.Registry
}

// newRig dựng ingest trên d với nguồn selftest, tệp tạm trong thư mục riêng, đúng các route vai trò ingest phục vụ
// (/hooks/*, /health của ingest và /health/live, /health/ready). mod sửa tùy chọn trước khi dựng.
func newRig(t *testing.T, d *db.DB, mod func(*webhooks.IngestOptions)) *rig {
	t.Helper()
	reg := metrics.NewRegistry()
	spool := &webhooks.Spool{Dir: t.TempDir()}
	o := webhooks.IngestOptions{
		DB:      d,
		Sources: []webhooks.Source{selftest.NewSource(secret)},
		Spool:   spool,
		Lockout: webhooks.NewLockout(webhooks.LockoutOptions{}),
		Metrics: reg,
		Logger:  slog.New(slog.DiscardHandler),
	}
	if mod != nil {
		mod(&o)
	}
	in, err := webhooks.NewIngest(o)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	observability.NewHealth(d.Pool).Register(mux)
	in.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &rig{srv: srv, in: in, spool: o.Spool, reg: reg}
}

type reply struct {
	status int
	body   string
	ctype  string
}

// post gửi POST /hooks/<source> với thân body từ địa chỉ ip (header cf-connecting-ip); sig rỗng thì ký đúng.
func (r *rig) post(t *testing.T, source string, body []byte, ip, sig string, extra http.Header) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, r.srv.URL+"/hooks/"+source, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range extra {
		req.Header[k] = v
	}
	if sig == "" {
		sig = sign(body)
	}
	req.Header.Set("X-Selftest-Signature", sig)
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("Cf-Connecting-Ip", ip)
	}
	return do(t, req)
}

func (r *rig) get(t *testing.T, path string) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, r.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return do(t, req)
}

func do(t *testing.T, req *http.Request) reply {
	t.Helper()
	status, h, b := testkit.Do(t, req)
	return reply{status: status, body: string(b), ctype: h.Get("Content-Type")}
}

// readOnlyDir trả một thư mục tạm chỉ đọc (trả lại quyền khi test xong để TempDir xóa được).
func readOnlyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // thư mục cần quyền x
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // thư mục cần quyền x
	return dir
}

// counts trả số dòng webhook_deliveries và số việc webhook.split.
func counts(t *testing.T, d *db.DB) (deliveries, jobs int) {
	t.Helper()
	ctx := context.WithoutCancel(t.Context())
	if err := d.Pool.QueryRow(ctx, "SELECT count(*) FROM platform.webhook_deliveries").Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if err := d.Pool.QueryRow(ctx, "SELECT count(*) FROM platform.jobs WHERE kind = 'webhook.split'").Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	return deliveries, jobs
}

// counter trả tổng của counter name (mọi nhãn khi label rỗng) trong rows.
func counter(rows []metrics.Row, name, label string) int64 {
	var n int64
	for _, r := range rows {
		if r.Name == name && r.Kind == metrics.KindCounter && (label == "" || r.Label == label) {
			n += r.Count
		}
	}
	return n
}

func newSelftest() *selftest.Source { return selftest.NewSource(secret) }

func dbTxOptions() db.TxOptions { return db.TxOptions{} }
