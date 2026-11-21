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
	// Hai phút sau, chưa báo được (Telegram lỗi, lịch trễ): vẫn là lỗi mới, không mất cảnh báo.
	later, err := observability.ErrorRules(t.Context(), d, time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(keys(later), want) {
		t.Fatalf("lỗi chưa báo phải còn là lỗi mới sau 2 phút: %v", keys(later))
	}
	// Đã báo (có alert_events của key): không còn mới.
	exec(t, d, "INSERT INTO platform.alert_events (key, severity, text) VALUES ($1, 'normal', 'thử')", want)
	done, err := observability.ErrorRules(t.Context(), d, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(keys(done), want) {
		t.Fatalf("lỗi đã báo không còn là lỗi mới: %v", keys(done))
	}
	// Gặp lần đầu quá 1 giờ: không báo nữa dù chưa từng báo.
	if late, err := observability.ErrorRules(t.Context(), d, time.Now().Add(61*time.Minute)); err != nil ||
		len(late) != 0 {
		t.Fatalf("quá 1 giờ: %v %v", keys(late), err)
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

// Tóm tắt đọc bảng của Task 19: sao lưu base thành công gần nhất (lần hỏng sau đó không tính), lần diễn tập gần nhất
// kèm ok (kể cả khi hỏng), cảnh báo 24 giờ mới nhất trước, không có tin tổng kết.
func TestSummaryBackupAndAlerts(t *testing.T) {
	app, owner := openOwner(t)
	now := time.Now().UTC()

	empty, err := observability.Summary(t.Context(), app, now)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Backup.LastBaseAgeH != nil || empty.Backup.LastDrillAgeH != nil || empty.Backup.LastDrillOk != nil ||
		len(empty.Alerts) != 0 || empty.Alerts == nil {
		t.Fatalf("database mới: backup %+v, alerts %v; muốn rỗng (alerts là mảng rỗng, không nil)", empty.Backup, empty.Alerts)
	}

	exec(t, owner, `INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES
		('base', true, now() - interval '30 hours 10 minutes'), ('base', false, now() - interval '1 hour'),
		('restore_drill', true, now() - interval '50 hours'), ('restore_drill', false, now() - interval '5 hours 10 minutes')`)
	exec(t, owner, `INSERT INTO platform.alert_events (key, severity, text, sent_at) VALUES
		('dead:k', 'normal', 'dead:k: 1 việc', now() - interval '2 hours'),
		('queue-lag', 'urgent', 'queue-lag', now() - interval '1 hour'),
		('digest:2026-11-18', 'normal', 'tổng kết', now() - interval '30 minutes'),
		('cu', 'normal', 'cũ', now() - interval '25 hours')`)

	s, err := observability.Summary(t.Context(), app, now)
	if err != nil {
		t.Fatal(err)
	}
	b := s.Backup
	if b.LastBaseAgeH == nil || *b.LastBaseAgeH != 30 || b.LastDrillAgeH == nil || *b.LastDrillAgeH != 5 ||
		b.LastDrillOk == nil || *b.LastDrillOk {
		t.Fatalf("backup = base %v, drill %v ok %v; muốn 30, 5, false", deref(b.LastBaseAgeH), deref(b.LastDrillAgeH), b.LastDrillOk)
	}
	if len(s.Alerts) != 2 || s.Alerts[0].Key != "queue-lag" || s.Alerts[0].Severity != "urgent" ||
		s.Alerts[1].Key != "dead:k" || s.Alerts[0].SentAt.Location() != time.UTC {
		t.Fatalf("alerts = %+v; muốn queue-lag rồi dead:k (UTC), không có digest và dòng 25 giờ", s.Alerts)
	}
}

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// Đồng hồ Go lệch ±5 phút so với PostgreSQL: tuổi bản sao lưu và lần diễn tập (mốc ghi bằng now() của PostgreSQL) vẫn
// tính theo đồng hồ PostgreSQL. Mốc x giờ 3 phút bắt đồng hồ Go chậm, x giờ 57 phút bắt đồng hồ Go nhanh (Task 445).
func TestSummaryBackupClockSkew(t *testing.T) {
	for _, skew := range []time.Duration{-5 * time.Minute, 5 * time.Minute} {
		for _, extra := range []string{"3 minutes", "57 minutes"} {
			t.Run(skew.String()+" "+extra, func(t *testing.T) {
				app, owner := openOwner(t)
				exec(t, owner, `INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES
					('base', true, now() - interval '30 hours' - $1::interval),
					('restore_drill', true, now() - interval '5 hours' - $1::interval)`, extra)
				s, err := observability.Summary(t.Context(), app, time.Now().UTC().Add(skew))
				if err != nil {
					t.Fatal(err)
				}
				if b := s.Backup; b.LastBaseAgeH == nil || *b.LastBaseAgeH != 30 || b.LastDrillAgeH == nil || *b.LastDrillAgeH != 5 {
					t.Fatalf("backup = base %v, drill %v; muốn 30, 5", deref(b.LastBaseAgeH), deref(b.LastDrillAgeH))
				}
			})
		}
	}
}
