package order_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
)

// editOp là một bước sửa ngẫu nhiên: kind 0 thêm dòng, 1 bỏ dòng, 2 đổi số lượng, 3 đổi giảm dòng, 4 đổi giảm cấp
// đơn, 5 đổi mã (rỗng, mã A, mã B).
type editOp struct {
	order, kind, pick int
	qty               int32
	amount            int64
}

// TestEdit_Invariants (rapid, kế hoạch M2-20): 2–3 biến thể có tồn ngẫu nhiên, 1–3 đơn, chuỗi 1–8 lần sửa ngẫu nhiên
// (thêm, bỏ, đổi số lượng, đổi giảm giá, đổi mã) → sau mỗi bước: (1) lần sửa bị từ chối chỉ vì hết hàng, mã bị từ
// chối, giảm giá vượt tiền, và khi đó đơn không đổi; lần được thì version + 1; (2) tổng tiền đơn khớp dòng (trigger
// đã qua lúc commit; kiểm lại cả net và grand_total); (3) tổng giữ held của mỗi biến thể = tổng số lượng của các đơn
// còn mở = số dư held; (4) used_count của mỗi mã = số lượt active.
func TestEdit_Invariants(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	m := e.manager()
	iter := 0
	rapid.Check(t, func(rt *rapid.T) {
		iter++
		vs := make([]uuid.UUID, rapid.IntRange(2, 3).Draw(rt, "variants"))
		for i := range vs {
			vs[i] = e.variant(rapid.Int64Range(1, 25).Draw(rt, "price")*10000, rapid.Int32Range(2, 8).Draw(rt, "stock"))
		}
		codes := []string{fmt.Sprintf("PA%d", iter), fmt.Sprintf("PB%d", iter)}
		coupons := []uuid.UUID{e.editCoupon(codes[0], 0), e.editCoupon(codes[1], 300000)}
		var orders []order.Order
		for range rapid.IntRange(1, 3).Draw(rt, "orders") {
			v := vs[rapid.IntRange(0, len(vs)-1).Draw(rt, "first")]
			if res, err := e.createM2(e.m2Input(20000, ln(v, 1, 0))); err == nil {
				orders = append(orders, res.Order)
			} else if !errors.Is(err, order.ErrOutOfStock) {
				rt.Fatalf("tạo đơn: %v", err)
			}
		}
		if len(orders) == 0 {
			return
		}
		ops := rapid.SliceOfN(rapid.Custom(func(rt *rapid.T) editOp {
			return editOp{
				order: rapid.IntRange(0, len(orders)-1).Draw(rt, "order"), kind: rapid.IntRange(0, 5).Draw(rt, "kind"),
				pick: rapid.IntRange(0, 5).Draw(rt, "pick"), qty: rapid.Int32Range(1, 4).Draw(rt, "qty"),
				amount: rapid.SampledFrom([]int64{0, 10000, 50000, 300000}).Draw(rt, "amount"),
			}
		}), 1, 8).Draw(rt, "ops")
		for i, op := range ops {
			if msg := e.editStep(orders[op.order].ID, op, vs, codes, m); msg != "" {
				rt.Fatalf("bước %d (%+v): %s", i, op, msg)
			}
			if msg := e.editInvariants(orders, vs, coupons); msg != "" {
				rt.Fatalf("bước %d (%+v): %s", i, op, msg)
			}
		}
	})
}

// editStep chạy một bước sửa op trên đơn id bằng người sửa m; trả mô tả chỗ sai (rỗng khi đúng): được thì version + 1
// (hoặc giữ nguyên khi không trường nào đổi); bị từ chối chỉ vì hết hàng, mã bị từ chối, giảm giá vượt tiền, và khi
// đó đơn không đổi.
func (e *orderEnv) editStep(id uuid.UUID, op editOp, vs []uuid.UUID, codes []string, m order.Actor) string {
	cur := e.get(id)
	res, err := e.editOrder(id, editFromOp(cur, op, vs, codes), m)
	switch {
	case err == nil:
		if res.Order.Version != cur.Version+1 && (res.Order.Version != cur.Version || len(res.Fields) != 0) {
			return fmt.Sprintf("version %d → %d (%v)", cur.Version, res.Order.Version, res.Fields)
		}
	case errors.Is(err, order.ErrOutOfStock), errors.Is(err, promotion.ErrCouponRejected),
		errors.Is(err, order.ErrLineDiscountExceeds), errors.Is(err, order.ErrOrderDiscountExceeds):
		if after := e.get(id); !sameOrder(after, cur) {
			return fmt.Sprintf("bị từ chối (%v) mà đơn đổi", err)
		}
	default:
		return err.Error()
	}
	return ""
}

// editFromOp dựng đầu vào sửa đơn cur theo op: dòng giữ nguyên mang itemId, dòng mới là biến thể chưa có trong đơn.
func editFromOp(cur order.Order, op editOp, vs []uuid.UUID, codes []string) order.EditInput {
	in := order.EditInput{Version: cur.Version}
	lines := make([]order.EditLine, 0, len(cur.Items)+1)
	for _, it := range cur.Items {
		lines = append(lines, editKeep(it, it.Qty, it.LineDiscount))
	}
	k := op.pick % len(lines)
	switch op.kind {
	case 0:
		for _, v := range vs {
			if !slices.ContainsFunc(cur.Items, func(it order.Item) bool { return it.VariantID == v }) {
				lines = append(lines, editAdd(v, op.qty, 0))
				break
			}
		}
	case 1:
		if len(lines) > 1 {
			lines = slices.Delete(lines, k, k+1)
		}
	case 2:
		lines[k].Quantity = op.qty
	case 3:
		lines[k].LineDiscount = op.amount
	case 4:
		in.OrderDiscount = &op.amount
	default:
		code := ""
		if op.pick%3 > 0 {
			code = codes[op.pick%3-1]
		}
		in.CouponCode = &code
	}
	if op.kind <= 3 {
		in.Lines = &lines
	}
	return in
}

// editInvariants trả mô tả bất biến sai đầu tiên, rỗng khi mọi bất biến đúng.
func (e *orderEnv) editInvariants(orders []order.Order, vs, coupons []uuid.UUID) string {
	e.t.Helper()
	ids := make([]uuid.UUID, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
	}
	if n := e.count(`SELECT count(*) FROM ord.orders o, LATERAL (SELECT coalesce(sum(net_amount), 0) AS net,
			coalesce(sum(quantity::bigint * unit_price), 0) AS sub FROM ord.order_items WHERE order_id = o.id) i
		WHERE o.id = ANY($1) AND (i.net <> o.grand_total - o.shipping_fee_charged OR i.sub <> o.subtotal
			OR o.grand_total <> o.subtotal - o.discount_total + o.shipping_fee_charged
			OR o.discount_total <> o.line_discount_total + o.order_discount + o.coupon_discount
			OR o.cod_amount <> o.grand_total)`, ids); n != 0 {
		return fmt.Sprintf("%d đơn lệch tổng tiền", n)
	}
	for _, v := range vs {
		qty := e.count(`SELECT coalesce(sum(i.quantity), 0)::int FROM ord.order_items i JOIN ord.orders o ON o.id = i.order_id
			WHERE i.variant_id = $1 AND o.status IN ('draft', 'new', 'confirmed')`, v)
		res := e.count(`SELECT coalesce(sum(quantity), 0)::int FROM inventory.stock_reservations
			WHERE variant_id = $1 AND state = 'held'`, v)
		if held := int(e.balance(v).Held); qty != res || res != held {
			return fmt.Sprintf("biến thể %s: số lượng trên đơn %d, giữ held %d, số dư held %d", v, qty, res, held)
		}
	}
	for _, c := range coupons {
		active := e.count(`SELECT count(*) FROM promotion.coupon_redemptions WHERE coupon_id = $1 AND status = 'active'`, c)
		if used := e.usedCount(c); used != active {
			return fmt.Sprintf("mã %s: used_count %d, lượt active %d", c, used, active)
		}
	}
	return ""
}
