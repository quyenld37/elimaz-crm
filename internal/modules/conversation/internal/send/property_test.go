package send_test

import (
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/testkit"
)

// Quy luật (Review Focus 1, ADR-25): với chuỗi ngẫu nhiên 1–5 tin, mỗi tin một phản hồi của kênh (ok, reject_policy,
// fail_before_send rồi ok, timeout_after_send) và echo không metadata tới trước, sau hay không tới: mỗi client_ref tới
// kênh không quá 1 lần; mỗi mã tin kênh đã cấp nằm trên đúng 1 dòng (trừ tin kênh nhận mà không trả lời và không có
// echo: 0 dòng, tin checking); tin sent nào cũng mang mã của kênh.
func TestSendProperty_AtMostOncePerClientRef(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	unanswered := map[string]bool{} // mã kênh đã cấp mà CRM không biết (timeout_after_send) và không có echo
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(rt, "tin")
		for i := range n {
			b := rapid.SampledFrom(propBehaviors).Draw(rt, "kênh"+strconv.Itoa(i))
			echo := rapid.SampledFrom(propEchoes).Draw(rt, "echo"+strconv.Itoa(i))
			if mid := e.propStep(i, b, echo); b == "timeout_after_send" && echo == "none" {
				unanswered[mid] = true
			}
		}
		e.checkAtMostOnce(rt, unanswered)
	})
}

// Phản hồi của kênh giả và lúc echo tới trong quy luật.
var (
	propBehaviors = []string{"ok", "reject_policy", "fail_before_send", "timeout_after_send"} //nolint:gochecknoglobals // bảng hằng của test
	propEchoes    = []string{"none", "before", "after"}                                       //nolint:gochecknoglobals // bảng hằng của test
)

// propStep gửi tin thứ i với phản hồi b của kênh và echo không metadata tới theo echo; trả mã kênh sẽ cấp cho tin.
func (e *env) propStep(i int, b, echo string) string {
	e.chat.SetSendBehavior(b)
	e.mustSend(e.fakeConv, send.Input{Text: "tin " + strconv.Itoa(i)})
	mid := "fakechat-" + strconv.Itoa(len(e.chat.Sent())+1) // mã kênh sẽ cấp nếu nhận tin
	delivered := b != "reject_policy"
	if echo == "before" && delivered {
		e.echo(mid, fake.Key, "")
	}
	if b == "fail_before_send" {
		testkit.RunRound(e.t, e.staff.Owner, e.worker, 10)
		e.chat.SetSendBehavior("ok")
	}
	e.staff.DrainJobs(e.t, e.worker)
	if echo == "after" && delivered {
		e.echo(mid, fake.Key, "")
	}
	return mid
}

// checkAtMostOnce kiểm quy luật trên mọi lần kênh giả đã nhận.
func (e *env) checkAtMostOnce(rt *rapid.T, unanswered map[string]bool) {
	seen := map[string]int{}
	for _, s := range e.chat.Sent() {
		seen[s.Command.ClientRef]++
		if seen[s.Command.ClientRef] > 1 {
			rt.Fatalf("client_ref %s tới kênh %d lần", s.Command.ClientRef, seen[s.Command.ClientRef])
		}
		want := 1
		if unanswered[s.ExternalID] {
			want = 0 // tin checking chưa có mã; Task 157 chờ echo
		}
		if c := e.rowsWithMID(e.fakePage, s.ExternalID); c != want {
			rt.Fatalf("mã %s nằm trên %d dòng, muốn %d", s.ExternalID, c, want)
		}
	}
	if c := e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE origin = 'staff'
		AND send_status = 'sent' AND external_id IS NULL`); c != 0 {
		rt.Fatalf("%d tin sent không mang mã của kênh", c)
	}
}
