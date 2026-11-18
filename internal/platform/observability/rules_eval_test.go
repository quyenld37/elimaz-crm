package observability

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func pgKeys(s pgStats, o RuleOptions) []string {
	var out []string
	for _, a := range pgRules(s, o) {
		out = append(out, a.key)
	}
	return out
}

// Luật đọc view hệ thống của PostgreSQL khó dựng thật (WAL, dòng chết, câu chậm): kiểm ngưỡng trên số đã đọc.
func TestPGRuleThresholds(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	o := RuleOptions{Now: now, ConnBudget: 20}
	cases := []struct {
		name string
		s    pgStats
		key  string
		want bool
	}{
		{"archive tắt", pgStats{archiveOn: false}, "wal-archive", false},
		{"đẩy lỗi sau lần đạt", pgStats{archiveOn: true, lastArchived: now.Add(-time.Minute), lastFailed: now.Add(-30 * time.Second)}, "wal-archive", true},
		{"lần đạt cũ 6 phút", pgStats{archiveOn: true, lastArchived: now.Add(-6 * time.Minute)}, "wal-archive", true},
		{"đạt 1 phút trước, lỗi cũ hơn", pgStats{archiveOn: true, lastArchived: now.Add(-time.Minute), lastFailed: now.Add(-time.Hour)}, "wal-archive", false},
		{"chưa đẩy lần nào, đặt lại 10 phút", pgStats{archiveOn: true, statsReset: now.Add(-10 * time.Minute)}, "wal-archive", true},
		{"chưa đẩy lần nào, đặt lại 1 phút", pgStats{archiveOn: true, statsReset: now.Add(-time.Minute)}, "wal-archive", false},
		{"WAL 2 GiB", pgStats{walBytes: 2 << 30}, "wal-size", false},
		{"WAL quá 2 GiB", pgStats{walBytes: 2<<30 + 1}, "wal-size", true},
		{"dòng chết 50.000", pgStats{jobsDeadTuples: 50_000}, "jobs-dead-tuples", false},
		{"dòng chết 50.001", pgStats{jobsDeadTuples: 50_001}, "jobs-dead-tuples", true},
		{"14/20 kết nối", pgStats{appConns: 14}, "pg-connections", false},
		{"15/20 kết nối", pgStats{appConns: 15}, "pg-connections", true},
		{"không transaction dài", pgStats{}, "long-tx", false},
		{"transaction dài", pgStats{longTx: 1}, "long-tx", true},
		{"không chờ khóa", pgStats{}, "lock-wait", false},
		{"chờ khóa", pgStats{lockWait: 2}, "lock-wait", true},
		{"không câu chậm", pgStats{}, "slow-query", false},
		{"câu chậm", pgStats{slow: []slowQuery{{queryID: 42, calls: 100, meanMs: 501}}}, "slow-query", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := slices.Contains(pgKeys(c.s, o), c.key); got != c.want {
				t.Fatalf("%s: có = %v, muốn %v (%v)", c.key, got, c.want, pgKeys(c.s, o))
			}
		})
	}
	if slices.Contains(pgKeys(pgStats{appConns: 99}, RuleOptions{Now: now}), "pg-connections") {
		t.Fatal("ConnBudget 0: không xét pg-connections")
	}
}

// Mức của từng luật PostgreSQL và văn bản không mang câu SQL.
func TestPGRuleSeverityAndText(t *testing.T) {
	now := time.Now()
	s := pgStats{archiveOn: true, lastArchived: now.Add(-time.Hour), walBytes: 3 << 30, slow: []slowQuery{{queryID: 7, calls: 120, meanMs: 900}}}
	want := map[string]string{"wal-archive": "urgent", "wal-size": "normal", "slow-query": "normal"}
	for _, a := range pgRules(s, RuleOptions{Now: now}) {
		if w, ok := want[a.key]; ok && a.severity != w {
			t.Fatalf("%s mức %s, muốn %s", a.key, a.severity, w)
		}
		if a.key == "slow-query" && !strings.Contains(a.text, "7") {
			t.Fatalf("slow-query phải nêu queryid: %s", a.text)
		}
	}
}

// streak: chuỗi phút liền (cho phép hở một phút) ở cuối dãy thỏa điều kiện, phút cuối phải gần now.
func TestStreak(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 30, 0, time.UTC)
	m := func(i int) time.Time { return now.Truncate(time.Minute).Add(time.Duration(-i) * time.Minute) }
	pos := func(v float64) bool { return v > 0 }
	pts := []point{{m(4), 1}, {m(3), 1}, {m(2), 1}, {m(1), 1}, {m(0), 1}}
	if d := streak(pts, now, pos); d != 5*time.Minute {
		t.Fatalf("5 phút liền: %v", d)
	}
	gap := []point{{m(4), 1}, {m(3), 1}, {m(1), 1}, {m(0), 1}}
	if d := streak(gap, now, pos); d != 5*time.Minute {
		t.Fatalf("hở một phút vẫn liền: %v", d)
	}
	broken := []point{{m(4), 1}, {m(3), 0}, {m(2), 1}, {m(1), 1}, {m(0), 1}}
	if d := streak(broken, now, pos); d != 3*time.Minute {
		t.Fatalf("có số 0 ở giữa: %v", d)
	}
	stale := []point{{m(9), 1}, {m(8), 1}, {m(7), 1}}
	if d := streak(stale, now, pos); d != 0 {
		t.Fatalf("số cuối cũ: %v", d)
	}
	if d := streak(nil, now, pos); d != 0 {
		t.Fatalf("không số: %v", d)
	}
}

// Tin tổng kết chỉ gửi từ 08:00 giờ Việt Nam (01:00 UTC).
func TestDigestDue(t *testing.T) {
	if digestDue(time.Date(2026, 11, 18, 0, 59, 0, 0, time.UTC)) {
		t.Fatal("07:59 giờ Việt Nam chưa tới giờ")
	}
	if !digestDue(time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC)) || !digestDue(time.Date(2026, 11, 18, 16, 59, 0, 0, time.UTC)) {
		t.Fatal("từ 08:00 tới 23:59 giờ Việt Nam là tới giờ")
	}
	if digestDue(time.Date(2026, 11, 18, 17, 30, 0, 0, time.UTC)) {
		t.Fatal("00:30 giờ Việt Nam ngày mới chưa tới giờ")
	}
}
