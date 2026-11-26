package send_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// hide ẩn (hidden) hay hiện comment id bằng nhân viên của env.
func (e *env) hide(id uuid.UUID, hidden bool) (send.Comment, error) {
	e.t.Helper()
	return inTx(e, func(tx pgx.Tx) (send.Comment, error) {
		return send.SetCommentHidden(e.t.Context(), tx, e.env(), e.user, id, hidden, e.clock.Now())
	})
}

// hideCalls là các lần máy Meta giả nhận POST {comment} (ẩn/hiện).
func (e *env) hideCalls(commentExt string) int {
	n := 0
	for _, c := range e.fg.Calls(commentExt) {
		if c.Method == "POST" {
			n++
		}
	}
	return n
}

// revisions đếm dòng lịch sử nguồn staff của comment id với trạng thái ẩn hidden.
func (e *env) revisions(id uuid.UUID, hidden bool) int {
	return e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM conversation.message_revisions WHERE message_id = $1
		AND source = 'staff' AND is_hidden = $2`, id, hidden)
}

// CH11 phần tay: ẩn → Meta is_hidden = true (1 POST); tin is_hidden = true, vẫn là dòng của hội thoại (danh sách tin vẫn
// hiện), 1 dòng lịch sử staff, báo hiệu; hiện lại → is_hidden = false trên Meta và trong CRM.
func TestHideComment_CH11(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_h1", commenterID, time.Now().Add(-time.Hour))
	c, err := e.hide(th.comment, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != th.comment || c.ConversationID != th.conv || c.IsHidden || c.Origin != "customer" {
		t.Fatalf("comment trả về = %+v", c)
	}
	e.drain()
	if fc, _ := e.fg.CommentByID(th.commentExt); !fc.Hidden || e.hideCalls(th.commentExt) != 1 {
		t.Fatalf("Meta: ẩn = %v, %d lần POST", fc.Hidden, e.hideCalls(th.commentExt))
	}
	if r := e.row(th.comment); !r.Hidden || r.Conv != th.conv {
		t.Fatalf("comment trong CRM = %+v", r)
	}
	if n := e.revisions(th.comment, true); n != 1 {
		t.Fatalf("lịch sử ẩn: %d", n)
	}
	if n := e.signals(th.conv); n < 1 {
		t.Fatal("thiếu báo hiệu hội thoại")
	}
	if _, err := e.hide(th.comment, false); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if fc, _ := e.fg.CommentByID(th.commentExt); fc.Hidden {
		t.Fatal("Meta vẫn ẩn sau khi hiện lại")
	}
	if r := e.row(th.comment); r.Hidden || e.revisions(th.comment, false) != 1 {
		t.Fatalf("sau khi hiện lại = %+v", r)
	}
}

// Webhook ẩn tới trước khi lệnh ghi kết quả (comment đã is_hidden trong CRM) → OnDone không ghi thêm lịch sử.
func TestHideComment_AlreadyHidden(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_h2", commenterID, time.Now().Add(-time.Hour))
	if _, err := e.hide(th.comment, true); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE conversation.messages SET is_hidden = true WHERE id = $1`, th.comment)
	e.drain()
	if n := e.revisions(th.comment, true); n != 0 {
		t.Fatalf("lịch sử staff ghi trùng: %d", n)
	}
}

// Mất kết nối sau khi Meta đọc yêu cầu ẩn → in_doubt; Meta đã ẩn → tra lại ra done, CRM is_hidden, không POST lần hai.
func TestHideComment_InDoubtLookup(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_h3", commenterID, time.Now().Add(-time.Hour))
	e.fg.DropAfterRead(th.commentExt)
	if _, err := e.hide(th.comment, true); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r := e.row(th.comment); r.Hidden {
		t.Fatal("chưa rõ đã ẩn mà CRM đã ghi ẩn")
	}
	e.fg.AddComment(fakegraph.Comment{
		ID: th.commentExt, ParentID: postExt, FromID: commenterID, Message: "Còn size M không shop", Hidden: true,
		Created: time.Now().Add(-time.Hour).UTC(),
	})
	e.resolveDone(1)
	if r := e.row(th.comment); !r.Hidden {
		t.Fatal("tra thấy đã ẩn mà CRM chưa ghi")
	}
	if n := e.hideCalls(th.commentExt); n != 1 {
		t.Fatalf("Meta nhận %d lần POST, muốn 1", n)
	}
}

// Tin không phải comment của khách (tin hộp thư, câu trả lời của Page) → ErrNotFound / ErrNotCustomerComment; comment đã
// xóa → comment_removed; không có quyền trên Page → ErrNotFound.
func TestHideComment_Invalid(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_h4", commenterID, time.Now().Add(-time.Hour))
	inbox := e.mustSend(e.fbConv, send.Input{Text: textHello})
	if _, err := e.hide(inbox.ID, true); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("tin hộp thư: %v", err)
	}
	reply, err := e.reply(th, textReply)
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE conversation.messages SET external_id = 'x_r1' WHERE id = $1`, reply.ID)
	if _, err := e.hide(reply.ID, true); !errors.Is(err, send.ErrNotCustomerComment) {
		t.Errorf("câu trả lời của Page: %v", err)
	}
	e.exec(`UPDATE conversation.messages SET removed_at = now() WHERE id = $1`, th.comment)
	if _, err := e.hide(th.comment, true); !errors.As(err, new(*send.NotAllowedError)) {
		t.Errorf("comment đã xóa: %v", err)
	}
	e.user.Grants = nil
	if _, err := e.hide(th.comment, true); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("không quyền: %v", err)
	}
}

// Page chỉ xem → trả lời, nhắn riêng, ẩn đều ErrReadOnly (409 channel_read_only), không ghi tin, không ghi lệnh.
func TestCommentActions_ReadOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	th := e.commentThread(postExt+"_h5", commenterID, time.Now().Add(-time.Hour))
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.fbPage)
	if _, err := e.reply(th, textReply); !errors.Is(err, send.ErrReadOnly) {
		t.Errorf("trả lời: %v", err)
	}
	if _, err := e.private(th, textPrivate); !errors.Is(err, send.ErrReadOnly) {
		t.Errorf("nhắn riêng: %v", err)
	}
	if _, err := e.hide(th.comment, true); !errors.Is(err, send.ErrReadOnly) {
		t.Errorf("ẩn: %v", err)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages WHERE origin = 'staff'`); n != 0 {
		t.Errorf("có %d tin nhân viên", n)
	}
	var jobs int
	if err := e.staff.Owner.Pool.QueryRow(t.Context(), `SELECT count(*) FROM platform.jobs WHERE kind = ANY($1)`,
		[]string{send.KindReplyComment, send.KindPrivateReply, send.KindHideComment}).Scan(&jobs); err != nil || jobs != 0 {
		t.Errorf("có %d lệnh (%v)", jobs, err)
	}
}
