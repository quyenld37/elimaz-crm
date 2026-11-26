package send_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
)

// Task 497: câu trả lời comment, tin riêng "không rõ đã gửi" đi đúng luồng của Task 157 (chờ echo 60 giây, rồi nhân
// viên gửi lại hay bỏ); tin tự động không đổi mốc tin nhân viên; gửi lại tin chào giữ nguồn và nút trả lời nhanh.

// staffMark đọc last_staff_message_at, last_message_at của hội thoại conv (nil khi chưa có).
func (e *env) staffMark(conv uuid.UUID) (staff, last *time.Time) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT last_staff_message_at, last_message_at FROM conversation.conversations
			WHERE id = $1`, conv).Scan(&staff, &last)
	})
	return staff, last
}

// privateInDoubt nhắn riêng comment th mà Meta đọc rồi rớt kết nối → tin riêng checking, +61 s in_doubt (đồng hồ giả
// dừng ở lúc gửi); trả tin riêng và hội thoại hộp thư của nó.
func (e *env) privateInDoubt(th thread) (send.Message, uuid.UUID) {
	e.t.Helper()
	start := time.Now().UTC()
	e.clock.Freeze(start)
	e.fg.DropAfterRead(fbPageExt + "/messages")
	m, err := e.private(th, textPrivate)
	if err != nil {
		e.t.Fatal(err)
	}
	e.drain()
	inbox := e.row(m.ID).Conv
	e.checkReveal(m.ID, inbox, start)
	return m, inbox
}

// fbAccount đọc Page Facebook của env.
func (e *env) fbAccount() channel.ChannelAccount {
	e.t.Helper()
	acc, err := inTx(e, func(tx pgx.Tx) (channel.ChannelAccount, error) {
		return channel.Account(e.t.Context(), tx, e.fbPage)
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return acc
}

// checkRecovered: kết quả gửi lại res của tin cũ old (kind, comment cha th) là tin mới queued cùng loại, cùng comment
// cha, cùng hội thoại conv, mayDuplicate; tin cũ discarded.
func (e *env) checkRecovered(res send.ResendResult, old send.Message, kind string, th thread, conv uuid.UUID) {
	e.t.Helper()
	m := res.Message
	if !res.MayDuplicate || m.ID == old.ID || m.ClientRef == old.ClientRef || m.Kind != kind || m.Origin != "staff" ||
		m.ParentMessageID == nil || *m.ParentMessageID != th.comment || m.ConversationID != conv || m.SendStatus != "queued" ||
		m.Body != old.Body {
		e.t.Fatalf("gửi lại = %+v (tin cũ %+v)", res, old)
	}
	if r := e.row(m.ID); r.Kind != kind || r.Parent != th.commentExt || r.Conv != conv {
		e.t.Fatalf("dòng tin mới = %+v", r)
	}
	if got := e.message(old.ID); got.Status != "discarded" {
		e.t.Fatalf("tin cũ = %+v, muốn discarded", got)
	}
}

// Câu trả lời comment mất kết nối sau khi Meta đọc → checking, +61 s in_doubt; gửi lại → câu trả lời mới (kind comment,
// cùng comment cha), Meta nhận lần 2, tin mới sent, tin cũ discarded, lệnh cũ expired. Câu khác cũng kẹt → bỏ: trả đúng
// kind, comment cha; Meta không nhận thêm.
func TestRecoverCommentReply_InDoubt(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_d1", commenterID, time.Now().Add(-time.Hour))
	start := time.Now().UTC()
	e.clock.Freeze(start)
	e.fg.DropAfterRead(th.commentExt + "/comments")
	m, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	e.drain()
	e.checkReveal(m.ID, th.conv, start)

	res := e.mustResend(m.ID)
	e.checkRecovered(res, m, "comment", th, th.conv)
	e.drain()
	if n := e.replyCalls(th.commentExt); n != 2 {
		t.Fatalf("Meta nhận %d lần POST, muốn 2", n)
	}
	if r := e.row(res.Message.ID); r.Status != "sent" || r.ExternalID == "" {
		t.Fatalf("câu trả lời mới = %+v", r)
	}

	e.fg.DropAfterRead(th.commentExt + "/comments")
	other, err := e.reply(th, "Dạ chị chọn màu nào ạ")
	if err != nil {
		t.Fatal(err)
	}
	e.drain()
	d, err := e.discard(other.ID)
	if err != nil || d.Kind != "comment" || d.ParentMessageID == nil || *d.ParentMessageID != th.comment ||
		d.SendStatus != "discarded" || d.ConversationID != th.conv {
		t.Fatalf("bỏ câu trả lời checking: %+v, %v", d, err)
	}
	e.drain()
	if n := e.replyCalls(th.commentExt); n != 3 {
		t.Fatalf("sau khi bỏ Meta nhận %d lần POST, muốn 3", n)
	}
}

// Tin riêng mất kết nối → in_doubt sau 60 giây, comment ở trạng thái "đã nhắn riêng"; bỏ → trả kind private_reply,
// báo hiệu hội thoại comment, nhắn riêng mở lại (available) và nhắn riêng lần nữa được (Meta chưa nhận thì gửi được).
func TestRecoverPrivateReply_DiscardUnlocks(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_d2", commenterID, time.Now().Add(-time.Hour))
	m, inbox := e.privateInDoubt(th)
	if st := e.privateState(th); st.State != send.PrivateReplyUsed {
		t.Fatalf("tin riêng in_doubt: trạng thái = %+v, muốn used", st)
	}
	before := e.signals(th.conv)
	d, err := e.discard(m.ID)
	if err != nil || d.Kind != "private_reply" || d.ParentMessageID == nil || *d.ParentMessageID != th.comment ||
		d.ConversationID != inbox || d.SendStatus != "discarded" {
		t.Fatalf("bỏ tin riêng: %+v, %v", d, err)
	}
	if n := e.signals(th.conv); n != before+1 {
		t.Fatalf("bỏ tin riêng: %d báo hiệu hội thoại comment, muốn 1", n-before)
	}
	if st := e.privateState(th); st.State != send.PrivateReplyAvailable {
		t.Fatalf("sau khi bỏ: trạng thái = %+v, muốn available", st)
	}
	again := e.privateSent(th)
	if n := e.privateCalls(th.commentExt); n != 2 {
		t.Fatalf("Meta nhận %d lần nhắn riêng, muốn 2", n)
	}
	if st := e.privateState(th); st.State != send.PrivateReplyUsed || st.MessageID == nil || *st.MessageID != again.ID {
		t.Fatalf("sau khi nhắn lại: %+v", st)
	}
}

// Tin riêng in_doubt → gửi lại: tin riêng mới trong cùng hộp thư, cùng comment cha, mayDuplicate; Meta nhận lần 2,
// tin mới sent; trạng thái nhắn riêng trỏ tin mới. Gửi lại lần nữa tin cũ (đã discarded) → ErrNotRecoverable.
func TestRecoverPrivateReply_Resend(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_d3", commenterID, time.Now().Add(-time.Hour))
	m, inbox := e.privateInDoubt(th)
	res := e.mustResend(m.ID)
	e.checkRecovered(res, m, "private_reply", th, inbox)
	e.drain()
	if n := e.privateCalls(th.commentExt); n != 2 {
		t.Fatalf("Meta nhận %d lần nhắn riêng, muốn 2", n)
	}
	if r := e.row(res.Message.ID); r.Status != "sent" {
		t.Fatalf("tin riêng mới = %+v", r)
	}
	if st := e.privateState(th); st.State != send.PrivateReplyUsed || st.MessageID == nil || *st.MessageID != res.Message.ID {
		t.Fatalf("trạng thái nhắn riêng = %+v", st)
	}
	if _, err := e.resendAs(e.user, m.ID); err == nil {
		t.Fatal("gửi lại tin đã bỏ: muốn ErrNotRecoverable")
	}
}

// Tin tự động (tin chào, nhắn lại: EnqueueAuto, Automated; tự trả lời comment, tự nhắn riêng: QueueAutoCommentReply,
// QueueAutoPrivateReply) đẩy mốc tin cuối nhưng không đổi last_staff_message_at; tin của nhân viên thì đổi.
func TestAutomatedKeepsStaffMark(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_d4", commenterID, time.Now().Add(-time.Hour))
	acc := e.fbAccount()
	now := e.clock.Now()
	var private uuid.UUID
	if _, err := inTx(e, func(tx pgx.Tx) (uuid.UUID, error) {
		if _, err := send.EnqueueAuto(t.Context(), tx, e.d, acc, e.fbConv, send.AutoInput{Text: "Chào chị"}, now); err != nil {
			return uuid.Nil, err //nolint:wrapcheck // test
		}
		if _, err := send.Automated(t.Context(), tx, e.d, e.fbConv, "Shop nhắn lại chị ạ", now); err != nil {
			return uuid.Nil, err //nolint:wrapcheck // test
		}
		if _, err := send.QueueAutoCommentReply(t.Context(), tx, e.env(), th.comment, "Dạ shop inbox chị ạ", now); err != nil {
			return uuid.Nil, err //nolint:wrapcheck // test
		}
		return send.QueueAutoPrivateReply(t.Context(), tx, e.env(), th.comment, textPrivate, now)
	}); err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT conversation_id FROM conversation.messages WHERE kind = 'private_reply'`).Scan(&private)
	})
	for name, conv := range map[string]uuid.UUID{"hộp thư": e.fbConv, "comment": th.conv, "tin riêng": private} {
		if staff, last := e.staffMark(conv); staff != nil || last == nil {
			t.Errorf("%s sau tin tự động: mốc nhân viên %v, tin cuối %v; muốn nil, có", name, staff, last)
		}
	}
	e.mustSend(e.fbConv, send.Input{Text: textHello})
	if staff, _ := e.staffMark(e.fbConv); staff == nil {
		t.Error("tin nhân viên phải đặt mốc nhân viên")
	}
}

// Tin chào (tự động, có nút trả lời nhanh) chưa gửi được vì kênh bận (failed rate_limited) → nhân viên gửi lại: tin mới
// vẫn origin automation, cùng nút; Meta nhận tin kèm quick_replies; không đổi mốc tin nhân viên.
func TestResendGreetingKeepsQuickReplies(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	acc := e.fbAccount()
	in := send.AutoInput{Text: "Chào chị, chị cần gì ạ", QuickReplies: []channel.QuickReply{
		{Title: "Xem size", Payload: "SIZE"}, {Title: "Gặp nhân viên", Payload: "STAFF"},
	}}
	if _, err := inTx(e, func(tx pgx.Tx) (uuid.UUID, error) {
		return send.EnqueueAuto(t.Context(), tx, e.d, acc, e.fbConv, in, e.clock.Now())
	}); err != nil {
		t.Fatal(err)
	}
	var old uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `UPDATE conversation.messages SET send_status = 'failed', fail_reason = 'rate_limited'
			WHERE origin = 'automation' RETURNING id`).Scan(&old)
	})
	res := e.mustResend(old)
	if res.Message.Origin != "automation" || res.Message.Kind != "message" || res.Message.Body != in.Text || res.MayDuplicate {
		t.Fatalf("gửi lại tin chào = %+v", res)
	}
	var qr string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT coalesce(quick_replies::text, '') FROM conversation.messages WHERE id = $1`,
			res.Message.ID).Scan(&qr)
	})
	if qr == "" {
		t.Fatal("tin gửi lại mất nút trả lời nhanh")
	}
	e.drain()
	if r := e.row(res.Message.ID); r.Status != "sent" {
		t.Fatalf("tin gửi lại = %+v", r)
	}
	msg, _ := e.body(0)["message"].(map[string]any)
	if qrs, _ := msg["quick_replies"].([]any); len(qrs) != 2 {
		t.Fatalf("Meta nhận %v, muốn 2 nút trả lời nhanh", msg)
	}
	if staff, _ := e.staffMark(e.fbConv); staff != nil {
		t.Fatalf("gửi lại tin chào đổi mốc nhân viên: %v", staff)
	}
}
