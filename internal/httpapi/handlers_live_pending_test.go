package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit/livetest"
)

// livePendingPath là đường danh sách (suffix "") hay nhắc (suffix "/remind") của phiên s.
func livePendingPath(s uuid.UUID, suffix string) string {
	return "/api/v1/live-sessions/" + s.String() + "/pending-drafts" + suffix
}

// customerRepliesInInbox: khách của comment cm nhắn vào hộp thư của Page (tạo hộp thư nếu chưa có) lúc at.
func (e *liveClaimAPIEnv) customerRepliesInInbox(cm uuid.UUID, body string, at time.Time) {
	e.t.Helper()
	e.exec(`WITH c AS (
			SELECT cv.tenant_id, cv.channel_account_id, cv.customer_id, split_part(cv.external_thread_ref, ':', 2) AS psid
			FROM conversation.messages m JOIN conversation.conversations cv ON cv.id = m.conversation_id WHERE m.id = $1),
		box AS (
			INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind, external_thread_ref,
				last_message_at, last_customer_message_at)
			SELECT tenant_id, channel_account_id, customer_id, 'inbox', psid, $3, $3 FROM c RETURNING id, tenant_id, channel_account_id)
		INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id, direction, kind,
			origin, body, sent_at)
		SELECT tenant_id, id, channel_account_id, 'm_' || gen_random_uuid()::text, 'in', 'message', 'customer', $2, $3 FROM box`,
		cm, body, at.UTC())
}

// LV04 qua HTTP (đường trọn trong gói): comment live → chốt (giữ hàng) → khách trả lời trong hộp thư → danh sách
// "thiếu địa chỉ" có đơn, customerRepliedAfterAsk = true, người chốt An → updateOrder (PATCH, thêm địa chỉ, version) →
// confirmOrder → đơn confirmed, "có thể bán" không đổi so với lúc nháp, đơn biến khỏi danh sách, confirmedCount 1.
// Nhắc trên Page chỉ xem → 200, skipped lý do chỉ xem. Quyền: không live.operate → remind 403; Page khác → list,
// remind 404; mẫu thiếu → 400.
func TestLivePendingAPI_CompleteAndRemind(t *testing.T) {
	t.Parallel()
	e := newLiveClaimAPIEnv(t)
	e.staff.SeedAdminUnits(t)
	var opID, tpl uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id FROM identity.users WHERE email = $1`, liveOp).Scan(&opID)
	})
	e.staff.Grant(t, e.tenant, opID, []string{"order.confirm"}, "all", "")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.reply_templates (tenant_id, shortcut, title, body)
			VALUES (current_setting('app.tenant_id')::uuid, 'nhac', 'Nhắc địa chỉ', 'Bạn gửi giúp shop địa chỉ cho đơn {ma_don} nhé')
			RETURNING id`).Scan(&tpl)
	})
	v := e.variant(5)
	cm := e.comment("Lan", "u1", "chốt", time.Now().Add(-time.Minute))
	claim := mustOK(t, "claimLiveComment", e.send(http.MethodPost, claimPath(cm), liveOp, claimBody(v, 2, `,"askAddress":false`)),
		http.StatusCreated)
	orderID := claim["orderId"]
	held := livetest.Available(t, e.staff, e.tenant, v)
	if held != 3 {
		t.Fatalf("có thể bán sau chốt = %d", held)
	}
	e.customerRepliesInInbox(cm, "12 Lê Lợi, phường Bến Thành", time.Now())

	list := mustOK(t, "listPendingDrafts", e.send(http.MethodGet, livePendingPath(e.session, ""), liveViewer, ""), http.StatusOK)
	items, _ := list["items"].([]any)
	if len(items) != 1 || list["confirmedCount"] != float64(0) || list["expiredCount"] != float64(0) {
		t.Fatalf("danh sách: %v", list)
	}
	row, _ := items[0].(map[string]any)
	by, _ := row["claimedBy"].(map[string]any)
	cust, _ := row["customer"].(map[string]any)
	lines, _ := row["items"].([]any)
	ask, _ := row["ask"].(map[string]any)
	can, _ := row["canRemind"].(map[string]any)
	if row["orderId"] != orderID || row["orderCode"] != claim["orderCode"] || row["customerRepliedAfterAsk"] != true ||
		by["name"] != "An" || cust["name"] != "Lan" || len(lines) != 1 || row["grandTotal"] != float64(240000) ||
		row["expiresAt"] == nil || ask["status"] != "not_requested" || can["allowed"] != false || can["reason"] == nil {
		t.Errorf("dòng: %v", row)
	}

	rem := mustOK(t, "remindPendingDrafts", e.send(http.MethodPost, livePendingPath(e.session, "/remind"), liveOp,
		fmt.Sprintf(`{"templateId":%q}`, tpl)), http.StatusOK)
	res, _ := rem["results"].([]any)
	r0, _ := res[0].(map[string]any)
	if len(res) != 1 || r0["orderId"] != orderID || r0["outcome"] != "skipped" || r0["reason"] != "Page đang ở chế độ chỉ xem, tin chưa được gửi" {
		t.Errorf("nhắc Page chỉ xem: %v", rem)
	}

	o := mustOK(t, "getOrder", e.send(http.MethodGet, "/api/v1/orders/"+fmt.Sprint(orderID), liveOp, ""), http.StatusOK)
	patched := mustOK(t, "updateOrder", e.send(http.MethodPatch, "/api/v1/orders/"+fmt.Sprint(orderID), liveOp,
		fmt.Sprintf(`{"version":%v,"shipAddress":%s}`, o["version"], fullAddr)), http.StatusOK)
	done := mustOK(t, "confirmOrder", e.send(http.MethodPost, "/api/v1/orders/"+fmt.Sprint(orderID)+"/confirm", liveOp,
		versionBody(patched["version"])), http.StatusOK)
	if done["status"] != "confirmed" {
		t.Fatalf("sau confirm: %v", done)
	}
	if got := livetest.Available(t, e.staff, e.tenant, v); got != held {
		t.Errorf("có thể bán sau xác nhận = %d, muốn %d", got, held)
	}
	list = mustOK(t, "listPendingDrafts", e.send(http.MethodGet, livePendingPath(e.session, ""), liveViewer, ""), http.StatusOK)
	if items, _ := list["items"].([]any); len(items) != 0 || list["confirmedCount"] != float64(1) {
		t.Errorf("sau hoàn tất: %v", list)
	}

	remind := fmt.Sprintf(`{"templateId":%q}`, tpl)
	expect(t, "không live.operate", e.send(http.MethodPost, livePendingPath(e.session, "/remind"), liveViewer, remind),
		http.StatusForbidden, "")
	expect(t, "nhắc Page khác", e.send(http.MethodPost, livePendingPath(e.session, "/remind"), liveOther, remind),
		http.StatusNotFound, "not_found")
	expect(t, "danh sách Page khác", e.send(http.MethodGet, livePendingPath(e.session, ""), liveOther, ""),
		http.StatusNotFound, "not_found")
	expect(t, "phiên lạ", e.send(http.MethodGet, livePendingPath(uuid.New(), ""), liveViewer, ""), http.StatusNotFound, "not_found")
	expect(t, "thiếu mẫu", e.send(http.MethodPost, livePendingPath(e.session, "/remind"), liveOp, `{}`), http.StatusBadRequest, "")
}

// [Task 501] Dòng "thiếu địa chỉ" mang conversationId (hội thoại comment của lần chốt) và inboxConversationId (hộp thư
// của khách trên Page): chưa có hộp thư → không có trường; khách nhắn vào hộp thư → mã hộp thư đó.
func TestLivePendingAPI_ConversationIDs(t *testing.T) {
	t.Parallel()
	e := newLiveClaimAPIEnv(t)
	v := e.variant(5)
	cm := e.comment("Lan", "u1", "chốt", time.Now().Add(-time.Minute))
	mustOK(t, "claimLiveComment", e.send(http.MethodPost, claimPath(cm), liveOp, claimBody(v, 1, `,"askAddress":false`)),
		http.StatusCreated)
	var commentConv uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT conversation_id FROM conversation.messages WHERE id = $1`, cm).Scan(&commentConv)
	})
	row := func() map[string]any {
		t.Helper()
		list := mustOK(t, "listPendingDrafts", e.send(http.MethodGet, livePendingPath(e.session, ""), liveViewer, ""), http.StatusOK)
		items, _ := list["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("danh sách: %v", list)
		}
		r, _ := items[0].(map[string]any)
		return r
	}
	if r := row(); r["conversationId"] != commentConv.String() || r["inboxConversationId"] != nil {
		t.Fatalf("trước khi có hộp thư: %v", r)
	}
	e.customerRepliesInInbox(cm, "12 Lê Lợi, phường Bến Thành", time.Now())
	var inbox uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id FROM conversation.conversations WHERE kind = 'inbox'`).Scan(&inbox)
	})
	if r := row(); r["conversationId"] != commentConv.String() || r["inboxConversationId"] != inbox.String() {
		t.Fatalf("sau khi có hộp thư: %v", r)
	}
}
