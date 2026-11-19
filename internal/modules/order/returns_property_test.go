package order_test

import (
	"errors"
	"fmt"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order"
)

// TestReturns_Invariants (rapid): đơn đã giao 1–2 dòng (số lượng 1–3, đơn giá lẻ, giảm dòng dưới ngưỡng duyệt), chuỗi ngẫu nhiên tạo
// phiếu (dòng, số ngẫu nhiên), nhận (tình trạng ngẫu nhiên), hủy, đóng → sau mỗi bước, với mỗi dòng: Σ số trả của phiếu
// chưa hủy ≤ số mua; Σ refund_amount của dòng phiếu đã nhận sellable/damaged ≤ net_amount; khi mọi đơn vị đã trả qua
// phiếu đã nhận và không dòng nào missing thì bằng đúng net_amount.
func TestReturns_Invariants(t *testing.T) {
	e := newM2Env(t)
	rapid.Check(t, func(rt *rapid.T) {
		var lines []order.LineInput
		for range rapid.IntRange(1, 2).Draw(rt, "lines") {
			qty := rapid.Int32Range(1, 3).Draw(rt, "qty")
			price := rapid.Int64Range(1, 99999).Draw(rt, "price")
			disc := rapid.Int64Range(0, int64(qty)*price/20).Draw(rt, "discount") // dưới ngưỡng duyệt 10%
			lines = append(lines, ln(e.variant(price, 10), qty, disc))
		}
		o := e.deliveredOrder(lines...)
		var rs []order.Return
		for range rapid.IntRange(1, 8).Draw(rt, "steps") {
			e.returnPropStep(rt, o, &rs)
			if msg := e.checkReturnInvariants(o); msg != "" {
				rt.Fatal(msg)
			}
		}
	})
}

// returnPropStep áp một thao tác ngẫu nhiên; lỗi nghiệp vụ hợp lệ (vượt số, sai trạng thái) bị bỏ qua.
func (e *orderEnv) returnPropStep(rt *rapid.T, o order.Order, rs *[]order.Return) {
	op := rapid.SampledFrom([]string{"create", "create", "receive", "cancel", "close"}).Draw(rt, "op")
	var err error
	if op == "create" {
		var in []order.ReturnLineInput
		for _, it := range o.Items {
			if rapid.Bool().Draw(rt, "pick") {
				in = append(in, rl(it.ID, rapid.IntRange(1, 3).Draw(rt, "rqty")))
			}
		}
		if len(in) == 0 {
			return
		}
		var r order.Return
		if r, err = e.createReturn(o.ID, custReturn(in...)); err == nil {
			*rs = append(*rs, r)
		}
	} else {
		if len(*rs) == 0 {
			return
		}
		i := rapid.IntRange(0, len(*rs)-1).Draw(rt, "return")
		r := e.getReturn((*rs)[i].ID)
		switch op {
		case "receive":
			var lines []order.ReceiveLine
			for _, it := range r.Items {
				cond := rapid.SampledFrom([]string{order.ConditionSellable, order.ConditionDamaged, order.ConditionMissing}).
					Draw(rt, "cond")
				lines = append(lines, order.ReceiveLine{OrderItemID: it.OrderItemID, Condition: cond})
			}
			_, err = e.receive(r, lines...)
		case "cancel":
			_, err = e.cancelReturn(r)
		default:
			_, err = e.closeReturn(r)
		}
	}
	if err != nil && !errors.Is(err, order.ErrReturnQtyExceeds) && !errors.Is(err, order.ErrReturnNotAllowed) {
		rt.Fatalf("%s: lỗi lạ %v", op, err)
	}
}

func (e *orderEnv) checkReturnInvariants(o order.Order) string {
	for _, it := range o.Items {
		returned := e.count(`SELECT coalesce(sum(ri.quantity), 0)::int FROM ord.return_items ri
			JOIN ord.returns r ON r.id = ri.return_id WHERE ri.order_item_id = $1 AND r.status <> 'cancelled'`, it.ID)
		received := e.count(`SELECT coalesce(sum(ri.quantity), 0)::int FROM ord.return_items ri
			JOIN ord.returns r ON r.id = ri.return_id WHERE ri.order_item_id = $1 AND r.status IN ('restocked', 'closed')`,
			it.ID)
		missing := e.count(`SELECT count(*) FROM ord.return_items ri JOIN ord.returns r ON r.id = ri.return_id
			WHERE ri.order_item_id = $1 AND r.status <> 'cancelled' AND ri.condition = 'missing'
			AND r.status IN ('restocked', 'closed')`, it.ID)
		refunded := int64(e.count(`SELECT coalesce(sum(ri.refund_amount), 0)::int FROM ord.return_items ri
			JOIN ord.returns r ON r.id = ri.return_id WHERE ri.order_item_id = $1 AND r.status IN ('restocked', 'closed')
			AND ri.condition IN ('sellable', 'damaged')`, it.ID))
		switch {
		case returned > int(it.Qty):
			return fmt.Sprintf("dòng %s: trả %d > mua %d", it.ID, returned, it.Qty)
		case refunded > it.NetAmount:
			return fmt.Sprintf("dòng %s: hoàn %d > net %d", it.ID, refunded, it.NetAmount)
		case received == int(it.Qty) && missing == 0 && refunded != it.NetAmount:
			return fmt.Sprintf("dòng %s: trả hết, hoàn %d ≠ net %d", it.ID, refunded, it.NetAmount)
		}
	}
	return ""
}
