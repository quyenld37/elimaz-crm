package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// commentAPIEnv là newSendAPIEnv có thêm hội thoại comment conv trên Page A (người comment "u-42" dưới bài "post-a") với
// comment của khách comment (tạo lúc at) và comment của khách khác ở hội thoại khác other.
type commentAPIEnv struct {
	*chatAPIEnv
	conv, comment, other uuid.UUID
}

func newCommentAPIEnv(t *testing.T, at time.Time) *commentAPIEnv {
	t.Helper()
	e := &commentAPIEnv{chatAPIEnv: newSendAPIEnv(t)}
	ctx := t.Context()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		var post uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'post-a') RETURNING id`, e.pageA).Scan(&post); err != nil {
			return err //nolint:wrapcheck // test
		}
		for i, out := range []struct{ conv, msg *uuid.UUID }{{&e.conv, &e.comment}, {new(uuid.UUID), &e.other}} {
			if err := tx.QueryRow(ctx, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id,
				kind, external_thread_ref, post_id, last_message_at) VALUES (current_setting('app.tenant_id')::uuid, $1, $2,
				'comment', $3, $4, $5) RETURNING id`, e.pageA, e.ka, fmt.Sprintf("post-a:u-4%d", 2+i), post, at).Scan(out.conv); err != nil {
				return err //nolint:wrapcheck // test
			}
			if err := tx.QueryRow(ctx, `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
				external_id, direction, kind, origin, post_id, body, sent_at) VALUES (current_setting('app.tenant_id')::uuid,
				$1, $2, $3, 'in', 'comment', 'customer', $4, 'Còn size M không', $5) RETURNING id`,
				*out.conv, e.pageA, fmt.Sprintf("post-a_c%d", i), post, at).Scan(out.msg); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return e
}

// jobs đếm lệnh loại kind (platform.jobs không có RLS).
func (e *commentAPIEnv) jobs(kind string) int {
	e.t.Helper()
	var n int
	if err := e.staff.Owner.Pool.QueryRow(e.t.Context(), `SELECT count(*) FROM platform.jobs WHERE kind = $1`, kind).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *commentAPIEnv) path(edge string) string {
	return "/api/v1/conversations/" + e.conv.String() + "/" + edge
}

func (e *commentAPIEnv) body(id uuid.UUID, text string) string {
	return fmt.Sprintf(`{"commentMessageId":%q,"text":%q}`, id, text)
}

// CH05 qua HTTP: trả lời → 201 kind comment, comment cha, 1 lệnh trả lời; nhắn riêng → 201 kind private_reply trong
// hội thoại hộp thư có sẵn của khách (convA, không tạo thêm), 1 lệnh nhắn riêng; lần hai → 409 private_reply_used,
// không thêm lệnh.
func TestCommentActionsAPI_CH05(t *testing.T) {
	t.Parallel()
	e := newCommentAPIEnv(t, time.Now().Add(-time.Hour))
	got := mustOK(t, "replyToComment", e.send(http.MethodPost, e.path("comment-replies"), chatSaleA,
		e.body(e.comment, "Dạ còn ạ")), http.StatusCreated)
	if got["kind"] != "comment" || got["parentMessageId"] != e.comment.String() || got["conversationId"] != e.conv.String() ||
		got["sendStatus"] != "queued" || got["origin"] != "staff" {
		t.Fatalf("câu trả lời = %v", got)
	}
	if n := e.jobs(conversation.ReplyCommentKind); n != 1 {
		t.Fatalf("%d lệnh trả lời", n)
	}
	got = mustOK(t, "sendPrivateReply", e.send(http.MethodPost, e.path("private-replies"), chatSaleA,
		e.body(e.comment, "Shop gửi chị bảng size")), http.StatusCreated)
	if got["kind"] != "private_reply" || got["parentMessageId"] != e.comment.String() || got["conversationId"] != e.convA.String() {
		t.Fatalf("tin riêng = %v", got)
	}
	if n := e.count(`SELECT count(*) FROM conversation.conversations WHERE kind = 'inbox' AND customer_id = $1`, e.ka); n != 1 {
		t.Fatalf("khách có %d hội thoại hộp thư, muốn 1", n)
	}
	expect(t, "lần hai", e.send(http.MethodPost, e.path("private-replies"), chatSaleA, e.body(e.comment, "Lần hai")),
		http.StatusConflict, "private_reply_used")
	if n := e.jobs(conversation.PrivateReplyKind); n != 1 {
		t.Fatalf("%d lệnh nhắn riêng", n)
	}
}

// 20 yêu cầu nhắn riêng song song cùng comment, khóa chống trùng khác nhau → đúng 1 lần 201, còn lại 409
// private_reply_used; 1 tin riêng, 1 lệnh.
func TestCommentActionsAPI_PrivateReplyRace(t *testing.T) {
	t.Parallel()
	e := newCommentAPIEnv(t, time.Now().Add(-time.Hour))
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for range 20 {
		wg.Go(func() {
			rec := serveAs(e.srv, e.keys, http.MethodPost, e.path("private-replies"), chatSaleA, uuid.NewString(),
				e.body(e.comment, "Shop gửi chị bảng size"))
			mu.Lock()
			defer mu.Unlock()
			codes[rec.Code]++
			if rec.Code == http.StatusConflict {
				var b map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || b["error"] != "private_reply_used" {
					t.Errorf("409 lạ: %s", rec.Body.String())
				}
			}
		})
	}
	wg.Wait()
	if codes[http.StatusCreated] != 1 || codes[http.StatusConflict] != 19 {
		t.Fatalf("mã trả về %v", codes)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE kind = 'private_reply'`); n != 1 {
		t.Fatalf("%d tin riêng", n)
	}
	if n := e.jobs(conversation.PrivateReplyKind); n != 1 {
		t.Fatalf("%d lệnh nhắn riêng", n)
	}
}

// Ẩn → 202 comment của khách (isHidden còn false tới khi lệnh xong), 1 lệnh ẩn; hiện → 202, thêm 1 lệnh.
func TestCommentActionsAPI_Hide(t *testing.T) {
	t.Parallel()
	e := newCommentAPIEnv(t, time.Now().Add(-time.Hour))
	got := mustOK(t, "hideComment", e.send(http.MethodPost, "/api/v1/messages/"+e.comment.String()+"/hide", chatSaleA, ""),
		http.StatusAccepted)
	if got["id"] != e.comment.String() || got["kind"] != "comment" || got["origin"] != "customer" ||
		got["direction"] != "in" || got["isHidden"] != false || got["body"] != "Còn size M không" {
		t.Fatalf("comment = %v", got)
	}
	mustOK(t, "unhideComment", e.send(http.MethodPost, "/api/v1/messages/"+e.comment.String()+"/unhide", chatSaleA, ""),
		http.StatusAccepted)
	if n := e.jobs(conversation.HideCommentKind); n != 2 {
		t.Fatalf("%d lệnh ẩn/hiện, muốn 2", n)
	}
}

// Ca từ chối, không ghi tin, không ghi lệnh: comment của hội thoại khác → 422; hội thoại Page B → 404; tin hộp thư → ẩn
// 404; comment cách 7 ngày → nhắn riêng 409 send_not_allowed private_reply_expired; Page chỉ xem → cả bốn 409
// channel_read_only.
func TestCommentActionsAPI_Refusals(t *testing.T) {
	t.Parallel()
	e := newCommentAPIEnv(t, time.Now().Add(-7*24*time.Hour-time.Hour))
	expect(t, "comment khác", e.send(http.MethodPost, e.path("comment-replies"), chatSaleA, e.body(e.other, "x")),
		http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "Page B", e.send(http.MethodPost, "/api/v1/conversations/"+e.convB.String()+"/comment-replies", chatSaleA,
		e.body(e.comment, "x")), http.StatusNotFound, "not_found")
	var inboxMsg uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
			external_id, direction, kind, origin, body, sent_at) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'm-1',
			'in', 'message', 'customer', 'chào', now()) RETURNING id`, e.convA, e.pageA).Scan(&inboxMsg)
	})
	expect(t, "tin hộp thư", e.send(http.MethodPost, "/api/v1/messages/"+inboxMsg.String()+"/hide", chatSaleA, ""),
		http.StatusNotFound, "not_found")
	rec := e.send(http.MethodPost, e.path("private-replies"), chatSaleA, e.body(e.comment, "x"))
	var b map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || rec.Code != http.StatusConflict ||
		b["error"] != "send_not_allowed" || b["denyReason"] != "private_reply_expired" {
		t.Fatalf("quá 7 ngày: %d %s", rec.Code, rec.Body.String())
	}
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.pageA)
	for name, c := range map[string][2]string{
		"trả lời":    {e.path("comment-replies"), e.body(e.comment, "x")},
		"nhắn riêng": {e.path("private-replies"), e.body(e.comment, "x")},
		"ẩn":         {"/api/v1/messages/" + e.comment.String() + "/hide", ""},
		"hiện":       {"/api/v1/messages/" + e.comment.String() + "/unhide", ""},
	} {
		expect(t, name, e.send(http.MethodPost, c[0], chatSaleA, c[1]), http.StatusConflict, "channel_read_only")
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE origin = 'staff'`); n != 0 {
		t.Fatalf("ca từ chối đã ghi %d tin", n)
	}
	for _, k := range []string{conversation.ReplyCommentKind, conversation.PrivateReplyKind, conversation.HideCommentKind} {
		if n := e.jobs(k); n != 0 {
			t.Fatalf("ca từ chối đã ghi %d lệnh %s", n, k)
		}
	}
}
