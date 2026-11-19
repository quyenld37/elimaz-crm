package order_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// NeedsApproval (Q3): giảm tay (giảm dòng + cấp đơn, không tính mã) vượt ngưỡng và người gọi không có quyền duyệt.
func TestNeedsApproval(t *testing.T) {
	ten := order.Settings{DiscountApprovalThresholdBP: 1000}
	twenty := order.Settings{DiscountApprovalThresholdBP: 2000}
	tot := func(line, ord, coupon int64) pricing.Totals {
		return pricing.Totals{Subtotal: 1000000, LineDiscountTotal: line, OrderDiscount: ord, CouponDiscount: coupon}
	}
	for _, c := range []struct {
		name       string
		t          pricing.Totals
		s          order.Settings
		canApprove bool
		want       bool
	}{
		{"giảm tay đúng 10%", tot(0, 100000, 0), ten, false, false},
		{"giảm tay 100.001", tot(0, 100001, 0), ten, false, true},
		{"giảm dòng + cấp đơn cộng lại vượt", tot(60000, 40001, 0), ten, false, true},
		{"có quyền duyệt", tot(0, 100001, 0), ten, true, false},
		{"mã 200.000, giảm tay 0", tot(0, 0, 200000), ten, false, false},
		{"mã không tính vào giảm tay", tot(0, 100000, 200000), ten, false, false},
		{"ngưỡng 20%, giảm tay 150.000", tot(0, 150000, 0), twenty, false, false},
		{"ngưỡng 0, giảm 1 đồng", tot(1, 0, 0), order.Settings{}, false, true},
		{"không giảm, tạm tính 0", pricing.Totals{}, order.Settings{}, false, false},
	} {
		if got := order.NeedsApproval(c.t, c.s, c.canApprove); got != c.want {
			t.Errorf("%s: NeedsApproval = %v, mong %v", c.name, got, c.want)
		}
	}
}

// dh16 là đơn DH16 (tạm tính 1.000.000, một dòng) với ý định intent, giảm cấp đơn discount.
func (e *orderEnv) dh16(v uuid.UUID, intent string, discount int64) order.CreateInput {
	in := e.m2Input(0, ln(v, 1, 0))
	in.Intent, in.OrderDiscount = intent, discount
	return in
}

// createAs tạo đơn in với tác nhân a.
func (e *orderEnv) createAs(in order.CreateInput, a order.Actor) order.Order {
	e.t.Helper()
	var res order.CreateResult
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		res, err = e.writer().Create(e.t.Context(), tx, in, a, e.now())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("Create: %v", err)
	}
	return res.Order
}

// DH16/AT29: sale giảm tay 100.000 → confirmed; 100.001 → new, chờ duyệt giảm giá (lịch sử có lý do, chỉ
// order.created); manager cùng đầu vào → confirmed, discountApprovedBy = manager; mã giảm 200.000, giảm tay 0 →
// confirmed; ý định draft vượt ngưỡng → draft chờ duyệt, ý định new → new chờ duyệt.
func TestCreate_DiscountApproval(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(1000000, 100)
	sale := e.actor
	manager := order.Actor{
		TenantID: e.tenant, UserID: e.staff.User(t, e.tenant, "quanly@elimaz.test", "active"), CanApproveDiscount: true,
	}

	if o := e.createAs(e.dh16(v, order.IntentConfirmed, 100000), sale); o.Status != order.StatusConfirmed ||
		o.PendingReason != "" || o.DiscountApprovedBy != nil {
		t.Errorf("giảm đúng ngưỡng: %+v", o)
	}

	o := e.createAs(e.dh16(v, order.IntentConfirmed, 100001), sale)
	if o.Status != order.StatusNew || o.PendingReason != order.PendingDiscountApproval || o.ConfirmedAt != nil ||
		o.DiscountApprovedBy != nil || o.Version != 1 || o.GrandTotal != 899999 {
		t.Errorf("vượt ngưỡng: %+v", o)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status IS NULL
		AND to_status = 'new' AND to_pending = 'discount_approval' AND reason = 'manual_discount_over_threshold'`,
		o.ID); n != 1 {
		t.Errorf("lịch sử chờ duyệt: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_id = $1`, o.ID.String()); n != 1 {
		t.Errorf("%d sự kiện, mong 1 (order.created)", n)
	}
	if b := e.balance(v); b.Held != 2 {
		t.Errorf("đơn chờ duyệt vẫn giữ hàng: %+v", b)
	}

	m := e.createAs(e.dh16(v, order.IntentConfirmed, 100001), manager)
	if m.Status != order.StatusConfirmed || m.PendingReason != "" || m.DiscountApprovedBy == nil ||
		*m.DiscountApprovedBy != manager.UserID || m.DiscountApprovedAt == nil || !m.DiscountApprovedAt.Equal(e.now()) {
		t.Errorf("manager: %+v", m)
	}
	if mm := e.createAs(e.dh16(v, order.IntentConfirmed, 100000), manager); mm.DiscountApprovedBy != nil {
		t.Errorf("manager không vượt ngưỡng vẫn ghi người duyệt: %+v", mm)
	}

	fixed := couponTerms{
		code: "GIAM200K", kind: promotion.KindFixed, value: 200000, from: sale10("", 0).from, to: sale10("", 0).to,
	}
	e.coupon(fixed)
	in := e.dh16(v, order.IntentConfirmed, 0)
	in.CouponCode = "GIAM200K"
	if c := e.createAs(in, sale); c.Status != order.StatusConfirmed || c.CouponDiscount != 200000 || c.GrandTotal != 800000 {
		t.Errorf("mã 200.000: %+v", c)
	}

	if d := e.createAs(e.dh16(v, order.IntentDraft, 100001), sale); d.Status != order.StatusDraft ||
		d.PendingReason != order.PendingDiscountApproval || d.DraftExpiresAt == nil {
		t.Errorf("nháp vượt ngưỡng: %+v", d)
	}
	if n := e.createAs(e.dh16(v, order.IntentNew, 100001), sale); n.Status != order.StatusNew ||
		n.PendingReason != order.PendingDiscountApproval {
		t.Errorf("new vượt ngưỡng: %+v", n)
	}
}

// Ngưỡng đổi thành 2000 (Task M2-15) → sale giảm tay 150.000 → confirmed; 200.001 → chờ duyệt.
func TestCreate_DiscountApprovalThresholdSetting(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(1000000, 10)
	if err := e.app(func(tx pgx.Tx) error {
		s, err := order.LoadSettings(t.Context(), tx)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		bp := 2000
		_, err = order.Writer{}.UpdateSettings(t.Context(), tx, s.Version,
			order.SettingsPatch{DiscountApprovalThresholdBP: &bp}, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if o := e.createAs(e.dh16(v, order.IntentConfirmed, 150000), e.actor); o.Status != order.StatusConfirmed {
		t.Errorf("ngưỡng 20%%, giảm 15%%: %+v", o)
	}
	if o := e.createAs(e.dh16(v, order.IntentConfirmed, 200001), e.actor); o.Status != order.StatusNew ||
		o.PendingReason != order.PendingDiscountApproval {
		t.Errorf("ngưỡng 20%%, giảm 20,0001%%: %+v", o)
	}
}
