package send_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// blockedUntil đọc mốc chặn gửi của Page acc (nil khi chưa có).
func (e *env) blockedUntil(acc uuid.UUID) *time.Time {
	e.t.Helper()
	var at *time.Time
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT blocked_until FROM channel.channel_accounts WHERE id = $1`, acc).Scan(&at)
	})
	return at
}

// pageStatus đọc trạng thái của Page acc.
func (e *env) pageStatus(acc uuid.UUID) string {
	e.t.Helper()
	var s string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status FROM channel.channel_accounts WHERE id = $1`, acc).Scan(&s)
	})
	return s
}

// sentNow gửi chữ text vào hội thoại conv, chạy lệnh, đòi tin sent.
func (e *env) sentNow(conv uuid.UUID, text string) {
	e.t.Helper()
	m := e.mustSend(conv, send.Input{Text: text})
	e.drain()
	if got := e.message(m.ID); got.Status != "sent" {
		e.t.Fatalf("tin %q = %+v, muốn sent", text, got)
	}
}

// wantBlockedUntil đòi mốc chặn gửi của Page Facebook đúng bằng want.
func (e *env) wantBlockedUntil(want time.Time) {
	e.t.Helper()
	if at := e.blockedUntil(e.fbPage); at == nil || !at.Equal(want) {
		e.t.Fatalf("mốc chặn = %v, muốn %v", at, want)
	}
}

// mustAct chạy thao tác fn (trả lời comment, nhắn riêng) trên comment của th với chữ text, lỗi thì dừng.
func (e *env) mustAct(fn commentAction, th thread, text string) send.Message {
	e.t.Helper()
	m, err := e.act(fn, th, text)
	if err != nil {
		e.t.Fatal(err)
	}
	return m
}

// removeParent đánh dấu comment của th đã xóa rồi chạy lệnh.
func (e *env) removeParent(th thread) {
	e.t.Helper()
	e.exec(`UPDATE conversation.messages SET removed_at = now() WHERE id = $1`, th.comment)
	e.drain()
}

// jobFor dựng việc của lệnh với payload p, tenant tenant (nil: thiếu tenant).
func jobFor(t *testing.T, tenant *uuid.UUID, p map[string]any) queue.Job {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return queue.Job{ID: uuid.New(), TenantID: tenant, Payload: raw}
}

// looker trả Looker của lệnh mang tên kind.
func (e *env) looker(kind string) commands.Looker { //nolint:ireturn // Looker là interface của commands
	e.t.Helper()
	l, ok := e.handler(kind).(commands.Looker)
	if !ok {
		e.t.Fatalf("lệnh %s không có Lookup", kind)
	}
	return l
}

// jobExists đếm việc id loại kind.
func (e *env) jobExists(id uuid.UUID, kind string) int {
	e.t.Helper()
	var n int
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(), `SELECT count(*) FROM platform.jobs WHERE id = $1 AND kind = $2`,
		id, kind).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// Meta trả 551 (khách chặn Page) → tin failed blocked_by_user nhưng Page vẫn không degraded và không có lệnh cảnh báo
// token: chỉ lý do auth mới báo token hỏng (Task 182, mutation testing M4).
func TestMutM4_BlockedByUserKeepsPageActive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fg.FailNext(fbPageExt+"/messages", fakegraph.GraphErrorSpec{Code: 551, Type: "OAuthException", Message: "This person isn't available"})
	m := e.mustSend(e.fbConv, send.Input{Text: textHello})
	e.drain()
	if got := e.message(m.ID); got.Status != "failed" || got.FailReason != "blocked_by_user" {
		t.Fatalf("551: tin = %+v", got)
	}
	if s := e.pageStatus(e.fbPage); s == "degraded" {
		t.Fatal("551: Page bị degraded dù token không hỏng")
	}
	if n := e.alerts(); n != 0 {
		t.Fatalf("551: %d lệnh cảnh báo token, muốn 0", n)
	}
}

// Gửi bình thường (Meta không báo mức dùng) → Page không có mốc chặn gửi; Meta báo mức dùng 85% → mốc chặn đúng bằng
// lúc ghi xong + 1 giây (slowDownWait, đồng hồ đứng) (Task 182, mutation testing M4).
func TestMutM4_SlowDownExactUntil(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	t0 := time.Now().UTC().Truncate(time.Microsecond)
	e.clock.Freeze(t0)
	e.sentNow(e.fbConv, textHello)
	if at := e.blockedUntil(e.fbPage); at != nil {
		t.Fatalf("không giãn mà Page có mốc chặn %v", at)
	}
	e.fg.SetUsagePct(85)
	e.sentNow(e.fbConv, "Tin thứ hai")
	e.wantBlockedUntil(t0.Add(time.Second))
}

// Lệnh ẩn comment: ẩn bình thường → không mốc chặn gửi; Page chuyển chỉ xem trước khi lệnh chạy → không gọi Meta; Meta
// báo mức dùng 85% khi ẩn → mốc chặn đúng lúc ghi + 1 giây (Task 182, mutation testing M4).
func TestMutM4_HideExecuteReadOnlyAndSlowDown(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	t0 := time.Now().UTC().Truncate(time.Microsecond)
	e.clock.Freeze(t0)
	a := e.commentThread(postExt+"_m1", commenterID, time.Now().Add(-time.Hour))
	if _, err := e.hide(a.comment, true); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r := e.row(a.comment); !r.Hidden || e.hideCalls(a.commentExt) != 1 {
		t.Fatalf("ẩn: comment = %+v, Meta nhận %d", r, e.hideCalls(a.commentExt))
	}
	if at := e.blockedUntil(e.fbPage); at != nil {
		t.Fatalf("không giãn mà Page có mốc chặn %v", at)
	}

	b := e.commentThread(postExt+"_m2", "6100000000000044", time.Now().Add(-time.Hour))
	if _, err := e.hide(b.comment, true); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.fbPage)
	e.drain()
	if n := e.hideCalls(b.commentExt); n != 0 {
		t.Fatalf("chỉ xem: Meta nhận %d lần ẩn, muốn 0", n)
	}
	var status string
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), `SELECT status FROM platform.jobs WHERE kind = $1
		AND payload->>'messageId' = $2`, send.HideCommentKind().Name, b.comment.String()).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "dead" {
		t.Fatalf("chỉ xem: lệnh ẩn = %s, muốn dead (bị từ chối, không chờ gửi lại)", status)
	}
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'enabled' WHERE id = $1`, e.fbPage)

	c := e.commentThread(postExt+"_m3", "6100000000000045", time.Now().Add(-time.Hour))
	e.fg.SetUsagePct(85)
	if _, err := e.hide(c.comment, true); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if n := e.hideCalls(c.commentExt); n != 1 {
		t.Fatalf("Meta nhận %d lần ẩn, muốn 1", n)
	}
	e.wantBlockedUntil(t0.Add(time.Second))
}

// Việc thiếu tenant: Lookup của lệnh gửi tin, trả lời comment, ẩn comment → unknown, Execute → rejected bad_payload,
// không panic; Page mất token: Lookup trả lời comment và ẩn comment → unknown (chưa tra được, giữ in_doubt)
// (Task 182, mutation testing M4).
func TestMutM4_LookupWithoutTenantOrToken(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := t.Context()
	th := e.commentThread(postExt+"_m4", commenterID, time.Now().Add(-time.Hour))
	reply, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	m := e.mustSend(e.fbConv, send.Input{Text: textHello})
	hidden := true
	cases := map[string]queue.Job{
		send.KindSendMessage:         jobFor(t, nil, map[string]any{"messageId": m.ID}),
		send.ReplyCommentKind().Name: jobFor(t, nil, map[string]any{"messageId": reply.ID}),
		send.HideCommentKind().Name:  jobFor(t, nil, map[string]any{"messageId": th.comment, "hidden": hidden}),
	}
	for kind, j := range cases {
		if st, err := e.looker(kind).Lookup(ctx, j); err != nil || st != commands.StatusUnknown {
			t.Errorf("%s thiếu tenant: Lookup = %s, %v; muốn unknown", kind, st, err)
		}
	}
	out, err := e.handler(send.KindSendMessage).Execute(ctx, cases[send.KindSendMessage])
	if err != nil || out.Status != commands.StatusRejected {
		t.Errorf("thiếu tenant: Execute = %+v, %v; muốn rejected", out, err)
	}

	e.exec(`UPDATE channel.channel_accounts SET credential_enc = NULL, credential_kid = NULL WHERE id = $1`, e.fbPage)
	tenant := e.tenant
	for kind, j := range map[string]queue.Job{
		send.ReplyCommentKind().Name: jobFor(t, &tenant, map[string]any{"messageId": reply.ID}),
		send.HideCommentKind().Name:  jobFor(t, &tenant, map[string]any{"messageId": th.comment, "hidden": hidden}),
	} {
		if st, err := e.looker(kind).Lookup(ctx, j); err != nil || st != commands.StatusUnknown {
			t.Errorf("%s không token: Lookup = %s, %v; muốn unknown", kind, st, err)
		}
	}
}

// Việc của lịch send_reveal: hai hội thoại có tin checking quá 60 giây → kết quả {"revealed": 2}, cả hai tin in_doubt,
// mỗi hội thoại một báo hiệu (Task 182, mutation testing M4).
func TestMutM4_RevealHandlerCountsAllConversations(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a := e.mustSend(e.fbConv, send.Input{Text: textHello})
	b := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.exec(`UPDATE conversation.messages SET send_status = 'checking', send_checking_since = $1 WHERE id = ANY($2)`,
		time.Now().UTC().Add(-2*time.Minute), []uuid.UUID{a.ID, b.ID})
	beforeA, beforeB := e.signals(e.fbConv), e.signals(e.fakeConv)
	h := send.Parts(e.d).Jobs[0]
	tenant := e.tenant
	raw, err := inTx(e, func(tx pgx.Tx) (json.RawMessage, error) {
		return h.Handle(t.Context(), queue.Job{ID: uuid.New(), TenantID: &tenant, Payload: json.RawMessage(`{}`)}, tx)
	})
	if err != nil {
		t.Fatal(err)
	}
	var res map[string]int
	if err := json.Unmarshal(raw, &res); err != nil || res["revealed"] != 2 {
		t.Fatalf("kết quả = %s, %v; muốn revealed 2", raw, err)
	}
	for _, id := range []uuid.UUID{a.ID, b.ID} {
		if got := e.message(id); got.Status != "in_doubt" {
			t.Fatalf("tin %s = %+v, muốn in_doubt", id, got)
		}
	}
	if e.signals(e.fbConv) != beforeA+1 || e.signals(e.fakeConv) != beforeB+1 {
		t.Fatalf("báo hiệu: %d, %d mới; muốn 1, 1", e.signals(e.fbConv)-beforeA, e.signals(e.fakeConv)-beforeB)
	}
}

// Gửi lại tin của nhân viên khi cửa sổ 24 giờ đã đóng mà Page bật Human Agent (còn trong 7 ngày) → window_closed kèm
// hạn (còn gửi được bằng thẻ nhân viên), không phải từ chối như tin tự động (Task 182, mutation testing M4).
func TestMutM4_ResendStaffHumanAgentUntil(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.expireJob(m.ID)
	e.exec(`UPDATE channel.channel_accounts SET human_agent_enabled = true WHERE id = $1`, e.fakePage)
	e.exec(`UPDATE conversation.conversations SET last_customer_message_at = $1 WHERE id = $2`,
		e.clock.Now().Add(-25*time.Hour), e.fakeConv)
	_, err := e.resendAs(e.user, m.ID)
	na, ok := errors.AsType[*send.NotAllowedError](err)
	if !ok || na.Reason != channel.DenyWindowClosed || na.Until == nil {
		t.Fatalf("lỗi = %v, muốn window_closed kèm hạn thẻ nhân viên", err)
	}
}

// Tin chỉ có ảnh quá hạn: gửi lại được (tin mới có ảnh); bỏ tin ảnh khác → trả đúng ảnh của tin; bỏ tin thường không
// báo hiệu hội thoại rỗng (Task 182, mutation testing M4).
func TestMutM4_ImageResendAndDiscard(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	img, _ := e.addImage(e.tenant, send.OutgoingImagePurpose, "ready")
	m := e.mustSend(e.fbConv, send.Input{AttachmentFileID: &img})
	e.expireJob(m.ID)
	res, err := e.resendAs(e.user, m.ID)
	if err != nil || len(res.Message.Attachments) != 1 || *res.Message.Attachments[0].FileID != img {
		t.Fatalf("gửi lại tin ảnh: %+v, %v", res, err)
	}
	other := e.mustSend(e.fbConv, send.Input{AttachmentFileID: &img})
	e.expireJob(other.ID)
	d, err := e.discard(other.ID)
	if err != nil || len(d.Attachments) != 1 || d.Attachments[0].FileID == nil || *d.Attachments[0].FileID != img {
		t.Fatalf("bỏ tin ảnh: %+v, %v", d, err)
	}
	if n := e.signals(uuid.Nil); n != 0 {
		t.Fatalf("%d báo hiệu cho hội thoại rỗng", n)
	}
}

// Gửi lại câu trả lời comment quá hạn khi comment cha đã bị xóa → send_not_allowed comment_removed, tin cũ giữ
// nguyên (Task 182, mutation testing M4).
func TestMutM4_ResendReplyParentRemoved(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_m5", commenterID, time.Now().Add(-time.Hour))
	m, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE conversation.messages SET send_status = 'expired' WHERE id = $1`, m.ID)
	e.exec(`UPDATE conversation.messages SET removed_at = now() WHERE id = $1`, th.comment)
	_, err = e.resendAs(e.user, m.ID)
	if na, ok := errors.AsType[*send.NotAllowedError](err); !ok || na.Reason != channel.DenyCommentRemoved {
		t.Fatalf("lỗi = %v, muốn comment_removed", err)
	}
	if got := e.message(m.ID); got.Status != "expired" {
		t.Fatalf("tin cũ = %+v, muốn giữ expired", got)
	}
}

// Nhắn riêng: hội thoại hộp thư hay chữ toàn khoảng trắng → ErrInvalidInput; chữ đúng 2 000 ký tự → được (Task 182,
// mutation testing M4).
func TestMutM4_PrivateReplyInput(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_m6", commenterID, time.Now().Add(-time.Hour))
	if _, err := e.private(thread{conv: e.fbConv, comment: th.comment}, textPrivate); !errors.Is(err, send.ErrInvalidInput) {
		t.Errorf("hộp thư: lỗi = %v, muốn ErrInvalidInput", err)
	}
	if _, err := e.private(th, "   "); !errors.Is(err, send.ErrInvalidInput) {
		t.Errorf("khoảng trắng: lỗi = %v, muốn ErrInvalidInput", err)
	}
	if _, err := e.private(th, strings.Repeat("a", 2000)); err != nil {
		t.Errorf("đúng 2 000 ký tự: %v", err)
	}
}

// Trạng thái nhắn riêng của câu trả lời của Page (comment không phải của khách, đã có mã kênh) → expired, không phải
// available (Task 182, mutation testing M4).
func TestMutM4_PrivateReplyStatusOfStaffComment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_m7", commenterID, time.Now().Add(-time.Hour))
	m := e.mustAct(send.ReplyToComment, th, textReply)
	e.drain()
	if r := e.row(m.ID); r.Status != "sent" || r.ExternalID == "" {
		t.Fatalf("câu trả lời = %+v", r)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages r JOIN conversation.messages c
		ON c.id = $2 WHERE r.id = $1 AND r.post_id = c.post_id`, m.ID, th.comment); n != 1 {
		t.Fatal("câu trả lời comment không mang bài của comment cha")
	}
	if st := e.privateState(thread{comment: m.ID}); st.State != send.PrivateReplyExpired {
		t.Fatalf("trạng thái nhắn riêng câu của Page = %+v, muốn expired", st)
	}
}

// Comment cha bị xóa sau khi bấm nhắn riêng, trước khi lệnh chạy → Meta không nhận, tin failed lý do comment_removed
// (Task 182, mutation testing M4).
func TestMutM4_PrivateExecuteParentRemoved(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_m8", commenterID, time.Now().Add(-time.Hour))
	m := e.mustAct(send.PrivateReply, th, textPrivate)
	e.removeParent(th)
	if n := e.privateCalls(th.commentExt); n != 0 {
		t.Fatalf("Meta nhận %d lần nhắn riêng, muốn 0", n)
	}
	if r := e.row(m.ID); r.Status != "failed" || r.FailReason != string(channel.DenyCommentRemoved) {
		t.Fatalf("tin riêng = %+v, muốn failed comment_removed", r)
	}
}

// Lệnh trả lời comment không gọi Meta khi: tin đã sent trước khi lệnh chạy; Page chuyển chỉ xem (tin failed
// read_only); comment cha đã xóa (failed comment_removed). Nhân viên không có quyền trên Page → access.ErrNotFound
// (Task 182, mutation testing M4).
func TestMutM4_ReplyExecuteRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	stranger := identity.AuthUser{UserID: e.user.UserID, TenantID: e.tenant}
	th := e.commentThread(postExt+"_m9", commenterID, time.Now().Add(-time.Hour))
	if _, err := inTx(e, func(tx pgx.Tx) (send.Message, error) {
		return send.ReplyToComment(t.Context(), tx, e.env(), stranger, th.conv,
			send.CommentInput{CommentMessageID: th.comment, Text: textReply}, e.clock.Now())
	}); !errors.Is(err, access.ErrNotFound) {
		t.Fatalf("không quyền: lỗi = %v, muốn ErrNotFound", err)
	}

	sent, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE conversation.messages SET send_status = 'sent' WHERE id = $1`, sent.ID)
	e.drain()
	if n := e.replyCalls(th.commentExt); n != 0 {
		t.Fatalf("tin đã sent: Meta nhận %d lần, muốn 0", n)
	}

	ro, err := e.reply(th, "Dạ chị chọn màu nào ạ")
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.fbPage)
	e.drain()
	if n := e.replyCalls(th.commentExt); n != 0 {
		t.Fatalf("chỉ xem: Meta nhận %d lần, muốn 0", n)
	}
	if r := e.row(ro.ID); r.Status != "failed" {
		t.Fatalf("chỉ xem: tin = %+v, muốn failed", r)
	}
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'enabled' WHERE id = $1`, e.fbPage)

	gone := e.mustAct(send.ReplyToComment, th, "Dạ shop còn hàng ạ")
	e.removeParent(th)
	if n := e.replyCalls(th.commentExt); n != 0 {
		t.Fatalf("comment đã xóa: Meta nhận %d lần, muốn 0", n)
	}
	if r := e.row(gone.ID); r.Status != "failed" || r.FailReason != string(channel.DenyCommentRemoved) {
		t.Fatalf("comment đã xóa: tin = %+v, muốn failed comment_removed", r)
	}
}

// Gửi tin đúng 2 000 ký tự → được; tin ghi người gửi là nhân viên; lệnh chạy ghi chỉ số send_click_to_api_ms (Task 182,
// mutation testing M4).
func TestMutM4_SendMaxTextStaffAndMetric(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fakeConv, send.Input{Text: strings.Repeat("a", 2000)})
	if r := e.row(m.ID); r.Staff == nil || *r.Staff != e.user.UserID {
		t.Fatalf("người gửi = %v, muốn %s", r.Staff, e.user.UserID)
	}
	e.reg.Drain()
	e.drain()
	var n int64
	for _, r := range e.reg.Drain() {
		if r.Name == "send_click_to_api_ms" {
			n += r.Count
		}
	}
	if n != 1 {
		t.Fatalf("send_click_to_api_ms = %d lần, muốn 1", n)
	}
}

// Tin nhắn lại tự động (Automated) trong cửa sổ 24 giờ → trả mã lệnh command.fb.auto_send có thật; tin không có nút
// (quick_replies NULL); đoạn trích hội thoại là chữ của tin (Task 182, mutation testing M4).
func TestMutM4_AutomatedQueuesCommand(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const text = "Dạ shop nhắn lại chị ạ"
	job, err := inTx(e, func(tx pgx.Tx) (uuid.UUID, error) {
		return send.Automated(t.Context(), tx, e.d, e.fakeConv, text, e.clock.Now())
	})
	if err != nil || job == uuid.Nil || e.jobExists(job, send.KindAutoSend) != 1 {
		t.Fatalf("Automated = %s, %v; muốn mã lệnh auto_send", job, err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE command_job_id = $1
		AND origin = 'automation' AND quick_replies IS NULL`, job); n != 1 {
		t.Fatalf("%d tin tự động không nút, muốn 1", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.conversations WHERE id = $1 AND last_snippet = $2`,
		e.fakeConv, text); n != 1 {
		t.Fatal("đoạn trích hội thoại không phải chữ của tin tự động")
	}
}

// Tự trả lời comment, tự nhắn riêng → trả mã lệnh có thật đúng loại; comment đã xóa → tự nhắn riêng ErrAutoNotAllowed
// (Task 182, mutation testing M4).
func TestMutM4_AutoCommentActionsQueue(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	type queueFn func(ctx context.Context, tx pgx.Tx, env send.Env, commentID uuid.UUID, text string, now time.Time) (uuid.UUID, error)
	run := func(fn queueFn, comment uuid.UUID) (uuid.UUID, error) {
		return inTx(e, func(tx pgx.Tx) (uuid.UUID, error) {
			return fn(t.Context(), tx, e.env(), comment, textReply, e.clock.Now())
		})
	}
	a := e.commentThread(postExt+"_m10", commenterID, time.Now().Add(-time.Hour))
	if job, err := run(send.QueueAutoCommentReply, a.comment); err != nil || job == uuid.Nil ||
		e.jobExists(job, send.KindAutoReplyComment) != 1 {
		t.Fatalf("tự trả lời comment = %s, %v", job, err)
	}
	if job, err := run(send.QueueAutoPrivateReply, a.comment); err != nil || job == uuid.Nil ||
		e.jobExists(job, send.KindAutoPrivateReply) != 1 {
		t.Fatalf("tự nhắn riêng = %s, %v", job, err)
	}
	b := e.commentThread(postExt+"_m11", "6100000000000046", time.Now().Add(-time.Hour))
	e.exec(`UPDATE conversation.messages SET removed_at = now() WHERE id = $1`, b.comment)
	if _, err := run(send.QueueAutoPrivateReply, b.comment); !errors.Is(err, send.ErrAutoNotAllowed) {
		t.Fatalf("comment đã xóa: lỗi = %v, muốn ErrAutoNotAllowed", err)
	}
}

// Ghi chú lưu tên người ghi (display_name của nhân viên) vào sender_name (Task 182, mutation testing M4).
func TestMutM4_NoteSenderName(t *testing.T) {
	t.Parallel()
	e := newNoteEnv(t)
	n, err := e.add(e.sale, e.conv, "Khách hẹn chiều mai")
	if err != nil {
		t.Fatal(err)
	}
	if c := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE id = $1 AND sender_name = $2`,
		n.ID, "sale@elimaz.test"); c != 1 {
		t.Fatal("sender_name của ghi chú không phải tên nhân viên")
	}
}

// OnTerminal(dead) của lệnh gửi cho tin đã sent (không còn queued) mang lý do auth → không đổi tin, không báo token
// Page hỏng (Task 182, mutation testing M4).
func TestMutM4_TerminalIgnoresSettledMessage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.mustSend(e.fakeConv, send.Input{Text: textHello})
	e.drain()
	e.exec(`UPDATE conversation.messages SET fail_reason = 'auth' WHERE id = $1`, m.ID)
	h, ok := e.handler(send.KindSendMessage).(interface {
		OnTerminal(ctx context.Context, j queue.Job, status string, tx pgx.Tx) error
	})
	if !ok {
		t.Fatal("lệnh gửi không có OnTerminal")
	}
	tenant := e.tenant
	if _, err := inTx(e, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, h.OnTerminal(t.Context(), jobFor(t, &tenant, map[string]any{"messageId": m.ID}), queue.StatusDead, tx)
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.message(m.ID); got.Status != "sent" {
		t.Fatalf("tin = %+v, muốn giữ sent", got)
	}
	if s := e.pageStatus(e.fakePage); s == "degraded" || e.alerts() != 0 {
		t.Fatalf("Page = %s, %d cảnh báo; muốn không báo token hỏng", s, e.alerts())
	}
}
