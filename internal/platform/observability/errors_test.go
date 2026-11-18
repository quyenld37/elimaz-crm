package observability_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/testkit"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	return testkit.OpenAppDB(t, db.PoolOptions{DefaultTx: db.TxWorker})
}

func TestScrubMessageRemovesPhoneAndEmail(t *testing.T) {
	got := observability.ScrubMessage("gọi 0912345678 lỗi, mail a@b.vn")
	if strings.Contains(got, "0912345678") || strings.Contains(got, "a@b.vn") {
		t.Fatalf("ScrubMessage còn dữ liệu cá nhân: %q", got)
	}
	if !strings.Contains(got, "gọi") || !strings.Contains(got, "lỗi") {
		t.Fatalf("ScrubMessage xóa mất phần không nhạy cảm: %q", got)
	}
	if short := observability.ScrubMessage("mã 1234567 còn"); !strings.Contains(short, "1234567") {
		t.Fatalf("chuỗi 7 chữ số không phải SĐT, phải giữ: %q", short)
	}
}

var (
	longDigits = regexp.MustCompile(`[0-9]{8,}`)
	emailLike  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
)

// Quy luật: sau khi lọc không còn chuỗi từ 8 chữ số, không còn email, tối đa 300 ký tự, UTF-8 hợp lệ.
func TestScrubMessageProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		parts := rapid.SliceOf(rapid.OneOf(
			rapid.StringMatching(`[0-9]{1,15}`),
			rapid.StringMatching(`[a-z0-9._]{1,8}@[a-z0-9]{1,8}\.(vn|com)`),
			rapid.String(),
			rapid.Just(" "),
		)).Draw(t, "parts")
		got := observability.ScrubMessage(strings.Join(parts, ""))
		if longDigits.MatchString(got) {
			t.Fatalf("còn chuỗi ≥ 8 chữ số: %q", got)
		}
		if emailLike.MatchString(got) {
			t.Fatalf("còn email: %q", got)
		}
		if n := utf8.RuneCountInString(got); n > 300 {
			t.Fatalf("dài %d ký tự, tối đa 300", n)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("không còn là UTF-8 hợp lệ: %q", got)
		}
	})
}

func frame(fn, file string, line int) runtime.Frame {
	return runtime.Frame{Function: fn, File: file, Line: line}
}

func TestFingerprintIgnoresLineAndDepthBeyondThree(t *testing.T) {
	err := errors.New("x")
	a := []runtime.Frame{frame("p.A", "/x/a.go", 10), frame("p.B", "/x/b.go", 20), frame("p.C", "/x/c.go", 30), frame("p.D", "/x/d.go", 1)}
	b := []runtime.Frame{frame("p.A", "/y/a.go", 11), frame("p.B", "/y/b.go", 99), frame("p.C", "/y/c.go", 31), frame("p.E", "/y/e.go", 2)}
	if observability.Fingerprint(err, a) != observability.Fingerprint(errors.New("khác nội dung"), b) {
		t.Fatal("cùng kiểu lỗi, cùng 3 khung đầu (khác số dòng, thư mục dựng, khung thứ tư) phải cùng dấu vân tay")
	}
	c := []runtime.Frame{frame("p.A", "/x/a.go", 10), frame("p.Z", "/x/b.go", 20), frame("p.C", "/x/c.go", 30)}
	if observability.Fingerprint(err, a) == observability.Fingerprint(err, c) {
		t.Fatal("khác một trong 3 khung đầu phải khác dấu vân tay")
	}
	if observability.Fingerprint(err, a) == observability.Fingerprint(&customErr{}, a) {
		t.Fatal("khác kiểu lỗi phải khác dấu vân tay")
	}
	if fp := observability.Fingerprint(err, a); !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(fp) {
		t.Fatalf("dấu vân tay phải là sha256 hex: %q", fp)
	}
}

type customErr struct{}

func (*customErr) Error() string { return "custom" }

type errorRow struct {
	Count     int64
	Role      string
	Name      string
	Message   string
	Frames    string
	RequestID *string
	JobID     *uuid.UUID
}

func readError(t *testing.T, d *db.DB, fp string) errorRow {
	t.Helper()
	var r errorRow
	if err := d.Pool.QueryRow(context.Background(), `SELECT count, role, name, message, top_frames, sample_request_id,
		sample_job_id FROM platform.error_events WHERE fingerprint = $1`, fp).
		Scan(&r.Count, &r.Role, &r.Name, &r.Message, &r.Frames, &r.RequestID, &r.JobID); err != nil {
		t.Fatalf("đọc error_events %s: %v", fp, err)
	}
	return r
}

func countErrors(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Pool.QueryRow(context.Background(), "SELECT count(*) FROM platform.error_events").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// failAtSiteA và failAtSiteB là hai chỗ gọi khác nhau; RecordError lấy khung gọi tại chỗ gọi.
func failAtSiteA(ctx context.Context, d *db.DB, c observability.ErrorContext) (string, bool) {
	return observability.RecordError(ctx, d, fmt.Errorf("gọi 0912345678: %w", errBoom), c)
}

func failAtSiteB(ctx context.Context, d *db.DB, c observability.ErrorContext) (string, bool) {
	return observability.RecordError(ctx, d, fmt.Errorf("gọi 0912345678: %w", errBoom), c)
}

var errBoom = errors.New("boom")

func TestRecordErrorGroupsBySite(t *testing.T) {
	d := openDB(t)
	reqID := "req-" + uuid.NewString()
	c := observability.ErrorContext{Role: "api", RequestID: reqID}
	var fps []string
	var news []bool
	for range 2 {
		fp, isNew := failAtSiteA(t.Context(), d, c)
		fps, news = append(fps, fp), append(news, isNew)
	}
	if fps[0] != fps[1] {
		t.Fatalf("hai lỗi cùng chỗ phải cùng dấu vân tay: %v", fps)
	}
	if !news[0] || news[1] {
		t.Fatalf("lần đầu isNew = true, lần sau false; được %v", news)
	}
	row := readError(t, d, fps[0])
	if row.Count != 2 || row.Role != "api" {
		t.Fatalf("dòng gộp: %+v, muốn count = 2, role api", row)
	}
	if strings.Contains(row.Message, "0912345678") {
		t.Fatalf("message chưa lọc SĐT: %q", row.Message)
	}
	if row.RequestID == nil || *row.RequestID != reqID {
		t.Fatalf("sample_request_id = %v, muốn %s", row.RequestID, reqID)
	}
	if !strings.Contains(row.Frames, "failAtSiteA") {
		t.Fatalf("top_frames phải bắt đầu ở chỗ gọi RecordError: %q", row.Frames)
	}

	fpB, isNew := failAtSiteB(t.Context(), d, c)
	if fpB == fps[0] || !isNew {
		t.Fatalf("lỗi chỗ khác phải là dòng mới: fp %s, isNew %v", fpB, isNew)
	}
	if n := countErrors(t, d); n != 2 {
		t.Fatalf("error_events có %d dòng, muốn 2", n)
	}
}

func TestRecordErrorConcurrentCountsEveryOccurrence(t *testing.T) {
	d := openDB(t)
	const n = 20
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		fps  = map[string]bool{}
		news int
	)
	for range n {
		wg.Go(func() {
			fp, isNew := failAtSiteA(context.Background(), d, observability.ErrorContext{Role: "worker"})
			mu.Lock()
			defer mu.Unlock()
			fps[fp] = true
			if isNew {
				news++
			}
		})
	}
	wg.Wait()
	if len(fps) != 1 || news != 1 {
		t.Fatalf("%d dấu vân tay, %d lần isNew; muốn 1 và 1", len(fps), news)
	}
	for fp := range fps {
		if row := readError(t, d, fp); row.Count != n {
			t.Fatalf("count = %d, muốn %d", row.Count, n)
		}
	}
}

func TestRecordErrorJobSampleAndMetrics(t *testing.T) {
	d := openDB(t)
	reg := metrics.NewRegistry()
	jobID := uuid.New()
	fp, _ := observability.RecordError(t.Context(), d, errBoom, observability.ErrorContext{Role: "worker", JobID: jobID, Metrics: reg})
	row := readError(t, d, fp)
	if row.JobID == nil || *row.JobID != jobID || row.RequestID != nil {
		t.Fatalf("mẫu: job %v, request %v; muốn job %s, không có request", row.JobID, row.RequestID, jobID)
	}
	var got int64
	for _, r := range reg.Drain() {
		if r.Name == "errors" && r.Label == "worker" {
			got += r.Count
		}
	}
	if got != 1 {
		t.Fatalf("counter errors{worker} = %d, muốn 1", got)
	}
}

type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // test
}

func (b *lockedBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRecordErrorDatabaseDownOnlyLogs(t *testing.T) {
	d := openDB(t)
	d.Pool.Close()
	var buf lockedBuf
	reg := metrics.NewRegistry()
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	done := make(chan string, 1)
	go func() {
		fp, _ := observability.RecordError(context.Background(), d, fmt.Errorf("gọi 0912345678: %w", errBoom),
			observability.ErrorContext{Role: "api", Logger: logger, Metrics: reg})
		done <- fp
	}()
	select {
	case fp := <-done:
		if fp == "" {
			t.Fatal("database lỗi vẫn phải trả dấu vân tay")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RecordError treo khi database lỗi")
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 || lines[0] == "" {
		t.Fatalf("muốn đúng một dòng log, được %d: %q", len(lines), buf.String())
	}
	if strings.Contains(lines[0], "0912345678") {
		t.Fatalf("log có nội dung lỗi gốc: %s", lines[0])
	}
	if len(reg.Drain()) == 0 {
		t.Fatal("counter errors phải cộng cả khi không ghi được database")
	}
}

func TestPanicErrorUsesPanicSiteFrames(t *testing.T) {
	d := openDB(t)
	var pe error
	func() {
		defer func() {
			if v := recover(); v != nil {
				pe = observability.NewPanicError(v)
			}
		}()
		panicHere()
	}()
	fp, _ := observability.RecordError(t.Context(), d, pe, observability.ErrorContext{Role: "api"})
	row := readError(t, d, fp)
	if !strings.HasPrefix(row.Frames, "elimaz.com.vn/crm/internal/platform/observability_test.panicHere") {
		t.Fatalf("khung đầu phải là chỗ panic, được %q", row.Frames)
	}
	if row.Name != "panic(string)" || strings.Contains(row.Message, "0912345678") || !strings.Contains(row.Message, "boom") {
		t.Fatalf("name %q, message %q", row.Name, row.Message)
	}
}

func panicHere() { panic("boom 0912345678") }
