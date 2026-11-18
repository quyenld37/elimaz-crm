package webhooks_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
)

type errorRow struct {
	Fingerprint, Role, Name, Message string
	Count                            int64
}

func errorRows(t *testing.T, d *db.DB) []errorRow {
	t.Helper()
	rows, err := d.Pool.Query(context.Background(),
		"SELECT fingerprint, role, name, message, count FROM platform.error_events ORDER BY first_seen_at")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []errorRow
	for rows.Next() {
		var r errorRow
		if err := rows.Scan(&r.Fingerprint, &r.Role, &r.Name, &r.Message, &r.Count); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// loggedRig dựng ingest trên database thử, log JSON ghi vào bộ đệm để quét.
func loggedRig(t *testing.T) (*db.DB, *rig, *testkit.SyncBuffer) {
	t.Helper()
	d := openDB(t)
	logs := &testkit.SyncBuffer{}
	r := newRig(t, d, func(o *webhooks.IngestOptions) { o.Logger = slog.New(slog.NewJSONHandler(logs, nil)) })
	return d, r, logs
}

func ingestErrors(rows []errorRow) int64 {
	var n int64
	for _, r := range rows {
		n += r.Count
	}
	return n
}

// Database trả lỗi (PgError: database còn chạy) khi lưu webhook: bản nhận vẫn vào tệp tạm (200), lỗi được ghi vào
// error_events với role ingest, gộp theo dấu vân tay như api, worker (hai lần cùng chỗ → một dòng count 2); log, dòng
// lỗi và span không chứa SĐT, địa chỉ, token (Task 319).
func TestIngestDatabaseErrorRecordedAndScrubbed(t *testing.T) {
	exp := testkit.CaptureSpans(t)

	d, r, logs := loggedRig(t)
	webhooks.SetSave(r.in, func(context.Context, webhooks.Entry) error {
		return fmt.Errorf("lưu webhook: %w: %w", &pgconn.PgError{Code: "23514", Message: "vi phạm ràng buộc"}, testkit.PersonalDataError())
	})
	for i := range 2 {
		if got := r.post(t, "selftest", pingBody(fmt.Sprint("pg", i)), "", "", nil); got.status != http.StatusOK {
			t.Fatalf("POST = %d %q, muốn 200 (đã ghi tệp tạm)", got.status, got.body)
		}
	}
	rows := errorRows(t, d)
	if len(rows) != 1 || rows[0].Role != "ingest" || rows[0].Count != 2 || len(rows[0].Fingerprint) != 64 {
		t.Fatalf("error_events = %+v, muốn một dòng role ingest count 2", rows)
	}
	if rows[0].Name != "*fmt.wrapErrors" {
		t.Fatalf("name = %q, muốn kiểu lỗi trong cùng như api, worker", rows[0].Name)
	}
	if counter(r.reg.Drain(), "errors", "ingest") != 2 {
		t.Fatal("counter errors{ingest} phải là 2")
	}
	testkit.AssertNoPersonalData(t, "error_events.message", rows[0].Message)
	testkit.AssertNoPersonalData(t, "log ingest", logs.String())
	if !strings.Contains(logs.String(), "SQLSTATE 23514") {
		t.Fatalf("log mất phần không nhạy cảm: %s", logs.String())
	}
	testkit.AssertNoPersonalData(t, "span", testkit.SpanText(exp.GetSpans().Snapshots()))
}

// Database không trả lời (ngừng, quá hạn): ingest ghi tệp tạm, không ghi error_events (không chờ thêm 2 s mỗi webhook
// đúng lúc database ngừng; spec 04: việc đó do bộ kiểm từ ngoài và nhịp sống báo).
// Gồm cả PgError của database quá tải hay đang tắt (quá hạn câu lệnh 57014, đang tắt 57P01, quá nhiều kết nối
// 53300, mất kết nối 08006) và lỗi lúc nối (*pgconn.ConnectError bọc PgError).
func TestIngestDatabaseDownNotRecorded(t *testing.T) {
	d := openDB(t)
	r := newRig(t, d, nil)
	cases := map[string]error{
		"quá hạn ctx":       context.DeadlineExceeded,
		"statement_timeout": &pgconn.PgError{Code: "57014"},
		"đang tắt":          &pgconn.PgError{Code: "57P01"},
		"quá nhiều kết nối": &pgconn.PgError{Code: "53300"},
		"mất kết nối":       &pgconn.PgError{Code: "08006"},
		"lỗi lúc nối":       connectError(t),
	}
	for name, cause := range cases {
		webhooks.SetSave(r.in, func(context.Context, webhooks.Entry) error { return fmt.Errorf("lưu webhook: %w", cause) })
		if got := r.post(t, "selftest", pingBody("down "+name), "", "", nil); got.status != http.StatusOK {
			t.Fatalf("%s: POST = %d, muốn 200", name, got.status)
		}
		if rows := errorRows(t, d); len(rows) != 0 {
			t.Fatalf("%s: error_events = %+v, muốn rỗng", name, rows)
		}
	}
}

// connectError trả *pgconn.ConnectError thật (nối tới cổng không có PostgreSQL).
func connectError(t *testing.T) error {
	t.Helper()
	_, err := pgconn.Connect(t.Context(), deadURL)
	if _, ok := errors.AsType[*pgconn.ConnectError](err); !ok {
		t.Fatalf("muốn *pgconn.ConnectError, được %T %v", err, err)
	}
	return err //nolint:wrapcheck // test cần đúng lỗi của pgconn
}

// Panic khi xử lý webhook: 500 {"error":"internal"} không lộ nội dung; error_events có dòng panic role ingest; log và
// span không chứa dữ liệu cá nhân; tiến trình phục vụ tiếp.
func TestIngestPanicRecordedAndScrubbed(t *testing.T) {
	exp := testkit.CaptureSpans(t)

	d, r, logs := loggedRig(t)
	webhooks.SetSave(r.in, func(context.Context, webhooks.Entry) error { panic(testkit.PersonalDataError()) })
	got := r.post(t, "selftest", pingBody("panic"), "", "", nil)
	if got.status != http.StatusInternalServerError || !strings.Contains(got.body, `"error":"internal"`) {
		t.Fatalf("POST = %d %q, muốn 500 internal", got.status, got.body)
	}
	testkit.AssertNoPersonalData(t, "câu trả lời", got.body)
	rows := errorRows(t, d)
	if len(rows) != 1 || rows[0].Role != "ingest" || !strings.HasPrefix(rows[0].Name, "panic(") {
		t.Fatalf("error_events = %+v, muốn một dòng panic role ingest", rows)
	}
	if ingestErrors(rows) != 1 {
		t.Fatalf("count = %d", ingestErrors(rows))
	}
	testkit.AssertNoPersonalData(t, "error_events.message", rows[0].Message)
	testkit.AssertNoPersonalData(t, "log ingest", logs.String())
	testkit.AssertNoPersonalData(t, "span", testkit.SpanText(exp.GetSpans().Snapshots()))

	webhooks.SetSave(r.in, func(context.Context, webhooks.Entry) error { return nil })
	if got := r.post(t, "selftest", pingBody("sau"), "", "", nil); got.status != http.StatusOK {
		t.Fatalf("sau panic POST = %d, muốn 200", got.status)
	}
}

// Tệp tạm cũng lỗi (database và đĩa đều hỏng): 503, lỗi ghi tệp tạm được ghi vào error_events (database còn chạy thì
// ghi được), log không lộ nội dung lỗi database.
func TestIngestSpoolErrorRecorded(t *testing.T) {
	d := openDB(t)
	dir := readOnlyDir(t)
	var logs testkit.SyncBuffer
	r := newRig(t, d, func(o *webhooks.IngestOptions) {
		o.Spool = &webhooks.Spool{Dir: dir}
		o.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	})
	webhooks.SetSave(r.in, func(context.Context, webhooks.Entry) error {
		return fmt.Errorf("lưu: %w", errors.Join(context.DeadlineExceeded, testkit.PersonalDataError()))
	})
	if got := r.post(t, "selftest", pingBody("x"), "", "", nil); got.status != http.StatusServiceUnavailable {
		t.Fatalf("POST = %d, muốn 503", got.status)
	}
	if rows := errorRows(t, d); len(rows) != 1 || rows[0].Role != "ingest" {
		t.Fatalf("error_events = %+v, muốn một dòng (lỗi tệp tạm) role ingest", rows)
	}
	testkit.AssertNoPersonalData(t, "log ingest", logs.String())
}
