package observability_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/observability"
)

// check chạy CheckRules với o (Now mặc định là bây giờ) và trả các key cùng Alerter giả.
func check(t *testing.T, e *env, o observability.RuleOptions) ([]string, *fakeAlerter) {
	t.Helper()
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	f := &fakeAlerter{}
	keys, err := observability.CheckRules(t.Context(), e.d, f, o)
	if err != nil {
		t.Fatalf("CheckRules: %v", err)
	}
	if len(keys) != len(f.sent) {
		t.Fatalf("key trả về %v khác số lần Notify %d", keys, len(f.sent))
	}
	return keys, f
}

func has(keys []string, k string) bool { return slices.Contains(keys, k) }

func wantKey(t *testing.T, keys []string, k string, want bool) {
	t.Helper()
	if has(keys, k) != want {
		t.Fatalf("key %q có = %v, muốn %v; các key: %v", k, !want, want, keys)
	}
}

// fresh ghi nhịp sống mới cho worker và ingest để hai luật nhịp sống không lẫn vào ca khác.
func fresh(t *testing.T, e *env) {
	t.Helper()
	e.heartbeat(t, "worker@a", "worker", time.Now())
	e.heartbeat(t, "ingest@a", "ingest", time.Now())
}

func TestRuleQueueLag(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	e.job(t, "t.q", "pending", `{}`, time.Now().Add(-30*time.Second), nil)
	keys, _ := check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "queue-lag", false)
	e.job(t, "t.q", "pending", `{}`, time.Now().Add(-61*time.Second), nil)
	keys, f := check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "queue-lag", true)
	if s, _ := f.find("queue-lag"); s.severity != "urgent" {
		t.Fatalf("queue-lag mức %q", s.severity)
	}
}

// dead:<kind> chỉ mang tên luật, số, kind, mã việc: không đọc payload.
func TestRuleDeadNoPayload(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	keys, _ := check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "dead:msg.send", false)
	fin := time.Now().Add(-time.Second)
	id := e.job(t, "msg.send", "dead", `{"phone":"0911"}`, time.Now(), &fin)
	e.exec(t, "UPDATE platform.jobs SET last_error = 'gọi 0911 lỗi' WHERE id = $1", id)
	keys, f := check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "dead:msg.send", true)
	s, _ := f.find("dead:msg.send")
	if s.severity != "normal" || strings.Contains(s.text, "0911") || !strings.Contains(s.text, id.String()) {
		t.Fatalf("văn bản cảnh báo: %+v", s)
	}
	// Đã báo sau lúc việc dead: không báo lại việc cũ.
	e.alertSent(t, "dead:msg.send", 0)
	keys, _ = check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "dead:msg.send", false)
}

func TestRuleInDoubt(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	recent := time.Now().Add(-60 * time.Second)
	e.job(t, "cmd.x", "in_doubt", `{}`, time.Now(), &recent)
	keys, _ := check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "in-doubt:cmd.x", false)
	old := time.Now().Add(-121 * time.Second)
	e.job(t, "cmd.x", "in_doubt", `{}`, time.Now(), &old)
	keys, _ = check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "in-doubt:cmd.x", true)
}

// minutes chèn gauge name của process ở n phút liền kết thúc ở phút của now.
func minutes(t *testing.T, e *env, now time.Time, n int, process, name, label string, v float64) {
	t.Helper()
	for i := range n {
		e.metric(t, now.Add(-time.Duration(i)*time.Minute), process, name, label, v)
	}
}

func TestRuleSpool(t *testing.T) {
	now := time.Now()
	e := newEnv(t)
	fresh(t, e)
	minutes(t, e, now, 4, "ingest@a", "spool_files", "", 3)
	keys, _ := check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "spool", false)
	e.metric(t, now.Add(-4*time.Minute), "ingest@a", "spool_files", "", 3)
	keys, _ = check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "spool", true)
	// Phút mới nhất đã về 0: hết báo.
	e2 := newEnv(t)
	fresh(t, e2)
	minutes(t, e2, now.Add(-time.Minute), 5, "ingest@a", "spool_files", "", 3)
	e2.metric(t, now, "ingest@a", "spool_files", "", 0)
	keys, _ = check(t, e2, observability.RuleOptions{Now: now})
	wantKey(t, keys, "spool", false)
}

func TestRuleListenDown(t *testing.T) {
	now := time.Now()
	e := newEnv(t)
	fresh(t, e)
	e.metric(t, now.Add(-time.Minute), "api@a", "rt_listening", "", 1)
	e.metric(t, now, "api@a", "rt_listening", "", 0)
	keys, _ := check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "listen-down", false)
	keys, _ = check(t, e, observability.RuleOptions{Now: now.Add(time.Minute)})
	wantKey(t, keys, "listen-down", false) // phút sau chưa có số: chưa đủ 2 phút liền
	e.metric(t, now.Add(time.Minute), "api@a", "rt_listening", "", 0)
	keys, _ = check(t, e, observability.RuleOptions{Now: now.Add(time.Minute)})
	wantKey(t, keys, "listen-down", true)
	// Tiến trình đã tắt (số cuối cũ hơn 2 phút) không báo.
	keys, _ = check(t, e, observability.RuleOptions{Now: now.Add(10 * time.Minute)})
	wantKey(t, keys, "listen-down", false)
}

func TestRuleHeartbeats(t *testing.T) {
	e := newEnv(t)
	e.heartbeat(t, "worker@cu", "worker", time.Now().Add(-time.Hour)) // container cũ, đã thay
	e.heartbeat(t, "worker@moi", "worker", time.Now().Add(-10*time.Second))
	e.heartbeat(t, "ingest@a", "ingest", time.Now().Add(-10*time.Second))
	keys, _ := check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "worker-heartbeat", false)
	wantKey(t, keys, "ingest-heartbeat", false)
	e.heartbeat(t, "worker@moi", "worker", time.Now().Add(-91*time.Second))
	e.heartbeat(t, "ingest@a", "ingest", time.Now().Add(-91*time.Second))
	keys, _ = check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "worker-heartbeat", true)
	wantKey(t, keys, "ingest-heartbeat", true)
	// Chưa có nhịp sống nào của vai trò: cũng báo.
	e2 := newEnv(t)
	keys, _ = check(t, e2, observability.RuleOptions{})
	wantKey(t, keys, "worker-heartbeat", true)
	wantKey(t, keys, "ingest-heartbeat", true)
}

func TestRuleBackupAge(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('base', true, now() - interval '25 hours')")
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('base', false, now() - interval '1 hour')")
	keys, _ := check(t, e, observability.RuleOptions{BackupExpected: true})
	wantKey(t, keys, "backup-age", false)
	keys, _ = check(t, e, observability.RuleOptions{BackupExpected: true, Now: time.Now().Add(2 * time.Hour)})
	wantKey(t, keys, "backup-age", true)
	keys, _ = check(t, e, observability.RuleOptions{BackupExpected: false, Now: time.Now().Add(2 * time.Hour)})
	wantKey(t, keys, "backup-age", false)
	e2 := newEnv(t)
	keys, _ = check(t, e2, observability.RuleOptions{BackupExpected: true})
	wantKey(t, keys, "backup-age", true)
}

// backup-verify (Task 21, ADR-37 điều 3): lần kiểm bản sao lưu hằng tuần gần nhất hỏng thì báo; lần sau đạt thì thôi.
// Chưa có lần kiểm nào thì không báo (lịch chưa chạy lần đầu); tắt BackupExpected thì không báo.
func TestRuleBackupVerify(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok) VALUES ('base', true)")
	keys, _ := check(t, e, observability.RuleOptions{BackupExpected: true})
	wantKey(t, keys, "backup-verify", false)

	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('restore_verify', true, now() - interval '8 days')")
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok, finished_at) VALUES ('restore_verify', false, now() - interval '1 hour')")
	// Một lần diễn tập (kind khác) đạt sau đó không che lần kiểm hỏng.
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok) VALUES ('restore_drill', true)")
	keys, f := check(t, e, observability.RuleOptions{BackupExpected: true})
	wantKey(t, keys, "backup-verify", true)
	wantKey(t, keys, "backup-age", false)
	for _, n := range f.sent {
		if n.key == "backup-verify" && n.severity != observability.SeverityUrgent {
			t.Fatalf("backup-verify phải là urgent, có %q", n.severity)
		}
	}
	keys, _ = check(t, e, observability.RuleOptions{BackupExpected: false})
	wantKey(t, keys, "backup-verify", false)

	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok) VALUES ('restore_verify', true)")
	keys, _ = check(t, e, observability.RuleOptions{BackupExpected: true})
	wantKey(t, keys, "backup-verify", false)

	// Không còn base tốt nào nhưng lần kiểm cuối hỏng: báo cả hai.
	e2 := newEnv(t)
	fresh(t, e2)
	e2.exec(t, "INSERT INTO platform.backup_runs (kind, ok) VALUES ('restore_verify', false)")
	keys, _ = check(t, e2, observability.RuleOptions{BackupExpected: true})
	wantKey(t, keys, "backup-age", true)
	wantKey(t, keys, "backup-verify", true)
}

func TestRuleDBSize(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	keys, _ := check(t, e, observability.RuleOptions{DBDiskBudgetBytes: 1 << 50})
	wantKey(t, keys, "db-size", false)
	keys, _ = check(t, e, observability.RuleOptions{DBDiskBudgetBytes: 1 << 20})
	wantKey(t, keys, "db-size", true)
	keys, _ = check(t, e, observability.RuleOptions{}) // không đặt ngân sách: không xét
	wantKey(t, keys, "db-size", false)
}

func TestRuleDisk(t *testing.T) {
	now := time.Now()
	e := newEnv(t)
	fresh(t, e)
	minutes(t, e, now, 2, "api@a", "disk_used_pct", "root", 79)
	minutes(t, e, now, 1, "worker@a", "disk_used_pct", "root", 95)
	keys, _ := check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "disk:api@a", false)
	wantKey(t, keys, "disk:worker@a", false)
	minutes(t, e, now, 2, "ingest@a", "disk_used_pct", "spool", 81)
	keys, f := check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "disk:ingest@a", true)
	if s, _ := f.find("disk:ingest@a"); s.severity != "urgent" {
		t.Fatalf("disk mức %q", s.severity)
	}
}

func TestRuleHooksAttack(t *testing.T) {
	now := time.Now()
	e := newEnv(t)
	fresh(t, e)
	e.metric(t, now.Add(-3*time.Minute), "ingest@a", "hooks_blocked", "", 5)
	keys, _ := check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "hooks-attack", false)
	e.metric(t, now.Add(-time.Minute), "ingest@a", "hooks_blocked", "", 2)
	keys, _ = check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "hooks-attack", true)
}

func TestRuleHeartbeatPing(t *testing.T) {
	now := time.Now()
	e := newEnv(t)
	fresh(t, e)
	minutes(t, e, now, 4, "worker@a", "heartbeat_ping_failed", "", 1)
	keys, _ := check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "heartbeat-ping", false)
	e.metric(t, now.Add(-4*time.Minute), "worker@a", "heartbeat_ping_failed", "", 1)
	keys, _ = check(t, e, observability.RuleOptions{Now: now})
	wantKey(t, keys, "heartbeat-ping", true)
}

// Database mới, mọi thứ tốt: không key nào.
func TestRulesQuiet(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	e.exec(t, "INSERT INTO platform.backup_runs (kind, ok) VALUES ('base', true)")
	keys, _ := check(t, e, observability.RuleOptions{BackupExpected: true, DBDiskBudgetBytes: 1 << 50, ConnBudget: 1000})
	if len(keys) != 0 {
		t.Fatalf("muốn không key nào, có %v", keys)
	}
}

// Việc in_doubt kết thúc trước lần báo trước nhưng đủ 120 s sau lần đó vẫn được báo (mốc là lúc việc đủ tuổi).
func TestRuleInDoubtAfterEarlierAlert(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	fin := time.Now().Add(-150 * time.Second) // đủ 120 s lúc now - 30 s
	e.job(t, "cmd.y", "in_doubt", `{}`, time.Now(), &fin)
	e.alertSent(t, "in-doubt:cmd.y", 60*time.Second) // lần báo trước (việc khác) lúc now - 60 s
	keys, _ := check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "in-doubt:cmd.y", true)
}

// Việc dead kết thúc sau lúc đọc của lượt trước (trong lúc lượt đó đang gửi) vẫn được báo ở lượt sau: mốc của
// alert_events là lúc đọc, không phải lúc ghi.
func TestRuleDeadDuringSendNotLost(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	tg, client := newTelegram(t)
	a := observability.NewTelegramAlerter(e.d, tgConfig(), client, quiet())
	now := time.Now()
	early := now.Add(-time.Minute)
	e.job(t, "msg.z", "dead", `{}`, now, &early)
	late := now.Add(-2 * time.Second)
	e.job(t, "msg.z", "dead", `{}`, now, &late)
	// Lượt 1 đọc lúc now - 5 s: chỉ thấy việc sớm; việc muộn kết thúc sau lúc đọc.
	keys, err := observability.CheckRules(t.Context(), e.d, a, observability.RuleOptions{Now: now.Add(-5 * time.Second)})
	if err != nil || !has(keys, "dead:msg.z") || len(tg.calls()) == 0 {
		t.Fatalf("lượt 1: %v %v", keys, err)
	}
	keys, err = observability.CheckRules(t.Context(), e.d, a, observability.RuleOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	wantKey(t, keys, "dead:msg.z", true)
}

// Luật lỗi của Task 20 đi qua CheckRules: dấu vân tay mới → new-error:<8 ký tự>, hơn 50 lỗi trong 5 phút →
// error-spike, cả hai mức normal; văn bản không có nội dung lỗi.
func TestRuleErrorsWired(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	fp := strings.Repeat("ab", 32)
	e.exec(t, `INSERT INTO platform.error_events (fingerprint, role, name, message, top_frames)
		VALUES ($1, 'api', 'n', 'gọi 0911 lỗi', 'f')`, fp)
	e.exec(t, `INSERT INTO platform.metrics_minute (bucket, process, name, label, count, sum, max)
		VALUES ($1, 'api@a', 'errors', 'api', 51, 51, 0)`, time.Now().UTC().Truncate(time.Minute))
	keys, f := check(t, e, observability.RuleOptions{})
	for _, k := range []string{"new-error:" + fp[:8], "error-spike"} {
		wantKey(t, keys, k, true)
		s, _ := f.find(k)
		if s.severity != observability.SeverityNormal || strings.Contains(s.text, "0911") {
			t.Fatalf("%s: mức %q, văn bản %q", k, s.severity, s.text)
		}
	}
}

// new-error không mất khi lần gửi trước hỏng hay lịch chạy trễ: dấu vân tay gặp lần đầu 5 phút trước, chưa từng báo →
// vẫn báo; đã có alert_events của key → thôi; gặp lần đầu quá 1 giờ → không báo nữa.
func TestRuleNewErrorNotLost(t *testing.T) {
	e := newEnv(t)
	fresh(t, e)
	fp := strings.Repeat("cd", 32)
	e.exec(t, `INSERT INTO platform.error_events (fingerprint, role, name, message, top_frames, first_seen_at)
		VALUES ($1, 'api', 'n', 'm', 'f', now() - interval '5 minutes')`, fp)
	keys, _ := check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "new-error:"+fp[:8], true)
	e.alertSent(t, "new-error:"+fp[:8], 4*time.Minute)
	keys, _ = check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "new-error:"+fp[:8], false)

	old := strings.Repeat("ef", 32)
	e.exec(t, `INSERT INTO platform.error_events (fingerprint, role, name, message, top_frames, first_seen_at)
		VALUES ($1, 'api', 'n', 'm', 'f', now() - interval '61 minutes')`, old)
	keys, _ = check(t, e, observability.RuleOptions{})
	wantKey(t, keys, "new-error:"+old[:8], false)
}
