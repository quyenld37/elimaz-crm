package inbound_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/adapters/fake"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// CH08, CH01: tin đầu tiên của PSID u-1 trên Page 1 → 1 khách lead tên tạm, danh tính fb_psid phạm vi mã Page; 1 hội
// thoại inbox; 1 tin in; chưa đọc 1; mốc tin khách là mốc của Meta; một báo hiệu conversation.changed đòi
// conversation.read trên page:<Page 1>; chỉ số webhook_to_signal_ms có mẫu.
func TestInboundFirstMessageCreatesLeadAndConversation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive("u-1", "mid-1", "Áo này còn size M không shop?", 0)
	e.noDeadJobs()

	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers WHERE lifecycle = 'lead'
		AND name_source = 'placeholder'`); n != 1 {
		t.Fatalf("khách lead tên tạm = %d, mong 1", n)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customer_identities WHERE kind = 'fb_psid'
		AND scope = $1 AND value = 'u-1' AND source = 'chat'`, pageExt1); n != 1 {
		t.Fatalf("danh tính fb_psid = %d, mong 1", n)
	}
	c := e.conversation(e.page1, "u-1")
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.conversations`); n != 1 || c.Kind != "inbox" {
		t.Fatalf("hội thoại = %d, loại %q", n, c.Kind)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1`, c.CustomerID); n != 1 {
		t.Errorf("hội thoại không trỏ khách giữ danh tính (%d)", n)
	}
	ms := e.messages(c.ID)
	if len(ms) != 1 || ms[0].Direction != "in" || ms[0].Origin != "customer" || ms[0].Kind != "message" ||
		deref(ms[0].ExternalID) != "mid-1" || !ms[0].SentAt.Equal(at(0)) {
		t.Fatalf("tin: %+v", ms)
	}
	if c.Unread != 1 || c.LastCustomerAt == nil || !c.LastCustomerAt.Equal(at(0)) || !c.LastMessageAt.Equal(at(0)) ||
		c.Status != "open" || deref(c.Snippet) != "Áo này còn size M không shop?" {
		t.Fatalf("hội thoại: %+v", c)
	}
	sig := e.changedSignals(c.ID)
	if len(sig) != 1 {
		t.Fatalf("báo hiệu changed = %d, mong 1", len(sig))
	}
	s := sig[0]
	if s.TenantID != e.tenant || s.Requires != "conversation.read" || s.Scope == nil ||
		*s.Scope != (identity.Scope{Type: identity.ScopePage, ID: e.page1.String()}) || s.Entity.Type != deps.EntityConversation {
		t.Errorf("báo hiệu: %+v", s)
	}
	found := false
	for _, r := range e.reg.Drain() {
		if r.Name == "webhook_to_signal_ms" && r.Label == facebook.Key {
			found = true
		}
	}
	if !found {
		t.Error("không có chỉ số webhook_to_signal_ms")
	}
}

// Tin thứ hai → chưa đọc 2, đoạn trích là tin mới (cắt 80 ký tự); cùng mid qua việc khác khóa → vẫn 2 tin, chưa đọc 2.
func TestInboundSecondMessageAndDuplicateMID(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive("u-1", "mid-1", "chào shop", 0)
	long := strings.Repeat("ă", 100)
	e.receive("u-1", "mid-2", long, 5)
	// Cùng mid-2, khóa chống trùng khác (Meta giao lại qua gói khác).
	e.deliver(channel.EventMessageReceived, "khac:mid-2", "u-1", at(5), message("u-1", "mid-2", long, 5))
	e.noDeadJobs()
	c := e.conversation(e.page1, "u-1")
	if n := len(e.messages(c.ID)); n != 2 {
		t.Fatalf("tin = %d, mong 2", n)
	}
	if c.Unread != 2 || deref(c.Snippet) != strings.Repeat("ă", 80) || !c.LastMessageAt.Equal(at(5)) {
		t.Fatalf("hội thoại: unread %d, trích %q, mốc %v", c.Unread, deref(c.Snippet), c.LastMessageAt)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers`); n != 1 {
		t.Errorf("khách = %d, mong 1", n)
	}
}

// Hai tin đến ngược thứ tự (tin cũ hơn xử lý sau) → mốc và đoạn trích là của tin mới hơn; mốc tin khách không lùi.
func TestInboundOutOfOrderKeepsNewest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive("u-1", "mid-new", "tin mới", 10)
	e.receive("u-1", "mid-old", "tin cũ", 3)
	c := e.conversation(e.page1, "u-1")
	if !c.LastMessageAt.Equal(at(10)) || deref(c.Snippet) != "tin mới" || !c.LastCustomerAt.Equal(at(10)) || c.Unread != 2 {
		t.Fatalf("hội thoại: %+v", c)
	}
}

// Hội thoại done (và pending) nhận tin mới → open, version tăng; spam giữ spam.
func TestInboundReopensConversation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for i, c := range []struct{ from, want string }{{"done", "open"}, {"pending", "open"}, {"spam", "spam"}} {
		user := "u-" + c.from
		e.receive(user, "mid-a-"+c.from, "a", i)
		before := e.conversation(e.page1, user)
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `UPDATE conversation.conversations SET status = $1 WHERE id = $2`, c.from, before.ID)
			return err //nolint:wrapcheck // test
		})
		e.receive(user, "mid-b-"+c.from, "b", i+10)
		got := e.conversation(e.page1, user)
		wantVersion := before.Version
		if c.want != c.from {
			wantVersion++
		}
		if got.Status != c.want || got.Version != wantVersion {
			t.Errorf("%s → %s v%d, mong %s v%d", c.from, got.Status, got.Version, c.want, wantVersion)
		}
	}
}

// Tài khoản read_only (mặc định) và disconnected vẫn lưu tin.
func TestInboundStoresForReadOnlyAndDisconnectedAccounts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE channel.channel_accounts SET status = 'disconnected' WHERE id = $1`, e.page2)
		return err //nolint:wrapcheck // test
	})
	e.receive("u-1", "mid-1", "page read only", 0)
	p2 := channel.MessagePayload{PageID: pageExt2, UserID: "u-2", MID: "mid-p2", Text: "page ngắt", SentAt: at(1)}
	e.enqueue(facebook.Key, channel.EventMessageReceived, "m:mid-p2", pageExt2, "u-2", at(1), p2)
	e.drain()
	e.noDeadJobs()
	for _, c := range []struct {
		page uuid.UUID
		user string
	}{{e.page1, "u-1"}, {e.page2, "u-2"}} {
		if n := len(e.messages(e.conversation(c.page, c.user).ID)); n != 1 {
			t.Errorf("Page %s: %d tin, mong 1", c.page, n)
		}
	}
}

// Postback không mid (nút bấm): lưu tin với tiêu đề nút và payload; giao lại → vẫn một tin. Referral: tin hệ thống,
// không đổi mốc tin khách, không tăng chưa đọc.
func TestInboundPostbackAndReferral(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	pb := channel.MessagePayload{PageID: pageExt1, UserID: "u-1", Text: "Xem bảng size", PostbackPayload: "SIZE", SentAt: at(0)}
	e.deliver(channel.EventMessageReceived, "b:u-1:0", "u-1", at(0), pb)
	e.deliver(channel.EventMessageReceived, "b:u-1:0:lai", "u-1", at(0), pb)
	e.deliver(channel.EventMessageReferral, "f:u-1:9", "u-1", at(9), channel.MessagePayload{PageID: pageExt1, UserID: "u-1", SentAt: at(9)})
	e.deliver(channel.EventMessageReferral, "f:u-1:9:lai", "u-1", at(9), channel.MessagePayload{PageID: pageExt1, UserID: "u-1", SentAt: at(9)})
	e.noDeadJobs()
	c := e.conversation(e.page1, "u-1")
	ms := e.messages(c.ID)
	if len(ms) != 2 {
		t.Fatalf("tin = %d, mong 2: %+v", len(ms), ms)
	}
	if ms[0].Origin != "customer" || deref(ms[0].Body) != "Xem bảng size" {
		t.Errorf("postback: %+v", ms[0])
	}
	var payload string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT quick_reply_payload FROM conversation.messages WHERE id = $1`, ms[0].ID).Scan(&payload)
	})
	if payload != "SIZE" {
		t.Errorf("payload nút = %q", payload)
	}
	if ms[1].Kind != "system" || ms[1].Origin != "system" || deref(ms[1].Body) != "Khách đến từ quảng cáo hoặc liên kết" {
		t.Errorf("referral: %+v", ms[1])
	}
	if c.Unread != 1 || !c.LastCustomerAt.Equal(at(0)) {
		t.Errorf("referral đổi hội thoại: %+v", c)
	}
}

// Loại sự kiện chưa xử lý → đếm inbound_ignored theo loại, việc xong; Page không có trong shop → inbound_ignored
// account_missing, việc xong, không ghi gì.
func TestInboundIgnoresUnknownTypeAndAccount(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.deliver("message.something_new", "x:1", "u-1", at(0), map[string]string{"pageId": pageExt1})
	other := channel.MessagePayload{PageID: "104000000000999", UserID: "u-9", MID: "mid-9", Text: "Page lạ", SentAt: at(1)}
	e.deliver(channel.EventMessageReceived, "m:mid-9", "u-9", at(1), other)
	e.noDeadJobs()
	if n := e.staff.Count(t, `SELECT count(*) FROM platform.jobs WHERE kind = 'inbound.facebook' AND status <> 'done'`); n != 0 {
		t.Errorf("%d việc chưa xong", n)
	}
	rows := e.reg.Drain()
	count := func(label string) int64 {
		var n int64
		for _, r := range rows {
			if r.Name == "inbound_ignored" && r.Label == label {
				n += r.Count
			}
		}
		return n
	}
	if count("message.something_new") != 1 || count("account_missing") != 1 {
		t.Errorf("inbound_ignored: loại lạ %d, Page lạ %d", count("message.something_new"), count("account_missing"))
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.messages`); n != 0 {
		t.Errorf("Page lạ mà lưu %d tin", n)
	}
}

// Kênh giả: tuyến fakechat_page ghi được qua tenancy.PutRoute (migration 0412); tin của kênh giả tách bằng adapter
// fakechat thật vào đúng Page giả.
func TestInboundFakechat(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		return tenancy.PutRoute(t.Context(), tx, fake.RouteKind, fakePage)
	}); err != nil {
		t.Fatalf("ghi tuyến fakechat_page: %v", err)
	}
	raw, err := json.Marshal(fake.Packet{Events: []fake.Event{
		{ID: "e1", Type: "message", PageID: fakePage, UserID: "fu-1", MID: "fmid-1", Text: "kênh giả", At: at(0)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := fake.New(secret32).Split(raw)
	if err != nil || len(res.Events) != 1 {
		t.Fatalf("tách: %v %+v", err, res)
	}
	ev := res.Events[0]
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		_, err := webhooks.EnqueueInbound(t.Context(), tx, queue.NewQueue(), e.tenant, fake.Key, ev, time.Now(), webhooks.ViaWebhook)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	e.drain()
	e.noDeadJobs()
	c := e.conversation(e.fakePage, "fu-1")
	if ms := e.messages(c.ID); len(ms) != 1 || deref(ms[0].Body) != "kênh giả" || c.Unread != 1 {
		t.Fatalf("kênh giả: %+v %+v", c, ms)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customer_identities WHERE kind = 'fb_psid' AND scope = $1`, fakePage); n != 1 {
		t.Errorf("danh tính kênh giả = %d", n)
	}
}

// Dữ liệu cá nhân: log mức debug của worker và payload mọi việc mới tạo không chứa nội dung tin hay SĐT.
func TestInboundDoesNotLeakContent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const text = "xin chào shop 0912345678"
	e.receive("u-1", "mid-1", text, 0)
	e.deliver(channel.EventMessageEcho, "m:mid-2", "u-1", at(1), channel.MessagePayload{
		PageID: pageExt1, UserID: "u-1", MID: "mid-2", Text: text, IsEcho: true, EchoAppID: businessSuiteAppID, SentAt: at(1),
	})
	e.deliver(channel.EventMessageEdited, "e:mid-1", "u-1", at(2), channel.EditPayload{PageID: pageExt1, UserID: "u-1", MID: "mid-1", Text: text + " sửa", EditedAt: at(2)})
	e.noDeadJobs()
	for _, s := range []string{"xin chào", "0912345678"} {
		if strings.Contains(e.logs.String(), s) {
			t.Errorf("log chứa %q", s)
		}
	}
	payloads := e.texts(`SELECT payload::text FROM platform.jobs`)
	if len(payloads) == 0 {
		t.Fatal("không đọc được việc nào")
	}
	for _, p := range payloads {
		if strings.Contains(p, "0912345678") || strings.Contains(p, "xin chào") {
			t.Errorf("payload việc chứa nội dung tin: %s", p)
		}
	}
}

// [Task 501, CH08] Hội thoại tạo khách mới đánh dấu created_customer (tin vào hộp thư, comment lần đầu); hội thoại sau
// của cùng khách (comment trên bài) không đánh dấu.
func TestInboundMarksConversationThatCreatedCustomer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.receive("u-1", "mid-1", "Còn hàng không shop?", 0)
	e.sendComment(commentEv("add", post1, "c-1", "", "u-1", "Lan", "Giá bao nhiêu?", 1))
	e.sendComment(commentEv("add", post1, "c-2", "", "u-9", "Hoa", "Còn size M?", 2))
	e.noDeadJobs()
	box := e.conversation(e.page1, "u-1")
	cm1, _ := e.commentConv(post1, "u-1")
	cm2, _ := e.commentConv(post1, "u-9")
	got := map[uuid.UUID]bool{}
	for _, id := range []uuid.UUID{box.ID, cm1.ID, cm2.ID} {
		got[id] = e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM conversation.conversations WHERE id = $1
			AND created_customer`, id) == 1
	}
	if !got[box.ID] || got[cm1.ID] || !got[cm2.ID] {
		t.Fatalf("created_customer: hộp thư %v, comment cùng khách %v, comment người mới %v", got[box.ID], got[cm1.ID], got[cm2.ID])
	}
}
