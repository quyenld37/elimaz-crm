package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// fuConversation tạo khách name và hội thoại hộp thư trên Page page: khách nhắn 5 giờ trước, nhân viên trả lời 4 giờ
// trước (đã gửi); trả mã hội thoại.
func fuConversation(t *testing.T, e *chAPIEnv, page uuid.UUID, psid, name string) uuid.UUID {
	t.Helper()
	var cust, conv uuid.UUID
	if err := tenancy.WithTenant(t.Context(), e.staff.Owner, e.tenant, func(tx pgx.Tx) (err error) {
		cust, err = customer.Create(t.Context(), tx, name)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO conversation.conversations (tenant_id, channel_account_id,
			customer_id, kind, external_thread_ref, last_message_at, last_customer_message_at, last_staff_message_at)
			VALUES ($1, $2, $3, 'inbox', $4, $6, $5, $6) RETURNING id`, e.tenant, page, cust, psid,
			now.Add(-5*time.Hour), now.Add(-4*time.Hour)).Scan(&conv); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
			direction, kind, origin, body, send_status, sent_at) VALUES ($1, $2, $3, 'out', 'message', 'staff', 'Dạ còn ạ',
			'sent', $4)`, e.tenant, conv, page, now.Add(-4*time.Hour))
		return err //nolint:wrapcheck // test
	})
	return conv
}

// CH16 qua API: sale-a (đọc Page A) chỉ thấy hội thoại Page A, đủ trường (Page, khách, mốc tin nhân viên, cửa sổ đóng
// sau 19 giờ); quản lý đọc mọi Page thấy cả hai, lọc pageId Page B → chỉ Page B; sale-a lọc Page B → rỗng; người chỉ
// được đọc một Page khác → rỗng; afterHours 5 → rỗng; afterHours 0, 24 → 400.
func TestFollowUpsAPI_List(t *testing.T) {
	t.Parallel()
	e := newChAPIEnv(t)
	a := fuConversation(t, e, e.pageA, "psid-a", "Chị An")
	b := fuConversation(t, e, e.pageB, "psid-b", "Chị Bình")
	ids := func(email, query string) []string {
		t.Helper()
		rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/follow-ups"+query, email, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", email, query, rec.Code, rec.Body.String())
		}
		items, _ := decodeJSON(t, rec)["items"].([]any)
		out := []string{}
		for _, it := range items {
			out = append(out, it.(map[string]any)["conversationId"].(string)) //nolint:forcetypeassert // test
		}
		return out
	}
	rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/follow-ups", chReaderA, "", "")
	items, _ := decodeJSON(t, rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("sale-a: %s", rec.Body.String())
	}
	it := items[0].(map[string]any) //nolint:forcetypeassert // test
	cust, _ := it["customer"].(map[string]any)
	staffAt, _ := time.Parse(time.RFC3339Nano, it["lastStaffMessageAt"].(string)) //nolint:forcetypeassert // test
	ends, _ := time.Parse(time.RFC3339Nano, it["windowEndsAt"].(string))          //nolint:forcetypeassert // test
	if it["conversationId"] != a.String() || it["pageId"] != e.pageA.String() || cust["name"] != "Chị An" ||
		it["lastStaffSnippet"] != "Dạ còn ạ" ||
		time.Since(staffAt) < 4*time.Hour-time.Minute || time.Until(ends) < 19*time.Hour-time.Minute ||
		time.Until(ends) > 19*time.Hour+time.Minute {
		t.Fatalf("mục = %v", it)
	}
	if got := ids(chManager, ""); len(got) != 2 {
		t.Fatalf("quản lý: %v", got)
	}
	if got := ids(chManager, "?pageId="+e.pageB.String()); len(got) != 1 || got[0] != b.String() {
		t.Fatalf("quản lý lọc Page B: %v", got)
	}
	if got := ids(chReaderA, "?pageId="+e.pageB.String()); len(got) != 0 {
		t.Fatalf("sale-a lọc Page B: %v", got)
	}
	if got := ids(chReaderNo, ""); len(got) != 0 {
		t.Fatalf("người đọc Page khác: %v", got)
	}
	if got := ids(chManager, "?afterHours=5&limit=10"); len(got) != 0 {
		t.Fatalf("afterHours 5: %v", got)
	}
	for _, q := range []string{"?afterHours=0", "?afterHours=24"} {
		if rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/follow-ups"+q, chManager, "", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", q, rec.Code, rec.Body.String())
		}
	}
}

// [Task 501] lastStaffSnippet là đoạn đầu 120 ký tự (cắt theo ký tự, không giữa chữ có dấu) của tin người thật cuối
// của Page; tin cuối là ảnh (không chữ) → không có trường.
func TestFollowUpsAPI_LastStaffSnippet(t *testing.T) {
	t.Parallel()
	e := newChAPIEnv(t)
	a := fuConversation(t, e, e.pageA, "psid-a", "Chị An")
	b := fuConversation(t, e, e.pageA, "psid-b", "Chị Bình")
	long := strings.Repeat("ạ", 130)
	at := time.Now().UTC().Add(-3 * time.Hour)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id,
			direction, kind, origin, body, send_status, sent_at) VALUES ($1, $2, $3, 'out', 'message', 'staff', $4, 'sent', $5),
			($1, $6, $3, 'out', 'message', 'staff', NULL, 'sent', $5)`, e.tenant, a, e.pageA, long, at, b)
		return err //nolint:wrapcheck // test
	})
	rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/follow-ups", chManager, "", "")
	items, _ := decodeJSON(t, rec)["items"].([]any)
	got := map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)                                  //nolint:forcetypeassert // test
		got[m["conversationId"].(string)] = m["lastStaffSnippet"] //nolint:forcetypeassert // test
	}
	if len(got) != 2 || got[a.String()] != strings.Repeat("ạ", 120) || got[b.String()] != nil {
		t.Fatalf("lastStaffSnippet = %v (%s)", got, rec.Body.String())
	}
}

// Bật quy tắc follow_up với mẫu không có → 422 invalid_input; tắt cùng cấu hình → 200.
func TestFollowUpsAPI_RuleNeedsTemplate(t *testing.T) {
	t.Parallel()
	e := newChAPIEnv(t)
	path := "/api/v1/channels/" + e.pageA.String() + "/automation-rules/follow_up"
	body := func(enabled string) string {
		return `{"enabled":` + enabled + `,"config":{"kind":"follow_up","afterHours":3,"templateId":"` + uuid.NewString() +
			`"},"expectedVersion":0}`
	}
	expect(t, "mẫu không có", serveAs(e.srv, e.keys, http.MethodPut, path, chOwner, "fu1", body("true")),
		http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "tắt", serveAs(e.srv, e.keys, http.MethodPut, path, chOwner, "fu2", body("false")), http.StatusOK, "")
}
