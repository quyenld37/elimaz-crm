package send_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

const (
	postExt     = fbPageExt + "_9001"
	commenterID = "6100000000000042"
	textReply   = "Dạ chị inbox shop để được tư vấn size nhé"
)

// thread là một hội thoại comment của khách commenterID dưới bài postExt trên Page Facebook của env, có một comment
// của khách mã commentExt (comment cũng có trên máy Meta giả).
type thread struct {
	conv, comment, customer uuid.UUID
	commentExt              string
}

// commentThread chèn (chủ bảng) khách, bài, hội thoại comment và comment commentExt của khách from, tạo lúc at.
func (e *env) commentThread(commentExt, from string, at time.Time) thread {
	e.t.Helper()
	th := thread{commentExt: commentExt}
	ctx := e.t.Context()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		var post uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			SELECT current_setting('app.tenant_id')::uuid, 'Chị Hoa', 'chi hoa' RETURNING id`).Scan(&th.customer); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2)
			ON CONFLICT (tenant_id, channel_account_id, external_id) DO UPDATE SET fetched_at = NULL RETURNING id`,
			e.fbPage, postExt).Scan(&post); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
			external_thread_ref, post_id, last_message_at, last_customer_message_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'comment', $3, $4, $5, $5) RETURNING id`,
			e.fbPage, th.customer, postExt+":"+from, post, at.UTC()).Scan(&th.conv); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
			external_id, direction, kind, origin, post_id, body, sender_name, sent_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, 'in', 'comment', 'customer', $4, 'Còn size M không shop',
			'Hoa', $5) RETURNING id`, th.conv, e.fbPage, commentExt, post, at.UTC()).Scan(&th.comment)
	})
	e.fg.AddComment(fakegraph.Comment{
		ID: commentExt, ParentID: postExt, FromID: from, FromName: "Hoa", Message: "Còn size M không shop", Created: at.UTC(),
	})
	return th
}

// reply trả lời comment của th bằng nhân viên của env.
func (e *env) reply(th thread, text string) (send.Message, error) {
	e.t.Helper()
	return e.act(send.ReplyToComment, th, text)
}

// commentAction là chữ ký chung của send.ReplyToComment, send.PrivateReply.
type commentAction func(ctx context.Context, tx pgx.Tx, env send.Env, u identity.AuthUser, conv uuid.UUID,
	in send.CommentInput, now time.Time) (send.Message, error)

// act chạy thao tác fn trên comment của th với chữ text bằng nhân viên của env, trong một transaction tenant.
func (e *env) act(fn commentAction, th thread, text string) (send.Message, error) {
	e.t.Helper()
	return inTx(e, func(tx pgx.Tx) (send.Message, error) {
		return fn(e.t.Context(), tx, e.env(), e.user, th.conv, send.CommentInput{CommentMessageID: th.comment, Text: text}, e.clock.Now())
	})
}

// row là một dòng tin đọc lại bằng chủ bảng: kiểu, hội thoại, comment cha, người gửi.
type row struct {
	Kind, Parent, Status, ExternalID, FailReason string
	Conv                                         uuid.UUID
	Staff                                        *uuid.UUID
	Hidden                                       bool
}

func (e *env) row(id uuid.UUID) row {
	e.t.Helper()
	var r row
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT kind, coalesce(parent_external_id, ''), coalesce(send_status, ''),
			coalesce(external_id, ''), coalesce(fail_reason, ''), conversation_id, sender_staff_id, is_hidden
			FROM conversation.messages WHERE id = $1`, id).
			Scan(&r.Kind, &r.Parent, &r.Status, &r.ExternalID, &r.FailReason, &r.Conv, &r.Staff, &r.Hidden)
	})
	return r
}

// handler trả lệnh của send mang tên kind.
func (e *env) handler(kind string) commands.Handler { //nolint:ireturn // lệnh là interface của commands
	e.t.Helper()
	for _, h := range send.Parts(e.d).Commands {
		if h.Kind().Name == kind {
			return h
		}
	}
	e.t.Fatalf("không có lệnh %s", kind)
	return nil
}

// replyCalls là các lần máy Meta giả nhận POST {comment}/comments.
func (e *env) replyCalls(commentExt string) int {
	n := 0
	for _, c := range e.fg.Calls(commentExt + "/comments") {
		if c.Method == "POST" {
			n++
		}
	}
	return n
}

// CH05: trả lời comment → queued trong hội thoại comment, kind comment, comment cha; worker → Meta nhận đúng 1
// POST /{comment}/comments với nội dung; tin sent mang mã comment trả lời, có sender_staff_id; chạy lại không gửi thêm.
func TestReplyToComment_CH05(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_c1", commenterID, time.Now().Add(-time.Hour))
	m, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != "comment" || m.SendStatus != "queued" || m.ParentMessageID == nil || *m.ParentMessageID != th.comment {
		t.Fatalf("tin trả lời = %+v", m)
	}
	e.drain()
	e.drain()
	if n := e.replyCalls(th.commentExt); n != 1 {
		t.Fatalf("Meta nhận %d lần trả lời, muốn 1", n)
	}
	var b map[string]any
	if err := json.Unmarshal(e.fg.Calls(th.commentExt + "/comments")[0].Body, &b); err != nil || b["message"] != textReply {
		t.Fatalf("thân gửi Meta = %s (%v)", e.fg.Calls(th.commentExt + "/comments")[0].Body, err)
	}
	r := e.row(m.ID)
	if r.Kind != "comment" || r.Parent != th.commentExt || r.Status != "sent" || r.ExternalID == "" || r.Conv != th.conv ||
		r.Staff == nil || *r.Staff != e.user.UserID {
		t.Fatalf("tin sau khi gửi = %+v", r)
	}
	if _, ok := e.fg.CommentByID(r.ExternalID); !ok {
		t.Fatalf("mã %s không phải comment trên Page", r.ExternalID)
	}
}

// Webhook feed của chính câu trả lời tới trước OnDone (inbound đã chèn dòng page_external cùng mã) → còn đúng 1 dòng
// mang mã đó, là dòng của CRM (origin staff, có sender_staff_id).
func TestReplyToComment_FeedEchoBeforeDone(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_c2", commenterID, time.Now().Add(-time.Hour))
	m, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	h := e.handler(send.KindReplyComment)
	raw, _ := json.Marshal(map[string]any{"messageId": m.ID})
	tenant := e.tenant
	job := queue.Job{ID: uuid.New(), Kind: send.KindReplyComment, TenantID: &tenant, Payload: raw}
	out, err := h.Execute(t.Context(), job)
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("Execute = %+v, %v", out, err)
	}
	var res struct {
		ExternalID string `json:"externalId"`
	}
	if err := json.Unmarshal(out.Result, &res); err != nil || res.ExternalID == "" {
		t.Fatalf("kết quả = %s", out.Result)
	}
	// Đường vào đã ghi comment của Page (webhook feed) trước khi lệnh ghi kết quả.
	e.exec(`INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id, direction,
		kind, origin, parent_external_id, body, sent_at)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, 'out', 'comment', 'page_external', $4, $5, now())`,
		th.conv, e.fbPage, res.ExternalID, th.commentExt, textReply)
	if n := e.rowsWithMID(e.fbPage, res.ExternalID); n != 1 {
		t.Fatalf("trước OnDone có %d dòng mang mã", n)
	}
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		return h.(commands.DoneHook).OnDone(t.Context(), job, out.Result, tx)
	}); err != nil {
		t.Fatal(err)
	}
	if n := e.rowsWithMID(e.fbPage, res.ExternalID); n != 1 {
		t.Fatalf("sau OnDone có %d dòng mang mã %s, muốn 1", n, res.ExternalID)
	}
	r := e.row(m.ID)
	if r.Status != "sent" || r.ExternalID != res.ExternalID || r.Staff == nil {
		t.Fatalf("dòng của CRM = %+v", r)
	}
	// Trigger của 0415: comment page_external chỉ xóa được khi transaction bật app.merge_reply; bật cờ cũng không xóa
	// được comment của khách.
	e.exec(`INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id, direction,
		kind, origin, parent_external_id, body, sent_at)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, 'out', 'comment', 'page_external', $4, 'Page trả lời', now())`,
		th.conv, e.fbPage, th.commentExt+"_page", th.commentExt)
	if err := e.tryDelete(th.commentExt+"_page", false); err == nil {
		t.Fatal("xóa comment page_external khi chưa bật cờ phải bị trigger chặn")
	}
	if err := e.tryDelete(th.commentExt, true); err == nil {
		t.Fatal("bật cờ vẫn phải chặn xóa comment của khách")
	}
	if err := e.tryDelete(th.commentExt+"_page", true); err != nil {
		t.Fatalf("bật cờ, comment page_external: %v", err)
	}
}

// tryDelete xóa (crm_app, trong tx tenant, lùi lại sau) dòng tin mang mã ngoài ext của Page Facebook, có bật cờ
// app.merge_reply hay không; trả lỗi của câu xóa.
func (e *env) tryDelete(ext string, flag bool) error {
	e.t.Helper()
	errDel := errors.New("lùi lại")
	var got error
	err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		if flag {
			if _, err := tx.Exec(e.t.Context(), `SELECT set_config('app.merge_reply', 'on', true)`); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		tag, err := tx.Exec(e.t.Context(), `DELETE FROM conversation.messages WHERE channel_account_id = $1 AND external_id = $2`,
			e.fbPage, ext)
		if err == nil && tag.RowsAffected() != 1 {
			err = fmt.Errorf("xóa %d dòng", tag.RowsAffected())
		}
		got = err
		return errDel
	})
	if !errors.Is(err, errDel) && got == nil {
		e.t.Fatalf("transaction thử xóa: %v", err)
	}
	return got
}

// Không rõ đã trả lời (mất kết nối sau khi Meta đọc yêu cầu) → in_doubt, tin checking; Meta có câu trả lời của Page cùng
// nội dung → tra lại ra done, tin sent mang mã đó, không POST lần hai.
func TestReplyToComment_InDoubtLookup(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_c3", commenterID, time.Now().Add(-time.Hour))
	e.fg.DropAfterRead(th.commentExt + "/comments")
	m, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r := e.row(m.ID); r.Status != "checking" {
		t.Fatalf("tin sau khi mất kết nối = %+v, muốn checking", r)
	}
	e.fg.AddComment(fakegraph.Comment{
		ID: th.commentExt + "_r9", ParentID: th.commentExt, FromID: fbPageExt, Message: textReply, Created: time.Now().UTC(),
	})
	e.resolveDone(1)
	if r := e.row(m.ID); r.Status != "sent" || r.ExternalID != th.commentExt+"_r9" {
		t.Fatalf("tin sau khi tra = %+v", r)
	}
	if n := e.replyCalls(th.commentExt); n != 1 {
		t.Fatalf("Meta nhận %d lần POST, muốn 1", n)
	}
}

// Câu trả lời cũ cùng chữ đã có trong CRM (dòng nhân viên mang mã đó) và Meta chỉ có câu đó → tra lại không khớp nhầm:
// lệnh giữ in_doubt, tin mới vẫn checking, không mang mã của câu cũ.
func TestReplyToComment_InDoubtOldReplySameText(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_c7", commenterID, time.Now().Add(-time.Hour))
	old, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	e.drain()
	oldExt := e.row(old.ID).ExternalID
	e.fg.DropAfterRead(th.commentExt + "/comments")
	m, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	e.drain()
	e.resolveDone(0)
	if r := e.row(m.ID); r.Status != "checking" || r.ExternalID != "" {
		t.Fatalf("tin mới sau khi tra = %+v (câu cũ %s)", r, oldExt)
	}
	if r := e.row(old.ID); r.ExternalID != oldExt || r.Status != "sent" {
		t.Fatalf("câu cũ = %+v", r)
	}
}

// Yêu cầu sai: chữ rỗng, quá 8 000 ký tự, comment của hội thoại khác, hội thoại hộp thư → lỗi, không ghi tin.
func TestReplyToComment_Invalid(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_c4", commenterID, time.Now().Add(-time.Hour))
	other := e.commentThread(postExt+"_c5", "6100000000000043", time.Now().Add(-time.Hour))
	long := make([]rune, 8001)
	for i := range long {
		long[i] = 'a'
	}
	for name, c := range map[string]struct {
		th   thread
		text string
		want error
	}{
		"rỗng":         {th, "  ", send.ErrInvalidInput},
		"quá dài":      {th, string(long), send.ErrInvalidInput},
		"comment khác": {thread{conv: th.conv, comment: other.comment}, textReply, send.ErrNotCustomerComment},
		"hộp thư":      {thread{conv: e.fbConv, comment: th.comment}, textReply, send.ErrInvalidInput},
	} {
		if _, err := e.reply(c.th, c.text); !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi = %v, muốn %v", name, err, c.want)
		}
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE origin = 'staff'`); n != 0 {
		t.Fatalf("có %d tin nhân viên", n)
	}
}

// Comment đã xóa → send_not_allowed comment_removed.
func TestReplyToComment_Removed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_c6", commenterID, time.Now().Add(-time.Hour))
	e.exec(`UPDATE conversation.messages SET removed_at = now() WHERE id = $1`, th.comment)
	_, err := e.reply(th, textReply)
	if na, ok := errors.AsType[*send.NotAllowedError](err); !ok || na.Reason != channel.DenyCommentRemoved {
		t.Fatalf("lỗi = %v, muốn comment_removed", err)
	}
}
