package automation_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

const greetText = "Dạ shop chào mình, mình cần shop tư vấn gì ạ?"

// greetButtons là hai nút trả lời nhanh của tin chào trong test.
func greetButtons() []automation.GreetingQuickReply {
	return []automation.GreetingQuickReply{{Title: "Tư vấn size", Payload: "SIZE"}, {Title: "Xem mẫu mới", Payload: "NEW"}}
}

// setGreeting lưu quy tắc greeting của Page (đọc version hiện tại).
func (e *env) setGreeting(enabled bool) {
	e.t.Helper()
	cfg, err := json.Marshal(automation.GreetingConfig{Kind: automation.KindGreeting, Text: greetText, QuickReplies: greetButtons()})
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.storeRule(automation.KindGreeting, enabled, cfg); err != nil {
		e.t.Fatal(err)
	}
}

// inboxMessage là tin khách user gửi Page lúc at.
func inboxMessage(user, mid, text string, at time.Time) channel.MessagePayload {
	return channel.MessagePayload{PageID: pageExt, UserID: user, MID: mid, Text: text, SentAt: at}
}

// enqueueInbox ghi sự kiện tin vào typ (message.received, referral) qua đường via; khóa chống trùng theo đường và mid.
func (e *env) enqueueInbox(typ string, p channel.MessagePayload, via string) {
	e.t.Helper()
	body, err := json.Marshal(p)
	if err != nil {
		e.t.Fatal(err)
	}
	ev := webhooks.InboundEvent{
		Type: typ, DedupeKey: via + ":m:" + typ + ":" + p.MID + ":" + p.UserID, OrderKey: facebook.Key + ":" + pageExt + ":" + p.UserID,
		OccurredAt: p.SentAt, Payload: body,
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(e.t.Context(), tx, queue.NewQueue(), e.tenant, facebook.Key, ev, time.Now().UTC(), via)
		return err //nolint:wrapcheck // test
	})
}

// receive giao tin khách p qua webhook rồi chạy worker tới hết việc tới hạn.
func (e *env) receive(p channel.MessagePayload) {
	e.t.Helper()
	e.enqueueInbox(channel.EventMessageReceived, p, webhooks.ViaWebhook)
	e.drain()
}

// convOf là mã hội thoại hộp thư của PSID user (uuid.Nil khi chưa có).
func (e *env) convOf(user string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		err := tx.QueryRow(e.t.Context(), `SELECT id FROM conversation.conversations WHERE kind = 'inbox'
			AND external_thread_ref = $1`, user).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err //nolint:wrapcheck // test
	})
	return id
}

// greetRun là kết quả lượt tin chào của hội thoại conv, số dòng lượt và số lệnh gửi tự động của hội thoại.
func (e *env) greetRun(conv uuid.UUID) (outcome string, rows, jobs int) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(max(outcome), ''), count(*) FROM conversation.automation_runs
			WHERE external_ref = $1`, "greet:"+conv.String()).Scan(&outcome, &rows)
	})
	jobs = e.staff.CountIn(e.t, e.tenant, `SELECT count(*) FROM platform.jobs j JOIN conversation.messages m
		ON j.payload->>'messageId' = m.id::text WHERE j.kind = $1 AND m.conversation_id = $2`, send.KindAutoSend, conv)
	return outcome, rows, jobs
}

// sentTo là các tin Page gửi tới PSID user trên máy Meta giả.
func (e *env) sentTo(user string) []sentBody {
	e.t.Helper()
	var out []sentBody
	for _, m := range e.fg.Messages() {
		if m.PSID != user || !m.FromPage {
			continue
		}
		var b sentBody
		if err := json.Unmarshal(m.Body, &b); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

// sentBody là phần thân Send API test cần đọc.
type sentBody struct {
	MessagingType string `json:"messaging_type"`
	Message       struct {
		Text         string `json:"text"`
		Metadata     string `json:"metadata"`
		QuickReplies []struct {
			ContentType string `json:"content_type"`
			Title       string `json:"title"`
			Payload     string `json:"payload"`
		} `json:"quick_replies"`
	} `json:"message"`
}

// greetRow là tin chào của hội thoại conv trong CRM.
type greetRow struct {
	Count                    int
	Status, Body, FailReason string
	QuickReplies             []byte
	Staff                    *uuid.UUID
}

func (e *env) greetMessage(conv uuid.UUID) greetRow {
	e.t.Helper()
	var r greetRow
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(e.t.Context(), `SELECT count(*) FROM conversation.messages WHERE conversation_id = $1
			AND origin = 'automation'`, conv).Scan(&r.Count); err != nil || r.Count == 0 {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(e.t.Context(), `SELECT send_status, coalesce(body, ''), coalesce(fail_reason, ''),
			coalesce(quick_replies, 'null'::jsonb), sender_staff_id FROM conversation.messages WHERE conversation_id = $1
			AND origin = 'automation'`, conv).Scan(&r.Status, &r.Body, &r.FailReason, &r.QuickReplies, &r.Staff)
	})
	return r
}

// PSID mới nhắn lần đầu → 1 lệnh command.fb.auto_send, Meta nhận đúng chữ, 2 nút đúng tiêu đề và payload,
// messaging_type RESPONSE, metadata là client_ref của tin; tin CRM origin automation, sent, có quick_replies, không
// người gửi; lượt done. Tin thứ hai, xử lý lại tin đầu (hỏi bù) → không thêm.
func TestGreeting_FirstMessageOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setGreeting(true)
	now := time.Now().UTC().Add(-time.Minute)
	first := inboxMessage(userA, "mid-g1", "shop ơi", now)
	e.receive(first)
	conv := e.convOf(userA)
	if out, rows, jobs := e.greetRun(conv); out != string(automation.OutcomeDone) || rows != 1 || jobs != 1 {
		t.Fatalf("lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	sent := e.sentTo(userA)
	if len(sent) != 1 {
		t.Fatalf("Meta nhận %d tin, muốn 1", len(sent))
	}
	b := sent[0]
	if b.MessagingType != "RESPONSE" || b.Message.Text != greetText || len(b.Message.QuickReplies) != 2 ||
		b.Message.QuickReplies[0].Title != "Tư vấn size" || b.Message.QuickReplies[0].Payload != "SIZE" ||
		b.Message.QuickReplies[1].Title != "Xem mẫu mới" || b.Message.QuickReplies[1].ContentType != "text" {
		t.Fatalf("thân gửi: %+v", b)
	}
	var staffAt *time.Time
	var snippet string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT last_staff_message_at, coalesce(last_snippet, '') FROM conversation.conversations
			WHERE id = $1`, conv).Scan(&staffAt, &snippet)
	})
	if staffAt != nil || snippet != greetText {
		t.Fatalf("hội thoại sau tin chào: mốc nhân viên %v (muốn rỗng: tin tự động không phải nhân viên), trích %q", staffAt, snippet)
	}
	row := e.greetMessage(conv)
	var qrs []map[string]string
	if err := json.Unmarshal(row.QuickReplies, &qrs); err != nil {
		t.Fatal(err)
	}
	if row.Count != 1 || row.Status != "sent" || row.Body != greetText || row.Staff != nil || len(qrs) != 2 ||
		qrs[0]["title"] != "Tư vấn size" || qrs[1]["payload"] != "NEW" {
		t.Fatalf("tin chào trong CRM: %+v, nút %v", row, qrs)
	}
	e.receive(inboxMessage(userA, "mid-g2", "còn hàng không", now.Add(time.Second)))
	e.enqueueInbox(channel.EventMessageReceived, first, webhooks.ViaBackfill)
	e.drain()
	if out, rows, jobs := e.greetRun(conv); out != string(automation.OutcomeDone) || rows != 1 || jobs != 1 || len(e.sentTo(userA)) != 1 {
		t.Fatalf("sau tin thứ hai, xử lý lại: lượt %q, %d dòng, %d lệnh, %d tin tới Meta", out, rows, jobs, len(e.sentTo(userA)))
	}
}

// Khách đã có hội thoại hộp thư cũ (đã xong) nhắn lại → không chào; quy tắc tắt → không lượt nào.
func TestGreeting_ReturningCustomerNotGreeted(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	now := time.Now().UTC().Add(-2 * time.Minute)
	e.receive(inboxMessage(userA, "mid-r1", "hỏi giá", now))
	conv := e.convOf(userA)
	if out, rows, _ := e.greetRun(conv); rows != 0 {
		t.Fatalf("quy tắc tắt: lượt %q", out)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.conversations SET status = 'done' WHERE id = $1`, conv)
		return err //nolint:wrapcheck // test
	})
	e.setGreeting(true)
	e.receive(inboxMessage(userA, "mid-r2", "shop ơi mình hỏi lại", now.Add(time.Minute)))
	if again := e.convOf(userA); again != conv {
		t.Fatalf("hội thoại mới %v, muốn dùng lại %v", again, conv)
	}
	if out, rows, jobs := e.greetRun(conv); rows != 0 || jobs != 0 || len(e.sentTo(userA)) != 0 {
		t.Fatalf("khách cũ: lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
}

// Khách bấm nút "Tư vấn size" → tin vào có quick_reply_payload = "SIZE", body = "Tư vấn size"; không chào lần nữa.
func TestGreeting_QuickReplyPressed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setGreeting(true)
	now := time.Now().UTC().Add(-time.Minute)
	e.receive(inboxMessage(userA, "mid-q1", "chào shop", now))
	press := inboxMessage(userA, "mid-q2", "Tư vấn size", now.Add(time.Second))
	press.QuickReplyPayload = "SIZE"
	e.receive(press)
	var payload, body string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT quick_reply_payload, body FROM conversation.messages WHERE external_id = 'mid-q2'`).
			Scan(&payload, &body)
	})
	if payload != "SIZE" || body != "Tư vấn size" {
		t.Fatalf("tin bấm nút: payload %q, body %q", payload, body)
	}
	if _, rows, jobs := e.greetRun(e.convOf(userA)); rows != 1 || jobs != 1 || len(e.sentTo(userA)) != 1 {
		t.Fatalf("bấm nút: %d dòng, %d lệnh, %d tin tới Meta", rows, jobs, len(e.sentTo(userA)))
	}
}

// Khách mở hội thoại từ quảng cáo (sự kiện referral tạo hội thoại, chỉ có tin hệ thống) rồi mới nhắn → vẫn là lần đầu,
// được chào; khách không liên hệ (đặt sau referral) → skipped_dnc, không lệnh.
func TestGreeting_AfterReferralAndDoNotContact(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setGreeting(true)
	now := time.Now().UTC().Add(-time.Minute)
	const userB = "7000000000000267"
	for _, u := range []string{userA, userB} {
		e.enqueueInbox(channel.EventMessageReferral, channel.MessagePayload{PageID: pageExt, UserID: u, SentAt: now}, webhooks.ViaWebhook)
	}
	e.drain()
	var cust uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `SELECT customer_id FROM conversation.conversations WHERE external_thread_ref = $1`,
			userB).Scan(&cust); err != nil {
			return err //nolint:wrapcheck // test
		}
		w := customer.Writer{Events: e.d.Events, Queue: e.d.Queue}
		return w.SetDoNotContact(t.Context(), tx, cust, true, customer.Actor{Type: customer.ActorSystem}, time.Now())
	})
	e.receive(inboxMessage(userA, "mid-f1", "mẫu này còn không", now.Add(time.Second)))
	e.receive(inboxMessage(userB, "mid-f2", "mẫu này còn không", now.Add(time.Second)))
	if out, rows, jobs := e.greetRun(e.convOf(userA)); out != string(automation.OutcomeDone) || rows != 1 || jobs != 1 {
		t.Fatalf("sau referral: lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	if out, rows, jobs := e.greetRun(e.convOf(userB)); out != string(automation.OutcomeSkippedDNC) || rows != 1 || jobs != 0 ||
		len(e.sentTo(userB)) != 0 {
		t.Fatalf("không liên hệ: lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
}

// Page read_only → skipped_read_only, 0 lệnh; tin tới bằng đường lịch sử → không dòng nào.
func TestGreeting_ReadOnlyAndHistory(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setGreeting(true)
	now := time.Now().UTC().Add(-time.Minute)
	const userB = "7000000000000367"
	e.enqueueInbox(channel.EventMessageReceived, inboxMessage(userB, "mid-h1", "tin cũ", now), webhooks.ViaHistory)
	e.drain()
	if out, rows, jobs := e.greetRun(e.convOf(userB)); e.convOf(userB) == uuid.Nil || rows != 0 || jobs != 0 {
		t.Fatalf("lịch sử: lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	e.setSendMode("read_only")
	e.receive(inboxMessage(userA, "mid-o1", "shop ơi", now))
	if out, rows, jobs := e.greetRun(e.convOf(userA)); out != string(automation.OutcomeSkippedReadOnly) || rows != 1 || jobs != 0 {
		t.Fatalf("chỉ xem: lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	if len(e.sentTo(userA))+len(e.sentTo(userB)) != 0 {
		t.Fatal("có tin tới Meta")
	}
}

// Lệnh tin chào không rõ đã tới chưa (mất kết nối sau khi Meta đọc yêu cầu) → in_doubt → tin discarded (không hỏi
// nhân viên, không gửi lại), lượt dropped, đúng 1 lời gọi Meta.
func TestGreeting_InDoubtDropped(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.setGreeting(true)
	e.fg.DropAfterRead(pageExt + "/messages")
	e.receive(inboxMessage(userA, "mid-d1", "shop ơi", time.Now().UTC().Add(-time.Minute)))
	e.drain()
	conv := e.convOf(userA)
	if out, rows, jobs := e.greetRun(conv); out != string(automation.OutcomeDropped) || rows != 1 || jobs != 1 {
		t.Fatalf("lượt %q, %d dòng, %d lệnh", out, rows, jobs)
	}
	if row := e.greetMessage(conv); row.Status != "discarded" || row.FailReason == "" {
		t.Fatalf("tin chào: %+v", row)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'in_doubt'`,
		send.KindAutoSend); n != 1 {
		t.Fatalf("lệnh in_doubt = %d", n)
	}
}

// Cấu hình tin chào sai (14 nút, tiêu đề 21 ký tự, payload rỗng, chữ rỗng hay quá 640 ký tự) → ErrInvalidRule; 13 nút,
// tiêu đề đúng 20 ký tự → lưu được. (PUT qua HTTP: hợp đồng trả 400, TestGreetingRuleAPI.)
func TestGreeting_ConfigLimits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	btn := func(title string) automation.GreetingQuickReply {
		return automation.GreetingQuickReply{Title: title, Payload: "P"}
	}
	many := func(n int) []automation.GreetingQuickReply {
		out := make([]automation.GreetingQuickReply, n)
		for i := range out {
			out[i] = btn("Nút")
		}
		return out
	}
	cases := []struct {
		name string
		text string
		qrs  []automation.GreetingQuickReply
		ok   bool
	}{
		{"13 nút, tiêu đề 20 ký tự", greetText, append(many(12), btn(strings.Repeat("ữ", 20))), true},
		{"không nút", greetText, []automation.GreetingQuickReply{}, true},
		{"14 nút", greetText, many(14), false},
		{"tiêu đề 21 ký tự", greetText, []automation.GreetingQuickReply{btn(strings.Repeat("ữ", 21))}, false},
		{"tiêu đề rỗng", greetText, []automation.GreetingQuickReply{btn("")}, false},
		{"payload rỗng", greetText, []automation.GreetingQuickReply{{Title: "A"}}, false},
		{"payload 1001", greetText, []automation.GreetingQuickReply{{Title: "A", Payload: strings.Repeat("x", 1001)}}, false},
		{"chữ rỗng", "  ", nil, false},
		{"chữ 641 ký tự", strings.Repeat("ữ", 641), nil, false},
	}
	for _, c := range cases {
		cfg, err := json.Marshal(automation.GreetingConfig{Kind: automation.KindGreeting, Text: c.text, QuickReplies: c.qrs})
		if err != nil {
			t.Fatal(err)
		}
		saveErr := e.storeRule(automation.KindGreeting, true, cfg)
		if c.ok != (saveErr == nil) || (!c.ok && !errors.Is(saveErr, automation.ErrInvalidRule)) {
			t.Errorf("%s: lỗi %v", c.name, saveErr)
		}
	}
}
