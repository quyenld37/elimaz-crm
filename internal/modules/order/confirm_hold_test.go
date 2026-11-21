package order_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
)

// holdState là phần tồn và lượt mã một lần xác nhận không được đổi (AT05, WAT07).
type holdState struct {
	balance                  inventory.Balance
	reserved, reservations   int // Σ số giữ, số dòng giữ của biến thể
	ledger                   int // số dòng sổ kho của biến thể
	used, redemptions, total int // used_count, số lượt active của mã, số lượt mọi trạng thái
}

func (e *orderEnv) holdOf(v, cid uuid.UUID) holdState {
	e.t.Helper()
	return holdState{
		balance: e.balance(v),
		reserved: e.count(`SELECT coalesce(sum(quantity), 0)::int FROM inventory.stock_reservations
			WHERE variant_id = $1 AND state = 'held'`, v),
		reservations: e.count(`SELECT count(*) FROM inventory.stock_reservations WHERE variant_id = $1`, v),
		ledger:       e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE variant_id = $1`, v),
		used:         e.usedCount(cid),
		redemptions: e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1
			AND status = 'active'`, cid),
		total: e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1`, cid),
	}
}

// heldOrder tạo đơn ý định intent (địa chỉ đủ) trên biến thể mới 1.000.000 đồng tồn 5, ×1, giảm cấp đơn discount,
// dùng một mã 10% (trần 50.000) chỉ còn đúng một lượt. Trả đơn, biến thể, mã.
func (e *orderEnv) heldOrder(intent string, discount int64) (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	code := "AT05X" + uuid.NewString()[:6]
	cid := e.coupon(sale10(code, 1))
	v := e.variant(1000000, 5)
	in := e.m2Input(0, ln(v, 1, 0))
	in.SellerID, in.Intent, in.ShipAddress, in.OrderDiscount, in.CouponCode = e.seller, intent, fullAddress(), discount, code
	o := e.mustCreateM2(in).Order
	if o.CouponDiscount != 50000 {
		e.t.Fatalf("đơn %s: giảm mã %d, mong 50.000", intent, o.CouponDiscount)
	}
	return o, v, cid
}

// AT05 (nửa sau), WAT07 (phần CRM, nửa sau): giữ hàng và lượt mã ghi một lần lúc tạo đơn; xác nhận ở mọi đường (new →
// confirm, nháp → chốt, chờ duyệt → quản lý duyệt, chờ duyệt → quản lý xác nhận, nháp chờ duyệt → duyệt → chốt) không
// giữ thêm (số dư, dòng giữ, sổ kho không đổi) và không dùng mã lần hai (mã max_uses 1: used_count vẫn 1, đúng một lượt).
func TestConfirm_NoSecondHoldOrRedemption(t *testing.T) {
	e := newM2Env(t)
	mgr := e.manager()
	cases := []struct {
		name     string
		intent   string
		discount int64
		steps    []func(o order.Order) (order.Order, error)
	}{
		{"new → confirm", order.IntentNew, 0, []func(order.Order) (order.Order, error){
			func(o order.Order) (order.Order, error) { return e.confirm(o.ID, o.Version, e.actor, e.now()) },
		}},
		{"nháp → chốt", order.IntentDraft, 0, []func(order.Order) (order.Order, error){
			func(o order.Order) (order.Order, error) { return e.confirm(o.ID, o.Version, e.actor, e.now()) },
		}},
		{"chờ duyệt → quản lý duyệt", order.IntentConfirmed, 100001, []func(order.Order) (order.Order, error){
			func(o order.Order) (order.Order, error) { return e.approve(o.ID, o.Version, mgr, e.now()) },
		}},
		{"chờ duyệt → quản lý xác nhận", order.IntentNew, 100001, []func(order.Order) (order.Order, error){
			func(o order.Order) (order.Order, error) { return e.confirm(o.ID, o.Version, mgr, e.now()) },
		}},
		{"nháp chờ duyệt → duyệt → chốt", order.IntentDraft, 100001, []func(order.Order) (order.Order, error){
			func(o order.Order) (order.Order, error) { return e.approve(o.ID, o.Version, mgr, e.now()) },
			func(o order.Order) (order.Order, error) {
				return e.confirm(o.ID, o.Version, e.actor, e.now().Add(time.Minute))
			},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { e.checkConfirmKeepsHold(t, c.intent, c.discount, c.steps) })
	}
}

// checkConfirmKeepsHold tạo đơn heldOrder(intent, discount), chạy lần lượt steps (đơn cuối phải confirmed) rồi so phần
// giữ hàng và lượt mã với lúc vừa tạo đơn.
func (e *orderEnv) checkConfirmKeepsHold(t *testing.T, intent string, discount int64,
	steps []func(o order.Order) (order.Order, error),
) {
	t.Helper()
	o, v, cid := e.heldOrder(intent, discount)
	if o.Status == order.StatusConfirmed {
		t.Fatalf("đơn đã confirmed ngay khi tạo: %+v", o)
	}
	before := e.holdOf(v, cid)
	if before.balance.Held != 1 || before.balance.Available != 4 || before.reserved != 1 ||
		before.reservations != 1 || before.used != 1 || before.redemptions != 1 || before.total != 1 {
		t.Fatalf("sau tạo đơn: %+v", before)
	}
	for i, step := range steps {
		var err error
		if o, err = step(o); err != nil {
			t.Fatalf("bước %d: %v", i+1, err)
		}
	}
	if o.Status != order.StatusConfirmed || o.CouponDiscount != 50000 {
		t.Fatalf("sau xác nhận: status %s, giảm mã %d", o.Status, o.CouponDiscount)
	}
	if after := e.holdOf(v, cid); after != before {
		t.Errorf("xác nhận đổi giữ hàng hay lượt mã:\ntrước %+v\nsau   %+v", before, after)
	}
}
