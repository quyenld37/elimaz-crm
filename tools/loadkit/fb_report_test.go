package main

import (
	"strings"
	"testing"
	"time"
)

func k6(vals map[string]map[string]float64) k6Metrics {
	m := k6Metrics{}
	for name, v := range vals {
		m[name] = struct {
			Values map[string]float64 `json:"values"`
		}{Values: v}
	}
	return m
}

// fbGood là một lượt đạt mọi ngưỡng của kế hoạch M4 Task 180.
func fbGood(mode string) fbReportInput {
	base := fbDBStats{Deadlocks: 3}
	final := fbDBStats{
		Deadlocks: 3, InboxRows: 100, InboxDistinct: 100, CommentRows: 200, CommentDistinct: 200, CommentLive: 200,
		Sends: 10, SendsSent: 10, OnHand: 10_000, Held: 30, Reserved: 30, Claims: 30, ClaimedComments: 30,
	}
	return fbReportInput{
		Mode: mode,
		Live: k6(map[string]map[string]float64{
			"webhook_ms": {"p(95)": 40}, "webhook_bad": {"count": 0}, "inbox_sent": {"count": 100},
			"comment_sent": {"count": 200}, "graph_push_bad": {"count": 0},
		}),
		Staff: k6(map[string]map[string]float64{"read_ms": {"p(95)": 50, "p(99)": 120}}),
		SSE: &fbSSEResult{
			Inbox:   fbLatency{Events: 100, Pairs: 5000, P95: 400, P99: 900},
			Comment: fbLatency{Events: 200, Pairs: 10_000, P95: 800},
		},
		Series: []fbStatsLine{{DB: base}, {DB: final, Graph: &fbGraphStats{
			Calls: 40, WithRef: 40,
			ClickToAPI: latencyResult{Count: 10, P95: 300},
		}}},
	}
}

func fbVerdict(t *testing.T, vs []verdict, name string) verdict {
	t.Helper()
	for _, v := range vs {
		if strings.HasPrefix(v.Name, name) {
			return v
		}
	}
	t.Fatalf("thiếu tiêu chí %q trong %+v", name, vs)
	return verdict{}
}

func TestFBEvaluate_AllPass(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{modeFB, modeFBChaos} {
		in := fbGood(mode)
		if mode == modeFBChaos {
			in.Chaos = []chaosEvent{{Fault: "giết crm-worker", Recovered: true, RecoverySec: 30}}
			in.Live2 = []liveRegain{{Fault: "giết crm-worker", Video: "5", Regained: true, Sec: 6}}
		}
		for _, v := range evaluateFB(in) {
			if v.Status != statusPass {
				t.Errorf("%s: %s = %s (%s)", mode, v.Name, v.Status, v.Got)
			}
		}
	}
}

// Mỗi ngưỡng của kế hoạch có ca làm nó KHÔNG ĐẠT (không nới, không bỏ qua khi số đo xấu).
func TestFBEvaluate_EachThresholdFails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		crit string
		mut  func(*fbReportInput)
	}{
		{critWebhookP95, func(in *fbReportInput) { in.Live["webhook_ms"].Values["p(95)"] = 300 }},
		{critFBInboxP95, func(in *fbReportInput) { in.SSE.Inbox.P95 = 2000 }},
		{critFBInboxP99, func(in *fbReportInput) { in.SSE.Inbox.P99 = 5000 }},
		{critFBCommentP95, func(in *fbReportInput) { in.SSE.Comment.P95 = 3000 }},
		{critFBSendP95, func(in *fbReportInput) { in.Series[1].Graph.ClickToAPI.P95 = 1000 }},
		{critReadP95, func(in *fbReportInput) { in.Staff["read_ms"].Values["p(95)"] = 200 }},
		{critReadP99, func(in *fbReportInput) { in.Staff["read_ms"].Values["p(99)"] = 500 }},
		{critWebhookNon200, func(in *fbReportInput) { in.Live["webhook_bad"].Values["count"] = 1 }},
		{critFBInboxOnce, func(in *fbReportInput) { in.Series[1].DB.InboxDistinct = 99 }},
		{critFBInboxOnce, func(in *fbReportInput) { in.Live["inbox_sent"].Values["count"] = 101 }},
		{critFBCommentsAll, func(in *fbReportInput) { in.Series[1].DB.CommentLive = 199 }},
		{critFBCommentsAll, func(in *fbReportInput) { in.Live["graph_push_bad"].Values["count"] = 1 }},
		{critFBRefOnce, func(in *fbReportInput) { in.Series[1].Graph.DuplicateRef = 1 }},
		{critFBHolds, func(in *fbReportInput) { in.Series[1].DB.OverHeld = 1 }},
		{critFBHolds, func(in *fbReportInput) { in.Series[1].DB.Reserved = 31 }},
		{critFBDeadlocks, func(in *fbReportInput) { in.Series[1].DB.Deadlocks = 4 }},
		{critFBNoDead, func(in *fbReportInput) { in.Series[1].DB.Dead = 1 }},
		{critFBSendsDone, func(in *fbReportInput) { in.Series[1].DB.SendsBad = 1 }},
	}
	for _, c := range cases {
		in := fbGood(modeFB)
		c.mut(&in)
		if v := fbVerdict(t, evaluateFB(in), c.crit); v.Status != statusFail {
			t.Errorf("%s: %s (%s), muốn KHÔNG ĐẠT", c.crit, v.Status, v.Got)
		}
	}
}

func TestFBEvaluate_Chaos(t *testing.T) {
	t.Parallel()
	in := fbGood(modeFBChaos)
	in.Chaos = []chaosEvent{{Fault: "giết crm-api", Recovered: true, RecoverySec: 121}, {Fault: "ngắt PostgreSQL 60 s"}}
	in.Live2 = []liveRegain{
		{Fault: "giết crm-worker", Video: "5", Regained: true, Sec: 15.5},
		{Fault: "giết crm-worker", Video: "6"},
	}
	vs := evaluateFB(in)
	fails := 0
	for _, v := range vs {
		if v.Status == statusFail {
			fails++
		}
	}
	if fails != 4 {
		t.Errorf("muốn 4 KHÔNG ĐẠT (trở lại 121 s, không trở lại, luồng 15,5 s, luồng không nối lại), có %d: %+v", fails, vs)
	}
	// Bài hỏng giữa tải không chấm độ trễ (mất luồng lúc api chết là có chủ ý) và không đòi mọi tin gửi xong (in_doubt
	// khi giết worker giữa lúc gửi là đúng, CH18).
	for _, v := range vs {
		if strings.HasPrefix(v.Name, critFBInboxP95) || strings.HasPrefix(v.Name, critFBSendsDone) {
			t.Errorf("bài hỏng giữa tải có tiêu chí %q", v.Name)
		}
	}
	if v := fbVerdict(t, evaluateFB(fbGood(modeFBChaos)), critRecovery); v.Status != statusNA {
		t.Errorf("không có lần hỏng nào: %s", v.Status)
	}
}

func TestParseLiveRegain(t *testing.T) {
	t.Parallel()
	in := "fault,video,start,regained\ngiết crm-worker,5,100.0,106.5\ngiết crm-worker,6,100.0,\nhỏng,dòng\n"
	got, err := parseLiveRegain(strings.NewReader(in))
	if err != nil || len(got) != 2 || !got[0].Regained || got[0].Sec != 6.5 || got[1].Regained || got[1].Video != "6" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRenderFB(t *testing.T) {
	t.Parallel()
	in := fbGood(modeFB)
	in.Start = time.Unix(0, 0)
	md := renderFB(in, evaluateFB(in))
	for _, want := range []string{"### Bài fb", critFBCommentP95, "client_ref", "deadlock"} {
		if !strings.Contains(md, want) {
			t.Errorf("thiếu %q trong\n%s", want, md)
		}
	}
}
