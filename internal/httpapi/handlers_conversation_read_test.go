package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/convtest"
)

const (
	crSale   = "sale-doc@elimaz.test"
	crPage2  = "page2-doc@elimaz.test"
	crCSKH   = "cskh-doc@elimaz.test"
	crReader = "chi-doc-chat@elimaz.test"
)

// convReadEnv dùng lại môi trường của test người phụ trách (Task 160: Page 1, Page 2 kênh giả, adapter giả, An, Bình,
// quản lý…) và thêm: Page 1 gửi được (Page 2 chỉ xem); sale đọc, trả lời Page 1; crPage2 đọc, trả lời Page 2; CSKH đọc
// toàn shop và có customer.read; crReader chỉ đọc Page 1. now là giờ thật lúc dựng (chính sách gửi tính theo giờ thật).
type convReadEnv struct {
	*assignAPIEnv
	now time.Time
}

func newConvReadEnv(t *testing.T) *convReadEnv {
	t.Helper()
	e := &convReadEnv{assignAPIEnv: newAssignAPIEnv(t), now: time.Now().UTC().Truncate(time.Second)}
	e.exec(`UPDATE channel.channel_accounts SET send_mode = 'enabled' WHERE id = $1`, e.page1)
	rw := []string{"conversation.read", "conversation.reply"}
	e.ids[crSale] = convtest.Staff(t, e.staff, e.tenant, e.page1, crSale, rw...)
	e.ids[crPage2] = convtest.Staff(t, e.staff, e.tenant, e.page2, crPage2, rw...)
	e.ids[crCSKH] = convtest.Staff(t, e.staff, e.tenant, uuid.Nil, crCSKH, "conversation.read", "customer.read")
	e.ids[crReader] = convtest.Staff(t, e.staff, e.tenant, e.page1, crReader, "conversation.read")
	return e
}

// exec chạy một câu bằng chủ bảng trong ngữ cảnh tenant (dựng dữ liệu thử).
func (e *convReadEnv) exec(sql string, args ...any) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, execStatement(e.t.Context(), sql, args))
}

func execStatement(ctx context.Context, sql string, args []any) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err //nolint:wrapcheck // test
	}
}

// conv tạo khách và hội thoại inbox trên page, tin khách cuối lúc lastCustomer, tin cuối lúc at, unread chưa đọc.
func (e *convReadEnv) conv(page uuid.UUID, thread string, lastCustomer, at time.Time, unread int) uuid.UUID {
	e.t.Helper()
	id := convtest.Conversation(e.t, e.staff, e.tenant, page, thread)
	e.exec(`UPDATE conversation.conversations SET last_customer_message_at = $2, last_message_at = $3, unread_count = $4
		WHERE id = $1`, id, lastCustomer, at, unread)
	return id
}

func (e *convReadEnv) msg(conv, page uuid.UUID, ext string, at time.Time) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO conversation.messages (tenant_id, conversation_id,
			channel_account_id, external_id, direction, kind, origin, body, sent_at)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, 'in', 'message', 'customer', 'chào', $4)
			RETURNING id`, conv, page, ext, at).Scan(&id)
	})
	return id
}

func itemIDs(body map[string]any) []string {
	items, _ := body["items"].([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		if c, ok := m["conversation"].(map[string]any); ok {
			m = c
		}
		id, _ := m["id"].(string)
		out = append(out, id)
	}
	return out
}

// [Task 159, PQ04 cho M4] Sale chỉ có quyền trên Page 1: danh sách, số đếm chỉ có Page 1; chi tiết, tin, thẻ khách,
// bối cảnh lên đơn, đánh dấu đã đọc, đổi trạng thái của hội thoại Page 2 → 404 not_found (từ chối ở máy chủ, không lộ
// sự tồn tại); hội thoại của Page 1 → 200/204.
func TestConversationReadAPI_PQ04(t *testing.T) {
	t.Parallel()
	e := newConvReadEnv(t)
	c1 := e.conv(e.page1, "psid-r1", e.now.Add(-time.Hour), e.now.Add(-time.Hour), 1)
	c2 := e.conv(e.page2, "psid-r2", e.now.Add(-time.Hour), e.now.Add(-30*time.Minute), 1)
	m1 := e.msg(c1, e.page1, "mid-r1", e.now.Add(-time.Hour))
	m2 := e.msg(c2, e.page2, "mid-r2", e.now.Add(-time.Hour))
	code, body := e.do(http.MethodGet, "/api/v1/conversations", crSale, "", "")
	if got := itemIDs(body); code != http.StatusOK || len(got) != 1 || got[0] != c1.String() {
		t.Fatalf("danh sách của sale: %d %v", code, got)
	}
	if code, body := e.do(http.MethodGet, "/api/v1/conversations?pageId="+e.page2.String(), crSale, "", ""); code != http.StatusOK ||
		len(itemIDs(body)) != 0 {
		t.Fatalf("sale lọc Page 2: %d %v", code, body)
	}
	code, body = e.do(http.MethodGet, "/api/v1/conversations/counts", crSale, "", "")
	byPage, _ := body["byPage"].([]any)
	if code != http.StatusOK || len(byPage) != 1 || body["unassigned"] != float64(1) || body["mine"] != float64(0) {
		t.Fatalf("số đếm của sale: %d %v", code, body)
	}
	for _, c := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "", ""},
		{http.MethodGet, "/messages", ""},
		{http.MethodGet, "/customer-card", ""},
		{http.MethodGet, "/order-context", ""},
		{http.MethodPost, "/read", fmt.Sprintf(`{"upToMessageId":%q}`, m2)},
		{http.MethodPost, "/status", `{"status":"done","expectedVersion":1}`},
	} {
		path := "/api/v1/conversations/" + c2.String() + c.path
		key := ""
		if c.method == http.MethodPost {
			key = "k-" + c.path[1:]
		}
		expect(t, "Page 2 "+c.method+c.path, serveAs(e.srv, e.keys, c.method, path, crSale, key, c.body),
			http.StatusNotFound, "not_found")
	}
	for _, p := range []string{"", "/messages", "/customer-card", "/order-context"} {
		if code, _ := e.do(http.MethodGet, "/api/v1/conversations/"+c1.String()+p, crSale, "", ""); code != http.StatusOK {
			t.Errorf("Page 1 %s: %d", p, code)
		}
	}
	if code, _ := e.do(http.MethodPost, "/api/v1/conversations/"+c1.String()+"/read", crSale, "r1",
		fmt.Sprintf(`{"upToMessageId":%q}`, m1)); code != http.StatusNoContent {
		t.Fatalf("đánh dấu đã đọc Page 1: %d", code)
	}
	expect(t, "chỉ đọc đổi trạng thái", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/conversations/"+c1.String()+"/status",
		crReader, "s0", `{"status":"done","expectedVersion":1}`), http.StatusForbidden, "forbidden")
	expect(t, "tin của hội thoại khác", serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/conversations/"+c1.String()+"/read",
		crSale, "r2", fmt.Sprintf(`{"upToMessageId":%q}`, m2)), http.StatusNotFound, "not_found")
}

// [Task 159, CH09] Chi tiết hội thoại trả sendPolicy cho mục đích message: tin khách cuối cách 25 giờ → allowed false,
// reason window_closed; cách 2 giờ → allowed true, mode standard, until = tin khách cuối + 24 giờ; Page chỉ xem →
// reason read_only. Kèm Page (sendMode), doNotContact, blockedByUser.
func TestConversationReadAPI_SendPolicy(t *testing.T) {
	t.Parallel()
	e := newConvReadEnv(t)
	for _, c := range []struct {
		name   string
		page   uuid.UUID
		last   time.Duration
		allow  bool
		reason string
		until  time.Time
	}{
		{"25 giờ", e.page1, 25 * time.Hour, false, "window_closed", time.Time{}},
		{"2 giờ", e.page1, 2 * time.Hour, true, "", e.now.Add(22 * time.Hour)},
		{"Page chỉ xem", e.page2, 2 * time.Hour, false, "read_only", time.Time{}},
	} {
		id := e.conv(c.page, "psid-"+c.name, e.now.Add(-c.last), e.now.Add(-c.last), 0)
		code, d := e.do(http.MethodGet, "/api/v1/conversations/"+id.String(), crCSKH, "", "")
		pol, _ := d["sendPolicy"].(map[string]any)
		page, _ := d["page"].(map[string]any)
		if code != http.StatusOK || pol["allowed"] != c.allow || d["doNotContact"] != false || d["blockedByUser"] != false ||
			page["id"] != c.page.String() {
			t.Errorf("%s: %d %v", c.name, code, d)
			continue
		}
		if c.reason != "" && pol["reason"] != c.reason {
			t.Errorf("%s: reason %v, muốn %s", c.name, pol["reason"], c.reason)
		}
		if c.allow {
			until, err := time.Parse(time.RFC3339, fmt.Sprint(pol["until"]))
			if pol["mode"] != "standard" || err != nil || !until.Equal(c.until) {
				t.Errorf("%s: mode %v until %v, muốn standard %s", c.name, pol["mode"], pol["until"], c.until)
			}
		}
	}
}

// [Task 159, CH01, CH06, CH07] Danh sách qua API: mới nhất trước, có unreadCount; assignee=me sau khi nhận; tagId;
// limit 2 + nextCursor → trang sau không trùng; q quá ngắn → 422 query_too_short; assignee lạ → 422 invalid_input;
// cursor hỏng → 400 invalid_input; limit 101 → 400.
func TestConversationReadAPI_List(t *testing.T) {
	t.Parallel()
	e := newConvReadEnv(t)
	a := e.conv(e.page1, "psid-l1", e.now.Add(-3*time.Hour), e.now.Add(-3*time.Hour), 0)
	b := e.conv(e.page1, "psid-l2", e.now.Add(-2*time.Hour), e.now.Add(-2*time.Hour), 2)
	c := e.conv(e.page1, "psid-l3", e.now.Add(-time.Hour), e.now.Add(-time.Hour), 0)
	code, body := e.do(http.MethodGet, "/api/v1/conversations", crSale, "", "")
	items, _ := body["items"].([]any)
	if got := itemIDs(body); code != http.StatusOK || fmt.Sprint(got) != fmt.Sprint([]string{c.String(), b.String(), a.String()}) {
		t.Fatalf("danh sách: %d %v", code, got)
	}
	if mid, _ := items[1].(map[string]any); mid["unreadCount"] != float64(2) {
		t.Fatalf("unreadCount = %v", mid["unreadCount"])
	}
	if code, _ := e.do(http.MethodPost, "/api/v1/conversations/"+b.String()+"/claim", crSale, "cl", ""); code != http.StatusOK {
		t.Fatalf("nhận: %d", code)
	}
	if _, body := e.do(http.MethodGet, "/api/v1/conversations?assignee=me", crSale, "", ""); fmt.Sprint(itemIDs(body)) !=
		fmt.Sprint([]string{b.String()}) {
		t.Fatalf("của tôi: %v", itemIDs(body))
	}
	var tag uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO conversation.tags (tenant_id, name, color)
			VALUES (current_setting('app.tenant_id')::uuid, 'Đã lên đơn', 'green') RETURNING id`).Scan(&tag)
	})
	e.exec(`INSERT INTO conversation.conversation_tag_links (tenant_id, conversation_id, tag_id)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2)`, a, tag)
	if _, body := e.do(http.MethodGet, "/api/v1/conversations?tagId="+tag.String(), crSale, "", ""); fmt.Sprint(itemIDs(body)) !=
		fmt.Sprint([]string{a.String()}) {
		t.Fatalf("theo nhãn: %v", itemIDs(body))
	}
	_, body = e.do(http.MethodGet, "/api/v1/conversations?limit=2", crSale, "", "")
	next, _ := body["nextCursor"].(string)
	first := itemIDs(body)
	_, body = e.do(http.MethodGet, "/api/v1/conversations?limit=2&cursor="+url.QueryEscape(next), crSale, "", "")
	if len(first) != 2 || next == "" || fmt.Sprint(itemIDs(body)) != fmt.Sprint([]string{a.String()}) || body["nextCursor"] != nil {
		t.Fatalf("phân trang: %v, %v, next %v", first, itemIDs(body), body["nextCursor"])
	}
	expect(t, "q ngắn", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/conversations?q=56", crSale, "", ""),
		http.StatusUnprocessableEntity, "query_too_short")
	expect(t, "assignee lạ", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/conversations?assignee=ai", crSale, "", ""),
		http.StatusUnprocessableEntity, "invalid_input")
	expect(t, "cursor hỏng", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/conversations?cursor=xx", crSale, "", ""),
		http.StatusBadRequest, "invalid_input")
	expect(t, "limit 101", serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/conversations?limit=101", crSale, "", ""),
		http.StatusBadRequest, "")
}

// [Task 159] Tin qua API theo thứ tự thời gian, olderCursor/newerCursor; tin thường không có privateReply, cờ ẩn/xóa
// false, editedCount 0, đính kèm rỗng. Đánh dấu đã đọc → 204, unreadCount về 0. Đổi trạng thái: version cũ → 409
// version_conflict; đúng → 200 chi tiết có status done, version + 1; gửi lại cùng khóa → Idempotent-Replayed.
func TestConversationReadAPI_MessagesReadStatus(t *testing.T) {
	t.Parallel()
	e := newConvReadEnv(t)
	id := e.conv(e.page1, "psid-m1", e.now.Add(-time.Hour), e.now.Add(-time.Hour), 3)
	var m []uuid.UUID
	for i := range 3 {
		m = append(m, e.msg(id, e.page1, fmt.Sprintf("mid-m%d", i), e.now.Add(-time.Hour+time.Duration(i)*time.Minute)))
	}
	base := "/api/v1/conversations/" + id.String()
	code, body := e.do(http.MethodGet, base+"/messages?limit=2", crSale, "", "")
	items, _ := body["items"].([]any)
	older, _ := body["olderCursor"].(string)
	if code != http.StatusOK || fmt.Sprint(itemIDs(body)) != fmt.Sprint([]string{m[1].String(), m[2].String()}) ||
		older == "" || body["newerCursor"] == nil {
		t.Fatalf("trang tin: %d %v", code, body)
	}
	msg0, _ := items[0].(map[string]any)
	atts, _ := msg0["attachments"].([]any)
	if _, has := msg0["privateReply"]; has || msg0["isHidden"] != false || msg0["removed"] != false ||
		msg0["editedCount"] != float64(0) || atts == nil || len(atts) != 0 || msg0["body"] != "chào" ||
		msg0["direction"] != "in" {
		t.Fatalf("tin thường = %v", msg0)
	}
	_, body = e.do(http.MethodGet, base+"/messages?limit=2&before="+url.QueryEscape(older), crSale, "", "")
	if fmt.Sprint(itemIDs(body)) != fmt.Sprint([]string{m[0].String()}) || body["olderCursor"] != nil {
		t.Fatalf("trang trước: %v", body)
	}
	if code, _ := e.do(http.MethodPost, base+"/read", crSale, "rd", fmt.Sprintf(`{"upToMessageId":%q}`, m[2])); code != http.StatusNoContent {
		t.Fatalf("đã đọc: %d", code)
	}
	_, d := e.do(http.MethodGet, base, crSale, "", "")
	conv, _ := d["conversation"].(map[string]any)
	if conv["unreadCount"] != float64(0) || conv["version"] != float64(1) {
		t.Fatalf("sau khi đọc: %v", conv)
	}
	expect(t, "version cũ", serveAs(e.srv, e.keys, http.MethodPost, base+"/status", crSale, "st0",
		`{"status":"done","expectedVersion":2}`), http.StatusConflict, "version_conflict")
	code, d = e.do(http.MethodPost, base+"/status", crSale, "st1", `{"status":"done","expectedVersion":1}`)
	conv, _ = d["conversation"].(map[string]any)
	if code != http.StatusOK || conv["status"] != "done" || conv["version"] != float64(2) {
		t.Fatalf("đổi trạng thái: %d %v", code, d)
	}
	again := serveAs(e.srv, e.keys, http.MethodPost, base+"/status", crSale, "st1", `{"status":"done","expectedVersion":1}`)
	if again.Code != http.StatusOK || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại cùng khóa: %d", again.Code)
	}
}

// [Task 159, DH03 phần hiển thị] Thẻ khách qua API: chỉ số của M2 (returnRateBp đúng bằng customer.Get: 8 giao, 2 hoàn
// → 2000); sale không có customer.read → không có phones; CSKH có → phones. Bối cảnh lên đơn: Page 1 có nguồn hộp
// thư → trafficSourceId; người bán của Page.
func TestConversationReadAPI_CardAndOrderContext(t *testing.T) {
	t.Parallel()
	e := newConvReadEnv(t)
	id := e.conv(e.page1, "psid-c1", e.now.Add(-time.Hour), e.now.Add(-time.Hour), 0)
	e.exec(`UPDATE customer.customers SET order_count = 10, delivered_count = 8, returned_count = 2
		WHERE id = (SELECT customer_id FROM conversation.conversations WHERE id = $1)`, id)
	e.exec(`INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, scope, value, value_reversed, source)
		SELECT current_setting('app.tenant_id')::uuid, customer_id, 'phone', '', '+84905556666', '66665550948', 'staff'
		FROM conversation.conversations WHERE id = $1`, id)
	base := "/api/v1/conversations/" + id.String()
	code, body := e.do(http.MethodGet, base+"/customer-card", crSale, "", "")
	cu, _ := body["customer"].(map[string]any)
	if _, has := cu["phones"]; code != http.StatusOK || has || cu["returnRateBp"] != float64(2000) ||
		cu["deliveredCount"] != float64(8) || cu["returnedCount"] != float64(2) {
		t.Fatalf("thẻ của sale: %d %v", code, body)
	}
	_, body = e.do(http.MethodGet, base+"/customer-card", crCSKH, "", "")
	cu, _ = body["customer"].(map[string]any)
	if phones, _ := cu["phones"].([]any); len(phones) != 1 || phones[0] != "+84905556666" {
		t.Fatalf("thẻ của CSKH: %v", body)
	}
	src := e.staff.TrafficSource(t, e.tenant, "Hộp thư", true)
	e.exec(`INSERT INTO conversation.page_settings (tenant_id, channel_account_id, inbox_traffic_source_id)
		VALUES (current_setting('app.tenant_id')::uuid, $1, $2)`, e.page1, src)
	code, body = e.do(http.MethodGet, base+"/order-context", crSale, "", "")
	if code != http.StatusOK || body["trafficSourceId"] != src.String() || body["channelAccountId"] != e.page1.String() ||
		body["conversationId"] != id.String() || body["sellerId"] == nil || body["customerId"] == nil {
		t.Fatalf("bối cảnh lên đơn: %d %v", code, body)
	}
}
