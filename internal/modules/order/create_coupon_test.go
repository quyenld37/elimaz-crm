package order_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// couponTerms là mã phần trăm value (phần vạn), trần maxDiscount (0: không trần), số lượt maxUses (0: không giới hạn),
// hạn [from, to).
type couponTerms struct {
	code               string
	kind               string
	value, maxDiscount int64
	maxUses            int32
	from, to           time.Time
}

// sale10 là mã SALE10 của kế hoạch (10%, trần 50.000) còn hạn ở giờ thử e.now().
func sale10(code string, maxUses int32) couponTerms {
	return couponTerms{
		code: code, kind: promotion.KindPercent, value: 1000, maxDiscount: 50000, maxUses: maxUses,
		from: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), to: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
	}
}

// coupon tạo mã theo c bằng promotion.Writer.CreateCoupon; trả mã của mã.
func (e *orderEnv) coupon(c couponTerms) uuid.UUID {
	e.t.Helper()
	in := promotion.CouponInput{
		Code: c.code, Name: "Mã thử " + c.code, DiscountKind: c.kind, Value: c.value, ReleaseOnCancel: true,
		ValidFrom: c.from, ValidTo: c.to,
	}
	if c.maxDiscount > 0 {
		in.MaxDiscount = &c.maxDiscount
	}
	if c.maxUses > 0 {
		in.MaxUses = &c.maxUses
	}
	var out promotion.Coupon
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = promotion.Writer{}.CreateCoupon(e.t.Context(), tx,
			promotion.Actor{TenantID: e.tenant, UserID: e.actor.UserID}, in)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo mã %s: %v", c.code, err)
	}
	return out.ID
}

// rejectReason trả lý do từ chối mã trong err ("" nếu err không phải từ chối mã).
func rejectReason(err error) promotion.Reason {
	if re, ok := errors.AsType[*promotion.RejectError](err); ok && errors.Is(err, promotion.ErrCouponRejected) {
		return re.Reason
	}
	return ""
}

// dh01 là đơn DH01 (2 × 250.000 giảm dòng 20.000, 1 × 180.000, phí 30.000) với mã code.
func (e *orderEnv) dh01(v1, v2 uuid.UUID, code string) order.CreateInput {
	in := e.m2Input(30000, ln(v1, 2, 20000), ln(v2, 1, 0))
	in.CouponCode = code
	return in
}

// Mutation M2-32 (create_coupon.go:38): mã % tính trên tạm tính trừ giảm dòng và giảm cấp đơn. 2 × 250.000, giảm dòng
// 20.000, giảm đơn 30.000, mã 10% không trần → nền 450.000, giảm mã 45.000, tổng 405.000.
func TestCreateCoupon_BaseAfterManualDiscounts(t *testing.T) {
	e := newM2Env(t)
	c := sale10("BASE10", 0)
	c.maxDiscount = 0
	e.coupon(c)
	v := e.variant(250000, 5)
	in := e.m2Input(0, ln(v, 2, 20000))
	in.OrderDiscount, in.CouponCode = 30000, "BASE10"
	o := e.mustCreateM2(in).Order
	if o.CouponDiscount != 45000 || o.GrandTotal != 405000 {
		t.Fatalf("giảm mã %d, tổng %d; mong 45.000, 405.000", o.CouponDiscount, o.GrandTotal)
	}
}

// Mutation M2-32 (create_coupon.go:32): mã gắn SĐT, khách mới chưa có danh tính SĐT → luật mã so với SĐT người nhận;
// SĐT người nhận khác → bound_other; khách có danh tính SĐT đúng số gắn mã gửi hộ người khác → so với SĐT của khách.
func TestCreateCoupon_BoundPhoneUsesRecipient(t *testing.T) {
	e := newM2Env(t)
	c := sale10("", 0)
	if err := e.app(func(tx pgx.Tx) error {
		_, err := promotion.Writer{}.CreateCoupon(e.t.Context(), tx, promotion.Actor{TenantID: e.tenant, UserID: e.actor.UserID},
			promotion.CouponInput{
				Code: "PHONE10", Name: "Mã gắn SĐT", DiscountKind: c.kind, Value: c.value, ReleaseOnCancel: true,
				ValidFrom: c.from, ValidTo: c.to, BoundPhone: shipPhone,
			})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	v := e.variant(250000, 5)
	in := e.m2Input(0, ln(v, 1, 0))
	in.CouponCode = "PHONE10"
	if o := e.mustCreateM2(in).Order; o.CouponDiscount != 25000 {
		t.Fatalf("giảm mã %d, mong 25.000", o.CouponDiscount)
	}
	other := e.m2Input(0, ln(v, 1, 0))
	other.CouponCode = "PHONE10"
	addr := *fullAddress()
	addr.Phone = "0909111222"
	other.ShipAddress = &addr
	if _, err := e.createM2(other); rejectReason(err) != promotion.ReasonBoundOther {
		t.Fatalf("SĐT người nhận khác: %v, mong bound_other", err)
	}
	// Khách có danh tính SĐT đúng số gắn mã, gửi hộ tới người nhận số khác → vẫn dùng SĐT của khách.
	owner := e.m2Input(0, ln(v, 1, 0))
	owner.CouponCode = "PHONE10"
	owner.Customer = customer.OrderCustomerInput{FullName: "Chị Mai", Phone: shipPhone, PhoneIsCustomers: true}
	owner.ShipAddress = &addr
	if o := e.mustCreateM2(owner).Order; o.CouponDiscount != 25000 {
		t.Fatalf("khách có SĐT gắn mã, gửi hộ: giảm %d, mong 25.000", o.CouponDiscount)
	}
}

// DH01 với SALE10 (gõ chữ thường): couponDiscount 50.000 (10% của 660.000, chặn trần), grandTotal 640.000, phần giảm
// chia về dòng (trigger tổng tiền qua lúc commit), used_count 1, dòng coupon_redemptions active, sự kiện coupon.used.
func TestCreateCoupon_DH01(t *testing.T) {
	e := newM2Env(t)
	cid := e.coupon(sale10("SALE10", 0))
	v1, v2 := e.variant(250000, 5), e.variant(180000, 5)
	o := e.mustCreateM2(e.dh01(v1, v2, "sale10")).Order
	if o.CouponDiscount != 50000 || o.GrandTotal != 640000 || o.CODAmount != 640000 || o.DiscountTotal != 70000 ||
		o.Subtotal != 680000 || o.LineDiscountTotal != 20000 || o.Status != order.StatusConfirmed {
		t.Errorf("đơn sai: %+v", o)
	}
	var alloc, net int64
	for _, it := range o.Items {
		alloc += it.AllocatedOrderDiscount
		net += it.NetAmount
	}
	if alloc != 50000 || net != 610000 {
		t.Errorf("phân bổ dòng: allocated %d (mong 50000), net %d (mong 610000): %+v", alloc, net, o.Items)
	}
	if got := e.get(o.ID); got.CouponDiscount != 50000 || got.GrandTotal != 640000 {
		t.Errorf("Get sau commit: %+v", got)
	}
	if n := e.count(`SELECT used_count FROM promotion.coupons WHERE id = $1`, cid); n != 1 {
		t.Errorf("used_count = %d", n)
	}
	if n := e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1 AND order_id = $2
		AND status = 'active' AND discount = 50000 AND customer_id = $3`, cid, o.ID, o.CustomerID); n != 1 {
		t.Errorf("dòng coupon_redemptions: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'coupon.used'
		AND aggregate_type = 'order_coupon' AND aggregate_id = $1 AND seq = $2 AND (payload->>'discount')::bigint = 50000`,
		o.ID.String(), o.Version); n != 1 {
		t.Errorf("sự kiện coupon.used: %d", n)
	}
}

// KM01: max_uses 2 → đơn thứ ba bị từ chối exhausted; không có đơn thứ ba, giữ hàng không đổi, used_count 2.
func TestCreateCoupon_Exhausted(t *testing.T) {
	e := newM2Env(t)
	cid := e.coupon(sale10("KM01", 2))
	v1, v2 := e.variant(250000, 10), e.variant(180000, 10)
	e.mustCreateM2(e.dh01(v1, v2, "KM01"))
	e.mustCreateM2(e.dh01(v1, v2, "KM01"))
	held := e.balance(v1).Held
	_, err := e.createM2(e.dh01(v1, v2, "KM01"))
	if rejectReason(err) != promotion.ReasonExhausted {
		t.Fatalf("đơn thứ ba: %v, mong exhausted", err)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 2 {
		t.Errorf("%d đơn, mong 2", n)
	}
	if b := e.balance(v1); b.Held != held {
		t.Errorf("giữ hàng đổi: %d → %d", held, b.Held)
	}
	if n := e.count(`SELECT used_count FROM promotion.coupons WHERE id = $1`, cid); n != 2 {
		t.Errorf("used_count = %d", n)
	}
}

// KM02 và các lý do khác: mã hết hạn → expired; mã không có → not_found; cả lần tạo lùi (không khách mới, không đơn).
func TestCreateCoupon_Rejected(t *testing.T) {
	e := newM2Env(t)
	old := sale10("KM02", 0)
	old.from, old.to = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	e.coupon(old)
	v1, v2 := e.variant(250000, 10), e.variant(180000, 10)
	customers := e.count(`SELECT count(*) FROM customer.customers`)
	for code, want := range map[string]promotion.Reason{"KM02": promotion.ReasonExpired, "KHONGCO": promotion.ReasonNotFound} {
		if _, err := e.createM2(e.dh01(v1, v2, code)); rejectReason(err) != want {
			t.Errorf("%s: %v, mong %s", code, err, want)
		}
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 0 {
		t.Errorf("%d đơn, mong 0", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != customers {
		t.Errorf("khách mới còn lại: %d → %d", customers, n)
	}
	if b := e.balance(v1); b.Held != 0 {
		t.Errorf("giữ hàng: %+v", b)
	}
	in := e.dh01(v1, v2, " \t")
	if _, err := e.createM2(in); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("mã toàn khoảng trắng: %v, mong ErrInvalidInput", err)
	}
}

// WAT07 (phần CRM): max_uses 1, hai đơn cùng mã cùng lúc, lặp 20 lần → mỗi lần đúng một đơn có mã, một exhausted;
// used_count 1.
func TestCreateCoupon_ConcurrentLastUse(t *testing.T) {
	e := newM2Env(t)
	v1, v2 := e.variant(250000, 100), e.variant(180000, 100)
	for i := range 20 {
		code := "WAT07X" + uuid.NewString()[:6]
		cid := e.coupon(sale10(code, 1))
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for j := range 2 {
			wg.Go(func() { _, errs[j] = e.createM2(e.dh01(v1, v2, code)) })
		}
		wg.Wait()
		ok, exhausted := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case rejectReason(err) == promotion.ReasonExhausted:
				exhausted++
			default:
				t.Fatalf("lần %d: lỗi lạ %v", i, err)
			}
		}
		if ok != 1 || exhausted != 1 {
			t.Fatalf("lần %d: %d thành công, %d exhausted", i, ok, exhausted)
		}
		if n := e.count(`SELECT used_count FROM promotion.coupons WHERE id = $1`, cid); n != 1 {
			t.Fatalf("lần %d: used_count = %d", i, n)
		}
	}
	if n := e.count(`SELECT count(*) FROM ord.orders`); n != 20 {
		t.Errorf("%d đơn, mong 20", n)
	}
}

// Gửi lại cùng khóa nghiệp vụ: cùng mã (khác hoa thường) → trả đơn cũ, không dùng mã lần hai; khác mã hay bỏ mã →
// ErrDuplicateOrder.
func TestCreateCoupon_Replay(t *testing.T) {
	e := newM2Env(t)
	cid := e.coupon(sale10("SALE10", 0))
	e.coupon(sale10("SALE20", 0))
	v1, v2 := e.variant(250000, 10), e.variant(180000, 10)
	in := e.dh01(v1, v2, "SALE10")
	first := e.mustCreateM2(in).Order
	in.CouponCode = "sale10"
	if res := e.mustCreateM2(in); !res.Replayed || res.Order.ID != first.ID {
		t.Errorf("gửi lại cùng mã: %+v", res)
	}
	for _, code := range []string{"SALE20", ""} {
		in.CouponCode = code
		if _, err := e.createM2(in); !errors.Is(err, order.ErrDuplicateOrder) {
			t.Errorf("gửi lại với mã %q: %v, mong ErrDuplicateOrder", code, err)
		}
	}
	if n := e.count(`SELECT used_count FROM promotion.coupons WHERE id = $1`, cid); n != 1 {
		t.Errorf("used_count = %d", n)
	}
	plain := e.m2Input(0, ln(v1, 1, 0))
	e.mustCreateM2(plain)
	plain.CouponCode = "SALE10"
	if _, err := e.createM2(plain); !errors.Is(err, order.ErrDuplicateOrder) {
		t.Errorf("đơn không mã gửi lại kèm mã: %v, mong ErrDuplicateOrder", err)
	}
}
