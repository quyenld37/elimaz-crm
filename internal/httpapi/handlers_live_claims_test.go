package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/cfaccess/cfaccesstest"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/livetest"
)

const (
	liveOp     = "live-op@elimaz.test"     // live.operate + conversation.read trên Page, order.create, order.update
	liveNoOrd  = "live-noord@elimaz.test"  // live.operate + conversation.read trên Page, không quyền đơn
	liveViewer = "live-viewer@elimaz.test" // chỉ conversation.read trên Page
	liveOther  = "live-other@elimaz.test"  // live.operate + conversation.read trên Page khác, order.create
)

// liveClaimAPIEnv: tenant A, người bán ELIMAZ, kho MAIN, một Page kênh giả (chỉ xem) có phiên live đang chạy.
type liveClaimAPIEnv struct {
	t       *testing.T
	staff   *testkit.Staff
	keys    *cfaccesstest.AccessKeys
	srv     http.Handler
	shop    *invtest.Shop
	tenant  uuid.UUID
	page    uuid.UUID
	session uuid.UUID
	n       int
	mu      sync.Mutex
}

func newLiveClaimAPIEnv(t *testing.T) *liveClaimAPIEnv {
	t.Helper()
	e := &liveClaimAPIEnv{t: t, staff: testkit.OpenStaff(t), keys: cfaccesstest.AccessKeyServer(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = invtest.NewShop(t, tx, e.tenant)
		return order.EnsureDefaults(t.Context(), tx, e.tenant)
	})
	e.page = e.staff.ChannelAccount(t, e.tenant, "fakechat", "page-live-172", "Shop thử")
	otherPage := uuid.NewString()
	for email, g := range map[string]struct {
		page  string
		perms []string
		order bool
	}{
		liveOp:     {e.page.String(), []string{"live.operate", "conversation.read"}, true},
		liveNoOrd:  {e.page.String(), []string{"live.operate", "conversation.read"}, false},
		liveViewer: {e.page.String(), []string{"conversation.read"}, false},
		liveOther:  {otherPage, []string{"live.operate", "conversation.read"}, true},
	} {
		id := e.staff.User(t, e.tenant, email, "active")
		e.staff.Grant(t, e.tenant, id, g.perms, "page", g.page)
		if g.order {
			e.staff.Grant(t, e.tenant, id, []string{"order.create", "order.update", "order.read"}, "all", "")
		}
	}
	e.exec(`UPDATE identity.users SET display_name = 'An' WHERE email = $1`, liveOp)
	e.session = livetest.LiveSession(t, e.staff, e.tenant, e.page, "lv-http", time.Now())
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Realtime = &realtimetest.RecordingBus{}
	e.srv = NewServer(d)
	return e
}

// exec chạy câu sửa dữ liệu thử sql (chủ bảng) trong tenant A.
func (e *liveClaimAPIEnv) exec(sql string, args ...any) {
	e.t.Helper()
	livetest.Exec(e.t, e.staff, e.tenant, sql, args...)
}

// variant tạo biến thể giá 120 000 của người bán ELIMAZ, nạp onHand ở kho MAIN.
func (e *liveClaimAPIEnv) variant(onHand int32) (id uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id = e.shop.Variant(e.t, tx, e.shop.Seller, 120_000, onHand)
		return nil
	})
	return id
}

// comment chèn comment live body của khách mới tên name (mã người from) dưới bài lv-http, lúc at; trả mã tin.
func (e *liveClaimAPIEnv) comment(name, from, body string, at time.Time) uuid.UUID {
	e.t.Helper()
	cust := livetest.Customer(e.t, e.staff, e.tenant, name)
	return livetest.LiveComment(e.t, e.staff, e.tenant, e.page, cust, "lv-http", from, "cm-"+from, body, at).ID
}

func (e *liveClaimAPIEnv) send(method, path, email, body string) *httptest.ResponseRecorder {
	key := ""
	if method != http.MethodGet {
		e.mu.Lock()
		e.n++
		key = fmt.Sprintf("claim-%d-%s", e.n, uuid.NewString())
		e.mu.Unlock()
	}
	return serveAs(e.srv, e.keys, method, path, email, key, body)
}

func claimBody(v uuid.UUID, qty int, extra string) string {
	return fmt.Sprintf(`{"items":[{"variantId":%q,"qty":%d}],"mode":"new"%s}`, v, qty, extra)
}

func claimPath(id uuid.UUID) string { return "/api/v1/live-comments/" + id.String() + "/claim" }

// Chốt comment live qua HTTP (Task 172): 201 kèm đơn, ghi nhận (tên An), ask not_requested (Page chưa chọn mẫu); lần hai
// 409 comment_claimed kèm claimedBy, orderId, orderCode; comment khác cùng khách → 409 open_draft_exists kèm drafts;
// hết hàng → 409 out_of_stock (variantId, variantIds, available); Page người bán khác → 422 seller_mismatch; không
// live.operate → 403; không order.create → 403; Page khác → 404; yêu cầu sai → 400.
func TestLiveClaimsAPI_Claim(t *testing.T) {
	t.Parallel()
	e := newLiveClaimAPIEnv(t)
	v, empty := e.variant(5), e.variant(0)
	c1 := e.comment("Lan", "u1", "chốt 0901234567", time.Now().Add(-time.Minute))
	got := mustOK(t, "claimLiveComment", e.send(http.MethodPost, claimPath(c1), liveOp, claimBody(v, 2, "")), http.StatusCreated)
	claim, _ := got["claim"].(map[string]any)
	by, _ := claim["by"].(map[string]any)
	ask, _ := got["ask"].(map[string]any)
	if got["orderCode"] == "" || by["name"] != "An" || claim["orderId"] != got["orderId"] || ask["status"] != "not_requested" ||
		ask["reason"] == nil {
		t.Fatalf("201: %v", got)
	}
	cases := []struct {
		name, path, email, body, code string
		status                        int
		check                         func(map[string]any) bool
	}{
		{"đã chốt", claimPath(c1), liveOp, claimBody(v, 1, ""), "comment_claimed", http.StatusConflict, func(b map[string]any) bool {
			return b["claimedBy"] == "An" && b["orderId"] == got["orderId"] && b["orderCode"] == got["orderCode"]
		}},
		{"không live.operate", claimPath(c1), liveViewer, claimBody(v, 1, ""), "forbidden", http.StatusForbidden, nil},
		{"không order.create", claimPath(c1), liveNoOrd, claimBody(v, 1, ""), "forbidden", http.StatusForbidden, nil},
		{"Page khác", claimPath(c1), liveOther, claimBody(v, 1, ""), "not_found", http.StatusNotFound, nil},
		{"comment lạ", claimPath(uuid.New()), liveOp, claimBody(v, 1, ""), "not_found", http.StatusNotFound, nil},
		{
			"thêm vào nháp thiếu đơn", claimPath(c1), liveOp, fmt.Sprintf(`{"items":[{"variantId":%q,"qty":1}],"mode":"add_to_draft"}`, v),
			"invalid_input", http.StatusBadRequest, nil,
		},
	}
	c2 := e.comment("Mai", "u2", "chốt", time.Now().Add(-time.Minute))
	cases = append(cases, struct {
		name, path, email, body, code string
		status                        int
		check                         func(map[string]any) bool
	}{"hết hàng", claimPath(c2), liveOp, claimBody(empty, 1, ""), "out_of_stock", http.StatusConflict, func(b map[string]any) bool {
		ids, _ := b["variantIds"].([]any)
		return len(ids) == 1 && ids[0] == empty.String() && b["variantId"] == empty.String() && b["available"] == float64(0)
	}})
	for _, c := range cases {
		w := e.send(http.MethodPost, c.path, c.email, c.body)
		var b map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &b)
		if w.Code != c.status || b["error"] != c.code || (c.check != nil && !c.check(b)) {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	e.exec(`UPDATE conversation.conversations SET customer_id = (SELECT customer_id FROM ord.orders LIMIT 1)
		WHERE id = (SELECT conversation_id FROM conversation.messages WHERE id = $1)`, c2)
	w := e.send(http.MethodPost, claimPath(c2), liveOp, claimBody(v, 1, ""))
	b := decodeJSON(t, w)
	drafts, _ := b["drafts"].([]any)
	if w.Code != http.StatusConflict || b["error"] != "open_draft_exists" || len(drafts) != 1 {
		t.Fatalf("open_draft_exists: %d %s", w.Code, w.Body)
	}
	d0, _ := drafts[0].(map[string]any)
	items, _ := d0["items"].([]any)
	if d0["orderId"] != got["orderId"] || len(items) != 1 || d0["version"] == nil {
		t.Errorf("đơn nháp gợi ý: %v", d0)
	}
	add := fmt.Sprintf(`{"items":[{"variantId":%q,"qty":1}],"mode":"add_to_draft","orderId":%q,"expectedVersion":%v}`,
		v, got["orderId"], d0["version"])
	if r := mustOK(t, "thêm vào nháp", e.send(http.MethodPost, claimPath(c2), liveOp, add), http.StatusCreated); r["orderId"] != got["orderId"] {
		t.Errorf("thêm vào nháp: %v", r)
	}
	c3 := e.comment("Thu", "u3", "chốt", time.Now())
	e.exec(`UPDATE channel.channel_accounts SET seller_id = $2 WHERE id = $1`, e.page, e.shop.Partner)
	w = e.send(http.MethodPost, claimPath(c3), liveOp, claimBody(v, 1, ""))
	if b := decodeJSON(t, w); w.Code != http.StatusUnprocessableEntity || b["error"] != "seller_mismatch" {
		t.Errorf("seller_mismatch: %d %s", w.Code, w.Body)
	}
}

// Cùng Idempotency-Key 50 lần song song → 1 đơn; mọi câu trả lời là 201 cùng thân hay 409 request_in_progress.
func TestLiveClaimsAPI_SameKey(t *testing.T) {
	t.Parallel()
	e := newLiveClaimAPIEnv(t)
	v := e.variant(5)
	c := e.comment("Lan", "u1", "chốt", time.Now().Add(-time.Minute))
	key := "same-" + uuid.NewString()
	recs := make([]*httptest.ResponseRecorder, 50)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Go(func() {
			recs[i] = serveAs(e.srv, e.keys, http.MethodPost, claimPath(c), liveOp, key, claimBody(v, 1, ""))
		})
	}
	wg.Wait()
	var body string
	for _, r := range recs {
		switch {
		case r.Code == http.StatusCreated && (body == "" || body == r.Body.String()):
			body = r.Body.String()
		case r.Code == http.StatusConflict && decodeJSON(t, r)["error"] == "request_in_progress":
		default:
			t.Fatalf("câu trả lời lạ: %d %s (thân 201 trước: %s)", r.Code, r.Body, body)
		}
	}
	if body == "" {
		t.Fatal("không câu 201 nào")
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("có %d đơn", n)
	}
}

// Bối cảnh và danh sách comment (Task 172): bối cảnh có thẻ khách, đơn nháp của khách trong phiên, SĐT tách từ
// comment, người bán; danh sách theo thứ tự đến kèm ghi nhận, nhắn riêng none, khách có nháp; lọc có SĐT, chưa chốt;
// trang tiếp theo đúng vị trí; mốc hỏng → 400; Page khác → 404.
func TestLiveClaimsAPI_ContextAndComments(t *testing.T) {
	t.Parallel()
	e := newLiveClaimAPIEnv(t)
	v := e.variant(5)
	base := time.Now().Add(-10 * time.Minute).UTC()
	c1 := e.comment("Lan", "u1", "lấy 1 cái, sđt 0901 234 567", base)
	c2 := e.comment("Mai", "u2", "đẹp quá", base.Add(time.Minute))
	c3 := e.comment("Thu", "u3", "còn size 38 không", base.Add(2*time.Minute))
	mustOK(t, "claimLiveComment", e.send(http.MethodPost, claimPath(c1), liveOp, claimBody(v, 1, `,"askAddress":false`)), http.StatusCreated)
	ctxBody := mustOK(t, "getLiveCommentContext", e.send(http.MethodGet, "/api/v1/live-comments/"+c1.String()+"/context", liveViewer, ""), http.StatusOK)
	phones, _ := ctxBody["phones"].([]any)
	drafts, _ := ctxBody["openDrafts"].([]any)
	cust, _ := ctxBody["customer"].(map[string]any)
	if len(phones) != 1 || phones[0] != "0901234567" || len(drafts) != 1 || ctxBody["sellerId"] != e.shop.Seller.String() || cust["name"] != "Lan" {
		t.Errorf("bối cảnh: %v", ctxBody)
	}
	if w := e.send(http.MethodGet, "/api/v1/live-comments/"+c1.String()+"/context", liveOther, ""); w.Code != http.StatusNotFound {
		t.Errorf("bối cảnh Page khác: %d", w.Code)
	}
	list := func(q, email string) map[string]any {
		t.Helper()
		return mustOK(t, "listLiveComments", e.send(http.MethodGet, "/api/v1/live-sessions/"+e.session.String()+"/comments"+q, email, ""), http.StatusOK)
	}
	all := list("", liveViewer)["items"].([]any) //nolint:forcetypeassert // test
	if len(all) != 3 {
		t.Fatalf("danh sách: %v", all)
	}
	first, _ := all[0].(map[string]any)
	claim, _ := first["claim"].(map[string]any)
	pr, _ := first["privateReply"].(map[string]any)
	if first["messageId"] != c1.String() || claim == nil || first["hasPhone"] != true || first["customerHasOpenDraft"] != true ||
		pr["state"] != "none" {
		t.Errorf("comment 1: %v", first)
	}
	if second, _ := all[1].(map[string]any); second["claim"] != nil || second["hasPhone"] != false || second["customerHasOpenDraft"] != false {
		t.Errorf("comment 2: %v", second)
	}
	if items := list("?phoneOnly=true", liveViewer)["items"].([]any); len(items) != 1 { //nolint:forcetypeassert // test
		t.Errorf("lọc có SĐT: %v", items)
	}
	if items := list("?unhandledOnly=true", liveViewer)["items"].([]any); len(items) != 2 { //nolint:forcetypeassert // test
		t.Errorf("lọc chưa chốt: %v", items)
	}
	page := list("?limit=2", liveViewer)
	next, _ := page["nextCursor"].(string)
	rest := list("?after="+url.QueryEscape(next), liveViewer)["items"].([]any) //nolint:forcetypeassert // test
	if r0, _ := rest[0].(map[string]any); next == "" || len(rest) != 1 || r0["messageId"] != c3.String() {
		t.Errorf("trang tiếp theo %q: %v", next, rest)
	}
	slack := list("?after="+url.QueryEscape(base.Add(2*time.Minute).Format(time.RFC3339Nano)+","+c3.String()), liveViewer)["items"].([]any) //nolint:forcetypeassert // test
	if s0, _ := slack[0].(map[string]any); len(slack) != 1 || s0["messageId"] != c3.String() {
		t.Errorf("mốc của màn hình lùi 30 giây: %v, muốn đúng c3 (c2 sớm hơn 60 giây)", slack)
	}
	if second, _ := all[1].(map[string]any); second["messageId"] != c2.String() {
		t.Errorf("thứ tự: %v", all)
	}
	if w := e.send(http.MethodGet, "/api/v1/live-sessions/"+e.session.String()+"/comments?after=hong", liveViewer, ""); w.Code != http.StatusBadRequest {
		t.Errorf("mốc hỏng: %d", w.Code)
	}
	if w := e.send(http.MethodGet, "/api/v1/live-sessions/"+e.session.String()+"/comments", liveOther, ""); w.Code != http.StatusNotFound {
		t.Errorf("Page khác: %d", w.Code)
	}
}
