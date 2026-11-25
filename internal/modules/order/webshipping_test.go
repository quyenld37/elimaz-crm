package order_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/order"
)

// shipChoice là lựa chọn vtp/STD phí fee của tài khoản acc (đã kiểm, không vấn đề).
func shipChoice(acc uuid.UUID, fee int64) *order.WebShipChoice {
	return &order.WebShipChoice{AccountID: &acc, CarrierKey: "vtp", CarrierName: "Viettel Post", Service: "STD", Fee: fee}
}

// withShipping gắn lựa chọn c và phí khách trả fee vào đơn in, báo giá lại để Quoted khớp số CRM.
func (e *webEnv) withShipping(in order.WebOrderInput, c *order.WebShipChoice, fee int64) order.WebOrderInput {
	e.t.Helper()
	in.Shipping, in.ShippingFee = c, fee
	q := e.mustQuote(order.WebQuoteInput{Lines: in.Lines, ShippingFee: fee})
	in.Quoted = order.WebQuoted{Fingerprint: q.Fingerprint, GrandTotal: q.GrandTotal}
	return in
}

func (e *webEnv) shippingOf(id uuid.UUID) *order.WebShipChoice {
	e.t.Helper()
	return e.get(id).WebShipping
}

// Task 490: đơn gửi thẳng mang lựa chọn đã kiểm → đơn lưu đúng hãng, dịch vụ, phí (order.Get), không price_review;
// gửi lại cùng ExternalRef khi lựa chọn nay có vấn đề → Replayed, không lỗi; miễn phí ship (khách trả 0 < phí hãng
// 30.000) vẫn nhận; phí dự phòng; không gửi lựa chọn → WebShipping nil (client cũ).
func TestCreateFromWeb_ShippingChoiceStored(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	acc := uuid.New()
	in := e.withShipping(e.webInput("0903000201", "", wl(v[0], 1, 200000)), shipChoice(acc, 30000), 30000)
	res := e.mustCreateWeb(in)
	got := e.shippingOf(res.OrderID)
	if res.PriceReview || got == nil || *got.AccountID != acc || got.CarrierKey != "vtp" || got.CarrierName != "Viettel Post" ||
		got.Service != "STD" || got.Fee != 30000 || got.Problem != "" || got.Estimated() {
		t.Fatalf("kết quả %+v, lựa chọn lưu %+v", res, got)
	}
	// Gửi lại cùng ExternalRef mà lựa chọn nay có vấn đề (hãng vừa bị ẩn): trả đơn đã có trước mọi kiểm lựa chọn,
	// không ghi đè lựa chọn đã lưu.
	in.Shipping = shipChoice(acc, 30000)
	in.Shipping.Problem = order.ShipProblemCarrierUnavailable
	if again := e.mustCreateWeb(in); !again.Replayed || again.OrderID != res.OrderID {
		t.Fatalf("gửi lại: %+v, lần đầu %+v", again, res)
	}
	if got := e.shippingOf(res.OrderID); got == nil || got.Problem != "" {
		t.Fatalf("lựa chọn bị ghi đè: %+v", got)
	}
	free := e.withShipping(e.webInput("0903000202", "", wl(v[0], 1, 200000)), shipChoice(acc, 30000), 0)
	res = e.mustCreateWeb(free)
	if o := e.orderRow(res.OrderID); o.fee != 0 || o.grand != 200000 || res.PriceReview {
		t.Fatalf("miễn phí ship: đơn %+v kết quả %+v", o, res)
	}
	if got := e.shippingOf(res.OrderID); got == nil || got.Fee != 30000 {
		t.Fatalf("miễn phí ship: lựa chọn %+v, mong phí hãng 30.000", got)
	}
	est := e.withShipping(e.webInput("0903000203", "", wl(v[0], 1, 200000)), &order.WebShipChoice{Fee: 35000}, 35000)
	res = e.mustCreateWeb(est)
	if got := e.shippingOf(res.OrderID); got == nil || !got.Estimated() || got.Fee != 35000 {
		t.Fatalf("phí dự phòng: %+v", got)
	}
	old := e.mustCreateWeb(e.webInput("0903000204", "", wl(v[0], 1, 200000)))
	if got := e.shippingOf(old.OrderID); got != nil {
		t.Fatalf("không gửi lựa chọn mà đơn có %+v", got)
	}
}

// Gửi thẳng mà lựa chọn sai: hãng ẩn → ErrShipCarrierUnavailable, phí lệch hay khách trả hơn phí hãng →
// ErrShipFeeMismatch, chưa kiểm được → ErrShipUnverified; không đơn, không giữ hàng, không khách.
func TestCreateFromWeb_ShippingChoiceSyncRefused(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	acc := uuid.New()
	problem := func(p string) *order.WebShipChoice {
		c := shipChoice(acc, 30000)
		c.Problem = p
		return c
	}
	cases := []struct {
		name string
		c    *order.WebShipChoice
		fee  int64
		want error
	}{
		{"hãng ẩn", problem(order.ShipProblemCarrierUnavailable), 30000, order.ErrShipCarrierUnavailable},
		{"phí lệch", problem(order.ShipProblemFeeMismatch), 30000, order.ErrShipFeeMismatch},
		{"chưa kiểm được", problem(order.ShipProblemUnverified), 30000, order.ErrShipUnverified},
		{"khách trả hơn phí hãng", shipChoice(acc, 30000), 30001, order.ErrShipFeeMismatch},
	}
	for _, c := range cases {
		in := e.withShipping(e.webInput("0903000211", "", wl(v[0], 1, 200000)), c.c, c.fee)
		if _, err := e.createWeb(in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, mong %v", c.name, err, c.want)
		}
	}
	if n := e.count(`SELECT (SELECT count(*) FROM ord.orders) + (SELECT count(*) FROM inventory.stock_reservations)
		+ (SELECT count(*) FROM customer.customers)`); n != 0 {
		t.Errorf("còn %d dòng đơn, giữ hàng, khách sau các lần bị từ chối", n)
	}
}

// Từ hàng đợi (QĐ15): lựa chọn không khớp vẫn tạo theo số CRM, price_review, lựa chọn lưu kèm vấn đề; khách trả hơn
// phí hãng cũng thành fee_mismatch.
func TestCreateFromWeb_ShippingChoiceQueued(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	acc := uuid.New()
	for _, c := range []struct {
		problem string
		fee     int64
		want    string
	}{
		{order.ShipProblemCarrierUnavailable, 30000, order.ShipProblemCarrierUnavailable},
		{order.ShipProblemUnverified, 30000, order.ShipProblemUnverified},
		{"", 40000, order.ShipProblemFeeMismatch},
	} {
		ch := shipChoice(acc, 30000)
		ch.Problem = c.problem
		in := e.withShipping(e.webInput("0903000221", "", wl(v[0], 1, 200000)), ch, c.fee)
		in.SubmittedVia = order.SubmittedQueued
		res := e.mustCreateWeb(in)
		got := e.shippingOf(res.OrderID)
		if !res.PriceReview || got == nil || got.Problem != c.want || *got.AccountID != acc {
			t.Errorf("%q: kết quả %+v lựa chọn %+v", c.want, res, got)
		}
		if o := e.orderRow(res.OrderID); !o.review || o.fee != c.fee {
			t.Errorf("%q: đơn %+v", c.want, o)
		}
	}
}

// Báo giá có lựa chọn: dấu vân tay gồm lựa chọn (khác không lựa chọn, khác hãng khác; cùng lựa chọn → cùng dấu); lựa
// chọn có vấn đề hay khách trả hơn phí hãng → lỗi tương ứng; phí ngoài giới hạn → ErrInvalidInput.
func TestQuoteWeb_ShippingChoice(t *testing.T) {
	t.Parallel()
	e := newQuoteEnv(t)
	_, v := e.product(5, 200000)
	lines := []order.WebLine{wl(v[0], 1, 200000)}
	a, b := uuid.New(), uuid.New()
	fp := func(c *order.WebShipChoice) string {
		return e.mustQuote(order.WebQuoteInput{Lines: lines, ShippingFee: 30000, Shipping: c}).Fingerprint
	}
	none, withA, withB := fp(nil), fp(shipChoice(a, 30000)), fp(shipChoice(b, 30000))
	if none == withA || withA == withB || withA != fp(shipChoice(a, 30000)) || none != fp(nil) {
		t.Fatalf("dấu vân tay: không %s, a %s, b %s", none, withA, withB)
	}
	bad := shipChoice(a, 30000)
	bad.Problem = order.ShipProblemCarrierUnavailable
	if _, err := e.quote(order.WebQuoteInput{Lines: lines, ShippingFee: 30000, Shipping: bad}); !errors.Is(err, order.ErrShipCarrierUnavailable) {
		t.Errorf("hãng ẩn: %v", err)
	}
	if _, err := e.quote(order.WebQuoteInput{Lines: lines, ShippingFee: 30001, Shipping: shipChoice(a, 30000)}); !errors.Is(err, order.ErrShipFeeMismatch) {
		t.Errorf("khách trả hơn phí hãng: %v", err)
	}
	if _, err := e.quote(order.WebQuoteInput{Lines: lines, ShippingFee: 0, Shipping: shipChoice(a, -1)}); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("phí hãng âm: %v", err)
	}
}
