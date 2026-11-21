package order_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
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
		o := e.drawDeliveredOrder(rt, 2, 3)
		var rs []order.Return
		for range rapid.IntRange(1, 8).Draw(rt, "steps") {
			e.returnPropStep(rt, o, &rs)
			if msg := e.checkReturnInvariants(o); msg != "" {
				rt.Fatal(msg)
			}
		}
	})
}

// drawDeliveredOrder tạo đơn đã giao 1–maxLines dòng (số lượng 1–maxQty, đơn giá lẻ) có giảm dòng và giảm cấp đơn
// (WAT31: phần chia về dòng), tổng giảm tay không quá ngưỡng duyệt 10% (mỗi phần ≤ 5% tạm tính).
func (e *orderEnv) drawDeliveredOrder(rt *rapid.T, maxLines int, maxQty int32) order.Order {
	var lines []order.LineInput
	var subtotal int64
	for range rapid.IntRange(1, maxLines).Draw(rt, "lines") {
		qty := rapid.Int32Range(1, maxQty).Draw(rt, "qty")
		price := rapid.Int64Range(1, 99999).Draw(rt, "price")
		disc := rapid.Int64Range(0, int64(qty)*price/20).Draw(rt, "discount")
		lines = append(lines, ln(e.variant(price, 10), qty, disc))
		subtotal += int64(qty) * price
	}
	in := e.m2Input(30000, lines...)
	in.OrderDiscount = rapid.Int64Range(0, subtotal/20).Draw(rt, "orderDiscount")
	return e.deliveredFrom(in)
}

// propCreate tạo một phiếu khách trả trên các dòng rút ngẫu nhiên của o (số 1–maxQty), thêm vào rs khi tạo được.
func (e *orderEnv) propCreate(rt *rapid.T, o order.Order, rs *[]order.Return, maxQty int) error {
	var in []order.ReturnLineInput
	for _, it := range o.Items {
		if rapid.Bool().Draw(rt, "pick") {
			in = append(in, rl(it.ID, rapid.IntRange(1, maxQty).Draw(rt, "rqty")))
		}
	}
	if len(in) == 0 {
		return nil
	}
	r, err := e.createReturn(o.ID, custReturn(in...))
	if err == nil {
		*rs = append(*rs, r)
	}
	return err
}

// propPick đọc lại một phiếu rút ngẫu nhiên trong rs; rs rỗng → false.
func (e *orderEnv) propPick(rt *rapid.T, rs []order.Return) (order.Return, bool) {
	if len(rs) == 0 {
		return order.Return{}, false
	}
	return e.getReturn(rs[rapid.IntRange(0, len(rs)-1).Draw(rt, "return")].ID), true
}

// propErr: lỗi nghiệp vụ hợp lệ (vượt số, sai trạng thái) bị bỏ qua; lỗi khác làm hỏng lượt.
func propErr(rt *rapid.T, op string, err error) {
	if err != nil && !errors.Is(err, order.ErrReturnQtyExceeds) && !errors.Is(err, order.ErrReturnNotAllowed) {
		rt.Fatalf("%s: lỗi lạ %v", op, err)
	}
}

// returnedOf là Σ số trả của các phiếu chưa hủy trên dòng đơn item.
func (e *orderEnv) returnedOf(item uuid.UUID) int {
	return e.count(`SELECT coalesce(sum(ri.quantity), 0)::int FROM ord.return_items ri
			JOIN ord.returns r ON r.id = ri.return_id WHERE ri.order_item_id = $1 AND r.status <> 'cancelled'`, item)
}

// returnPropStep áp một thao tác ngẫu nhiên; lỗi nghiệp vụ hợp lệ (vượt số, sai trạng thái) bị bỏ qua.
func (e *orderEnv) returnPropStep(rt *rapid.T, o order.Order, rs *[]order.Return) {
	op := rapid.SampledFrom([]string{"create", "create", "receive", "cancel", "close"}).Draw(rt, "op")
	var err error
	if op == "create" {
		err = e.propCreate(rt, o, rs, 3)
	} else {
		r, ok := e.propPick(rt, *rs)
		if !ok {
			return
		}
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
	propErr(rt, op, err)
}

func (e *orderEnv) checkReturnInvariants(o order.Order) string {
	for _, it := range o.Items {
		returned := e.returnedOf(it.ID)
		received := e.count(`SELECT coalesce(sum(ri.quantity), 0)::int FROM ord.return_items ri
			JOIN ord.returns r ON r.id = ri.return_id WHERE ri.order_item_id = $1 AND r.status IN ('restocked', 'closed')`,
			it.ID)
		missing := e.count(`SELECT count(*) FROM ord.return_items ri JOIN ord.returns r ON r.id = ri.return_id
			WHERE ri.order_item_id = $1 AND r.status <> 'cancelled' AND ri.condition = 'missing'
			AND r.status IN ('restocked', 'closed')`, it.ID)
		refunded := int64(e.count(`SELECT coalesce(sum(ri.refund_amount), 0)::int FROM ord.return_items ri
			JOIN ord.returns r ON r.id = ri.return_id WHERE ri.order_item_id = $1 AND r.status IN ('restocked', 'closed')
			AND ri.condition IN ('sellable', 'damaged')`, it.ID))
		if msg := lineBreach(it, returned, received, missing, refunded); msg != "" {
			return msg
		}
	}
	return ""
}

// lineBreach kiểm một dòng đơn it: Σ số trả của phiếu chưa hủy (returned) ≤ số mua; Σ tiền hoàn của phiếu đã nhận
// (refunded) ≤ net; mọi đơn vị đã trả qua phiếu đã nhận (received) và không món thiếu (missing = 0) → hoàn đúng net.
// Sai → câu mô tả, đúng → "".
func lineBreach(it order.Item, returned, received, missing int, refunded int64) string {
	switch {
	case returned > int(it.Qty):
		return fmt.Sprintf("dòng %s: trả %d > mua %d", it.ID, returned, it.Qty)
	case refunded > it.NetAmount:
		return fmt.Sprintf("dòng %s: hoàn %d > net %d", it.ID, refunded, it.NetAmount)
	case received == int(it.Qty) && missing == 0 && refunded != it.NetAmount:
		return fmt.Sprintf("dòng %s: trả hết, hoàn %d ≠ net %d", it.ID, refunded, it.NetAmount)
	}
	return ""
}
