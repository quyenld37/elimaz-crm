package send_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const textPrivate = "Dạ shop gửi chị bảng size và giá ạ"

// private nhắn riêng người comment của th bằng nhân viên của env.
func (e *env) private(th thread, text string) (send.Message, error) {
	e.t.Helper()
	return e.act(send.PrivateReply, th, text)
}

// privateState đọc trạng thái nhắn riêng của comment th (chi tiết hội thoại comment).
func (e *env) privateState(th thread) send.PrivateReplyState {
	e.t.Helper()
	st, err := inTx(e, func(tx pgx.Tx) (send.PrivateReplyState, error) {
		return send.PrivateReplyStatus(e.t.Context(), tx, e.d.Registry, th.comment, e.clock.Now())
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

// privateSent nhắn riêng comment th, chạy lệnh, đòi tin riêng sent; trả tin.
func (e *env) privateSent(th thread) send.Message {
	e.t.Helper()
	m, err := e.private(th, textPrivate)
	if err != nil {
		e.t.Fatal(err)
	}
	e.drain()
	if r := e.row(m.ID); r.Status != "sent" {
		e.t.Fatalf("tin riêng = %+v", r)
	}
	return m
}

// privateCalls là các lần máy Meta giả nhận nhắn riêng (POST {page}/messages có recipient.comment_id).
func (e *env) privateCalls(commentExt string) int {
	n := 0
	for _, c := range e.fbCalls() {
		var b struct {
			Recipient struct {
				CommentID string `json:"comment_id"`
			} `json:"recipient"`
		}
		if json.Unmarshal(c.Body, &b) == nil && b.Recipient.CommentID == commentExt {
			n++
		}
	}
	return n
}

// inboxOf trả các hội thoại hộp thư của khách cust trên Page Facebook của env: mã, khóa luồng.
func (e *env) inboxOf(cust uuid.UUID) map[uuid.UUID]string {
	e.t.Helper()
	out := map[uuid.UUID]string{}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT id, external_thread_ref FROM conversation.conversations
			WHERE channel_account_id = $1 AND kind = 'inbox' AND customer_id = $2`, e.fbPage, cust)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var ref string
			if err := rows.Scan(&id, &ref); err != nil {
				return err //nolint:wrapcheck // test
			}
			out[id] = ref
		}
		return rows.Err()
	})
	return out
}

// CH05: nhắn riêng khi khách chưa có hội thoại hộp thư → tạo hội thoại hộp thư (khóa luồng = mã người comment) của cùng
// khách; tin private_reply ở đó, comment cha = comment; Meta nhận đúng 1 nhắn riêng (recipient.comment_id, metadata =
// client_ref); tin sent; trạng thái nhắn riêng của comment: trước available (hạn 7 ngày), sau used.
func TestPrivateReply_CH05(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	at := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	th := e.commentThread(postExt+"_p1", commenterID, at)
	if st := e.privateState(th); st.State != send.PrivateReplyAvailable || st.At == nil || !st.At.Equal(at.Add(7*24*time.Hour)) {
		t.Fatalf("trước khi nhắn: %+v", st)
	}
	m, err := e.private(th, textPrivate)
	if err != nil {
		t.Fatal(err)
	}
	inbox := e.inboxOf(th.customer)
	if len(inbox) != 1 || inbox[m.ConversationID] != commenterID {
		t.Fatalf("hội thoại hộp thư = %v, tin ở %s", inbox, m.ConversationID)
	}
	if m.Kind != "private_reply" || m.ParentMessageID == nil || *m.ParentMessageID != th.comment {
		t.Fatalf("tin riêng = %+v", m)
	}
	if st := e.privateState(th); st.State != send.PrivateReplyUsed {
		t.Fatalf("sau khi bấm: %+v, muốn used", st)
	}
	e.drain()
	e.drain()
	if n := e.privateCalls(th.commentExt); n != 1 {
		t.Fatalf("Meta nhận %d lần nhắn riêng, muốn 1", n)
	}
	b := e.body(0)
	if msgBody, _ := b["message"].(map[string]any); msgBody["text"] != textPrivate || msgBody["metadata"] == "" {
		t.Fatalf("thân gửi Meta = %v", b)
	}
	r := e.row(m.ID)
	if r.Kind != "private_reply" || r.Parent != th.commentExt || r.Status != "sent" || r.ExternalID == "" {
		t.Fatalf("tin riêng sau khi gửi = %+v", r)
	}
	st := e.privateState(th)
	if st.State != send.PrivateReplyUsed || st.MessageID == nil || *st.MessageID != m.ID {
		t.Fatalf("sau khi gửi: %+v", st)
	}
	if n := e.signals(th.conv); n < 1 {
		t.Fatal("hội thoại comment phải có báo hiệu (nút nhắn riêng khóa)")
	}
}

// Khách đã có hội thoại hộp thư trên Page → tin riêng vào đúng hội thoại đó, không tạo thêm.
func TestPrivateReply_ExistingInbox(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_p2", commenterID, time.Now().Add(-time.Hour))
	var inbox uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id,
			kind, external_thread_ref, last_message_at) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'inbox',
			'7000000000000777', now()) RETURNING id`, e.fbPage, th.customer).Scan(&inbox)
	})
	m, err := e.private(th, textPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if m.ConversationID != inbox || len(e.inboxOf(th.customer)) != 1 {
		t.Fatalf("tin riêng ở %s, muốn %s; hộp thư %v", m.ConversationID, inbox, e.inboxOf(th.customer))
	}
}

// Lần hai → ErrPrivateReplyUsed (409 private_reply_used). 20 yêu cầu song song (khóa chống trùng khác nhau) → đúng 1
// thành công, Meta nhận đúng 1 lời gọi.
func TestPrivateReply_OnlyOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_p3", commenterID, time.Now().Add(-time.Hour))
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, used := 0, 0
	var other []error
	for range 20 {
		wg.Go(func() {
			_, err := e.private(th, textPrivate)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, send.ErrPrivateReplyUsed):
				used++
			default:
				other = append(other, err)
			}
		})
	}
	wg.Wait()
	if ok != 1 || used != 19 || len(other) != 0 {
		t.Fatalf("thành công %d, đã dùng %d, lỗi khác %v", ok, used, other)
	}
	e.drain()
	if n := e.privateCalls(th.commentExt); n != 1 {
		t.Fatalf("Meta nhận %d lần nhắn riêng, muốn 1", n)
	}
	if _, err := e.private(th, textPrivate); !errors.Is(err, send.ErrPrivateReplyUsed) {
		t.Fatalf("lần sau khi đã gửi: %v", err)
	}
}

// Comment tạo cách 7 ngày → send_not_allowed private_reply_expired; trạng thái expired.
func TestPrivateReply_Expired(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_p4", commenterID, time.Now().Add(-7*24*time.Hour-time.Minute))
	_, err := e.private(th, textPrivate)
	if na, ok := errors.AsType[*send.NotAllowedError](err); !ok || na.Reason != channel.DenyPrivateReplyExpired {
		t.Fatalf("lỗi = %v, muốn private_reply_expired", err)
	}
	if st := e.privateState(th); st.State != send.PrivateReplyExpired {
		t.Fatalf("trạng thái = %+v", st)
	}
	if n := len(e.inboxOf(th.customer)); n != 0 {
		t.Fatalf("bị từ chối mà vẫn tạo %d hội thoại hộp thư", n)
	}
}

// Meta trả 10900 (đã nhắn riêng ngoài CRM) → tin failed lý do private_reply_used; lần sau 409 private_reply_used.
func TestPrivateReply_Meta10900(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_p5", commenterID, time.Now().Add(-time.Hour))
	e.fg.FailNext(fbPageExt+"/messages", fakegraph.GraphErrorSpec{Code: 10900, Type: "OAuthException", Message: "Activity already replied to"})
	m, err := e.private(th, textPrivate)
	if err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r := e.row(m.ID); r.Status != "failed" || r.FailReason != string(channel.DenyPrivateReplyUsed) {
		t.Fatalf("tin sau 10900 = %+v", r)
	}
	if send.FailReasonText(string(channel.DenyPrivateReplyUsed)) == send.FailReasonText("mã-lạ") {
		t.Fatal("lý do private_reply_used không có câu tiếng Việt riêng")
	}
	if st := e.privateState(th); st.State != send.PrivateReplyUsed {
		t.Fatalf("trạng thái = %+v", st)
	}
	if _, err := e.private(th, textPrivate); !errors.Is(err, send.ErrPrivateReplyUsed) {
		t.Fatalf("lần sau: %v", err)
	}
}

// recipient_id khác mã người comment → khách có thêm danh tính fb_psid (Page, PSID), hội thoại hộp thư vừa dựng đổi
// khóa luồng sang PSID; PSID đã thuộc khách khác → không gộp, tin hệ thống gợi ý trong hội thoại hộp thư, tin vẫn sent.
func TestPrivateReply_RecipientIdentity(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const psidNew, psidOther = "7000000000000555", "7000000000000666"
	// Máy Meta giả trả recipient_id = from.id của comment trên Meta; CRM nhớ mã người comment khác (commenterID).
	th := e.commentThread(postExt+"_p6", commenterID, time.Now().Add(-time.Hour))
	e.fg.AddComment(fakegraph.Comment{ID: th.commentExt, ParentID: postExt, FromID: psidNew, Message: "x", Created: time.Now().UTC()})
	m := e.privateSent(th)
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1
		AND kind = 'fb_psid' AND scope = $2 AND value = $3 AND source = 'chat'`, th.customer, fbPageExt, psidNew); n != 1 {
		t.Fatalf("khách có %d danh tính fb_psid mới, muốn 1", n)
	}
	if ref := e.inboxOf(th.customer)[m.ConversationID]; ref != psidNew {
		t.Fatalf("khóa luồng hộp thư = %q, muốn PSID", ref)
	}

	// PSID đã thuộc khách khác.
	th2 := e.commentThread(postExt+"_p7", "6100000000000099", time.Now().Add(-time.Hour))
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `WITH c AS (INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Chị Khác', 'chi khac') RETURNING id, tenant_id)
			INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, scope, value, source)
			SELECT tenant_id, id, 'fb_psid', $1, $2, 'chat' FROM c`, fbPageExt, psidOther)
		return err //nolint:wrapcheck // test
	})
	e.fg.AddComment(fakegraph.Comment{ID: th2.commentExt, ParentID: postExt, FromID: psidOther, Message: "x", Created: time.Now().UTC()})
	m2 := e.privateSent(th2)
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1
		AND kind = 'fb_psid' AND value = $2`, th2.customer, psidOther); n != 0 {
		t.Fatal("PSID đã có chủ không được gắn cho khách thứ hai")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customer_merges`); n != 0 {
		t.Fatal("không được gộp khách")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE conversation_id = $1
		AND kind = 'system' AND origin = 'system'`, m2.ConversationID); n != 1 {
		t.Fatalf("có %d tin hệ thống gợi ý, muốn 1", n)
	}
}
