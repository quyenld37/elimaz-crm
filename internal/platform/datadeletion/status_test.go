package datadeletion_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/datadeletion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

const baseURL = "https://hooks.elimaz.test"

// deadURL trỏ tới cổng không có PostgreSQL: mọi câu lỗi ngay.
const deadURL = "postgres://crm_app:x@127.0.0.1:1/crm?sslmode=disable&connect_timeout=1"

var codeRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

var errBadSig = errors.New("chữ ký sai")

// parse giả: "ok:<mã>" hợp lệ, còn lại sai chữ ký.
func parse(sr string) (string, error) {
	if id, ok := strings.CutPrefix(sr, "ok:"); ok && id != "" {
		return id, nil
	}
	return "", errBadSig
}

type rig struct {
	srv   *httptest.Server
	spool *datadeletion.Spool
}

func newRig(t *testing.T, d *db.DB) *rig {
	t.Helper()
	spool := &datadeletion.Spool{Dir: t.TempDir()}
	ep, err := datadeletion.NewEndpoint(datadeletion.EndpointOptions{
		DB: d, Spool: spool, Source: "facebook", BaseURL: baseURL, Parse: parse, Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ep.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &rig{srv: srv, spool: spool}
}

func (r *rig) post(t *testing.T, signed string) (int, map[string]string) {
	t.Helper()
	res, err := http.PostForm(r.srv.URL+"/hooks/facebook/data-deletion", url.Values{"signed_request": {signed}}) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var body map[string]string
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res.StatusCode, body
}

func (r *rig) status(t *testing.T, code string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, r.srv.URL+"/hooks/facebook/data-deletion/status/"+code, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return res.StatusCode, string(b)
}

func countRequests(t *testing.T, s *testkit.Staff) int {
	t.Helper()
	return s.Count(t, "SELECT count(*) FROM platform.data_deletion_requests")
}

func TestEndpoint_SignedOK(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	r := newRig(t, s.App)
	st, body := r.post(t, "ok:218471")
	if st != http.StatusOK || !codeRe.MatchString(body["confirmation_code"]) {
		t.Fatalf("POST: %d %v", st, body)
	}
	if body["url"] != baseURL+"/hooks/facebook/data-deletion/status/"+body["confirmation_code"] {
		t.Errorf("url = %q", body["url"])
	}
	if n := s.Count(t, "SELECT count(*) FROM platform.data_deletion_requests WHERE code = $1 AND source = 'facebook' AND user_ref_hash = $2 AND completed_at IS NULL",
		body["confirmation_code"], datadeletion.UserRefHash("218471")); n != 1 {
		t.Errorf("số dòng yêu cầu = %d; muốn 1", n)
	}
	if st, text := r.status(t, body["confirmation_code"]); st != http.StatusOK || text != "Yêu cầu "+body["confirmation_code"]+": đang xử lý" {
		t.Errorf("trang trạng thái: %d %q", st, text)
	}
	// Hai yêu cầu: hai mã khác nhau.
	if _, b2 := r.post(t, "ok:218471"); b2["confirmation_code"] == body["confirmation_code"] {
		t.Error("hai yêu cầu cùng mã")
	}
}

func TestEndpoint_BadSignature(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	r := newRig(t, s.App)
	for _, sr := range []string{"sai", ""} {
		if st, body := r.post(t, sr); st != http.StatusBadRequest || body["error"] != "bad_signature" || body["request_id"] == "" {
			t.Errorf("%q: %d %v; muốn 400 bad_signature", sr, st, body)
		}
	}
	if n := countRequests(t, s); n != 0 {
		t.Errorf("số dòng = %d; muốn 0", n)
	}
}

func TestEndpoint_DatabaseDownSpools(t *testing.T) {
	t.Parallel()
	dead, err := db.Open(t.Context(), deadURL, db.PoolOptions{DefaultTx: db.TxIngest})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dead.Pool.Close)
	r := newRig(t, dead)
	st, body := r.post(t, "ok:218471")
	if st != http.StatusOK || !codeRe.MatchString(body["confirmation_code"]) {
		t.Fatalf("database lỗi: %d %v; muốn 200 kèm mã", st, body)
	}
	files, _ := os.ReadDir(r.spool.Dir)
	if len(files) != 1 {
		t.Fatalf("số tệp tạm = %d; muốn 1", len(files))
	}
	raw, _ := os.ReadFile(r.spool.Dir + "/" + files[0].Name())
	if strings.Contains(string(raw), "218471") {
		t.Error("tệp tạm chứa mã người dùng gốc")
	}
	// Database trở lại: nạp tệp, xóa tệp; nạp lại lần hai không thêm dòng.
	s := testkit.OpenStaff(t)
	if n, err := r.spool.Drain(t.Context(), s.App); err != nil || n != 1 {
		t.Fatalf("Drain = %d, %v", n, err)
	}
	if n := s.Count(t, "SELECT count(*) FROM platform.data_deletion_requests WHERE code = $1 AND user_ref_hash = $2",
		body["confirmation_code"], datadeletion.UserRefHash("218471")); n != 1 {
		t.Errorf("sau khi nạp: %d dòng; muốn 1", n)
	}
	if files, _ := os.ReadDir(r.spool.Dir); len(files) != 0 {
		t.Errorf("còn %d tệp sau khi nạp", len(files))
	}
}

func TestEndpoint_StatusUnknown(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	r := newRig(t, s.App)
	for _, code := range []string{"0123456789abcdef0123456789abcdef", "khong-phai-ma", strings.Repeat("A", 32)} {
		if st, _ := r.status(t, code); st != http.StatusNotFound {
			t.Errorf("%s: %d; muốn 404", code, st)
		}
	}
}

// tenantProbe là Deleter ghi lại tenant đang đặt trong tx mỗi lần được gọi.
type tenantProbe struct {
	mu   sync.Mutex
	seen []string
}

func (p *tenantProbe) deleter(ctx context.Context, tx pgx.Tx, hash string) (int64, error) {
	var tenant string
	if err := tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&tenant); err != nil {
		return 0, err //nolint:wrapcheck // test
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, tenant+"|"+hash)
	return 1, nil
}

func TestProcessor_AllTenantsThenCompleted(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "Shop A"), s.Tenant(t, "Shop B")
	r := newRig(t, s.App)
	_, body := r.post(t, "ok:218471")
	code := body["confirmation_code"]
	probe := &tenantProbe{}
	p := datadeletion.NewProcessor(probe.deleter)
	if p.Kind().Name != "task."+datadeletion.ScheduleName || p.Schedule().Every != datadeletion.Every || p.Schedule().PerTenant {
		t.Errorf("Kind/Schedule = %+v %+v", p.Kind(), p.Schedule())
	}
	var after string
	if err := s.App.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := p.Handle(t.Context(), queue.Job{ID: uuid.New()}, tx); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(t.Context(), "SELECT coalesce(current_setting('app.tenant_id', true), '')").Scan(&after)
	}); err != nil {
		t.Fatal(err)
	}
	h := datadeletion.UserRefHash("218471")
	want := []string{a.String() + "|" + h, b.String() + "|" + h}
	slices.Sort(want)
	got := slices.Sorted(slices.Values(probe.seen))
	if !slices.Equal(got, want) {
		t.Errorf("Deleter được gọi với %v; muốn %v", got, want)
	}
	if after != "" {
		t.Errorf("app.tenant_id sau Handle = %q; muốn rỗng", after)
	}
	if n := s.Count(t, "SELECT count(*) FROM platform.data_deletion_requests WHERE code = $1 AND completed_at IS NOT NULL", code); n != 1 {
		t.Errorf("completed_at chưa có")
	}
	if st, text := r.status(t, code); st != http.StatusOK || !strings.HasPrefix(text, "Yêu cầu "+code+": đã xử lý lúc ") {
		t.Errorf("trang trạng thái: %d %q", st, text)
	}
	// Chạy lại: không còn yêu cầu chưa xong, không gọi Deleter.
	probe.seen = nil
	if err := s.App.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		_, err := p.Handle(t.Context(), queue.Job{ID: uuid.New()}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil || len(probe.seen) != 0 {
		t.Errorf("chạy lại: %v, gọi %v", err, probe.seen)
	}
}

func TestProcessor_DeleterErrorRollsBack(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	s.Tenant(t, "Shop A")
	r := newRig(t, s.App)
	r.post(t, "ok:1")
	boom := errors.New("lỗi xóa")
	p := datadeletion.NewProcessor(func(context.Context, pgx.Tx, string) (int64, error) { return 0, boom })
	err := s.App.RetryTx(t.Context(), db.TxOptions{}, func(tx pgx.Tx) error {
		_, err := p.Handle(t.Context(), queue.Job{ID: uuid.New()}, tx)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Handle = %v; muốn bọc lỗi của Deleter", err)
	}
	if n := s.Count(t, "SELECT count(*) FROM platform.data_deletion_requests WHERE completed_at IS NULL"); n != 1 {
		t.Errorf("yêu cầu phải còn chưa xong, có %d", n)
	}
}

func TestUserRefHash(t *testing.T) {
	t.Parallel()
	// sha256("218471") hex.
	if got := datadeletion.UserRefHash("218471"); !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got) || got == datadeletion.UserRefHash("218472") {
		t.Errorf("UserRefHash = %q", got)
	}
}
