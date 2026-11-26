package conversation_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

func (e *chatEnv) record(bus *realtimetest.RecordingBus, u identity.AuthUser, cust uuid.UUID, in conversation.CallInput,
	now time.Time,
) (conversation.CallLog, error) {
	var c conversation.CallLog
	err := e.tx(func(tx pgx.Tx) error {
		var err error
		c, err = conversation.Calls{Bus: bus}.Record(e.t.Context(), tx, u, cust, in, now)
		return err //nolint:wrapcheck // test
	})
	return c, err
}

// Ghi cuộc gọi (Task 169 Step 1; CH17, Q14): không nghe máy có hội thoại → 1 dòng call_logs, 1 tin system; hẹn gọi lại
// thiếu giờ → lỗi, có giờ 15:00 → 1 nhắc call_callback lúc 15:00 cho người gọi; audit không chứa chữ của ghi chú; dữ
// liệu sai, hội thoại Page không được trả lời → lỗi đúng loại.
func TestRecordCall(t *testing.T) {
	t.Parallel()
	e := newChatEnv(t)
	now := at(10, 0, 0)
	bus := &realtimetest.RecordingBus{}
	c, err := e.record(bus, e.sale, e.ka, conversation.CallInput{
		ConversationID: &e.convA, Outcome: "no_answer", Note: "khách bận họp ghi chú-riêng-xyz", RequestID: "req-1",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Outcome != "no_answer" || c.CustomerID != e.ka || c.ConversationID == nil || *c.ConversationID != e.convA ||
		c.CreatedBy != e.sale.UserID || c.Reminder != nil || c.Note != "khách bận họp ghi chú-riêng-xyz" {
		t.Errorf("cuộc gọi: %+v", c)
	}
	if n := e.count(`SELECT count(*) FROM conversation.call_logs WHERE customer_id = $1`, e.ka); n != 1 {
		t.Errorf("call_logs: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE conversation_id = $1 AND kind = 'system'
		AND origin = 'system' AND direction = 'out' AND body = 'Đã gọi: không nghe máy' AND sent_at = $2
		AND sender_staff_id = $3 AND send_status IS NULL`, e.convA, now, e.sale.UserID); n != 1 {
		t.Errorf("tin system trong hội thoại: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'call_log.create' AND entity_id = $1
		AND request_id = 'req-1'`, c.ID.String()); n != 1 {
		t.Errorf("audit cuộc gọi: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE after::text LIKE '%riêng-xyz%' OR before::text LIKE '%riêng-xyz%'`); n != 0 {
		t.Errorf("audit chứa ghi chú cuộc gọi")
	}
	if s := bus.Signals(); len(s) != 1 || s[0].Topic != conversation.TopicConversationChanged || s[0].Entity.ID != e.convA.String() ||
		s[0].Scope == nil || s[0].Scope.ID != e.pageA.String() {
		t.Errorf("báo hiệu: %+v", s)
	}

	// Hẹn gọi lại.
	if _, err := e.record(bus, e.sale, e.ka, conversation.CallInput{ConversationID: &e.convA, Outcome: "callback"}, now); !errors.Is(err, conversation.ErrCallInvalid) {
		t.Errorf("callback thiếu giờ: %v", err)
	}
	cb := at(15, 0, 0)
	c, err = e.record(bus, e.sale, e.ka, conversation.CallInput{ConversationID: &e.convA, Outcome: "callback", CallbackAt: &cb}, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Reminder == nil || !c.Reminder.RemindAt.Equal(cb) || c.Reminder.UserID != e.sale.UserID || c.Reminder.Source != "call_callback" ||
		c.CallbackAt == nil || !c.CallbackAt.Equal(cb) {
		t.Errorf("hẹn gọi lại: %+v", c)
	}
	if n := e.count(`SELECT count(*) FROM conversation.reminders WHERE source = 'call_callback' AND user_id = $1 AND remind_at = $2
		AND conversation_id = $3`, e.sale.UserID, cb, e.convA); n != 1 {
		t.Errorf("nhắc gọi lại: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE body = 'Đã gọi: hẹn gọi lại lúc 15:00 26/11/2026'`); n != 1 {
		t.Errorf("tin hẹn gọi lại: %d", n)
	}
}

// Ghi cuộc gọi bị từ chối (Task 169): dữ liệu sai, khách không có, hội thoại Page không được trả lời, hội thoại của
// khách khác, đơn không có; call_logs chỉ ghi thêm.
func TestRecordCallRefusals(t *testing.T) {
	t.Parallel()
	e := newChatEnv(t)
	now, cb := at(10, 0, 0), at(15, 0, 0)
	bus := &realtimetest.RecordingBus{}
	cases := []struct {
		name string
		u    identity.AuthUser
		cust uuid.UUID
		in   conversation.CallInput
		want error
	}{
		{"kết quả lạ", e.sale, e.ka, conversation.CallInput{Outcome: "busy"}, conversation.ErrCallInvalid},
		{"giờ gọi lại ở kết quả khác", e.sale, e.ka, conversation.CallInput{Outcome: "closed", CallbackAt: &cb}, conversation.ErrCallInvalid},
		{"hẹn gọi lại không gắn hội thoại", e.sale, e.ka, conversation.CallInput{Outcome: "callback", CallbackAt: &cb}, conversation.ErrCallInvalid},
		{"giờ gọi lại đã qua", e.sale, e.ka, conversation.CallInput{ConversationID: &e.convA, Outcome: "callback", CallbackAt: new(now.Add(-time.Hour))}, conversation.ErrCallInvalid},
		{"khách không có", e.sale, uuid.New(), conversation.CallInput{Outcome: "closed"}, customer.ErrNotFound},
		{"hội thoại Page B không được trả lời", e.sale, e.ka, conversation.CallInput{ConversationID: &e.convB, Outcome: "closed"}, conversation.ErrNotFound},
		{"chỉ đọc không ghi vào hội thoại", e.viewer, e.ka, conversation.CallInput{ConversationID: &e.convA, Outcome: "closed"}, conversation.ErrNotFound},
		{"đơn không có", e.sale, e.ka, conversation.CallInput{OrderID: new(uuid.New()), Outcome: "closed"}, conversation.ErrCallInvalid},
	}
	for _, c := range cases {
		if _, err := e.record(bus, c.u, c.cust, c.in, now); !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, mong %v", c.name, err, c.want)
		}
	}
	// Hội thoại không thuộc nhóm khách.
	var stranger uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO customer.customers (tenant_id, full_name, name_search)
			VALUES (current_setting('app.tenant_id')::uuid, 'Người lạ', 'nguoi la') RETURNING id`).Scan(&stranger)
	})
	if _, err := e.record(bus, e.sale, stranger, conversation.CallInput{ConversationID: &e.convA, Outcome: "closed"}, now); !errors.Is(err, conversation.ErrCallInvalid) {
		t.Errorf("hội thoại của khách khác: %v", err)
	}
	// Chỉ ghi thêm.
	if err := e.tx(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE conversation.call_logs SET note = 'sửa'`)
		return err //nolint:wrapcheck // test
	}); err == nil {
		t.Error("call_logs sửa được")
	}
}

// allPages là trưởng nhóm với quyền đọc, trả lời mọi Page (grant phạm vi all).
func (e *chatEnv) allPages() identity.AuthUser {
	return identity.AuthUser{UserID: e.lead.UserID, TenantID: e.tenant, Grants: []identity.Grant{
		{Permission: "conversation.read", Scope: identity.Scope{Type: identity.ScopeAll}},
		{Permission: "conversation.reply", Scope: identity.Scope{Type: identity.ScopeAll}},
	}}
}

// Cuộc gọi của cả nhóm khách (Task 169 Step 1): B đã gộp vào A → hỏi theo B thấy cuộc gọi ghi trên A; cuộc gọi gắn hội
// thoại Page không được xem thì bỏ, không gắn hội thoại thì hiện; trang 2 + 1 không trùng, không sót.
func TestListCustomerCalls(t *testing.T) {
	t.Parallel()
	e := newChatEnv(t)
	all := e.allPages()
	bus := &realtimetest.RecordingBus{}
	first, err := e.record(bus, e.sale, e.ka, conversation.CallInput{ConversationID: &e.convA, Outcome: "closed"}, at(9, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	onB, err := e.record(bus, all, e.kb, conversation.CallInput{ConversationID: &e.convB, Outcome: "declined"}, at(9, 30, 0))
	if err != nil {
		t.Fatal(err)
	}
	loose, err := e.record(bus, e.sale, e.ka, conversation.CallInput{Outcome: "wrong_number"}, at(10, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	calls := func(u identity.AuthUser, cust uuid.UUID, after *customer.ListCursor, limit int) ([]uuid.UUID, *customer.ListCursor) {
		t.Helper()
		var ids []uuid.UUID
		var next *customer.ListCursor
		e.mustTx(func(tx pgx.Tx) error {
			got, n, err := conversation.ListCustomerCalls(t.Context(), tx, u, cust, after, limit)
			for _, c := range got {
				ids = append(ids, c.ID)
			}
			next = n
			return err //nolint:wrapcheck // test
		})
		return ids, next
	}
	// created_at của call_logs là giờ ứng dụng (9:00, 9:30, 10:00): thứ tự mới nhất trước là loose,
	// onB, first.
	if got, _ := calls(all, e.kb, nil, 0); !slices.Equal(got, []uuid.UUID{loose.ID, onB.ID, first.ID}) {
		t.Errorf("mọi Page, hỏi theo hồ sơ đã gộp: %v", got)
	}
	if got, _ := calls(e.sale, e.kb, nil, 0); !slices.Equal(got, []uuid.UUID{loose.ID, first.ID}) {
		t.Errorf("chỉ Page A: %v", got)
	}
	p1, next := calls(all, e.ka, nil, 2)
	if next == nil {
		t.Fatal("thiếu trang sau")
	}
	parsed, err := customer.ParseListCursor(next.String())
	if err != nil || !parsed.CreatedAt.Equal(next.CreatedAt) || parsed.ID != next.ID {
		t.Fatalf("con trỏ: %v", err)
	}
	p2, last := calls(all, e.ka, &parsed, 2)
	if !slices.Equal(append(p1, p2...), []uuid.UUID{loose.ID, onB.ID, first.ID}) || last != nil {
		t.Errorf("hai trang: %v + %v, %v", p1, p2, last)
	}
}

// Hội thoại của cả nhóm khách (Task 169): nhãn, bài viết, tên khách của hồ sơ; Page A chỉ thấy convA; không quyền đọc
// Page nào → rỗng; khách không có → customer.ErrNotFound.
func TestListCustomerConversations(t *testing.T) {
	t.Parallel()
	e := newChatEnv(t)
	all := e.allPages()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `WITH t AS (INSERT INTO conversation.tags (tenant_id, name, color)
			VALUES (current_setting('app.tenant_id')::uuid, 'Đã lên đơn', 'green') RETURNING id, tenant_id)
			INSERT INTO conversation.conversation_tag_links (tenant_id, conversation_id, tag_id) SELECT tenant_id, $1, id FROM t`, e.convB)
		return err //nolint:wrapcheck // test
	})
	convs := func(u identity.AuthUser) []conversation.CustomerConversation {
		var out []conversation.CustomerConversation
		e.mustTx(func(tx pgx.Tx) error {
			var err error
			out, _, err = conversation.ListCustomerConversations(t.Context(), tx, u, e.ka, nil, 0)
			return err //nolint:wrapcheck // test
		})
		return out
	}
	got := convs(all)
	if len(got) != 2 || got[0].ID != e.convB || got[1].ID != e.convA {
		t.Fatalf("hội thoại mọi Page: %+v", got)
	}
	b := got[0]
	if b.Kind != "comment" || b.PageID != e.pageB || b.CustomerID != e.kb || b.CustomerName != "Lan FB" || b.PostID == nil ||
		*b.PostID != e.post || b.PostSnippet != "Váy hoa" || len(b.Tags) != 1 || b.Tags[0].Name != "Đã lên đơn" || b.HasOpenDraft {
		t.Errorf("hội thoại comment: %+v", b)
	}
	if got := convs(e.sale); len(got) != 1 || got[0].ID != e.convA || len(got[0].Tags) != 0 {
		t.Errorf("hội thoại Page A: %+v", got)
	}
	if got := convs(identity.AuthUser{UserID: e.sale.UserID, TenantID: e.tenant}); len(got) != 0 {
		t.Errorf("không quyền đọc Page nào: %+v", got)
	}
	e.mustTx(func(tx pgx.Tx) error {
		if _, _, err := conversation.ListCustomerConversations(t.Context(), tx, all, uuid.New(), nil, 0); !errors.Is(err, customer.ErrNotFound) {
			t.Errorf("khách không có: %v", err)
		}
		return nil
	})
}
