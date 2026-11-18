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

	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

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
