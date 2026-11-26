package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit"
)

// Nhân viên của chatAPIEnv: saleA đọc, trả lời Page A (và đọc khách cả shop); saleA2 như saleA; lead có mọi quyền hộp
// chat ở mọi Page.
const (
	chatSaleA  = "sale-a@elimaz.test"
	chatSaleA2 = "sale-a2@elimaz.test"
	chatLead   = "lead@elimaz.test"
	// chatCustomerOnly chỉ đọc khách, không có quyền hộp chat nào.
	chatCustomerOnly = "khach-only@elimaz.test"
)

// chatAPIEnv (Task 169): khách gốc ka, hồ sơ kb đã gộp vào ka; hội thoại inbox convA (Page A, ka, tạo 01/11), comment
// convB (Page B, kb, tạo 02/11); 2 đơn của ka (03/11, 04/11). Đồng hồ của máy chủ (now): giờ thật + 1 giờ, để cuộc
// gọi luôn mới hơn ghi chú (ghi bằng now() của database) mà không so hai đồng hồ ở mức giây.
type chatAPIEnv struct {
	t            *testing.T
	staff        *testkit.Staff
	keys         *cfaccesstest.AccessKeys
	srv          http.Handler
	tenant       uuid.UUID
	pageA, pageB uuid.UUID
	ka, kb       uuid.UUID
	saleA        uuid.UUID
	convA, convB uuid.UUID
	key          int
	now          time.Time
}

func newChatAPIEnv(t *testing.T) *chatAPIEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &chatAPIEnv{
		t: t, staff: s, keys: cfaccesstest.AccessKeyServer(t), tenant: s.Tenant(t, "A"), pageA: uuid.New(), pageB: uuid.New(),
		now: time.Now().UTC().Add(time.Hour).Truncate(time.Second),
	}
	for _, u := range []struct {
		email string
		all   bool
	}{{chatSaleA, false}, {chatSaleA2, false}, {chatLead, true}} {
		id := s.User(t, e.tenant, u.email, "active")
		if u.email == chatSaleA {
			e.saleA = id
		}
		s.Grant(t, e.tenant, id, []string{"customer.read", "customer.write"}, "all", "")
		if u.all {
			s.Grant(t, e.tenant, id, []string{"conversation.read", "conversation.reply", "conversation.assign"}, "all", "")
		} else {
			s.Grant(t, e.tenant, id, []string{"conversation.read", "conversation.reply"}, "page", e.pageA.String())
		}
	}
	ctx := t.Context()
	s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for _, id := range []*uuid.UUID{&e.ka, &e.kb} {
			if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name, name_search)
				VALUES (current_setting('app.tenant_id')::uuid, 'Chị Lan', 'chi lan') RETURNING id`).Scan(id); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		var post, seller, wh uuid.UUID
		var err error
		scan := func(out *uuid.UUID, sql string, args ...any) {
			if err == nil {
				err = tx.QueryRow(ctx, sql, args...).Scan(out)
			}
		}
		scan(new(uuid.UUID), `UPDATE customer.customers SET root_id = $1 WHERE id = $2 RETURNING id`, e.ka, e.kb)
		scan(&post, `INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'post') RETURNING id`, e.pageB)
		scan(&e.convA, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
			external_thread_ref, last_message_at, created_at, last_snippet) VALUES (current_setting('app.tenant_id')::uuid, $1,
			$2, 'inbox', 'psid', '2026-11-01Z', '2026-11-01Z', 'noi-dung-tin-mat') RETURNING id`, e.pageA, e.ka)
		scan(&e.convB, `INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind,
			external_thread_ref, post_id, last_message_at, created_at) VALUES (current_setting('app.tenant_id')::uuid, $1, $2,
			'comment', 'post:psid', $3, '2026-11-02Z', '2026-11-02Z') RETURNING id`, e.pageB, e.kb, post)
		scan(&seller, `INSERT INTO catalog.sellers (tenant_id, code, name, kind)
			VALUES (current_setting('app.tenant_id')::uuid, 'S1', 'Shop', 'own') RETURNING id`)
		scan(&wh, `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES (current_setting('app.tenant_id')::uuid, $1, 'MAIN', 'Kho') RETURNING id`, seller)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = tx.Exec(ctx, `INSERT INTO ord.orders (tenant_id, seller_id, code, customer_id, channel, idempotency_key, status,
				pending_reason, packing_status, ship_address, subtotal, cod_amount, grand_total, warehouse_id, ship_phone,
				ship_phone_reversed, created_at)
			SELECT current_setting('app.tenant_id')::uuid, $1, 'DH' || lpad(g::text, 7, '0'), $2, 'manual', gen_random_uuid(), 'new', '', 'none', '{}',
			       0, 0, 0, $3, '+84900000000', '00000000948', timestamptz '2026-11-02' + g * interval '1 day'
			FROM generate_series(1, 2) AS g`, seller, e.ka, wh)
		return err //nolint:wrapcheck // test
	})
	s.Grant(t, e.tenant, s.User(t, e.tenant, chatCustomerOnly, "active"), []string{"customer.read"}, "all", "")
	d := testServerDeps(t, s, e.keys, 0)
	d.Realtime, d.Now = &realtimetest.RecordingBus{}, func() time.Time { return e.now }
	e.srv = NewServer(d)
	return e
}

func (e *chatAPIEnv) send(method, path, email, body string) *httptest.ResponseRecorder {
	key := ""
	if method != http.MethodGet {
		e.key++
		key = fmt.Sprintf("chat-%d-%s", e.key, uuid.NewString())
	}
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

// at trả mốc now + d dạng RFC 3339 (thân JSON).
func (e *chatAPIEnv) at(d time.Duration) string { return e.now.Add(d).Format(time.RFC3339) }

func (e *chatAPIEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// Cuộc gọi qua HTTP (Task 169 Step 1): ghi cuộc gọi không nghe máy có hội thoại → 201, 1 dòng, 1 tin system, audit không
// có ghi chú; hẹn gọi lại thiếu giờ → 400, có giờ → nhắc cho người gọi; /me/reminders của người khác không thấy; cuộc
// gọi hỏi theo hồ sơ đã gộp thấy cuộc gọi của gốc; hội thoại của khách lọc theo Page.
func TestChatAPI_Calls(t *testing.T) {
	t.Parallel()
	e := newChatAPIEnv(t)
	ka := e.ka.String()
	call := mustOK(t, "createCallLog", e.send(http.MethodPost, "/api/v1/customers/"+ka+"/calls", chatSaleA,
		fmt.Sprintf(`{"conversationId":%q,"outcome":"no_answer","note":"ghi-chu-goi-mat"}`, e.convA)), http.StatusCreated)
	if call["outcome"] != "no_answer" || call["conversationId"] != e.convA.String() || call["createdBy"].(map[string]any)["name"] != chatSaleA {
		t.Errorf("cuộc gọi: %v", call)
	}
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE conversation_id = $1 AND kind = 'system'
		AND body = 'Đã gọi: không nghe máy'`, e.convA); n != 1 {
		t.Errorf("tin system: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'call_log.create'
		AND after::text NOT LIKE '%goi-mat%'`); n != 1 {
		t.Errorf("audit cuộc gọi không chứa ghi chú: %d", n)
	}
	expect(t, "callback thiếu giờ", e.send(http.MethodPost, "/api/v1/customers/"+ka+"/calls", chatSaleA,
		fmt.Sprintf(`{"conversationId":%q,"outcome":"callback"}`, e.convA)), http.StatusBadRequest, "invalid_input")
	expect(t, "hội thoại Page B", e.send(http.MethodPost, "/api/v1/customers/"+ka+"/calls", chatSaleA,
		fmt.Sprintf(`{"conversationId":%q,"outcome":"closed"}`, e.convB)), http.StatusNotFound, "not_found")
	expect(t, "khách không có", e.send(http.MethodPost, "/api/v1/customers/"+uuid.NewString()+"/calls", chatSaleA,
		`{"outcome":"closed"}`), http.StatusNotFound, "not_found")
	cb := mustOK(t, "createCallLog callback", e.send(http.MethodPost, "/api/v1/customers/"+ka+"/calls", chatSaleA,
		fmt.Sprintf(`{"conversationId":%q,"outcome":"callback","callbackAt":%q}`, e.convA, e.at(5*time.Hour))), http.StatusCreated)
	if !sameInstant(cb["callbackAt"], e.at(5*time.Hour)) {
		t.Errorf("giờ gọi lại: %v", cb)
	}

	mine := func(email, status string) []any {
		t.Helper()
		return mustOK(t, "listMyReminders", e.send(http.MethodGet, "/api/v1/me/reminders?status="+status, email, ""), http.StatusOK)["items"].([]any)
	}
	got := mine(chatSaleA, "pending")
	if len(got) != 1 || got[0].(map[string]any)["source"] != "call_callback" || !sameInstant(got[0].(map[string]any)["remindAt"], e.at(5*time.Hour)) {
		t.Errorf("nhắc gọi lại của sale A: %v", got)
	}
	if got := mine(chatSaleA2, "pending"); len(got) != 0 {
		t.Errorf("sale A2 thấy nhắc của sale A: %v", got)
	}

	// Cuộc gọi theo hồ sơ đã gộp; Page.
	listCallsAndConversations(t, e, fmt.Sprint(call["id"]))
}

// Nhắc việc qua HTTP (Task 169 Step 1): sale giao nhắc cho người khác → 403; giờ đã qua → 400; Page không được trả lời
// → 404; người khác đóng nhắc → 404; "xong" hai lần song song → một 200, một 409; hủy nhắc đã xong → 409; trưởng nhóm
// giao nhắc được.
func TestChatAPI_Reminders(t *testing.T) {
	t.Parallel()
	e := newChatAPIEnv(t)
	reminders := "/api/v1/conversations/" + e.convA.String() + "/reminders"
	expect(t, "sale giao nhắc cho người khác", e.send(http.MethodPost, reminders, chatSaleA,
		fmt.Sprintf(`{"remindAt":%q,"userId":%q}`, e.at(time.Hour), uuid.New())), http.StatusForbidden, "forbidden")
	expect(t, "giờ đã qua", e.send(http.MethodPost, reminders, chatSaleA, fmt.Sprintf(`{"remindAt":%q}`, e.at(-time.Hour))),
		http.StatusBadRequest, "invalid_input")
	expect(t, "hội thoại Page B", e.send(http.MethodPost, "/api/v1/conversations/"+e.convB.String()+"/reminders", chatSaleA,
		fmt.Sprintf(`{"remindAt":%q}`, e.at(time.Hour))), http.StatusNotFound, "not_found")
	r := mustOK(t, "createReminder", e.send(http.MethodPost, reminders, chatSaleA, fmt.Sprintf(`{"remindAt":%q,"note":"hỏi size"}`, e.at(time.Hour))),
		http.StatusCreated)
	id := fmt.Sprint(r["id"])
	if r["status"] != "pending" || r["source"] != "manual" || r["note"] != "hỏi size" {
		t.Errorf("nhắc: %v", r)
	}
	expect(t, "người khác đóng nhắc", e.send(http.MethodPost, "/api/v1/reminders/"+id+"/done", chatSaleA2, ""),
		http.StatusNotFound, "not_found")
	recs := make([]*httptest.ResponseRecorder, 2)
	keys := []string{"done-1-" + uuid.NewString(), "done-2-" + uuid.NewString()}
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() {
			recs[i] = serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/reminders/"+id+"/done", chatSaleA, keys[i], "")
		})
	}
	wg.Wait()
	codes := map[int]int{}
	for _, rec := range recs {
		codes[rec.Code]++
	}
	if codes[http.StatusOK] != 1 || codes[http.StatusConflict] != 1 {
		t.Errorf("xong song song: %v (%s | %s)", codes, recs[0].Body.String(), recs[1].Body.String())
	}
	expect(t, "hủy nhắc đã xong", e.send(http.MethodPost, "/api/v1/reminders/"+id+"/cancel", chatSaleA, ""),
		http.StatusConflict, "version_conflict")
	lead := mustOK(t, "createReminder giao", e.send(http.MethodPost, reminders, chatLead,
		fmt.Sprintf(`{"remindAt":%q,"userId":%q}`, e.at(30*time.Hour), e.saleA)), http.StatusCreated)
	if lead["userId"] != e.saleA.String() {
		t.Errorf("trưởng nhóm giao nhắc: %v", lead)
	}
}

// listCallsAndConversations: cuộc gọi hỏi theo hồ sơ đã gộp kb thấy cuộc gọi callID ghi trên gốc; con trỏ hỏng → 400;
// hội thoại của khách lọc theo Page.
func listCallsAndConversations(t *testing.T, e *chatAPIEnv, callID string) {
	t.Helper()
	ka, kb := e.ka.String(), e.kb.String()
	calls := mustOK(t, "listCustomerCalls", e.send(http.MethodGet, "/api/v1/customers/"+kb+"/calls", chatLead, ""), http.StatusOK)["items"].([]any)
	found := false
	for _, c := range calls {
		m := c.(map[string]any)
		found = found || (m["id"] == callID && m["note"] == "ghi-chu-goi-mat")
	}
	if len(calls) != 2 || !found {
		t.Errorf("cuộc gọi theo hồ sơ đã gộp: %v", calls)
	}
	expect(t, "con trỏ hỏng", e.send(http.MethodGet, "/api/v1/customers/"+ka+"/calls?cursor=xyz", chatLead, ""),
		http.StatusBadRequest, "invalid_input")
	convs := mustOK(t, "listCustomerConversations", e.send(http.MethodGet, "/api/v1/customers/"+ka+"/conversations", chatLead, ""),
		http.StatusOK)["items"].([]any)
	if len(convs) != 2 || convs[0].(map[string]any)["id"] != e.convB.String() || convs[1].(map[string]any)["lastSnippet"] != "noi-dung-tin-mat" {
		t.Errorf("hội thoại mọi Page: %v", convs)
	}
	convs = mustOK(t, "listCustomerConversations", e.send(http.MethodGet, "/api/v1/customers/"+kb+"/conversations?limit=1", chatSaleA, ""),
		http.StatusOK)["items"].([]any)
	if len(convs) != 1 || convs[0].(map[string]any)["id"] != e.convA.String() {
		t.Errorf("hội thoại Page A: %v", convs)
	}
}

// Dòng thời gian khách có hội thoại (C7, Task 169 Step 1): 2 đơn, 1 ghi chú, hội thoại hộp thư Page A, comment Page B
// (của hồ sơ đã gộp), 1 cuộc gọi trên hội thoại A → người đọc cả hai Page thấy 6 mục giảm dần; chỉ Page A → 5; hai
// trang 3 + 3 không trùng, không sót; thân không có nội dung tin, ghi chú cuộc gọi; người không có quyền hộp chat → 3
// mục như M2.
func TestChatAPI_CustomerTimeline(t *testing.T) {
	t.Parallel()
	e := newChatAPIEnv(t)
	ka := e.ka.String()
	mustOK(t, "addCustomerNote", e.send(http.MethodPost, "/api/v1/customers/"+ka+"/notes", chatLead, `{"body":"khách quen"}`),
		http.StatusCreated)
	mustOK(t, "createCallLog", e.send(http.MethodPost, "/api/v1/customers/"+ka+"/calls", chatSaleA,
		fmt.Sprintf(`{"conversationId":%q,"outcome":"closed","note":"ghi-chu-goi-mat"}`, e.convA)), http.StatusCreated)
	timeline := func(email, query string) (kinds []string, next string, body string) {
		t.Helper()
		rec := e.send(http.MethodGet, "/api/v1/customers/"+ka+"/timeline"+query, email, "")
		page := mustOK(t, "getCustomerTimeline", rec, http.StatusOK)
		for _, it := range page["items"].([]any) {
			m := it.(map[string]any)
			kinds = append(kinds, fmt.Sprint(m["kind"], ":", m["refId"]))
		}
		if c, ok := page["nextCursor"].(string); ok {
			next = c
		}
		return kinds, next, rec.Body.String()
	}
	all, next, body := timeline(chatLead, "")
	short := make([]string, 0, len(all))
	for _, k := range all {
		kind, _, _ := strings.Cut(k, ":")
		short = append(short, kind)
	}
	if want := "call,note,order_created,order_created,conversation,conversation"; strings.Join(short, ",") != want || next != "" {
		t.Errorf("mọi Page: %v (next %q), mong %s", short, next, want)
	}
	if !strings.Contains(all[4], e.convB.String()) || !strings.Contains(all[5], e.convA.String()) {
		t.Errorf("thứ tự hội thoại: %v", all)
	}
	if strings.Contains(body, "goi-mat") || strings.Contains(body, "noi-dung-tin-mat") {
		t.Errorf("thân lộ nội dung: %s", body)
	}
	pageA, _, _ := timeline(chatSaleA, "")
	if len(pageA) != 5 || strings.Contains(strings.Join(pageA, ","), e.convB.String()) {
		t.Errorf("chỉ Page A: %v", pageA)
	}
	if only, _, _ := timeline(chatCustomerOnly, ""); len(only) != 3 || strings.Contains(strings.Join(only, ","), "call") ||
		strings.Contains(strings.Join(only, ","), "conversation") {
		t.Errorf("không quyền hộp chat: %v", only)
	}
	p1, cur, _ := timeline(chatLead, "?limit=3")
	if cur == "" {
		t.Fatal("thiếu con trỏ trang sau")
	}
	p2, last, _ := timeline(chatLead, "?limit=3&cursor="+cur)
	if strings.Join(append(p1, p2...), ",") != strings.Join(all, ",") || last != "" {
		t.Errorf("hai trang: %v + %v (next %q), mong %v", p1, p2, last, all)
	}
}

// sameInstant: v (chuỗi RFC 3339 của thân JSON) là cùng thời điểm với want.
func sameInstant(v any, want string) bool {
	got, err1 := time.Parse(time.RFC3339, fmt.Sprint(v))
	w, err2 := time.Parse(time.RFC3339, want)
	return err1 == nil && err2 == nil && got.Equal(w)
}
