package order_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Quy luật của CreateFromWeb: giỏ ngẫu nhiên 1–5 dòng (có dòng chiến dịch cấm mã, dòng chiến dịch cho mã), giá báo
// đúng hoặc lệch, mã hợp lệ / hết hạn / không mã, gửi thẳng hay từ hàng đợi → đơn chỉ tạo khi (không lệch và mã áp
// được) hoặc từ hàng đợi; khi tạo: tổng bằng báo giá CRM (bỏ mã không áp được), tổng khớp CHECK, Σ phần chia = giảm
// cấp đơn + mã, dòng chiến dịch cấm mã không nhận phần mã, price_review ⇔ từ hàng đợi mà có lệch.
func TestCreateFromWeb_Invariants(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	pNo, vNo := e.product(10000, 400000, 150000)
	pYes, vYes := e.product(10000, 333000)
	_, vPlain := e.product(10000, 200000, 99000)
	e.campaign(20, false, pNo)
	e.campaign(33, true, pYes)
	noCoupon := map[uuid.UUID]bool{vNo[0]: true, vNo[1]: true}
	pool := []uuid.UUID{vNo[0], vNo[1], vYes[0], vPlain[0], vPlain[1]}
	price := map[uuid.UUID]int64{vNo[0]: 400000, vNo[1]: 150000, vYes[0]: 333000, vPlain[0]: 200000, vPlain[1]: 99000}
	e.quoteCoupon(promotion.CouponInput{Code: "PROP10", DiscountKind: promotion.KindPercent, Value: 1000})
	e.quoteCoupon(promotion.CouponInput{
		Code: "PROPOLD", DiscountKind: promotion.KindPercent, Value: 1000,
		ValidFrom: e.now.AddDate(0, 0, -3), ValidTo: e.now.AddDate(0, 0, -1),
	})
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		n++
		c := drawWebCase(rt, pool, price, noCoupon)
		phone := logging.Phone(fmt.Sprintf("09040%05d", n))
		in := order.WebOrderInput{
			ExternalRef: fmt.Sprintf("ELIMAZ-P%d", n), Customer: order.WebCustomer{Phone: phone, Name: "Khách thử"},
			ShipAddress: *fullAddress(), Lines: c.lines, CouponCode: c.code, ShippingFee: 25000, SubmittedVia: c.via,
		}
		web := e.mustQuote(order.WebQuoteInput{Lines: c.lines, CouponCode: c.code, ShippingFee: 25000, CustomerPhone: &phone})
		in.Quoted = order.WebQuoted{Fingerprint: web.Fingerprint, GrandTotal: web.GrandTotal}
		crmCode := c.code
		if !c.couponOK() {
			crmCode = ""
		}
		right := make([]order.WebLine, len(c.lines))
		for i, l := range c.lines {
			right[i] = wl(l.VariantID, l.Qty, price[l.VariantID])
		}
		crm := e.mustQuote(order.WebQuoteInput{Lines: right, CouponCode: crmCode, ShippingFee: 25000, CustomerPhone: &phone})
		res, err := e.createWeb(in)
		if !c.created() {
			var qc *order.QuoteChangedError
			var ci *order.CouponInvalidError
			if !errors.As(err, &qc) && !errors.As(err, &ci) {
				rt.Fatalf("mong từ chối (%+v), có %v", c, err)
			}
			return
		}
		if err != nil {
			rt.Fatalf("mong tạo đơn (%+v): %v", c, err)
		}
		e.checkWebInvariants(rt, c, res, crm.GrandTotal, noCoupon)
	})
}

// webCase là một ca ngẫu nhiên: giỏ (giá báo lệch khi changed), mã, cách gửi.
type webCase struct {
	lines   []order.WebLine
	changed bool
	code    string
	via     string
	// couponBase: giỏ có ít nhất một dòng được áp mã (không chỉ dòng chiến dịch cấm mã); không có thì nền mã 0 và mã
	// bị từ chối zero_discount (Task 433, C-05).
	couponBase bool
}

func (c webCase) couponOK() bool { return c.code == "" || (c.code != "PROPOLD" && c.couponBase) }

func (c webCase) created() bool {
	return c.via == order.SubmittedQueued || (!c.changed && c.couponOK())
}

func (c webCase) review() bool {
	return c.via == order.SubmittedQueued && (c.changed || !c.couponOK())
}

func drawWebCase(rt *rapid.T, pool []uuid.UUID, price map[uuid.UUID]int64, noCoupon map[uuid.UUID]bool) webCase {
	ids := rapid.SliceOfNDistinct(rapid.SampledFrom(pool), 1, 5, rapid.ID).Draw(rt, "lines")
	c := webCase{lines: make([]order.WebLine, len(ids))}
	for i, v := range ids {
		c.couponBase = c.couponBase || !noCoupon[v]
		c.lines[i] = wl(v, rapid.IntRange(1, 3).Draw(rt, "qty"), price[v])
		if rapid.Bool().Draw(rt, "lệch") {
			c.lines[i].QuotedUnitPrice += 1000
			c.changed = true
		}
	}
	c.code = rapid.SampledFrom([]string{"", "PROP10", "PROPOLD"}).Draw(rt, "mã")
	c.via = rapid.SampledFrom([]string{order.SubmittedSync, order.SubmittedQueued}).Draw(rt, "cách gửi")
	return c
}

// checkWebInvariants kiểm đơn res vừa tạo của ca c: cờ, tổng bằng báo giá CRM want, tổng khớp CHECK, Σ phần chia, dòng
// cấm mã không nhận phần mã.
func (e *webEnv) checkWebInvariants(rt *rapid.T, c webCase, res order.WebOrderResult, want int64, noCoupon map[uuid.UUID]bool) {
	if res.PriceReview != c.review() || res.GrandTotal != want {
		rt.Fatalf("kết quả %+v; mong review %v, tổng %d", res, c.review(), want)
	}
	o := e.orderRow(res.OrderID)
	if o.review != c.review() || o.grand != o.subtotal-o.discount+o.fee || o.grand != want {
		rt.Fatalf("đơn %+v", o)
	}
	var alloc, orderDisc int64
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT (SELECT sum(allocated_order_discount) FROM ord.order_items
			WHERE order_id = o.id), o.order_discount FROM ord.orders o WHERE o.id = $1`, res.OrderID).Scan(&alloc, &orderDisc)
	})
	if alloc != orderDisc+o.coupon {
		rt.Fatalf("Σ phần chia %d ≠ %d + %d", alloc, orderDisc, o.coupon)
	}
	for _, l := range c.lines {
		if it := e.itemRow(res.OrderID, l.VariantID); noCoupon[l.VariantID] && it.allocated != 0 {
			rt.Fatalf("dòng chiến dịch cấm mã nhận %d tiền mã", it.allocated)
		}
	}
}
