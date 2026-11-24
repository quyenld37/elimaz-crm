package order_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// webEnv là quoteEnv có danh mục hành chính mẫu, cài đặt và nguồn đơn mặc định (nguồn "Website" key web), và actor
// dịch vụ của khóa website.
type webEnv struct {
	*quoteEnv
	service order.Actor
	refSeq  int
}

func newWebEnv(t *testing.T) *webEnv {
	t.Helper()
	e := &webEnv{quoteEnv: newQuoteEnv(t)}
	e.staff.SeedAdminUnits(t)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { return order.EnsureDefaults(t.Context(), tx, e.tenant) })
	e.service = order.Actor{TenantID: e.tenant, UserID: uuid.New(), Type: order.ActorService}
	return e
}

// webInput là đơn web gửi thẳng (sync) của khách mới phone cho giỏ lines, đã báo giá lại bằng CRM (Quoted khớp).
func (e *webEnv) webInput(phone string, code string, lines ...order.WebLine) order.WebOrderInput {
	e.t.Helper()
	e.refSeq++
	in := order.WebOrderInput{
		ExternalRef:  fmt.Sprintf("ELIMAZ-%s-%d", e.now.Format("150405"), e.refSeq),
		Customer:     order.WebCustomer{Phone: logging.Phone(phone), Name: "Chị Mai"},
		ShipAddress:  *fullAddress(),
		Lines:        lines,
		CouponCode:   code,
		ShippingFee:  30000,
		SubmittedVia: order.SubmittedSync,
	}
	in.ShipAddress.Phone = logging.Phone(phone)
	q := e.mustQuote(order.WebQuoteInput{Lines: lines, CouponCode: code, ShippingFee: in.ShippingFee, CustomerPhone: &in.Customer.Phone})
	in.Quoted = order.WebQuoted{Fingerprint: q.Fingerprint, GrandTotal: q.GrandTotal}
	return in
}

func (e *webEnv) createWeb(in order.WebOrderInput) (order.WebOrderResult, error) {
	var res order.WebOrderResult
	err := e.app(func(tx pgx.Tx) error {
		var err error
		res, err = e.writer().CreateFromWeb(e.t.Context(), tx, in, e.service, e.now)
		return err //nolint:wrapcheck // test
	})
	return res, err
}

func (e *webEnv) mustCreateWeb(in order.WebOrderInput) order.WebOrderResult {
	e.t.Helper()
	res, err := e.createWeb(in)
	if err != nil {
		e.t.Fatalf("CreateFromWeb: %v", err)
	}
	return res
}

// webOrderRow là phần đơn web đọc thẳng từ bảng (chủ bảng).
type webOrderRow struct {
	status, channel, pending, via                         string
	source, customer                                      uuid.UUID
	subtotal, lineDisc, coupon, discount, fee, grand, cod int64
	review                                                bool
	quote                                                 []byte
}

func (e *webEnv) orderRow(id uuid.UUID) webOrderRow {
	e.t.Helper()
	var r webOrderRow
	var via *string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status, channel, pending_reason, submitted_via, traffic_source_id,
			customer_id, subtotal, line_discount_total, coupon_discount, discount_total, shipping_fee_charged,
			grand_total, cod_amount, price_review, web_quote FROM ord.orders WHERE id = $1`, id).Scan(
			&r.status, &r.channel, &r.pending, &via, &r.source, &r.customer, &r.subtotal, &r.lineDisc, &r.coupon,
			&r.discount, &r.fee, &r.grand, &r.cod, &r.review, &r.quote)
	})
	if via != nil {
		r.via = *via
	}
	return r
}

// webItemRow là một dòng đơn web: giảm dòng, phần chia mã, chiến dịch.
type webItemRow struct {
	lineDisc, allocated, net int64
	campaign                 *uuid.UUID
	revision, percent        *int32
	campDisc                 *int64
}

func (e *webEnv) itemRow(orderID, variant uuid.UUID) webItemRow {
	e.t.Helper()
	var r webItemRow
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT line_discount, allocated_order_discount, net_amount, web_campaign_id,
			web_campaign_revision, web_campaign_percent, web_campaign_discount
			FROM ord.order_items WHERE order_id = $1 AND variant_id = $2`, orderID, variant).Scan(
			&r.lineDisc, &r.allocated, &r.net, &r.campaign, &r.revision, &r.percent, &r.campDisc)
	})
	return r
}

func (e *webEnv) webSource() uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		id, err = order.SourceByKey(e.t.Context(), tx, order.SourceKeyWeb)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// [WB01], [WB06]: A thuộc chiến dịch 20 % cấm mã (1 × 400.000), B 1 × 200.000, mã 10 % → đơn new, kênh web, nguồn
// "Website"; A giảm chiến dịch 80.000 ghi mã, revision, phần trăm lên dòng, không nhận phần mã; mã chỉ trên B (20.000)
// đúng như báo giá; tổng = báo giá; giữ hàng 2 dòng, 1 lượt mã, 1 sự kiện order.created; giảm chiến dịch 20 % vượt
// ngưỡng duyệt giảm tay mặc định (10 %) mà đơn không chờ duyệt (giảm chiến dịch không phải giảm tay).
func TestCreateFromWeb_CampaignAndCoupon(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	pa, va := e.product(5, 400000)
	_, vb := e.product(5, 200000)
	camp := e.campaign(20, false, pa)
	e.quoteCoupon(promotion.CouponInput{Code: "WEB10", DiscountKind: promotion.KindPercent, Value: 1000})
	in := e.webInput("0903000111", "web10", wl(va[0], 1, 400000), wl(vb[0], 1, 200000))
	if in.Quoted.GrandTotal != 400000-80000+200000-20000+30000 {
		t.Fatalf("báo giá: %d", in.Quoted.GrandTotal)
	}
	res := e.mustCreateWeb(in)
	if res.Status != order.StatusNew || res.PriceReview || res.Replayed || res.GrandTotal != in.Quoted.GrandTotal || res.Code == "" {
		t.Fatalf("kết quả: %+v", res)
	}
	o := e.orderRow(res.OrderID)
	if o.status != order.StatusNew || o.channel != order.ChannelWeb || o.pending != "" || o.via != order.SubmittedSync ||
		o.source != e.webSource() || o.review || o.quote != nil {
		t.Errorf("đơn: %+v", o)
	}
	if o.subtotal != 600000 || o.lineDisc != 80000 || o.coupon != 20000 || o.discount != 100000 || o.fee != 30000 ||
		o.grand != 530000 || o.cod != 530000 {
		t.Errorf("tiền đơn: %+v", o)
	}
	a := e.itemRow(res.OrderID, va[0])
	if a.lineDisc != 80000 || a.allocated != 0 || a.net != 320000 || a.campaign == nil || *a.campaign != camp.ID ||
		int(*a.revision) != camp.Revision || *a.percent != 20 || *a.campDisc != 80000 {
		t.Errorf("dòng chiến dịch: %+v", a)
	}
	b := e.itemRow(res.OrderID, vb[0])
	if b.lineDisc != 0 || b.allocated != 20000 || b.net != 180000 || b.campaign != nil || b.campDisc != nil {
		t.Errorf("dòng thường: %+v", b)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM inventory.stock_reservations WHERE state = 'held'`:                                 2,
		`SELECT count(*) FROM promotion.coupon_redemptions`:                                                      1,
		`SELECT count(*) FROM platform.domain_events WHERE aggregate_type = 'order' AND topic = 'order.created'`: 1,
	} {
		if n := e.count(q); n != want {
			t.Errorf("%s = %d, mong %d", q, n, want)
		}
	}
}

// Chiến dịch cho dùng mã: mã chia trên cả hai dòng như báo giá; tổng đơn bằng báo giá.
func TestCreateFromWeb_CampaignAllowsCoupon(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	pa, va := e.product(5, 400000)
	_, vb := e.product(5, 200000)
	e.campaign(20, true, pa)
	e.quoteCoupon(promotion.CouponInput{Code: "ALL10", DiscountKind: promotion.KindPercent, Value: 1000})
	in := e.webInput("0903000112", "ALL10", wl(va[0], 1, 400000), wl(vb[0], 1, 200000))
	res := e.mustCreateWeb(in)
	o := e.orderRow(res.OrderID)
	if o.coupon != 52000 || o.grand != in.Quoted.GrandTotal || res.GrandTotal != in.Quoted.GrandTotal {
		t.Fatalf("đơn %+v, báo giá %d", o, in.Quoted.GrandTotal)
	}
	if a, b := e.itemRow(res.OrderID, va[0]), e.itemRow(res.OrderID, vb[0]); a.allocated+b.allocated != 52000 ||
		a.allocated == 0 || b.allocated == 0 {
		t.Errorf("chia mã: %+v %+v", a, b)
	}
}

// Cùng ExternalRef hai lần nối tiếp → cùng đơn (lần hai Replayed), 1 đơn, giữ hàng một lần; hai lần song song → một
// đơn, cả hai cùng OrderID.
func TestCreateFromWeb_SameExternalRef(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	in := e.webInput("0903000113", "", wl(v[0], 1, 200000))
	first := e.mustCreateWeb(in)
	second := e.mustCreateWeb(in)
	if second.OrderID != first.OrderID || !second.Replayed || second.Code != first.Code || second.GrandTotal != first.GrandTotal {
		t.Fatalf("lần hai %+v, lần đầu %+v", second, first)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 1 {
		t.Errorf("số đơn %d", n)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_reservations`); n != 1 {
		t.Errorf("số dòng giữ hàng %d", n)
	}

	par := e.webInput("0903000114", "", wl(v[0], 1, 200000))
	par.Note = "Gọi trước khi giao"
	var wg sync.WaitGroup
	results := make([]order.WebOrderResult, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Go(func() { results[i], errs[i] = e.createWeb(par) })
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil || results[i].OrderID != results[0].OrderID {
			t.Fatalf("lần %d: %+v %v (lần 0 %+v)", i, results[i], errs[i], results[0])
		}
	}
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE external_ref = $1`, par.ExternalRef); n != 1 {
		t.Errorf("số đơn song song %d", n)
	}
	// Bên thua không ghi gì lên đơn của bên thắng (kể cả khi Create trả Replayed): đúng một ghi chú, một bên Replayed=false.
	if n := e.count(`SELECT count(*) FROM ord.order_notes WHERE order_id = $1`, results[0].OrderID); n != 1 {
		t.Errorf("số ghi chú %d", n)
	}
	fresh := 0
	for _, r := range results {
		if !r.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Errorf("%d lần báo đơn mới, mong 1", fresh)
	}
}

// [WB10]: gửi thẳng với giá cũ → QuoteChangedError có báo giá CRM; 0 đơn, 0 giữ hàng, used_count không đổi. Tổng
// website khác tổng CRM (chiến dịch bắt đầu sau khi báo giá) cũng là đổi giá.
func TestCreateFromWeb_SyncQuoteChanged(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	p, v := e.product(5, 350000)
	e.quoteCoupon(promotion.CouponInput{Code: "SYNC10", DiscountKind: promotion.KindPercent, Value: 1000})
	in := e.webInput("0903000115", "SYNC10", wl(v[0], 1, 300000))
	_, err := e.createWeb(in)
	qc, ok := errors.AsType[*order.QuoteChangedError](err)
	if !ok || qc.Quote.Lines[0].UnitPrice != 350000 || !qc.Quote.Changed {
		t.Fatalf("mong QuoteChangedError giá 350.000, có %v", err)
	}
	stale := e.webInput("0903000115", "", wl(v[0], 1, 350000))
	e.campaign(10, true, p)
	if _, err := e.createWeb(stale); !errors.As(err, new(*order.QuoteChangedError)) {
		t.Fatalf("chiến dịch mới bật: mong QuoteChangedError, có %v", err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM ord.orders`:                            0,
		`SELECT count(*) FROM inventory.stock_reservations`:          0,
		`SELECT coalesce(sum(used_count), 0) FROM promotion.coupons`: 0,
		`SELECT count(*) FROM customer.customers`:                    0,
	} {
		if n := e.count(q); n != want {
			t.Errorf("%s = %d, mong %d", q, n, want)
		}
	}
}

// QĐ15: gửi từ hàng đợi với giá cũ → đơn theo giá CRM, price_review, web_quote giữ tổng website đã báo, submitted_via
// queued.
func TestCreateFromWeb_QueuedPriceChanged(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 350000)
	in := e.webInput("0903000116", "", wl(v[0], 2, 300000))
	in.SubmittedVia = order.SubmittedQueued
	res := e.mustCreateWeb(in)
	if !res.PriceReview || res.GrandTotal != 2*350000+30000 {
		t.Fatalf("kết quả %+v", res)
	}
	o := e.orderRow(res.OrderID)
	if !o.review || o.via != order.SubmittedQueued || o.grand != 730000 || o.pending != "" {
		t.Errorf("đơn %+v", o)
	}
	var total int64
	var unit int64
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT (web_quote->>'grandTotal')::bigint,
			(web_quote->'lines'->0->>'quotedUnitPrice')::bigint FROM ord.orders WHERE id = $1`, res.OrderID).Scan(&total, &unit)
	})
	if total != in.Quoted.GrandTotal || unit != 300000 {
		t.Errorf("web_quote: tổng %d, đơn giá %d (mong %d, 300000)", total, unit, in.Quoted.GrandTotal)
	}
}

// Mã hết hạn: gửi thẳng → CouponInvalidError expired, không đơn; từ hàng đợi → đơn không có mã, price_review.
func TestCreateFromWeb_ExpiredCoupon(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 350000)
	e.quoteCoupon(promotion.CouponInput{
		Code: "OLD10", DiscountKind: promotion.KindPercent, Value: 1000,
		ValidFrom: e.now.Add(-48 * time.Hour), ValidTo: e.now.Add(-time.Hour),
	})
	in := e.webInput("0903000117", "OLD10", wl(v[0], 1, 350000))
	in.Quoted.GrandTotal = 350000 - 35000 + 30000 // website đã báo khi mã còn hạn
	_, err := e.createWeb(in)
	if ce, ok := errors.AsType[*order.CouponInvalidError](err); !ok || ce.Reason != string(promotion.ReasonExpired) {
		t.Fatalf("mong CouponInvalidError expired, có %v", err)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Fatalf("số đơn %d", n)
	}
	in.SubmittedVia = order.SubmittedQueued
	res := e.mustCreateWeb(in)
	o := e.orderRow(res.OrderID)
	if !res.PriceReview || !o.review || o.coupon != 0 || o.grand != 380000 {
		t.Errorf("đơn %+v, kết quả %+v", o, res)
	}
	if n := e.count(`SELECT count(*) FROM promotion.coupon_redemptions`); n != 0 {
		t.Errorf("lượt mã %d", n)
	}
}

// Mã giới hạn 1 lượt mỗi khách mà khách đã dùng (báo giá không biết, Redeem từ chối lúc tạo đơn): gửi thẳng →
// CouponInvalidError customer_limit; từ hàng đợi → đơn không mã, price_review (lùi savepoint rồi tạo lại).
func TestCreateFromWeb_CouponRejectedAtRedeem(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(10, 200000)
	e.quoteCoupon(promotion.CouponInput{
		Code: "ONCE", DiscountKind: promotion.KindPercent, Value: 1000, PerCustomerLimit: ptr[int32](1),
	})
	e.mustCreateWeb(e.webInput("0903000118", "ONCE", wl(v[0], 1, 200000)))
	in := e.webInput("0903000118", "ONCE", wl(v[0], 1, 200000))
	_, err := e.createWeb(in)
	if ce, ok := errors.AsType[*order.CouponInvalidError](err); !ok || ce.Reason != string(promotion.ReasonCustomerLimit) {
		t.Fatalf("mong CouponInvalidError customer_limit, có %v", err)
	}
	in.SubmittedVia = order.SubmittedQueued
	res := e.mustCreateWeb(in)
	if o := e.orderRow(res.OrderID); !o.review || o.coupon != 0 || o.grand != 230000 {
		t.Errorf("đơn %+v", o)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 2 {
		t.Errorf("số đơn %d", n)
	}
	if n := e.count(`SELECT count(*) FROM promotion.coupon_redemptions`); n != 1 {
		t.Errorf("lượt mã %d", n)
	}
}

// [WB08]: tồn 1, đơn A lấy 1 → được; đơn B → ErrOutOfStock, chi tiết *OutOfStockError đúng biến thể, Available 0; tx
// lùi toàn bộ (không còn khách của B).
func TestCreateFromWeb_LastUnit(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(1, 200000)
	a := e.webInput("0903000119", "", wl(v[0], 1, 200000))
	b := e.webInput("0903000120", "", wl(v[0], 1, 200000))
	e.mustCreateWeb(a)
	_, err := e.createWeb(b)
	oe, ok := errors.AsType[*order.OutOfStockError](err)
	if !errors.Is(err, order.ErrOutOfStock) || !ok || oe.VariantID != v[0] || oe.Available != 0 {
		t.Fatalf("mong ErrOutOfStock của %s, có %v", v[0], err)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE value = '+84903000120'`); n != 0 {
		t.Errorf("còn danh tính của khách B: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 1 {
		t.Errorf("số khách %d", n)
	}
}

// Nguồn "Website" bị nhân viên đổi tên → đơn web vẫn gắn đúng nguồn đó (theo key).
func TestCreateFromWeb_RenamedSource(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	src := e.webSource()
	if err := e.app(func(tx pgx.Tx) error {
		_, err := order.Writer{}.UpdateSource(t.Context(), tx, src, 1, order.SourcePatch{Name: ptr("Web ELIMAZ")})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	res := e.mustCreateWeb(e.webInput("0903000121", "", wl(v[0], 1, 200000)))
	if o := e.orderRow(res.OrderID); o.source != src {
		t.Errorf("nguồn %s, mong %s", o.source, src)
	}
}

// [WB09]: biến thể ngừng bán, mã lạ → ItemUnavailableError liệt kê đúng các biến thể (thứ tự gửi); biến thể của người
// bán khác → ErrMixedSellers (ADR-29). Không đơn.
func TestCreateFromWeb_Unavailable(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, vs := e.product(5, 200000, 250000)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { catalogtest.Discontinue(t, tx, e.shop.Seller, vs[1]); return nil })
	unknown := uuid.New()
	in := e.webInput("0903000122", "", wl(vs[0], 1, 200000), wl(vs[1], 1, 250000), wl(unknown, 1, 1000))
	_, err := e.createWeb(in)
	ue, ok := errors.AsType[*order.ItemUnavailableError](err)
	if !ok || !slices.Equal(ue.VariantIDs, []uuid.UUID{vs[1], unknown}) {
		t.Fatalf("mong ItemUnavailableError [%s %s], có %v", vs[1], unknown, err)
	}
	partner := e.variantOf(e.shop.Partner, 100000, 5)
	mixed := e.webInput("0903000122", "", wl(vs[0], 1, 200000), wl(partner, 1, 100000))
	if _, err := e.createWeb(mixed); !errors.Is(err, order.ErrMixedSellers) {
		t.Fatalf("mong ErrMixedSellers, có %v", err)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Errorf("số đơn %d", n)
	}
}

// 05 mục 1: SĐT đã là danh tính của khách K → đơn thuộc K. WebAccountID mới → thành danh tính web_account của K;
// WebAccountID đã thuộc hồ sơ khác → đơn vẫn của hồ sơ SĐT, không gộp, danh tính giữ chủ cũ.
func TestCreateFromWeb_CustomerIdentity(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(10, 200000)
	k := e.newCustomer("Chị Kim", "0903000123")
	in := e.webInput("0903000123", "", wl(v[0], 1, 200000))
	in.Customer.WebAccountID = "acc-kim"
	res := e.mustCreateWeb(in)
	if o := e.orderRow(res.OrderID); o.customer != k.ID {
		t.Fatalf("khách của đơn %s, mong %s", o.customer, k.ID)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE kind = 'web_account' AND value = 'acc-kim'
		AND customer_id = $1`, k.ID); n != 1 {
		t.Errorf("danh tính web_account của K: %d", n)
	}
	other := e.webInput("0903000124", "", wl(v[0], 1, 200000))
	other.Customer.WebAccountID = "acc-kim"
	res2 := e.mustCreateWeb(other)
	o2 := e.orderRow(res2.OrderID)
	if o2.customer == k.ID {
		t.Fatalf("đơn SĐT khác bị gắn vào K")
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE value = 'acc-kim' AND customer_id = $1`, k.ID); n != 1 {
		t.Errorf("danh tính web_account đổi chủ")
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_merges`); n != 0 {
		t.Errorf("có %d dòng gộp khách", n)
	}
}

// Ghi chú của khách thành ghi chú nội bộ của đơn (tác giả là khóa dịch vụ), không gửi hãng.
func TestCreateFromWeb_Note(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	in := e.webInput("0903000125", "", wl(v[0], 1, 200000))
	in.Note = "  Giao giờ hành chính  "
	res := e.mustCreateWeb(in)
	if n := e.count(`SELECT count(*) FROM ord.order_notes WHERE order_id = $1 AND body = 'Giao giờ hành chính'
		AND author_id = $2`, res.OrderID, e.service.UserID); n != 1 {
		t.Errorf("ghi chú: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND carrier_note = ''`, res.OrderID); n != 1 {
		t.Errorf("ghi chú cho hãng phải rỗng")
	}
}

// Đầu vào sai → ErrInvalidInput: thiếu mã ngoài, cách gửi lạ, tổng báo âm.
func TestCreateFromWeb_InvalidInput(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	base := e.webInput("0903000126", "", wl(v[0], 1, 200000))
	for name, mut := range map[string]func(*order.WebOrderInput){
		"không mã ngoài": func(in *order.WebOrderInput) { in.ExternalRef = "" },
		"cách gửi lạ":    func(in *order.WebOrderInput) { in.SubmittedVia = "email" },
		"tổng báo âm":    func(in *order.WebOrderInput) { in.Quoted.GrandTotal = -1 },
		"không dòng":     func(in *order.WebOrderInput) { in.Lines = nil },
	} {
		in := base
		mut(&in)
		if _, err := e.createWeb(in); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("%s: mong ErrInvalidInput, có %v", name, err)
		}
	}
}

// Sự kiện, việc, audit sinh ra khi tạo đơn web (kể cả danh tính web) không chứa SĐT, tên, địa chỉ.
func TestCreateFromWeb_NoPersonalData(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	in := e.webInput("0903000127", "", wl(v[0], 1, 200000))
	in.Customer.WebAccountID = "acc-pii"
	in.SubmittedVia = order.SubmittedQueued
	in.Quoted.GrandTotal++
	e.mustCreateWeb(in)
	e.assertNoPII([]string{`SELECT coalesce(string_agg(web_quote::text, ' '), '') FROM ord.orders`},
		"903000127", "Chị Mai", "Chị Lan", "Lê Lợi", "acc-pii")
}
