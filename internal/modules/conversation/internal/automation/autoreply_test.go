package automation_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
)

// replies là ba câu trả lời công khai của các test tự trả lời.
var replies = []string{"Dạ shop inbox chị rồi ạ", "Shop đã nhắn riêng, chị xem hộp thư nhé", "Dạ chị check tin nhắn giúp shop ạ"} //nolint:gochecknoglobals // dữ liệu thử chỉ đọc

const privateText = "Dạ shop gửi chị bảng giá và size ạ"

// replyCfg là cấu hình auto_reply_comment mặc định của test: ba câu, tin riêng chữ, mọi bài, 24 giờ, không live.
func replyCfg() automation.ReplyConfig {
	return automation.ReplyConfig{
		Kind: automation.KindAutoReplyComment, Replies: replies,
		PrivateMessage: &automation.PrivateMessageConfig{Text: privateText},
		PostIDs:        []string{}, ExceptPostIDs: []string{}, PerUserPerPostHours: 24,
	}
}

// saveReply lưu quy tắc auto_reply_comment (đọc version hiện tại); trả lỗi của SaveRule.
func (e *env) saveReply(enabled bool, c automation.ReplyConfig) error {
	e.t.Helper()
	raw, err := json.Marshal(c)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		cur, err := automation.LoadRule(e.t.Context(), tx, e.page, automation.KindAutoReplyComment)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, _, err = automation.SaveRule(e.t.Context(), tx, e.page, automation.KindAutoReplyComment, enabled, raw, cur.Version, e.user)
		return err //nolint:wrapcheck // test
	})
}

func (e *env) setReply(enabled bool, c automation.ReplyConfig) {
	e.t.Helper()
	if err := e.saveReply(enabled, c); err != nil {
		e.t.Fatal(err)
	}
}

// outcomeOf là kết quả và số dòng lượt có external_ref ref.
func (e *env) outcomeOf(ref string) (string, int) {
	e.t.Helper()
	var out string
	var n int
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(max(outcome), ''), count(*) FROM conversation.automation_runs
			WHERE external_ref = $1`, ref).Scan(&out, &n)
	})
	return out, n
}

// both đòi lượt trả lời công khai và nhắn riêng của comment id có kết quả want.
func (e *env) both(id string, want automation.Outcome) {
	e.t.Helper()
	for _, ref := range []string{"reply:" + id, "pm:" + id} {
		if out, n := e.outcomeOf(ref); out != string(want) || n != 1 {
			e.t.Fatalf("lượt %s = %q (%d dòng), muốn %q", ref, out, n, want)
		}
	}
}

// noRuns đòi comment id không có lượt tự trả lời nào.
func (e *env) noRuns(id string) {
	e.t.Helper()
	for _, ref := range []string{"reply:" + id, "pm:" + id} {
		if out, n := e.outcomeOf(ref); n != 0 {
			e.t.Fatalf("lượt %s = %q, muốn không có", ref, out)
		}
	}
}

// replyCalls là các câu Page trả lời công khai comment id mà máy Meta giả nhận.
func (e *env) replyCalls(id string) []string {
	var out []string
	for _, c := range e.fg.Calls(id + "/comments") {
		if c.Method != "POST" {
			continue
		}
		var b struct {
			Message string `json:"message"`
		}
		msg := c.Query.Get("message")
		if msg == "" && json.Unmarshal(c.Body, &b) == nil {
			msg = b.Message
		}
		out = append(out, msg)
	}
	return out
}

// privateCalls là số lần máy Meta giả nhận nhắn riêng comment id (POST {page}/messages có recipient.comment_id).
func (e *env) privateCalls(id string) int {
	n := 0
	for _, c := range e.fg.Calls(pageExt + "/messages") {
		var b struct {
			Recipient struct {
				CommentID string `json:"comment_id"`
			} `json:"recipient"`
		}
		if c.Method == "POST" && json.Unmarshal(c.Body, &b) == nil && b.Recipient.CommentID == id {
			n++
		}
	}
	return n
}

// autoMessages là các tin origin automation có comment cha id: kind → (trạng thái, kind hội thoại, nội dung).
type autoMsg struct{ status, convKind, body string }

func (e *env) autoMessages(id string) map[string][]autoMsg {
	e.t.Helper()
	out := map[string][]autoMsg{}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), `SELECT m.kind, m.send_status, c.kind, m.body FROM conversation.messages m
			JOIN conversation.conversations c ON c.id = m.conversation_id
			WHERE m.origin = 'automation' AND m.parent_external_id = $1`, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var kind string
			var m autoMsg
			if err := rows.Scan(&kind, &m.status, &m.convKind, &m.body); err != nil {
				return err //nolint:wrapcheck // test
			}
			out[kind] = append(out[kind], m)
		}
		return rows.Err()
	})
	return out
}

// backdate lùi created_at của mọi lượt có external_ref ref d.
func (e *env) backdate(ref string, d time.Duration) {
	e.t.Helper()
	e.sqlExec(`UPDATE conversation.automation_runs SET created_at = created_at - $2::interval WHERE external_ref = $1`,
		ref, d.String())
}

// sqlExec chạy câu q trong tx tenant A (lỗi dừng test).
func (e *env) sqlExec(q string, args ...any) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(e.t.Context(), q, args...); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
		return nil
	})
}

// commentRow là mã tin và hội thoại comment của comment ngoài id.
func (e *env) commentRow(id string) (msgID, convID uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, conversation_id FROM conversation.messages WHERE external_id = $1`, id).
			Scan(&msgID, &convID)
	})
	return msgID, convID
}

// staffPrivate nhắn riêng comment id bằng một nhân viên có conversation.reply trên Page.
func (e *env) staffPrivate(id string) error {
	msgID, convID := e.commentRow(id)
	u := identity.AuthUser{UserID: e.user, TenantID: e.tenant, Grants: []identity.Grant{
		{Permission: "conversation.reply", Scope: identity.Scope{Type: identity.ScopePage, ID: e.page.String()}},
	}}
	return e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) error { //nolint:wrapcheck // test
		_, err := send.PrivateReply(e.t.Context(), tx, send.Env{Registry: e.d.Registry, Queue: e.d.Queue, Bus: e.d.Bus}, u, convID,
			send.CommentInput{CommentMessageID: msgID, Text: "Dạ nhân viên shop nhắn chị ạ"}, time.Now().UTC())
		return err //nolint:wrapcheck // test
	})
}

// wantReply là câu xoay vòng tính độc lập trong test: FNV-1a 32 bit của mã comment, chia lấy dư số câu.
func wantReply(id string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return replies[int(h.Sum32()%3)] // ba câu
}

// CH12, 01 mục 6: bật với 3 câu và tin riêng: comment c1 → 1 trả lời công khai (đúng câu tính trước), 1 tin riêng
// (trong hội thoại hộp thư), cả hai origin automation, sent; lượt done. Xử lý lại c1 ba lần (webhook, đọc định kỳ, hỏi
// bù) → vẫn 1 + 1.
func TestAutoReply_OncePerComment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setReply(true, replyCfg())
	c1 := e.comment(postA, userA, "giá bao nhiêu shop", time.Now().UTC().Add(-time.Minute))
	e.deliver(c1)
	e.drain() // việc chuẩn bị tin riêng → lệnh nhắn riêng
	if got := e.replyCalls(c1.CommentID); len(got) != 1 || got[0] != wantReply(c1.CommentID) {
		t.Fatalf("trả lời công khai: %q, muốn đúng một %q", got, wantReply(c1.CommentID))
	}
	if n := e.privateCalls(c1.CommentID); n != 1 {
		t.Fatalf("nhắn riêng: %d lần gọi Meta", n)
	}
	msgs := e.autoMessages(c1.CommentID)
	if r := msgs["comment"]; len(r) != 1 || r[0].status != "sent" || r[0].convKind != "comment" || r[0].body != wantReply(c1.CommentID) {
		t.Fatalf("tin trả lời tự động: %+v", r)
	}
	if r := msgs["private_reply"]; len(r) != 1 || r[0].status != "sent" || r[0].convKind != "inbox" || r[0].body != privateText {
		t.Fatalf("tin riêng tự động: %+v", r)
	}
	e.both(c1.CommentID, automation.OutcomeDone)
	for _, via := range []string{webhooks.ViaWebhook, webhooks.ViaCommentPoll, webhooks.ViaBackfill} {
		e.enqueue(c1, via)
	}
	e.drain()
	e.drain()
	if len(e.replyCalls(c1.CommentID)) != 1 || e.privateCalls(c1.CommentID) != 1 {
		t.Fatalf("xử lý lại: %d trả lời, %d tin riêng", len(e.replyCalls(c1.CommentID)), e.privateCalls(c1.CommentID))
	}
	e.both(c1.CommentID, automation.OutcomeDone)
}

// Cùng người, cùng bài: comment thứ hai 2 giờ sau → cả hai skipped_limit; 25 giờ sau → chạy; khác bài → chạy.
func TestAutoReply_PerUserPerPost(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setReply(true, replyCfg())
	now := time.Now().UTC().Add(-time.Minute)
	c1 := e.comment(postA, userA, "còn size M không", now)
	e.deliver(c1)
	e.drain()
	e.both(c1.CommentID, automation.OutcomeDone)
	e.backdate("reply:"+c1.CommentID, 2*time.Hour)
	e.backdate("pm:"+c1.CommentID, 2*time.Hour)
	c2 := e.comment(postA, userA, "shop ơi", now)
	e.deliver(c2)
	e.drain()
	e.both(c2.CommentID, automation.OutcomeSkippedLimit)
	if len(e.replyCalls(c2.CommentID)) != 0 || e.privateCalls(c2.CommentID) != 0 {
		t.Fatal("comment trong giới hạn vẫn được gửi")
	}
	e.backdate("reply:"+c1.CommentID, 23*time.Hour)
	e.backdate("pm:"+c1.CommentID, 23*time.Hour)
	c3 := e.comment(postA, userA, "shop ơi trả lời em", now)
	e.deliver(c3)
	e.drain()
	e.both(c3.CommentID, automation.OutcomeDone)
	d1 := e.comment(postB, userA, "bài này còn không", now)
	e.deliver(d1)
	e.drain()
	e.both(d1.CommentID, automation.OutcomeDone)
}

// Comment live, includeLive = false → skipped_live; bật includeLive → chạy.
func TestAutoReply_Live(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.sqlExec(`INSERT INTO conversation.live_sessions (tenant_id, channel_account_id, live_video_id, post_external_id, status,
		started_at, detected_by) VALUES ($1, $2, '990000000000166', $3, 'live', now(), 'manual')`, e.tenant, e.page, postB)
	e.setReply(true, replyCfg())
	now := time.Now().UTC().Add(-time.Minute)
	c := e.comment(postB, userA, "chốt", now)
	e.deliver(c)
	e.drain()
	e.both(c.CommentID, automation.OutcomeSkippedLive)
	cfg := replyCfg()
	cfg.IncludeLive = true
	e.setReply(true, cfg)
	c2 := e.comment(postB, userA, "chốt màu đen", now)
	e.deliver(c2)
	e.drain()
	e.both(c2.CommentID, automation.OutcomeDone)
}

// holdPrepare là việc chuẩn bị giả: luôn lỗi (việc hẹn chạy lại sau vài giây), để đường vào chạy xong mà việc chuẩn bị
// tin riêng tự động chưa chạy.
type holdPrepare struct{}

func (holdPrepare) Kind() queue.Kind {
	return queue.Kind{Name: automation.KindPrepareAutoPrivate, Policy: automation.Parts(deps.Deps{}).Jobs[0].Kind().Policy}
}

func (holdPrepare) Handle(context.Context, queue.Job, pgx.Tx) (json.RawMessage, error) {
	return nil, errors.New("giữ lại trong test")
}

// inboundOnly chạy đường vào tới hết việc tới hạn, giữ việc chuẩn bị tin riêng tự động lại (holdPrepare); release đưa
// việc đó về tới hạn ngay.
func (e *env) inboundOnly() (release func()) {
	e.t.Helper()
	hs := append(e.inboundHandlers(), holdPrepare{})
	testkit.DrainDue(e.t, e.staff.Owner, queue.NewWorker(e.staff.App, hs, queue.WorkerOptions{
		WorkerID: "in-only", Logger: slog.New(slog.DiscardHandler),
	}))
	return func() {
		if _, err := e.staff.Owner.Pool.Exec(e.t.Context(), `UPDATE platform.jobs SET run_at = now() WHERE kind = $1
			AND status = 'pending'`, automation.KindPrepareAutoPrivate); err != nil {
			e.t.Fatal(err)
		}
	}
}

// Nhân viên nhắn riêng c2 trước khi việc chuẩn bị tin riêng tự động chạy → lượt tự động skipped_policy (trả lời công
// khai vẫn chạy), 1 tin riêng tới Meta; nhân viên và tự động cùng lúc cho c3 (hai worker) → đúng 1 tin riêng tới Meta.
func TestAutoReply_StaffPrivateReply(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setReply(true, replyCfg())
	now := time.Now().UTC().Add(-time.Minute)
	c2 := e.comment(postA, userA, "giá sao shop", now)
	e.enqueue(c2, webhooks.ViaWebhook)
	release := e.inboundOnly()
	if err := e.staffPrivate(c2.CommentID); err != nil {
		t.Fatal(err)
	}
	release()
	e.drain()
	e.drain()
	if out, _ := e.outcomeOf("pm:" + c2.CommentID); out != string(automation.OutcomeSkippedPolicy) {
		var st, le string
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT status, coalesce(last_error,'') FROM platform.jobs WHERE kind = $1`, automation.KindPrepareAutoPrivate).Scan(&st, &le)
		})
		t.Fatalf("tin riêng sau nhân viên: %q (việc %s %s)", out, st, le)
	}
	if out, _ := e.outcomeOf("reply:" + c2.CommentID); out != string(automation.OutcomeDone) {
		t.Fatalf("trả lời công khai: %q", out)
	}
	if n := e.privateCalls(c2.CommentID); n != 1 {
		t.Fatalf("c2: %d tin riêng tới Meta", n)
	}
	if r := e.autoMessages(c2.CommentID)["private_reply"]; len(r) != 0 {
		t.Fatalf("c2: có tin riêng tự động %+v", r)
	}

	c3 := e.comment(postB, userA, "giá sao shop", now)
	e.enqueue(c3, webhooks.ViaWebhook)
	e.inboundOnly()()
	w2 := e.newWorker("w-2")
	var wg sync.WaitGroup
	var staffErr error
	wg.Go(func() { staffErr = e.staffPrivate(c3.CommentID) })
	wg.Go(func() { testkit.DrainDue(t, e.staff.Owner, e.worker) })
	wg.Go(func() { testkit.DrainDue(t, e.staff.Owner, w2) })
	wg.Wait()
	if staffErr != nil && !errors.Is(staffErr, send.ErrPrivateReplyUsed) {
		t.Fatal(staffErr)
	}
	e.drain()
	e.drain()
	if n := e.privateCalls(c3.CommentID); n != 1 {
		t.Fatalf("c3: %d tin riêng tới Meta, muốn 1", n)
	}
	out, _ := e.outcomeOf("pm:" + c3.CommentID)
	if (staffErr == nil) != (out == string(automation.OutcomeSkippedPolicy)) {
		t.Fatalf("c3: nhân viên lỗi %v, lượt tự động %q", staffErr, out)
	}
}

// Khách do_not_contact → cả hai skipped_dnc; Page read_only → skipped_read_only; bài trong exceptPostIds hay ngoài postIds
// → không lượt; comment của Page → không lượt.
func TestAutoReply_Gates(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	now := time.Now().UTC().Add(-time.Minute)
	cfg := replyCfg()
	cfg.ExceptPostIDs = []string{postB}
	e.setReply(true, cfg)
	b := e.comment(postB, userA, "giá", now)
	e.deliver(b)
	e.noRuns(b.CommentID)
	page := e.comment(postA, pageExt, "Dạ shop đây", now)
	e.deliver(page)
	e.noRuns(page.CommentID)
	cfg = replyCfg()
	cfg.PostIDs = []string{postB}
	e.setReply(true, cfg)
	a := e.comment(postA, userA, "giá", now)
	e.deliver(a)
	e.noRuns(a.CommentID)

	e.setReply(true, replyCfg())
	e.sqlExec(`UPDATE channel.channel_accounts SET send_mode = 'read_only' WHERE id = $1`, e.page)
	ro := e.comment(postA, userA, "giá", now)
	e.deliver(ro)
	e.both(ro.CommentID, automation.OutcomeSkippedReadOnly)
	e.sqlExec(`UPDATE channel.channel_accounts SET send_mode = 'enabled' WHERE id = $1`, e.page)
	e.sqlExec(`UPDATE customer.customers SET do_not_contact = true, do_not_contact_at = now()`)
	dnc := e.comment(postB, userA, "giá", now)
	e.deliver(dnc)
	e.both(dnc.CommentID, automation.OutcomeSkippedDNC)
	if len(e.replyCalls(ro.CommentID))+len(e.replyCalls(dnc.CommentID))+e.privateCalls(ro.CommentID)+e.privateCalls(dnc.CommentID) != 0 {
		t.Fatal("cổng chặn mà vẫn gửi")
	}
}

// Meta trả 10900 cho tin riêng tự động → tin failed, lượt failed, không thử lại.
func TestAutoReply_Meta10900(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setReply(true, replyCfg())
	e.fg.FailNext(pageExt+"/messages", fakegraph.GraphErrorSpec{Code: 10900, Type: "OAuthException", Message: "Activity already replied to"})
	c := e.comment(postA, userA, "giá", time.Now().UTC().Add(-time.Minute))
	e.deliver(c)
	e.drain()
	e.drain()
	if out, _ := e.outcomeOf("pm:" + c.CommentID); out != string(automation.OutcomeFailed) {
		t.Fatalf("lượt tin riêng sau 10900: %q", out)
	}
	if r := e.autoMessages(c.CommentID)["private_reply"]; len(r) != 1 || r[0].status != "failed" {
		t.Fatalf("tin riêng sau 10900: %+v", r)
	}
	if n := e.privateCalls(c.CommentID); n != 1 {
		t.Fatalf("10900: %d lần gọi Meta, muốn 1 (không thử lại)", n)
	}
}

// Tin riêng theo mẫu: mẫu không có → lưu bị từ chối; mẫu chung có biến {ten_page} → tin riêng là mẫu đã điền.
// Không có cả mẫu lẫn chữ → từ chối.
func TestAutoReply_Template(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	cfg := replyCfg()
	missing := uuid.New()
	cfg.PrivateMessage = &automation.PrivateMessageConfig{TemplateID: &missing}
	if err := e.saveReply(true, cfg); !errors.Is(err, automation.ErrInvalidRule) {
		t.Fatalf("mẫu không có: %v", err)
	}
	cfg.PrivateMessage = &automation.PrivateMessageConfig{}
	if err := e.saveReply(true, cfg); !errors.Is(err, automation.ErrInvalidRule) {
		t.Fatalf("không mẫu không chữ: %v", err)
	}
	var tpl uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.reply_templates (tenant_id, shortcut, title, body)
			VALUES ($1, 'gia', 'Giá', 'Dạ {ten_page} gửi chị bảng giá ạ') RETURNING id`, e.tenant).Scan(&tpl)
	})
	cfg.PrivateMessage = &automation.PrivateMessageConfig{TemplateID: &tpl}
	e.setReply(true, cfg)
	c := e.comment(postA, userA, "giá", time.Now().UTC().Add(-time.Minute))
	e.deliver(c)
	e.drain()
	if r := e.autoMessages(c.CommentID)["private_reply"]; len(r) != 1 || r[0].body != "Dạ Shop thử gửi chị bảng giá ạ" || r[0].status != "sent" {
		t.Fatalf("tin riêng theo mẫu: %+v", r)
	}
}

// Cấu hình đọc lại đúng dạng hợp đồng (chống đổi tên trường JSON).
func TestAutoReply_ConfigJSON(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(replyCfg())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"kind", "replies", "privateMessage", "postIds", "exceptPostIds", "includeLive", "perUserPerPostHours"} {
		if _, ok := m[k]; !ok {
			t.Errorf("thiếu trường %s trong %s", k, raw)
		}
	}
}
