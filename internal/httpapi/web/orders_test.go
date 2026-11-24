package web_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/cache"
)

const ordersPath = "/api/v1/web/orders"

// orderEnv là env có danh mục hành chính mẫu, cài đặt và nguồn đơn mặc định (nguồn Website), một khóa
// web.orders.create.
type orderEnv struct {
	*env

	key string
	seq atomic.Int64
}

func newOrderEnv(t *testing.T, mod func(*httpapi.Deps)) *orderEnv {
	t.Helper()
	e := newEnv(t, nil)
	e.staff.SeedAdminUnits(t)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { return order.EnsureDefaults(t.Context(), tx, e.tenant) })
	if mod != nil {
		e.serve(mod)
	}
	return &orderEnv{env: e, key: e.serviceKey("web.orders.create")}
}

// webOrder là một đơn website sẽ gửi: giỏ lines (quoteLine), mã, SĐT, và báo giá đã hiện cho khách (total, fp).
type webOrder struct {
	ref, phone, coupon, via string
	ip                      string // clientIp; rỗng: không gửi
	lines                   []string
	total                   int64
	fp                      string
}

// order báo giá giỏ lines qua /web/quote (như website) rồi trả đơn gửi thẳng khớp báo giá đó; mỗi đơn một mã ngoài.
func (o *orderEnv) order(phone, coupon string, lines ...string) webOrder {
	o.t.Helper()
	body := fmt.Sprintf(`{"lines":[%s],"shippingFee":30000,"couponCode":%q,"customerPhone":%q}`,
		strings.Join(lines, ","), coupon, phone)
	q := o.quote(o.key, body)
	return webOrder{
		ref: fmt.Sprintf("WEB-%d-%s", o.seq.Add(1), uuid.NewString()[:8]), phone: phone, coupon: coupon,
		via: order.SubmittedSync, lines: lines, total: q.GrandTotal, fp: q.Fingerprint,
	}
}

func (w webOrder) body() string {
	ip := ""
	if w.ip != "" {
		ip = fmt.Sprintf(`"clientIp":%q,`, w.ip)
	}
	return fmt.Sprintf(`{%s"externalRef":%q,"customer":{"phone":%q,"name":"Chị Mai"},`+
		`"shipAddress":{"name":"Chị Mai","phone":%q,"line":"12 Lê Lợi","provinceCode":"T01","wardCode":"T01001"},`+
		`"lines":[%s],"couponCode":%q,"shippingFee":30000,"quoted":{"fingerprint":%q,"grandTotal":%d},`+
		`"submittedVia":%q,"note":"Giao giờ hành chính"}`,
		ip, w.ref, w.phone, w.phone, strings.Join(w.lines, ","), w.coupon, w.fp, w.total, w.via)
}

// place gửi đơn w với Idempotency-Key key qua máy chủ thử (trong tiến trình).
func (o *orderEnv) place(key string, w webOrder) *httptest.ResponseRecorder {
	o.t.Helper()
	return o.placeRaw(o.key, key, w.body())
}

func (o *orderEnv) placeRaw(svcKey, idemKey, body string) *httptest.ResponseRecorder {
	o.t.Helper()
	req := httptest.NewRequestWithContext(o.t.Context(), http.MethodPost, ordersPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return o.send(req, svcKey, "Idempotency-Key", idemKey)
}

type createdBody struct {
	OrderID     uuid.UUID `json:"orderId"`
	Code        string    `json:"code"`
	Status      string    `json:"status"`
	GrandTotal  int64     `json:"grandTotal"`
	PriceReview bool      `json:"priceReview"`
}

type refusalBody struct {
	Error      string     `json:"error"`
	VariantID  string     `json:"variantId"`
	Available  *int       `json:"available"`
	Reason     string     `json:"reason"`
	VariantIDs []string   `json:"variantIds"`
	Quote      *quoteBody `json:"quote"`
}

func created(t *testing.T, code int, body []byte) createdBody {
	t.Helper()
	var c createdBody
	if code != http.StatusCreated || json.Unmarshal(body, &c) != nil || c.OrderID == uuid.Nil {
		t.Fatalf("mong 201 có orderId, có %d %s", code, body)
	}
	return c
}

func refused(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) refusalBody {
	t.Helper()
	var r refusalBody
	if rec.Code != status || json.Unmarshal(rec.Body.Bytes(), &r) != nil || r.Error != code {
		t.Fatalf("mong %d %s, có %d %s", status, code, rec.Code, rec.Body.String())
	}
	return r
}

func (o *orderEnv) ordersOf(ref string) int {
	return o.staff.CountIn(o.t, o.tenant, `SELECT count(*) FROM ord.orders WHERE external_ref = $1`, ref)
}

func (o *orderEnv) storedKeys(key string) int {
	return o.staff.CountIn(o.t, o.tenant, `SELECT count(*) FROM platform.idempotency_keys WHERE key = $1`, key)
}

func phone(n int) string { return fmt.Sprintf("09%08d", 10000000+n) }

// httpReply là một câu trả lời qua HTTP thật.
type httpReply struct {
	status   int
	body     []byte
	replayed string
}

func postHTTP(t *testing.T, base, svcKey, idemKey, body string) (httpReply, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+ordersPath, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+svcKey)
	req.Header.Set("Idempotency-Key", idemKey)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return httpReply{}, err //nolint:wrapcheck // test
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return httpReply{status: resp.StatusCode, body: b, replayed: resp.Header.Get("Idempotent-Replayed")}, err //nolint:wrapcheck // test
}

// [WB07] Máy chủ HTTP thật: 50 yêu cầu song song cùng khóa, cùng thân → đúng 1 đơn; mỗi câu trả lời là 201 cùng thân
// hoặc 409 request_in_progress. Đơn đã tạo có đúng 1 dòng audit order.create của khóa dịch vụ, không chép SĐT.
func TestWebCreateOrder_FiftyParallelSameKey(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, nil)
	p := o.product("P50", "[]")
	v := p.Variants[0].ID
	o.stock(v, 5)
	w := o.order(phone(1), "", quoteLine(v, 1, 350000))
	srv := httptest.NewServer(o.srv)
	t.Cleanup(srv.Close)
	replies := make([]httpReply, 50)
	var wg sync.WaitGroup
	for i := range replies {
		wg.Go(func() {
			r, err := postHTTP(t, srv.URL, o.key, w.ref, w.body())
			if err != nil {
				t.Errorf("yêu cầu %d: %v", i, err)
			}
			replies[i] = r
		})
	}
	wg.Wait()
	var first []byte
	for i, r := range replies {
		switch {
		case r.status == http.StatusCreated && first == nil:
			first = r.body
		case r.status == http.StatusCreated && !bytes.Equal(r.body, first):
			t.Errorf("yêu cầu %d: thân 201 khác: %s ≠ %s", i, r.body, first)
		case r.status == http.StatusConflict && bytes.Contains(r.body, []byte(`"request_in_progress"`)):
		case r.status != http.StatusCreated:
			t.Errorf("yêu cầu %d: %d %s", i, r.status, r.body)
		}
	}
	c := created(t, http.StatusCreated, first)
	if n := o.ordersOf(w.ref); n != 1 || c.Status != "new" || c.GrandTotal != w.total || c.PriceReview {
		t.Fatalf("mong 1 đơn new tổng %d, có %d đơn %+v", w.total, n, c)
	}
	if n := o.staff.CountIn(t, o.tenant, `SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1
		AND action = 'order.create' AND actor_type = 'service' AND (after->>'grandTotal')::bigint = $2`,
		c.OrderID.String(), w.total); n != 1 {
		t.Fatalf("mong 1 dòng audit order.create của khóa dịch vụ, có %d", n)
	}
	if n := o.staff.CountIn(t, o.tenant, `SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1
		AND (after::text LIKE '%910000001%' OR after::text LIKE '%Lê Lợi%' OR after::text LIKE '%Mai%')`,
		c.OrderID.String()); n != 0 {
		t.Fatalf("audit order.create chép SĐT, địa chỉ hay tên người nhận (%d dòng)", n)
	}
}

// Hai khóa khác nhau, cùng externalRef → 1 đơn, cả hai 201 cùng orderId (khóa nghiệp vụ). Mất câu trả lời: máy chủ đã
// commit rồi đóng kết nối không trả lời → gửi lại cùng khóa → 201 Idempotent-Replayed: true, vẫn 1 đơn [WB07].
func TestWebCreateOrder_SameExternalRefAndLostReply(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, nil)
	p := o.product("PREF", "[]")
	v := p.Variants[0].ID
	o.stock(v, 5)
	w := o.order(phone(2), "", quoteLine(v, 1, 350000))
	a := o.place(w.ref, w)
	b := o.place(w.ref+"-lan2", w)
	ca, cb := created(t, a.Code, a.Body.Bytes()), created(t, b.Code, b.Body.Bytes())
	if ca.OrderID != cb.OrderID || o.ordersOf(w.ref) != 1 {
		t.Fatalf("hai khóa cùng mã ngoài: %+v, %+v, %d đơn", ca, cb, o.ordersOf(w.ref))
	}

	lost := o.order(phone(3), "", quoteLine(v, 1, 350000))
	drop := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		o.srv.ServeHTTP(httptest.NewRecorder(), r) // xử lý và commit
		panic(http.ErrAbortHandler)                // đóng kết nối: client không nhận được câu trả lời
	}))
	t.Cleanup(drop.Close)
	if _, err := postHTTP(t, drop.URL, o.key, lost.ref, lost.body()); err == nil {
		t.Fatal("máy chủ thử vẫn trả lời")
	}
	srv := httptest.NewServer(o.srv)
	t.Cleanup(srv.Close)
	r, err := postHTTP(t, srv.URL, o.key, lost.ref, lost.body())
	if err != nil {
		t.Fatal(err)
	}
	if created(t, r.status, r.body); r.replayed != "true" || o.ordersOf(lost.ref) != 1 {
		t.Fatalf("gửi lại sau khi mất câu trả lời: replayed %q, %d đơn", r.replayed, o.ordersOf(lost.ref))
	}
}

// race gửi hai đơn cùng lúc (mỗi đơn khóa = mã ngoài) và trả hai câu trả lời theo thứ tự đơn.
func (o *orderEnv) race(a, b webOrder) [2]*httptest.ResponseRecorder {
	var out [2]*httptest.ResponseRecorder
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, w := range []webOrder{a, b} {
		wg.Go(func() {
			<-start
			out[i] = o.place(w.ref, w)
		})
	}
	close(start)
	wg.Wait()
	return out
}

// winnerLoser: đúng một 201; trả chỉ số bên thua.
func winnerLoser(t *testing.T, rs [2]*httptest.ResponseRecorder) int {
	t.Helper()
	switch {
	case rs[0].Code == http.StatusCreated && rs[1].Code != http.StatusCreated:
		return 1
	case rs[1].Code == http.StatusCreated && rs[0].Code != http.StatusCreated:
		return 0
	}
	t.Fatalf("mong đúng một 201: %d %s / %d %s", rs[0].Code, rs[0].Body, rs[1].Code, rs[1].Body)
	return -1
}

// [WB08] Tồn 1, hai khách đặt cùng lúc, 20 lần → mỗi lần đúng một 201 và một 409 out_of_stock có variantId,
// available 0; gửi lại cùng khóa của bên thua → cùng 409 (đã lưu, Idempotent-Replayed); tồn không âm, giữ đúng 1.
func TestWebCreateOrder_LastUnitRace(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, nil)
	for i := range 20 {
		p := o.product(fmt.Sprintf("R%02d", i), "[]")
		v := p.Variants[0].ID
		o.stock(v, 1)
		ws := [2]webOrder{o.order(phone(100+2*i), "", quoteLine(v, 1, 350000)), o.order(phone(101+2*i), "", quoteLine(v, 1, 350000))}
		rs := o.race(ws[0], ws[1])
		lose := winnerLoser(t, rs)
		r := refused(t, rs[lose], http.StatusConflict, "out_of_stock")
		if r.VariantID != v.String() || r.Available == nil || *r.Available != 0 {
			t.Fatalf("lần %d: thân hết hàng %s", i, rs[lose].Body)
		}
		again := o.place(ws[lose].ref, ws[lose])
		if again.Code != http.StatusConflict || !bytes.Equal(again.Body.Bytes(), rs[lose].Body.Bytes()) ||
			again.Header().Get("Idempotent-Replayed") != "true" {
			t.Fatalf("lần %d: gửi lại bên thua: %d %s %v", i, again.Code, again.Body, again.Header())
		}
		if n := o.ordersOf(ws[lose].ref); n != 0 {
			t.Fatalf("lần %d: bên thua có %d đơn", i, n)
		}
		if n := o.staff.CountIn(t, o.tenant, `SELECT count(*) FROM inventory.stock_balances
			WHERE variant_id = $1 AND on_hand = 1 AND held = 1`, v); n != 1 {
			t.Fatalf("lần %d: tồn sau tranh chiếc cuối sai", i)
		}
	}
}

// [WB11] Mã còn 1 lượt, hai đơn gửi thẳng cùng lúc → một 201 có mã, một 422 coupon_invalid lý do exhausted; used_count 1.
func TestWebCreateOrder_LastCouponUse(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, nil)
	p := o.product("PCP", "[]")
	v := p.Variants[0].ID
	o.stock(v, 5)
	now := time.Now()
	o.app(func(tx pgx.Tx) error {
		one := int32(1)
		_, err := promotion.Writer{}.CreateCoupon(t.Context(), tx, promotion.Actor{TenantID: o.tenant, UserID: o.owner.UserID},
			promotion.CouponInput{
				Code: "CUOI1", Name: "Mã cuối", DiscountKind: "fixed", Value: 50000, MaxUses: &one, ReleaseOnCancel: true,
				ValidFrom: now.Add(-24 * time.Hour), ValidTo: now.Add(30 * 24 * time.Hour),
			})
		return err //nolint:wrapcheck // test
	})
	a, b := o.order(phone(4), "CUOI1", quoteLine(v, 1, 350000)), o.order(phone(5), "CUOI1", quoteLine(v, 1, 350000))
	if a.total != 330000 || b.total != 330000 {
		t.Fatalf("báo giá có mã: %d, %d", a.total, b.total)
	}
	rs := o.race(a, b)
	lose := winnerLoser(t, rs)
	if r := refused(t, rs[lose], http.StatusUnprocessableEntity, "coupon_invalid"); r.Reason != "exhausted" {
		t.Fatalf("lý do: %s", rs[lose].Body)
	}
	if c := created(t, rs[1-lose].Code, rs[1-lose].Body.Bytes()); c.GrandTotal != 330000 {
		t.Fatalf("đơn thắng: %+v", c)
	}
	if n := o.staff.CountIn(t, o.tenant, `SELECT count(*) FROM promotion.coupons WHERE code = 'CUOI1' AND used_count = 1`); n != 1 {
		t.Fatal("used_count khác 1")
	}
}

// [WB13], [WB10] Giá bị sửa trong thân (300.000, CRM 350.000) → 422 quote_changed kèm báo giá CRM, không đơn; gửi lại
// cùng khóa → cùng câu trả lời đã lưu. queued cùng giá cũ → 201 priceReview theo số CRM. [WB09] biến thể ngừng bán →
// 422 item_unavailable kèm variantIds.
func TestWebCreateOrder_QuoteChangedAndUnavailable(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, nil)
	p := o.product("PQC", "[]")
	v, gone := p.Variants[0].ID, p.Variants[2].ID
	o.stock(v, 5)
	w := o.order(phone(6), "", quoteLine(v, 1, 350000))
	w.lines, w.total = []string{quoteLine(v, 1, 300000)}, 330000
	rec := o.place(w.ref, w)
	r := refused(t, rec, http.StatusUnprocessableEntity, "quote_changed")
	if r.Quote == nil || len(r.Quote.Lines) != 1 || r.Quote.Lines[0].UnitPrice != 350000 || r.Quote.GrandTotal != 380000 ||
		!r.Quote.Changed || o.ordersOf(w.ref) != 0 {
		t.Fatalf("quote_changed: %s, %d đơn", rec.Body, o.ordersOf(w.ref))
	}
	again := o.place(w.ref, w)
	if !bytes.Equal(again.Body.Bytes(), rec.Body.Bytes()) || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gửi lại: %d %s", again.Code, again.Body)
	}
	w.ref, w.via = w.ref+"-q", order.SubmittedQueued
	q := o.place(w.ref, w)
	if c := created(t, q.Code, q.Body.Bytes()); !c.PriceReview || c.GrandTotal != 380000 {
		t.Fatalf("queued giá cũ: %+v", c)
	}

	u := o.order(phone(7), "", quoteLine(v, 1, 350000), quoteLine(gone, 1, 350000))
	ru := refused(t, o.place(u.ref, u), http.StatusUnprocessableEntity, "item_unavailable")
	if len(ru.VariantIDs) != 1 || ru.VariantIDs[0] != gone.String() || o.ordersOf(u.ref) != 0 {
		t.Fatalf("item_unavailable: %+v", ru)
	}
}

// [WB06], [WB01] Đơn có giảm chiến dịch 20 % → 201 theo báo giá (280.000 + 30.000), dòng trong database mang mã chiến
// dịch; đơn kênh web, nguồn Website.
func TestWebCreateOrder_Campaign(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, nil)
	p := o.product("PCA", "[]")
	v := p.Variants[0].ID
	o.stock(v, 5)
	now := time.Now()
	c := o.campaign("Hè", now.Add(-time.Hour), now.Add(time.Hour), promotion.StateEnabled, p.ID)
	w := o.order(phone(8), "", quoteLine(v, 1, 350000))
	rec := o.place(w.ref, w)
	res := created(t, rec.Code, rec.Body.Bytes())
	if res.GrandTotal != 310000 {
		t.Fatalf("tổng có chiến dịch: %+v", res)
	}
	var campaign *uuid.UUID
	var channel string
	o.staff.Exec(t, o.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT i.web_campaign_id, o.channel FROM ord.order_items i
			JOIN ord.orders o ON o.id = i.order_id WHERE i.order_id = $1`, res.OrderID).Scan(&campaign, &channel)
	})
	if campaign == nil || *campaign != c.ID || channel != "web" {
		t.Fatalf("dòng đơn: chiến dịch %v (mong %s), kênh %s", campaign, c.ID, channel)
	}
}

// holdStockLock giữ khóa dòng stock_balances của v trong một transaction khác (chủ bảng, ngữ cảnh tenant) tới khi hàm
// trả về được gọi (nhả, chờ transaction xong; t.Cleanup cũng nhả). Goroutine giữ khóa có chủ là hàm nhả.
func (o *orderEnv) holdStockLock(v uuid.UUID) func() {
	o.t.Helper()
	locked, stop, done := make(chan error, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- o.staff.TryExec(o.t.Context(), o.tenant, func(tx pgx.Tx) error {
			var n int
			err := tx.QueryRow(o.t.Context(), `SELECT count(*) FROM (SELECT 1 FROM inventory.stock_balances
				WHERE variant_id = $1 FOR UPDATE) s`, v).Scan(&n)
			if err == nil && n != 1 {
				err = fmt.Errorf("khóa %d dòng tồn", n)
			}
			locked <- err
			<-stop
			return err
		})
	}()
	var once sync.Once
	release := func() {
		once.Do(func() {
			close(stop)
			if err := <-done; err != nil {
				o.t.Errorf("transaction giữ khóa: %v", err)
			}
		})
	}
	select {
	case err := <-locked:
		o.t.Cleanup(release)
		if err != nil {
			o.t.Fatalf("giữ khóa dòng tồn: %v", err)
		}
	case err := <-done: // transaction lỗi trước khi giữ được khóa
		o.t.Fatalf("mở transaction giữ khóa: %v", err)
	}
	return release
}

// Một transaction khác giữ khóa dòng stock_balances của biến thể, lock_timeout 50 ms → 503 retry_later có
// Retry-After, không còn dòng idempotency_keys; nhả khóa rồi gửi lại cùng khóa → 201, đúng 1 đơn.
func TestWebCreateOrder_LockTimeout(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, func(d *httpapi.Deps) { d.WriteLockTimeout = 50 * time.Millisecond })
	p := o.product("PLK", "[]")
	v := p.Variants[0].ID
	o.stock(v, 5)
	w := o.order(phone(9), "", quoteLine(v, 1, 350000))
	release := o.holdStockLock(v)
	rec := o.place(w.ref, w)
	refused(t, rec, http.StatusServiceUnavailable, "retry_later")
	if rec.Header().Get("Retry-After") == "" || o.storedKeys(w.ref) != 0 || o.ordersOf(w.ref) != 0 {
		t.Fatalf("503: Retry-After %q, %d khóa, %d đơn", rec.Header().Get("Retry-After"), o.storedKeys(w.ref), o.ordersOf(w.ref))
	}
	release()
	ok := o.place(w.ref, w)
	if created(t, ok.Code, ok.Body.Bytes()); o.ordersOf(w.ref) != 1 {
		t.Fatalf("sau khi nhả khóa: %d đơn", o.ordersOf(w.ref))
	}
}

// Khóa không có web.orders.create → 403; thân 1 MiB + 1 byte → 413; SĐT sai → 422 invalid_phone, không lưu. Ngưỡng 3
// đơn/phút cho một SĐT → đơn thứ 4 nhận 429 too_fast kèm Retry-After, không lưu; SĐT khác vẫn đặt được.
func TestWebCreateOrder_GuardsAndTooFast(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 24, 10, 0, 30, 0, time.UTC) // đồng hồ cố định: không qua ranh giới cửa sổ phút
	o := newOrderEnv(t, func(d *httpapi.Deps) {
		d.WebOrderLimiter = httpapi.NewWebOrderLimiter(cache.NewRateLimiter(nil, func() time.Time { return at }, nil),
			cache.Limit{Name: "web_order_customer", Max: 3, Window: time.Minute, FallbackMax: 3},
			[]byte("khoa-hmac-thu-cua-test-du-32-ky-tu!!"))
	})
	p := o.product("PGT", "[]")
	v := p.Variants[0].ID
	o.stock(v, 10)
	w := o.order(phone(10), "", quoteLine(v, 1, 350000))
	refused(t, o.placeRaw(o.env.key, w.ref, w.body()), http.StatusForbidden, "forbidden")
	big := `{"externalRef":"` + strings.Repeat("x", 1<<20) + `"}`
	refused(t, o.placeRaw(o.key, "to-qua", big[:1<<20+1]), http.StatusRequestEntityTooLarge, "body_too_large")
	bad := w
	bad.phone, bad.ref = "12345678", w.ref+"-sai"
	refused(t, o.place(bad.ref, bad), http.StatusUnprocessableEntity, "invalid_phone")
	if o.storedKeys(bad.ref) != 0 {
		t.Fatal("SĐT sai được lưu theo khóa")
	}
	for range 3 {
		w := o.order(phone(10), "", quoteLine(v, 1, 350000))
		rec := o.place(w.ref, w)
		created(t, rec.Code, rec.Body.Bytes())
	}
	fourth := o.order(phone(10), "", quoteLine(v, 1, 350000))
	rec := o.place(fourth.ref, fourth)
	refused(t, rec, http.StatusTooManyRequests, "too_fast")
	if rec.Header().Get("Retry-After") == "" || o.storedKeys(fourth.ref) != 0 || o.ordersOf(fourth.ref) != 0 {
		t.Fatalf("429: Retry-After %q, %d khóa", rec.Header().Get("Retry-After"), o.storedKeys(fourth.ref))
	}
	other := o.order(phone(11), "", quoteLine(v, 1, 350000))
	if r := o.place(other.ref, other); r.Code != http.StatusCreated {
		t.Fatalf("SĐT khác: %d %s", r.Code, r.Body)
	}
}
