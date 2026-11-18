package observability_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/testkit"
)

// openOwner mở database thử và trả pool crm_app (như tiến trình) và pool chủ bảng (dựng dữ liệu).
func openOwner(t *testing.T) (app, owner *db.DB) {
	t.Helper()
	_, ownerURL, appURL := testkit.StartPostgres(t)
	for _, p := range []struct {
		dst **db.DB
		url string
	}{{&app, appURL}, {&owner, ownerURL}} {
		d, err := db.Open(t.Context(), p.url, db.PoolOptions{DefaultTx: db.TxWorker})
		if err != nil {
			t.Fatalf("mở pool thử: %v", err)
		}
		t.Cleanup(d.Pool.Close)
		*p.dst = d
	}
	return app, owner
}

func exec(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Pool.Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func keys(hits []observability.RuleHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Key)
	}
	return out
}

func TestErrorRulesNewError(t *testing.T) {
	d := openDB(t)
	fp, _ := failAtSiteA(t.Context(), d, observability.ErrorContext{Role: "api"})
	hits, err := observability.ErrorRules(t.Context(), d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := "new-error:" + fp[:8]
	if !slices.Contains(keys(hits), want) {
		t.Fatalf("luật: %v, muốn có %s", keys(hits), want)
	}
	for _, h := range hits {
		if h.Key == want && h.Severity != "normal" {
			t.Fatalf("new-error mức %q, muốn normal", h.Severity)
		}
		if h.Key == want && (strings.Contains(h.Text, "0912345678") || strings.Contains(h.Text, "boom")) {
			t.Fatalf("văn bản cảnh báo không được chứa nội dung lỗi: %q", h.Text)
		}
	}
	// Hai phút sau: dòng không còn mới.
	later, err := observability.ErrorRules(t.Context(), d, time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(keys(later), want) {
		t.Fatalf("lỗi gặp lần đầu 2 phút trước không còn là lỗi mới: %v", keys(later))
	}
}

func TestErrorRulesSpike(t *testing.T) {
	for _, tc := range []struct {
		name  string
		total int
		want  bool
	}{{"51 lỗi", 51, true}, {"50 lỗi", 50, false}, {"49 lỗi", 49, false}} {
		t.Run(tc.name, func(t *testing.T) {
			app, owner := openOwner(t)
			now := time.Now().UTC()
			minute := now.Truncate(time.Minute)
			// Chia cho hai tiến trình, hai phút trong 5 phút gần nhất; thêm một dòng cũ 10 phút (không tính).
			a := tc.total / 2
			exec(t, owner, `INSERT INTO platform.metrics_minute (bucket, process, name, label, count, sum, max)
				VALUES ($1, 'api@a', 'errors', 'api', $2::bigint, $2::bigint, 0), ($3, 'worker@b', 'errors', 'worker', $4::bigint, $4::bigint, 0),
				       ($5, 'api@a', 'errors', 'api', 1000, 1000, 0)`,
				minute, a, minute.Add(-2*time.Minute), tc.total-a, minute.Add(-10*time.Minute))
			hits, err := observability.ErrorRules(t.Context(), app, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(keys(hits), "error-spike"); got != tc.want {
				t.Fatalf("error-spike = %v với %d lỗi, muốn %v (%v)", got, tc.total, tc.want, keys(hits))
			}
		})
	}
}

func TestSummary(t *testing.T) {
	app, owner := openOwner(t)
	exec(t, owner, `INSERT INTO platform.process_heartbeats (process, role, at) VALUES
		('api@h', 'api', now() - interval '5 seconds'), ('worker@h', 'worker', now() - interval '200 seconds')`)
	tenant := uuid.New()
	exec(t, owner, "INSERT INTO platform.tenants (id, name) VALUES ($1, 'A')", tenant)
	for i, st := range []string{"pending", "pending", "dead", "in_doubt", "done"} {
		exec(t, owner, `INSERT INTO platform.jobs (tenant_id, kind, mode, payload, dedupe_key, on_lease_expired,
			max_attempts, status, run_at) VALUES ($1, 'k', 'transactional', '{"phone":"0911"}', $2, 'retry', 8, $3,
			now() - make_interval(secs => $4))`, tenant, uuid.NewString(), st, 30*(i+1))
	}
	now := time.Now().UTC()
	exec(t, owner, `INSERT INTO platform.metrics_minute (bucket, process, name, label, count, sum, max) VALUES
		($1, 'ingest@a', 'spool_files', '', 1, 3, 3), ($2, 'ingest@a', 'spool_files', '', 1, 9, 9),
		($1, 'ingest@b', 'spool_files', '', 1, 2, 2)`, now.Truncate(time.Minute), now.Truncate(time.Minute).Add(-time.Minute))
	for i := range 12 {
		exec(t, owner, `INSERT INTO platform.error_events (fingerprint, role, name, message, top_frames, count,
			first_seen_at, last_seen_at) VALUES ($1, 'api', 'n', 'm', 'f', $2, now() - interval '1 hour', now())`,
			fmt.Sprintf("%064x", i), i+1)
	}
	exec(t, owner, `INSERT INTO platform.error_events (fingerprint, role, name, message, top_frames, count,
		first_seen_at, last_seen_at) VALUES ($1, 'api', 'cũ', 'm', 'f', 999, now() - interval '3 days',
		now() - interval '25 hours')`, fmt.Sprintf("%064x", 999))

	s, err := observability.Summary(t.Context(), app, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Processes) != 2 || s.Processes[0].Role != "api" || s.Processes[1].LastHeartbeatAgeS < 199 {
		t.Fatalf("processes = %+v", s.Processes)
	}
	if s.Queue.Pending != 2 || s.Queue.Dead != 1 || s.Queue.InDoubt != 1 || s.Queue.OldestPendingAgeS < 59 {
		t.Fatalf("queue = %+v, muốn pending 2, dead 1, in_doubt 1, việc chờ cũ nhất ≥ 60 s", s.Queue)
	}
	if s.Ingest.SpoolFiles != 5 {
		t.Fatalf("spoolFiles = %d, muốn 5 (giá trị cuối của mỗi tiến trình: 3 + 2)", s.Ingest.SpoolFiles)
	}
	if len(s.Errors) != 10 || s.Errors[0].Count != 12 || s.Errors[9].Count != 3 {
		t.Fatalf("errors: %d dòng, đầu %+v; muốn 10 dòng nhiều nhất trong 24 giờ", len(s.Errors), s.Errors)
	}
	for _, e := range s.Errors {
		if e.Name == "cũ" || e.FirstSeenAt.IsZero() || e.Message == "" {
			t.Fatalf("dòng lỗi sai: %+v", e)
		}
	}
}
