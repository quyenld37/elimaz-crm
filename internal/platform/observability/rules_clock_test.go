package observability_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/observability"
)

// clockSkews là độ lệch giả của đồng hồ Go so với PostgreSQL (máy app và máy database khác giờ).
func clockSkews() []time.Duration { return []time.Duration{-5 * time.Minute, 5 * time.Minute} }

// checkAt chạy CheckRules với đồng hồ Go giả goNow (RuleOptions.Now để trống như lịch chạy thật) và trả các key.
func checkAt(t *testing.T, e *env, a observability.Alerter, o observability.RuleOptions, goNow time.Time) []string {
	t.Helper()
	keys, err := observability.CheckRulesAt(t.Context(), e.d, a, o, goNow)
	if err != nil {
		t.Fatalf("CheckRules: %v", err)
	}
	return keys
}

// newError chèn một loại lỗi có first_seen_at = now() − ago của PostgreSQL (như UpsertErrorEvent ghi); trả 8 ký tự đầu
// dấu vân tay.
func (e *env) newError(t *testing.T, seed string, ago time.Duration) string {
	t.Helper()
	fp := strings.Repeat(seed, 64/len(seed))
	e.exec(t, `INSERT INTO platform.error_events (fingerprint, role, name, message, top_frames, first_seen_at, last_seen_at)
		VALUES ($1, 'api', 'n', 'm', 'f', now() - make_interval(secs => $2), now() - make_interval(secs => $2))`,
		fp, ago.Seconds())
	return fp[:8]
}

// Luật so mốc do PostgreSQL ghi (việc chờ, in_doubt, dead sau lần báo trước, bản sao lưu, loại lỗi mới) với đồng hồ
// PostgreSQL: đồng hồ Go lệch ±5 phút không đổi kết quả. Mỗi mốc đặt sát ngưỡng (trong 10 s đến 10 phút) để lệch 5
// phút làm phép so trộn đồng hồ ra sai.
func TestRulesPostgresClockSkew(t *testing.T) {
	for _, skew := range clockSkews() {
		for _, over := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v_over=%v", skew, over), func(t *testing.T) {
				e := newEnv(t)
				pick := func(under, past time.Duration) time.Duration {
					if over {
						return past
					}
					return under
				}
				e.job(t, "t.q", "pending", `{}`, pick(50*time.Second, 70*time.Second), nil)
				e.job(t, "cmd.x", "in_doubt", `{}`, 0, ago(pick(110*time.Second, 130*time.Second)))
				e.alertSent(t, "dead:msg.d", time.Minute)
				e.job(t, "msg.d", "dead", `{}`, 0, ago(pick(90*time.Second, 30*time.Second)))
				e.exec(t, `INSERT INTO platform.backup_runs (kind, ok, finished_at)
					VALUES ('base', true, now() - make_interval(secs => $1))`, pick(25*time.Hour+50*time.Minute,
					26*time.Hour+10*time.Minute).Seconds())
				short := e.newError(t, "ab", pick(57*time.Minute, 63*time.Minute))
				keys := checkAt(t, e, &fakeAlerter{}, observability.RuleOptions{BackupExpected: true}, time.Now().Add(skew))
				for k, want := range map[string]bool{
					"queue-lag": over, "in-doubt:cmd.x": over, "dead:msg.d": over, "backup-age": over,
					"new-error:" + short: !over,
				} {
					if has(keys, k) != want {
						t.Errorf("key %q có = %v, muốn %v; các key: %v", k, !want, want, keys)
					}
				}
			})
		}
	}
}

// Mốc alert_events của luật dead: là lúc đọc theo đồng hồ PostgreSQL (cùng đồng hồ với finished_at): đồng hồ Go lệch
// ±5 phút thì việc dead mới vẫn được báo ở lượt đầu và không bị báo lại ở lượt sau.
func TestRuleDeadAlertMarkPostgresClock(t *testing.T) {
	for _, skew := range clockSkews() {
		t.Run(skew.String(), func(t *testing.T) {
			e := newEnv(t)
			_, client := newTelegram(t)
			a := observability.NewTelegramAlerter(e.d, tgConfig(), client, quiet())
			e.job(t, "msg.s", "dead", `{}`, 0, ago(30*time.Second))
			if keys := checkAt(t, e, a, observability.RuleOptions{}, time.Now().Add(skew)); !has(keys, "dead:msg.s") {
				t.Fatalf("lượt 1 không báo việc dead mới: %v", keys)
			}
			if keys := checkAt(t, e, a, observability.RuleOptions{}, time.Now().Add(skew)); has(keys, "dead:msg.s") {
				t.Fatalf("lượt 2 báo lại việc dead đã báo: %v", keys)
			}
		})
	}
}
