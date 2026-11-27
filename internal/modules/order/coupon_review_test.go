package order_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// Task 433 (review độc lập M2, C-03/C-04/C-05): lượt mã khi hãng hoàn, mã giảm 0 đồng, sửa đơn khi mã hết hạn/tắt.

// oneUse là mã 10% (trần 50.000) chỉ một lượt tổng, một lượt mỗi khách, còn hạn ở giờ thử.
func oneUse(code string) couponTerms {
	c := sale10(code, 1)
	c.perCustomer = 1
	return c
}

// redemptionRow trả trạng thái và lý do trả của lượt mã (mới nhất) của đơn o.
func (e *orderEnv) redemptionRow(o uuid.UUID) (status, reason string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status, coalesce(release_reason, '') FROM promotion.coupon_redemptions
			WHERE order_id = $1 ORDER BY created_at DESC LIMIT 1`, o).Scan(&status, &reason)
	})
	return status, reason
}

// customerUses là tổng lượt của mã cid mà mọi khách đã dùng (coupon_customer_uses).
func (e *orderEnv) customerUses(cid uuid.UUID) int {
	e.t.Helper()
	return e.count(`SELECT coalesce(sum(used), 0)::int FROM promotion.coupon_customer_uses WHERE coupon_id = $1`, cid)
}

// carrierCoupon tạo đơn confirmed 2 × V1 + 1 × V2 dùng mã code, gắn vận đơn, lấy hàng, hãng báo hoàn → phiếu
// carrier_return đang requested. Trả đơn, phiếu.
func (e *orderEnv) carrierCoupon(code string) (order.Order, order.Return) {
	e.t.Helper()
	v1, v2 := e.variant(250000, 5), e.variant(180000, 5)
	in := e.dh01(v1, v2, code)
	o := e.mustCreateM2(in).Order
	if o.CouponDiscount == 0 {
		e.t.Fatalf("đơn không được giảm mã: %+v", o)
	}
	s := uuid.New()
	if err := e.attachBy(o.ID, s); err != nil {
		e.t.Fatal(err)
	}
	e.mustOnEvent(o.ID, s, order.StepPickedUp, true)
	return e.get(o.ID), e.openCarrierReturn(o.ID, s)
}

// fullBack là số nhận đủ, còn tốt mọi dòng của đơn o.
func fullBack(o order.Order) []order.ReceiveQtyLine {
	out := make([]order.ReceiveQtyLine, 0, len(o.Items))
	for _, it := range o.Items {
		out = append(out, qty(it.ID, it.Qty, 0, 0))
	}
	return out
}

// C-04: kho nhận kiện hoàn của hãng (giao không thành, đơn confirmed → returned) → trả lượt tổng và lượt của khách,
// dòng lượt released lý do carrier_returned; mã 1 lượt dùng lại được cho đơn sau.
func TestCarrierReturn_ReleasesCoupon(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	cid := e.coupon(oneUse("HOAN1"))
	o, r := e.carrierCoupon("HOAN1")
	if e.usedCount(cid) != 1 || e.customerUses(cid) != 1 {
		t.Fatalf("trước khi nhận: used %d, khách %d", e.usedCount(cid), e.customerUses(cid))
	}
	e.mustReceiveQty(r, fullBack(o)...)
	if g := e.get(o.ID); g.Status != order.StatusReturned {
		t.Fatalf("đơn: %s, mong returned", g.Status)
	}
	if e.usedCount(cid) != 0 || e.customerUses(cid) != 0 {
		t.Fatalf("sau hãng hoàn: used %d, khách %d; mong 0, 0", e.usedCount(cid), e.customerUses(cid))
	}
	if st, why := e.redemptionRow(o.ID); st != "released" || why != "carrier_returned" {
		t.Fatalf("lượt của đơn: %s %q, mong released carrier_returned", st, why)
	}
	v := e.variant(250000, 5)
	in := e.m2Input(0, ln(v, 1, 0))
	in.CouponCode = "HOAN1"
	if got := e.mustCreateM2(in).Order; got.CouponDiscount != 25000 {
		t.Fatalf("dùng lại mã 1 lượt: giảm %d, mong 25.000", got.CouponDiscount)
	}
}

// C-04: mã tắt release_on_cancel → hãng hoàn không trả lượt (cùng cờ với hủy đơn).
func TestCarrierReturn_KeepsCouponWhenFlagOff(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	cid := e.coupon(oneUse("GIULUOT"))
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE promotion.coupons SET release_on_cancel = false WHERE id = $1`, cid)
		return err //nolint:wrapcheck // test
	})
	o, r := e.carrierCoupon("GIULUOT")
	e.mustReceiveQty(r, fullBack(o)...)
	if e.usedCount(cid) != 1 || e.customerUses(cid) != 1 {
		t.Fatalf("mã tắt cờ: used %d, khách %d; mong 1, 1", e.usedCount(cid), e.customerUses(cid))
	}
	if st, _ := e.redemptionRow(o.ID); st != "active" {
		t.Fatalf("lượt của đơn: %s, mong active", st)
	}
}

// C-04, đồng thời: hai lần nhận cùng phiếu hoàn chạy song song → đúng một lần thành công, lượt chỉ trả một lần
// (used_count 0, không âm; mã khác không bị trừ).
func TestCarrierReturn_ConcurrentReceiveReleasesOnce(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	cid := e.coupon(sale10("HOANSS", 0))
	other := e.coupon(sale10("KHACSS", 0))
	v := e.variant(250000, 5)
	keep := e.m2Input(0, ln(v, 1, 0))
	keep.CouponCode = "HOANSS"
	e.mustCreateM2(keep)
	o, r := e.carrierCoupon("HOANSS")
	in := e.m2Input(0, ln(v, 1, 0))
	in.CouponCode = "KHACSS"
	e.mustCreateM2(in)
	lines := fullBack(o)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.receiveQty(r, lines...) })
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("hai lần nhận: %v, %v; mong đúng một lần thành công", errs[0], errs[1])
	}
	if e.usedCount(cid) != 1 || e.customerUses(cid) != 1 || e.usedCount(other) != 1 {
		t.Fatalf("used HOANSS %d (khách %d), KHACSS %d; mong 1, 1, 1", e.usedCount(cid), e.customerUses(cid),
			e.usedCount(other))
	}
}

// C-04: khách đã nhận hàng rồi trả (phiếu customer_return) → không trả lượt mã.
func TestCustomerReturn_KeepsCoupon(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	cid := e.coupon(oneUse("DANHAN"))
	v := e.variant(250000, 5)
	in := e.m2Input(30000, ln(v, 2, 0))
	in.CouponCode = "DANHAN"
	o := e.deliveredFrom(in)
	it := itemOf(t, o, v)
	r := e.mustReturn(o.ID, custReturn(rl(it.ID, 2)))
	if got := e.mustReceiveQty(r, qty(it.ID, 2, 0, 0)); got.Status != order.ReturnRestocked {
		t.Fatalf("phiếu: %s, mong restocked", got.Status)
	}
	if e.usedCount(cid) != 1 || e.customerUses(cid) != 1 {
		t.Fatalf("khách trả sau khi nhận: used %d, khách %d; mong 1, 1", e.usedCount(cid), e.customerUses(cid))
	}
	if st, _ := e.redemptionRow(o.ID); st != "active" {
		t.Fatalf("lượt của đơn: %s, mong active", st)
	}
}

// C-05: mã tính ra giảm 0 đồng (0,01% trên tạm tính 9.000) → tạo đơn bị từ chối zero_discount, không tiêu lượt.
func TestCreateCoupon_ZeroDiscount(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	c := oneUse("NHO")
	c.value, c.maxDiscount = 1, 0
	cid := e.coupon(c)
	v := e.variant(9000, 5)
	in := e.m2Input(0, ln(v, 1, 0))
	in.CouponCode = "NHO"
	if _, err := e.createM2(in); rejectReason(err) != promotion.ReasonZeroDiscount {
		t.Fatalf("tạo đơn: %v, mong coupon_rejected zero_discount", err)
	}
	if e.usedCount(cid) != 0 || e.customerUses(cid) != 0 ||
		e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1`, cid) != 0 {
		t.Fatal("mã giảm 0 vẫn tiêu lượt")
	}
}

// expiringCoupon tạo mã 10% không trần, tạm tính tối thiểu 300.000, hết hạn 30 phút sau giờ tạo đơn của test (sửa đơn
// chạy lúc e.now() + 1 giờ).
func (e *orderEnv) expiringCoupon(code string) uuid.UUID {
	e.t.Helper()
	return e.createCoupon(promotion.CouponInput{
		Code: code, Name: "Mã thử " + code, DiscountKind: promotion.KindPercent, Value: 1000, MinSubtotal: 300000,
		ReleaseOnCancel: true, ValidFrom: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), ValidTo: e.now().Add(30 * time.Minute),
	})
}

// C-03: mã hết hạn (rồi bị tắt) sau lúc tạo đơn → sửa số lượng vẫn giữ mã, phần giảm tính lại; dưới min_subtotal vẫn
// bị từ chối; bỏ mã rồi áp lại chính mã đó → theo luật hiện hành (expired).
func TestEdit_KeepCouponAfterExpiry(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	cid := e.expiringCoupon("HETHAN")
	_, i1, i2, got := e.couponOrderEdited(t, "HETHAN")
	if got.CouponDiscount != 93000 || got.GrandTotal != 837000 {
		t.Fatalf("mã hết hạn, sửa số lượng: giảm %d, tổng %d; mong 93.000, 837.000", got.CouponDiscount, got.GrandTotal)
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE promotion.coupons SET status = 'disabled' WHERE id = $1`, cid)
		return err //nolint:wrapcheck // test
	})
	got = e.mustEdit(got.ID, order.EditInput{Version: got.Version, Lines: editLines(editKeep(i1, 2, 0), editKeep(i2, 2, 0))},
		e.actor)
	if got.CouponDiscount != 86000 || e.usedCount(cid) != 1 {
		t.Fatalf("mã đã tắt, sửa số lượng: giảm %d, used %d; mong 86.000, 1", got.CouponDiscount, e.usedCount(cid))
	}
	if _, err := e.editOrder(got.ID, order.EditInput{Version: got.Version, Lines: editLines(editKeep(i2, 1, 0))},
		e.actor); rejectReason(err) != promotion.ReasonMinSubtotal {
		t.Fatalf("dưới min_subtotal: %v, mong coupon_rejected min_subtotal", err)
	}
	got = e.mustEdit(got.ID, order.EditInput{Version: got.Version, CouponCode: editPtr("")}, e.actor)
	if _, err := e.editOrder(got.ID, order.EditInput{Version: got.Version, CouponCode: editPtr("HETHAN")},
		e.actor); rejectReason(err) != promotion.ReasonDisabled {
		t.Fatalf("áp lại mã đã tắt: %v, mong coupon_rejected disabled", err)
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE promotion.coupons SET status = 'active' WHERE id = $1`, cid)
		return err //nolint:wrapcheck // test
	})
	if _, err := e.editOrder(got.ID, order.EditInput{Version: got.Version, CouponCode: editPtr("HETHAN")},
		e.actor); rejectReason(err) != promotion.ReasonExpired {
		t.Fatalf("áp lại mã hết hạn: %v, mong coupon_rejected expired", err)
	}
}

// boundPhoneCoupon tạo mã 10% (trần 50.000) gắn SĐT phone.
func (e *orderEnv) boundPhoneCoupon(code, phone string) uuid.UUID {
	e.t.Helper()
	c := sale10(code, 0)
	return e.createCoupon(promotion.CouponInput{
		Code: code, Name: "Mã gắn SĐT", DiscountKind: c.kind, Value: c.value, MaxDiscount: &c.maxDiscount,
		ReleaseOnCancel: true, ValidFrom: c.from, ValidTo: c.to, BoundPhone: phone,
	})
}

// C-03, luật SĐT gắn mã chung cho tạo và sửa: khách có sẵn mang danh tính SĐT gắn mã, đơn gửi hộ tới người nhận số khác
// → dùng được mã lúc tạo và lúc sửa (áp mã mới); khách không mang SĐT đó, người nhận số khác → bound_other cả hai lúc.
func TestCoupon_BoundPhoneSameRuleCreateAndEdit(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	const owned = "0912000111"
	e.boundPhoneCoupon("MAI10", owned)
	mai := e.newCustomer("Chị Mai", owned)
	stranger := e.newCustomer("Chị Hoa", "0912000222")
	v := e.variant(250000, 10)
	forCust := func(id uuid.UUID, code string) order.CreateInput {
		in := e.m2Input(0, ln(v, 1, 0))
		in.Customer, in.CouponCode = customer.OrderCustomerInput{CustomerID: &id}, code
		return in
	}
	if o := e.mustCreateM2(forCust(mai.ID, "MAI10")).Order; o.CouponDiscount != 25000 {
		t.Fatalf("tạo đơn gửi hộ cho khách có SĐT gắn mã: giảm %d, mong 25.000", o.CouponDiscount)
	}
	if _, err := e.createM2(forCust(stranger.ID, "MAI10")); rejectReason(err) != promotion.ReasonBoundOther {
		t.Fatalf("tạo đơn cho khách khác: %v, mong bound_other", err)
	}
	o := e.mustCreateM2(forCust(mai.ID, "")).Order
	got := e.mustEdit(o.ID, order.EditInput{Version: o.Version, CouponCode: editPtr("MAI10")}, e.actor)
	if got.CouponDiscount != 25000 {
		t.Fatalf("sửa đơn áp mã gắn SĐT của khách: giảm %d, mong 25.000", got.CouponDiscount)
	}
	so := e.mustCreateM2(forCust(stranger.ID, "")).Order
	if _, err := e.editOrder(so.ID, order.EditInput{Version: so.Version, CouponCode: editPtr("MAI10")},
		e.actor); rejectReason(err) != promotion.ReasonBoundOther {
		t.Fatalf("sửa đơn khách khác áp mã: %v, mong bound_other", err)
	}
}
