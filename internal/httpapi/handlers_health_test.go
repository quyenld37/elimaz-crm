package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

const testHealthToken = "thu-health-token-du-dai-32-ky-tu-abcdef"

func deepGet(t *testing.T, srv http.Handler, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health/deep", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// beat ghi nhịp sống tuổi age theo đồng hồ Go, như tiến trình thật (metrics.flusher ghi time.Now()) và như
// DeepHealth đọc (so với time.Now()). Không dùng now() của PostgreSQL: đồng hồ của VM Docker trên máy dev có thể lệch
// đồng hồ máy hàng phút (Task 440: workerHeartbeatAgeS = 929 thay 200).
func beat(t *testing.T, d Deps, process, role string, age time.Duration) {
	t.Helper()
	if _, err := d.DB.Pool.Exec(t.Context(), `INSERT INTO platform.process_heartbeats (process, role, at)
		VALUES ($1, $2, $3) ON CONFLICT (process) DO UPDATE SET at = EXCLUDED.at`,
		process, role, time.Now().Add(-age).UTC()); err != nil {
		t.Fatal(err)
	}
}

// /health/deep: thiếu hay sai thẻ → 401; worker 200 s tuổi → 503 ok:false; mọi thứ tốt → 200 ok:true.
func TestHealthDeep(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	d := testDeps(t, &buf)
	d.Config.HealthToken = testHealthToken
	srv := NewServer(d)

	for _, auth := range []string{"", "Bearer sai", "Basic " + testHealthToken, testHealthToken} {
		rec := deepGet(t, srv, auth)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("thẻ %q: mong 401, có %d", auth, rec.Code)
		}
		if b := decodeErr(t, rec); b.Error != codeUnauthenticated {
			t.Fatalf("mã lỗi %q", b.Error)
		}
	}

	beat(t, d, "worker@a", "worker", 200*time.Second)
	beat(t, d, "ingest@a", "ingest", 5*time.Second)
	rec := deepGet(t, srv, "Bearer "+testHealthToken)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || body["ok"] != false {
		t.Fatalf("worker 200 s: mong 503 ok:false, có %d %s", rec.Code, rec.Body)
	}
	if age, _ := body["workerHeartbeatAgeS"].(float64); age < 199 || age > 230 {
		t.Fatalf("workerHeartbeatAgeS = %v", body["workerHeartbeatAgeS"])
	}

	beat(t, d, "worker@a", "worker", 5*time.Second)
	rec = deepGet(t, srv, "Bearer "+testHealthToken)
	body = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || body["ok"] != true {
		t.Fatalf("mọi thứ tốt: mong 200 ok:true, có %d %s", rec.Code, rec.Body)
	}
	for _, k := range []string{"workerHeartbeatAgeS", "ingestHeartbeatAgeS", "oldestPendingAgeS", "dead", "inDoubt", "spoolFiles"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("thiếu khóa %s: %s", k, rec.Body)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q", got)
	}
}

type errorEventRow struct {
	Count     int64
	Role      string
	Name      string
	Message   string
	RequestID *string
}

func readErrorEvents(t *testing.T, staff *testkit.Staff) []errorEventRow {
	t.Helper()
	rows, err := staff.Owner.Pool.Query(context.Background(),
		"SELECT count, role, name, message, sample_request_id FROM platform.error_events ORDER BY first_seen_at")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []errorEventRow
	for rows.Next() {
		var r errorEventRow
		if err := rows.Scan(&r.Count, &r.Role, &r.Name, &r.Message, &r.RequestID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func errDeps(staff *testkit.Staff, logs *bytes.Buffer) Deps {
	return Deps{
		Config: config.Config{Role: config.RoleAPI}, DB: staff.App,
		Logger: slog.New(slog.NewJSONHandler(&lockedBuffer{buf: logs}, nil)),
	}
}

// Route thử panic với SĐT trong giá trị: 500 không có vết lỗi, error_events có dòng đã lọc kèm đúng mã yêu cầu.
func TestPanicRecordsScrubbedError(t *testing.T) {
	t.Parallel()
	staff := testkit.OpenStaff(t)
	logs := &bytes.Buffer{}
	rt := Route{Method: "GET", Pattern: "GET /test/panic", OperationID: "testPanic", Public: true}
	h := chain(errDeps(staff, logs), rt, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom 0912345678")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("mã %d, muốn 500", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body) != 2 || body["error"] != codeInternal {
		t.Fatalf("thân lỗi %q, muốn đúng {error: internal, request_id}", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "boom") || strings.Contains(rec.Body.String(), "0912345678") {
		t.Fatalf("câu trả lời lộ nội dung lỗi: %s", rec.Body.String())
	}
	if strings.Contains(logs.String(), "0912345678") {
		t.Fatalf("log có giá trị panic: %s", logs.String())
	}
	rows := readErrorEvents(t, staff)
	if len(rows) != 1 {
		t.Fatalf("error_events có %d dòng, muốn 1", len(rows))
	}
	r := rows[0]
	if strings.Contains(r.Message, "0912345678") || !strings.Contains(r.Message, "boom") || r.Role != "api" {
		t.Fatalf("dòng lỗi: %+v", r)
	}
	if want := rec.Header().Get("X-Request-Id"); r.RequestID == nil || *r.RequestID != want || want == "" {
		t.Fatalf("sample_request_id = %v, muốn %q", r.RequestID, want)
	}
}

// Lỗi không nhận ra (500) được ghi; lỗi đã biết (404) và client tự đóng kết nối thì không.
func TestHandlerErrorRecordsOnlyInternal(t *testing.T) {
	t.Parallel()
	staff := testkit.OpenStaff(t)
	d := errDeps(staff, &bytes.Buffer{})
	serve := func(ctx context.Context, err error) int {
		rec := httptest.NewRecorder()
		writeHandlerError(d, rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/x", nil), err)
		return rec.Code
	}
	if code := serve(t.Context(), fmt.Errorf("đọc: %w", errors.New("hỏng"))); code != http.StatusInternalServerError {
		t.Fatalf("mã %d", code)
	}
	serve(t.Context(), fmt.Errorf("tìm: %w", queue.ErrJobNotFound))
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	serve(cancelled, errors.New("client đã đi"))
	if rows := readErrorEvents(t, staff); len(rows) != 1 || rows[0].Count != 1 {
		t.Fatalf("error_events: %+v, muốn đúng một dòng count 1", rows)
	}
}

// /api/v1/platform/health: không quyền 403; có quyền đủ sáu khóa, errors tối đa 10 dòng kèm firstSeenAt, message.
func TestPlatformHealth(t *testing.T) {
	t.Parallel()
	e := newJobsEnv(t, 0)
	for i := range 12 {
		if _, err := e.staff.Owner.Pool.Exec(context.Background(), `INSERT INTO platform.error_events (fingerprint, role,
			name, message, top_frames, count) VALUES ($1, 'api', 'n', 'm', 'f', $2)`, fmt.Sprintf("%064x", i), i+1); err != nil {
			t.Fatal(err)
		}
	}
	if rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/platform/health", jobsManager, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("không quyền: mã %d, muốn 403", rec.Code)
	}
	rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/platform/health", jobsViewer, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("có quyền: mã %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"processes", "queue", "ingest", "backup", "errors", "alerts"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("thiếu khóa %s: %s", k, rec.Body.String())
		}
	}
	var errs []map[string]any
	if err := json.Unmarshal(got["errors"], &errs); err != nil {
		t.Fatal(err)
	}
	if len(errs) != 10 {
		t.Fatalf("errors có %d dòng, muốn 10", len(errs))
	}
	for _, k := range []string{"fingerprint", "name", "role", "message", "count", "firstSeenAt", "lastSeenAt"} {
		if _, ok := errs[0][k]; !ok {
			t.Fatalf("dòng lỗi thiếu %s: %v", k, errs[0])
		}
	}
}

// /health/deep mang lastFileSyncAgeM (Task M1-45): vắng khi chưa có lần chép; có (phút) khi cả ba bucket đã chép tốt.
func TestHealthDeepFileSyncAge(t *testing.T) {
	t.Parallel()
	adminURL, _, appURL := testkit.StartPostgres(t)
	var buf bytes.Buffer
	d := testDeps(t, &buf)
	app, err := db.Open(t.Context(), appURL, db.PoolOptions{MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Pool.Close)
	d.DB = app
	d.Config.HealthToken = testHealthToken
	srv := NewServer(d)
	get := func() map[string]any {
		var body map[string]any
		rec := deepGet(t, srv, "Bearer "+testHealthToken)
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if _, ok := get()["lastFileSyncAgeM"]; ok {
		t.Fatal("chưa có lần chép: không được có lastFileSyncAgeM")
	}
	admin, err := pgx.Connect(t.Context(), adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(context.Background()) }()
	// Mốc theo đồng hồ Go mà DeepHealth so (time.Now()), không theo now() của PostgreSQL (lệch đồng hồ VM Docker trên
	// máy dev: Task 440, 42 phút thay 30).
	if _, err := admin.Exec(t.Context(), `INSERT INTO platform.file_sync_runs (kind, bucket, ok, started_at, finished_at)
		SELECT 'copy', b, true, $1::timestamptz, $2::timestamptz
		FROM unnest(ARRAY['public', 'originals', 'private']) b`,
		time.Now().Add(-32*time.Minute).UTC(), time.Now().Add(-30*time.Minute).UTC()); err != nil {
		t.Fatal(err)
	}
	if age, _ := get()["lastFileSyncAgeM"].(float64); age < 30 || age > 31 {
		t.Fatalf("lastFileSyncAgeM = %v, muốn 30", age)
	}
}
