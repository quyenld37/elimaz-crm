package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/testkit"
)

// CH13 (API): GET /api/v1/messages/{id}/revisions → 200 hai dòng theo thứ tự ghi (nội dung cũ, cờ, nguồn, mốc);
// người đọc Page khác, tin không có → 404 thân lỗi chuẩn; không có conversation.read → 403.
func TestListMessageRevisions(t *testing.T) {
	t.Parallel()
	staff, keys := testkit.OpenStaff(t), cfaccesstest.AccessKeyServer(t)
	tenant := staff.Tenant(t, "A")
	page := staff.ChannelAccount(t, tenant, "facebook", "page-rev", "Page A")
	for email, scope := range map[string]string{"doc-rev@elimaz.test": page.String(), "khac-rev@elimaz.test": uuid.NewString()} {
		staff.Grant(t, tenant, staff.User(t, tenant, email, "active"), []string{"conversation.read"}, "page", scope)
	}
	staff.Grant(t, tenant, staff.User(t, tenant, "kho-rev@elimaz.test", "active"), []string{"order.read"}, "all", "")
	var msg uuid.UUID
	// Một câu dựng khách, bài, hội thoại comment, comment đã xóa và hai dòng lịch sử (sửa, xóa).
	staff.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `WITH ten AS (SELECT current_setting('app.tenant_id')::uuid AS id),
			cus AS (INSERT INTO customer.customers (tenant_id, full_name, name_search)
				SELECT id, 'Chị Lan', 'chi lan' FROM ten RETURNING id),
			pst AS (INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
				SELECT id, $1, 'p-1' FROM ten RETURNING id),
			cnv AS (INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
				external_thread_ref, post_id, last_message_at)
				SELECT ten.id, $1, cus.id, 'comment', 'p-1:psid-1', pst.id, now() FROM ten, cus, pst RETURNING id),
			msg AS (INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id,
				direction, kind, origin, body, removed_at, sent_at)
				SELECT ten.id, cnv.id, $1, 'c-1', 'in', 'comment', 'customer', 'Chốt 2 áo', now(), now() FROM ten, cnv
				RETURNING id),
			rev AS (INSERT INTO conversation.message_revisions (tenant_id, message_id, body, is_hidden, removed, source,
				recorded_at)
				SELECT ten.id, msg.id, r.body, false, r.removed, 'customer', r.at FROM ten, msg,
				(VALUES ('Chốt 1 áo', false, '2026-11-26 01:00Z'::timestamptz),
				        ('Chốt 2 áo', true, '2026-11-26 02:00Z'::timestamptz)) AS r (body, removed, at))
			SELECT id FROM msg`, page).Scan(&msg)
	})
	srv := NewServer(testServerDeps(t, staff, keys, 0))
	path := func(id uuid.UUID) string { return "/api/v1/messages/" + id.String() + "/revisions" }

	rec := serveAs(srv, keys, http.MethodGet, path(msg), "doc-rev@elimaz.test", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			Body       *string `json:"body"`
			IsHidden   bool    `json:"isHidden"`
			Removed    bool    `json:"removed"`
			Source     string  `json:"source"`
			RecordedAt string  `json:"recordedAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 || body.Items[0].Body == nil || *body.Items[0].Body != "Chốt 1 áo" || body.Items[0].Removed ||
		!body.Items[1].Removed || body.Items[1].Source != "customer" || body.Items[0].RecordedAt != "2026-11-26T01:00:00Z" {
		t.Fatalf("thân: %s", rec.Body.String())
	}
	for _, c := range []struct {
		name, email string
		id          uuid.UUID
		code        int
	}{
		{"Page khác", "khac-rev@elimaz.test", msg, http.StatusNotFound},
		{"tin không có", "doc-rev@elimaz.test", uuid.New(), http.StatusNotFound},
		{"không có quyền đọc hội thoại", "kho-rev@elimaz.test", msg, http.StatusForbidden},
	} {
		rec := serveAs(srv, keys, http.MethodGet, path(c.id), c.email, "", "")
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if rec.Code != c.code || e.Error == "" || len(rec.Body.String()) > 200 {
			t.Errorf("%s: %d %s, mong %d", c.name, rec.Code, rec.Body.String(), c.code)
		}
	}
}
