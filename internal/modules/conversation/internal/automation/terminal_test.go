package automation_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// 04 mục 5.3 (Task 182, review độc lập M4): tự trả lời công khai và tự nhắn riêng không rõ đã gửi → cả hai tin bỏ
// (discarded, lý do auto_dropped), không thành "không rõ đã gửi chưa" chờ nhân viên; lượt dropped; Meta nhận đúng 1 lần
// mỗi tin (không gửi lại).
func TestAutoReply_InDoubtDropped(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setReply(true, replyCfg())
	c := e.comment(postA, userA, "giá bao nhiêu shop", time.Now().UTC().Add(-time.Minute))
	e.fg.DropAfterRead(c.CommentID + "/comments")
	e.fg.DropAfterRead(pageExt + "/messages")
	e.deliver(c)
	e.drain()
	e.drain()
	e.both(c.CommentID, automation.OutcomeDropped)
	msgs := e.autoMessages(c.CommentID)
	for _, kind := range []string{"comment", "private_reply"} {
		if r := msgs[kind]; len(r) != 1 || r[0].status != "discarded" {
			t.Errorf("tin tự động %s: %+v, muốn một tin discarded", kind, r)
		}
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE origin = 'automation'
		AND parent_external_id = $1 AND fail_reason = 'auto_dropped'`, c.CommentID); n != 2 {
		t.Errorf("lý do auto_dropped: %d tin, muốn 2", n)
	}
	if len(e.replyCalls(c.CommentID)) != 1 || e.privateCalls(c.CommentID) != 1 {
		t.Fatalf("Meta nhận %d trả lời, %d tin riêng; muốn 1 và 1", len(e.replyCalls(c.CommentID)), e.privateCalls(c.CommentID))
	}
}

// 02 mục 2 (Task 182, review độc lập M4): Meta từ chối lệnh tự ẩn → lượt failed. Lỗi token (190) → Page degraded
// token_invalid ngay (không chờ lượt kiểm sức khỏe mỗi giờ), đúng 1 lệnh cảnh báo gấp channel-auth; lỗi khác (100) →
// Page vẫn active, không cảnh báo.
func TestAutoHide_RejectionReportsOnlyAuth(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		code        int
		msg, status string
		reason      string
		alerts      int
	}{
		{190, "Error validating access token", "degraded", channel.ReasonTokenInvalid, 1},
		{100, "(#100) Invalid parameter", "active", "", 0},
	} {
		e := newEnv(t)
		e.setRule(true, automation.HideAll)
		cm := e.comment(postA, userA, "size S còn không ạ", time.Now().UTC().Add(-2*time.Minute))
		e.fg.FailNext(cm.CommentID, fakegraph.GraphErrorSpec{Code: c.code, Type: "OAuthException", Message: c.msg})
		e.deliver(cm)
		e.drain()
		if out, _, _ := e.run(cm.CommentID); out != string(automation.OutcomeFailed) {
			t.Errorf("%d: lượt %q, muốn failed", c.code, out)
		}
		if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM channel.channel_accounts WHERE id = $1 AND status = $2
			AND coalesce(status_reason, '') = $3`, e.page, c.status, c.reason); n != 1 {
			t.Errorf("%d: Page không ở %s %q", c.code, c.status, c.reason)
		}
		if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1`, channel.AuthAlertKind); n != c.alerts {
			t.Errorf("%d: %d lệnh cảnh báo channel-auth, muốn %d", c.code, n, c.alerts)
		}
	}
}
