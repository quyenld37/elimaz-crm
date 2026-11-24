package e2e

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"elimaz.com.vn/crm/internal/testkit"
)

// pipelineBudget: từ lúc gửi webhook tới lúc màn hình nhận báo hiệu (kế hoạch M0 Task 18).
const pipelineBudget = 2000 * time.Millisecond

// Đường ống trọn vẹn: POST /hooks/selftest có chữ ký, 1 ping → ingest lưu → worker tách gói → selftest.Inbound phát
// pg_notify → api đẩy SSE changed selftest.pong đúng eventId dưới 2.000 ms; 10 lần, in thời gian lớn nhất. Các span
// nhận webhook (ingest), tách gói và xử lý sự kiện (worker) và pg_notify có cùng một TraceID.
func TestPipelineUnderTwoSecondsOneTrace(t *testing.T) {
	// Không t.Parallel (Task 484): newStack đặt tracer provider của cả tiến trình (telemetry.Setup).
	s := newStack(t, stackOptions{})
	c := s.openSSE()
	var worst time.Duration
	ids := make([]string, 10)
	for i := range ids {
		ids[i] = uuid.NewString()
		start := time.Now()
		if status := s.postPing(ids[i]); status != http.StatusOK {
			t.Fatalf("lần %d: POST /hooks/selftest = %d", i+1, status)
		}
		got, err := nextPong(c, pipelineBudget-time.Since(start))
		took := time.Since(start)
		if err != nil || got != ids[i] {
			t.Fatalf("lần %d: không nhận selftest.pong %s trong %v (nhận %q, %v)", i+1, ids[i], pipelineBudget, got, err)
		}
		worst = max(worst, took)
	}
	t.Logf("đường ống: thời gian lớn nhất của 10 lần là %v (ngưỡng %v)", worst, pipelineBudget)

	// Dấu vết của lần cuối: span job kết thúc ngay sau commit, có thể muộn hơn khung SSE một chút.
	want := []string{"nhận webhook", "job webhook.split", "job inbound.selftest", "pg_notify"}
	id := lastWebhookTrace(t, s, ids[len(ids)-1])
	var missing []string
	deadline := time.Now().Add(5 * time.Second)
	for {
		missing = missingInTrace(s.spansSnapshot(), id, want)
		if len(missing) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var names []string
	for _, sp := range s.spansSnapshot() {
		if sp.SpanContext().TraceID() == id {
			names = append(names, sp.Name())
		}
	}
	if len(missing) > 0 {
		t.Fatalf("dấu vết %s thiếu %v; có: %v", id, missing, names)
	}
	t.Logf("dấu vết %s có %d span: %v", id, len(names), names)
}

// lastWebhookTrace: TraceID của việc tách gói chứa eventId (trace_context ghi lúc ingest Enqueue trong span nhận
// webhook).
func lastWebhookTrace(t *testing.T, s *stack, eventID string) trace.TraceID {
	t.Helper()
	var tp string
	err := s.owner.Pool.QueryRow(context.Background(), `SELECT j.trace_context->>'traceparent' FROM platform.jobs j
		JOIN platform.webhook_deliveries d ON d.id::text = j.dedupe_key
		WHERE j.kind = 'webhook.split' AND position($1 in convert_from(d.body, 'UTF8')) > 0`, eventID).Scan(&tp)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		t.Fatalf("việc tách gói của %s không có traceparent: %q", eventID, tp)
	}
	id, err := trace.TraceIDFromHex(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// missingInTrace trả các mục của want chưa có span trong dấu vết id: "nhận webhook" là span máy chủ của POST
// /hooks/{source}; "pg_notify" là span SQL có câu pg_notify; còn lại so đúng tên span.
func missingInTrace(spans []sdktrace.ReadOnlySpan, id trace.TraceID, want []string) []string {
	found := map[string]bool{}
	for _, sp := range spans {
		if sp.SpanContext().TraceID() != id {
			continue
		}
		name := sp.Name()
		found[name] = true
		if sp.SpanKind() == trace.SpanKindServer && strings.Contains(name, "/hooks/") {
			found["nhận webhook"] = true
		}
		if strings.Contains(name, "pg_notify") {
			found["pg_notify"] = true
		}
		for _, a := range sp.Attributes() {
			if strings.Contains(a.Value.String(), "pg_notify") {
				found["pg_notify"] = true
			}
		}
	}
	var missing []string
	for _, w := range want {
		if !found[w] {
			missing = append(missing, w)
		}
	}
	return missing
}

// Nối ở tệp chạy thật (việc treo của Task 15): ingest và worker là tiến trình riêng; ping qua ingest → việc
// inbound.selftest done (không dead "no handler"); lệnh command.selftest (CRM_ENV khác production) → done.
func TestBinaryWorkerHandlesSelftest(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	_, ownerURL, appURL := testkit.StartPostgres(t)
	owner := openPool(t, ownerURL, false)
	ctx := t.Context()
	tenant, err := seedSelftestTenant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	_, _, istdout, istderr := startRole(t, bin, append(roleEnv("ingest", appURL, port), "SPOOL_DIR="+t.TempDir()))
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := waitReady(rctx, "http://127.0.0.1:"+strconv.Itoa(port)+"/health/ready"); err != nil {
		t.Fatalf("ingest: %v\n%s\n%s", err, istdout.String(), istderr.String())
	}
	cmd, done, stdout, stderr := startRole(t, bin, roleEnv("worker", appURL, 0))
	if status := postPingTo(t, "http://127.0.0.1:"+strconv.Itoa(port), "bin-1"); status != http.StatusOK {
		t.Fatalf("POST = %d", status)
	}
	if _, err := owner.Pool.Exec(ctx, `INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key, on_lease_expired,
		max_attempts, expires_at) VALUES ($1, 'command.selftest', 'command', 'bin-cmd', 'in_doubt', 8,
		now() + interval '3 minutes')`, tenant); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 15*time.Second, "inbound.selftest và command.selftest done", func() bool {
		return countRows(t, owner, `SELECT count(*) FROM platform.jobs WHERE status = 'done'
			AND kind IN ('inbound.selftest', 'command.selftest')`) == 2
	})
	if n := countRows(t, owner, `SELECT count(*) FROM platform.jobs WHERE status = 'dead'`); n != 0 {
		t.Fatalf("%d việc dead\n%s\n%s", n, stdout.String(), stderr.String())
	}
	stopWorker(t, cmd, done, stdout, stderr)
}
