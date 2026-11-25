package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
)

// choiceEnv là shipEnv (vtp 30.000, best 28.000, dịch vụ STD) có danh mục hành chính, nguồn đơn mặc định và khóa
// web.orders.create (orders.key); một biến thể v (giá 350.000, tồn 20).
type choiceEnv struct {
	*shipEnv
	orders *orderEnv
	v      uuid.UUID
}

func newChoiceEnv(t *testing.T) *choiceEnv {
	t.Helper()
	s := newShipEnv(t)
	s.staff.SeedAdminUnits(t)
	s.staff.Exec(t, s.tenant, func(tx pgx.Tx) error { return order.EnsureDefaults(t.Context(), tx, s.tenant) })
	c := &choiceEnv{shipEnv: s, orders: &orderEnv{env: s.env, key: s.serviceKey("web.orders.create")}}
	c.v = s.product("SC1", "[]").Variants[0].ID
	s.stock(c.v, 20)
	return c
}

// shipQuoteBody là thân báo phí của địa chỉ giao mẫu (T01/T01001 như webOrder.body).
const shipQuoteBody = `{"provinceCode":"T01","wardCode":"T01001","weightGrams":500,"value":350000,"cod":350000}`

// choice là shippingChoice JSON: acc nil → phí dự phòng; service rỗng → không gửi.
func choice(acc *uuid.UUID, service string, fee int64, quote string) string {
	parts := []string{fmt.Sprintf(`"fee":%d`, fee), `"quote":` + quote}
	if acc != nil {
		parts = append(parts, fmt.Sprintf(`"accountId":%q`, acc.String()))
	}
	if service != "" {
		parts = append(parts, fmt.Sprintf(`"service":%q`, service))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// quoteWith gửi /web/quote giỏ 1 × v, phí khách trả fee, kèm lựa chọn ch (rỗng: không gửi).
func (c *choiceEnv) quoteWith(fee int64, ch string) *httpResult {
	c.t.Helper()
	body := fmt.Sprintf(`{"lines":[%s],"shippingFee":%d`, quoteLine(c.v, 1, 350000), fee)
	if ch != "" {
		body += `,"shippingChoice":` + ch
	}
	rec := c.post(c.orders.key, "/api/v1/web/quote", body+"}")
	return &httpResult{code: rec.Code, body: rec.Body.Bytes()}
}

type httpResult struct {
	code int
	body []byte
}

func (r *httpResult) errCode() string {
	var b struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(r.body, &b)
	return b.Error
}

// orderBody là thân đơn 1 × v gửi qua via, phí khách trả fee, lựa chọn ch, tổng đã báo total (= 350.000 + fee).
func (c *choiceEnv) orderBody(ref, via string, fee int64, ch string) string {
	w := webOrder{
		ref: ref, phone: phone(int(c.orders.seq.Add(1))), via: via, lines: []string{quoteLine(c.v, 1, 350000)},
		total: 350000 + fee, fp: "fp",
	}
	b := strings.Replace(w.body(), `"shippingFee":30000,`, fmt.Sprintf(`"shippingFee":%d,"shippingChoice":%s,`, fee, ch), 1)
	if !strings.Contains(b, `"shippingChoice"`) {
		c.t.Fatal("không gắn được shippingChoice vào thân đơn")
	}
	return b
}

func (c *choiceEnv) webShipping(ref string) map[string]any {
	c.t.Helper()
	var raw []byte
	c.staff.Exec(c.t, c.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(c.t.Context(), `SELECT web_shipping FROM ord.orders WHERE external_ref = $1`, ref).Scan(&raw)
	})
	var m map[string]any
	if raw != nil {
		if err := json.Unmarshal(raw, &m); err != nil {
			c.t.Fatal(err)
		}
	}
	return m
}

// Task 490, báo giá: lựa chọn khớp báo phí → 200, dấu vân tay khác báo giá không lựa chọn; phí lệch, dịch vụ lạ, khách
// trả hơn phí hãng → 422 shipping_fee_mismatch; tài khoản không trên web, phí dự phòng khi shop chưa đặt → 422
// shipping_carrier_unavailable; khách miễn phí ship (trả 0) → 200.
func TestWebQuote_ShippingChoice(t *testing.T) {
	t.Parallel()
	c := newChoiceEnv(t)
	plain := c.quoteWith(30000, "")
	ok := c.quoteWith(30000, choice(&c.accVTP, "STD", 30000, shipQuoteBody))
	if plain.code != http.StatusOK || ok.code != http.StatusOK || string(plain.body) == string(ok.body) {
		t.Fatalf("báo giá: không lựa chọn %d %s; có %d %s", plain.code, plain.body, ok.code, ok.body)
	}
	if r := c.quoteWith(0, choice(&c.accVTP, "", 30000, shipQuoteBody)); r.code != http.StatusOK {
		t.Fatalf("miễn phí ship, không gửi dịch vụ: %d %s", r.code, r.body)
	}
	other := uuid.New()
	for name, cc := range map[string]struct {
		fee  int64
		ch   string
		want string
	}{
		"phí lệch":         {28000, choice(&c.accVTP, "STD", 28000, shipQuoteBody), "shipping_fee_mismatch"},
		"dịch vụ lạ":       {30000, choice(&c.accVTP, "FAST", 30000, shipQuoteBody), "shipping_fee_mismatch"},
		"trả hơn phí hãng": {30001, choice(&c.accVTP, "STD", 30000, shipQuoteBody), "shipping_fee_mismatch"},
		"hãng lạ":          {30000, choice(&other, "STD", 30000, shipQuoteBody), "shipping_carrier_unavailable"},
		"chưa có dự phòng": {30000, choice(nil, "", 30000, shipQuoteBody), "shipping_carrier_unavailable"},
	} {
		if r := c.quoteWith(cc.fee, cc.ch); r.code != http.StatusUnprocessableEntity || r.errCode() != cc.want {
			t.Errorf("%s: %d %s, mong 422 %s", name, r.code, r.body, cc.want)
		}
	}
}

// Task 490, tạo đơn: gửi thẳng khớp → 201, đơn lưu hãng, tên, dịch vụ, phí; miễn phí ship → 201; hãng bị ẩn → 422
// shipping_carrier_unavailable lưu theo khóa (gửi lại cùng khóa trả đúng câu đó, không đơn); đơn đã tạo gửi lại khóa
// khác sau khi hãng bị ẩn → 201 cùng đơn; địa chỉ khác thân báo phí → 422 shipping_fee_mismatch.
func TestWebCreateOrder_ShippingChoice(t *testing.T) {
	t.Parallel()
	c := newChoiceEnv(t)
	o := c.orders
	vtp := choice(&c.accVTP, "STD", 30000, shipQuoteBody)

	first := "SC-OK-1"
	rec := o.placeRaw(o.key, first, c.orderBody(first, order.SubmittedSync, 30000, vtp))
	created(t, rec.Code, rec.Body.Bytes())
	if m := c.webShipping(first); m["accountId"] != c.accVTP.String() || m["carrierKey"] != "vtp" ||
		m["carrierName"] != "Hãng vtp" || m["service"] != "STD" || m["fee"] != float64(30000) || m["problem"] != nil {
		t.Fatalf("lựa chọn lưu: %v", m)
	}
	free := "SC-FREE"
	rec = o.placeRaw(o.key, free, c.orderBody(free, order.SubmittedSync, 0, choice(&c.accBest, "", 28000, shipQuoteBody)))
	if b := created(t, rec.Code, rec.Body.Bytes()); b.GrandTotal != 350000 || b.PriceReview {
		t.Fatalf("miễn phí ship: %+v", b)
	}

	c.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE fulfillment.carrier_accounts SET show_on_web = false WHERE id = $1`, c.accVTP)
		return err //nolint:wrapcheck // test
	})
	hidden := "SC-HIDDEN"
	body := c.orderBody(hidden, order.SubmittedSync, 30000, vtp)
	refused(t, o.placeRaw(o.key, hidden, body), http.StatusUnprocessableEntity, "shipping_carrier_unavailable")
	again := o.placeRaw(o.key, hidden, body)
	refused(t, again, http.StatusUnprocessableEntity, "shipping_carrier_unavailable")
	if again.Header().Get("Idempotent-Replayed") != "true" || o.storedKeys(hidden) != 1 || o.ordersOf(hidden) != 0 {
		t.Fatalf("từ chối không được lưu theo khóa: %v, %d khóa, %d đơn", again.Header(), o.storedKeys(hidden), o.ordersOf(hidden))
	}
	rec = o.placeRaw(o.key, first+"-retry", c.orderBody(first, order.SubmittedSync, 30000, vtp))
	if rec.Code != http.StatusCreated || o.ordersOf(first) != 1 {
		t.Fatalf("đơn đã có, gửi lại khóa khác sau khi hãng ẩn: %d %s", rec.Code, rec.Body.String())
	}

	far := "SC-ADDR"
	farQuote := strings.Replace(shipQuoteBody, "T01001", "T01002", 1)
	refused(t, o.placeRaw(o.key, far, c.orderBody(far, order.SubmittedSync, 28000, choice(&c.accBest, "STD", 28000, farQuote))),
		http.StatusUnprocessableEntity, "shipping_fee_mismatch")
}

// Task 490, tạo đơn (tiếp): từ hàng đợi phí lệch → 201 priceReview, lựa chọn lưu kèm fee_mismatch; phí dự phòng khi
// shop chưa đặt → 422 shipping_carrier_unavailable, đặt 35.000 thì lệch → shipping_fee_mismatch, khớp → 201; mọi hãng
// lỗi, không phí dự phòng → 503 retry_later không lưu theo khóa.
func TestWebCreateOrder_ShippingChoiceQueuedAndFallback(t *testing.T) {
	t.Parallel()
	c := newChoiceEnv(t)
	o := c.orders
	queued := "SC-QUEUED"
	rec := o.placeRaw(o.key, queued, c.orderBody(queued, order.SubmittedQueued, 25000, choice(&c.accBest, "STD", 25000, shipQuoteBody)))
	if b := created(t, rec.Code, rec.Body.Bytes()); !b.PriceReview {
		t.Fatalf("hàng đợi, phí lệch: %+v", b)
	}
	if m := c.webShipping(queued); m["problem"] != order.ShipProblemFeeMismatch || m["accountId"] != c.accBest.String() {
		t.Fatalf("hàng đợi: lựa chọn lưu %v", m)
	}

	est := "SC-EST"
	refused(t, o.placeRaw(o.key, est, c.orderBody(est, order.SubmittedSync, 35000, choice(nil, "", 35000, shipQuoteBody))),
		http.StatusUnprocessableEntity, "shipping_carrier_unavailable")
	c.save(fulfillment.WebCarrierSettings{FallbackFee: new(int64(35000)), Version: 1})
	est2 := "SC-EST-2"
	refused(t, o.placeRaw(o.key, est2, c.orderBody(est2, order.SubmittedSync, 30000, choice(nil, "", 30000, shipQuoteBody))),
		http.StatusUnprocessableEntity, "shipping_fee_mismatch")
	est3 := "SC-EST-3"
	rec = o.placeRaw(o.key, est3, c.orderBody(est3, order.SubmittedSync, 35000, choice(nil, "", 35000, shipQuoteBody)))
	created(t, rec.Code, rec.Body.Bytes())
	if m := c.webShipping(est3); m["fee"] != float64(35000) || m["accountId"] != nil {
		t.Fatalf("phí dự phòng: %v", m)
	}

	c.best.fee.Store(0)
	c.vtp.fee.Store(0)
	c.save(fulfillment.WebCarrierSettings{Version: 2}) // bỏ dự phòng; version mới → khóa đệm báo phí mới
	down := "SC-DOWN"
	rec = o.placeRaw(o.key, down, c.orderBody(down, order.SubmittedSync, 28000, choice(&c.accBest, "STD", 28000, shipQuoteBody)))
	refused(t, rec, http.StatusServiceUnavailable, "retry_later")
	if o.storedKeys(down) != 0 || o.ordersOf(down) != 0 {
		t.Fatalf("503 bị lưu: %d khóa, %d đơn", o.storedKeys(down), o.ordersOf(down))
	}
}
