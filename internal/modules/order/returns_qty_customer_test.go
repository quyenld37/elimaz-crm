package order_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Khách trả hàng nhận theo số lượng và tiền hoàn (kế hoạch M3, Task M3-24). Đơn D1 delivered: vA ×1 net 245.902,
// vB ×2 net 354.098; phiếu tạo bằng CreateReturn của M2.

func (e *orderEnv) orderD1(t *testing.T) (o order.Order, va, vb uuid.UUID, ib order.Item) {
	t.Helper()
	va, vb = e.variant(245902, 5), e.variant(177049, 5)
	o = e.deliveredOrder(ln(va, 1, 0), ln(vb, 2, 0))
	ib = itemOf(t, o, vb)
	if itemOf(t, o, va).NetAmount != 245902 || ib.NetAmount != 354098 {
		t.Fatalf("đơn D1: %+v", o.Items)
	}
	return o, va, vb, ib
}

// receiveD1 tạo đơn D1, phiếu trả 2 vB rồi nhận vB theo s, d, m. Trả đơn (trước khi nhận), phiếu (trước khi nhận), dòng
// vB, tồn vB trước khi nhận, phiếu sau khi nhận.
func (e *orderEnv) receiveD1(t *testing.T, s, d, m int32) (o order.Order, r order.Return, ib order.Item,
	before inventory.Balance, got order.Return,
) {
	t.Helper()
	o, _, vb, ib := e.orderD1(t)
	r = e.mustReturn(o.ID, custReturn(rl(ib.ID, 2)))
	before = e.balance(vb)
	return o, r, ib, before, e.mustReceiveQty(r, qty(ib.ID, s, d, m))
}

// TD05/AT13: phiếu trả 2 vB, nhận S1 D1 M0 → vB on_hand +2, damaged +1; tiền hoàn dòng 354.098; đơn còn vA nên
// order.partially_returned (kind customer_return, refundAmount 354.098); đơn vẫn delivered.
func TestReceiveReturnQty_CustomerTD05(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, r, ib, b, got := e.receiveD1(t, 1, 1, 0)
	if got.Status != order.ReturnRestocked || got.RefundAmount != 354098 || got.Items[0].RefundAmount != 354098 {
		t.Fatalf("phiếu: %+v", got)
	}
	if n := e.balance(ib.VariantID); n.OnHand != b.OnHand+2 || n.Damaged != b.Damaged+1 || n.Available != b.Available+1 {
		t.Errorf("vB: %+v (trước %+v)", n, b)
	}
	if e.returnEvent(o.ID, order.TopicOrderReturned) != nil {
		t.Error("có order.returned khi đơn còn vA")
	}
	ev := e.returnEvent(o.ID, order.TopicOrderPartiallyReturned)
	if ev == nil || ev["kind"] != order.ReturnCustomer || ev["refundAmount"] != float64(354098) ||
		ev["missingQty"] != float64(0) || ev["returnId"] != r.ID.String() {
		t.Errorf("order.partially_returned: %v", ev)
	}
	if g := e.get(o.ID); g.Status != order.StatusDelivered || g.Version != o.Version+1 {
		t.Errorf("đơn: %s v%d (trước v%d)", g.Status, g.Version, o.Version)
	}
}

// Phiếu trả 2 vB, nhận S1 D0 M1 → vB on_hand +1, damaged giữ; tiền hoàn dòng 177.049 (một đơn vị, floor theo
// RefundFor); dòng mixed, missing_qty 1; sự kiện mang refundAmount 177.049, missingQty 1.
func TestReceiveReturnQty_CustomerPartialMissing(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, r, ib, b, got := e.receiveD1(t, 1, 0, 1)
	if got.Status != order.ReturnRestocked || got.RefundAmount != 177049 || got.Items[0].RefundAmount != 177049 {
		t.Fatalf("phiếu: %+v", got)
	}
	if n := e.balance(ib.VariantID); n.OnHand != b.OnHand+1 || n.Damaged != b.Damaged || n.Available != b.Available+1 {
		t.Errorf("vB: %+v (trước %+v)", n, b)
	}
	if c, s, d, m := e.lineQty(r.ID, ib.ID); c != order.ConditionMixed || s != 1 || d != 0 || m != 1 {
		t.Errorf("dòng vB: %s %d/%d/%d", c, s, d, m)
	}
	ev := e.returnEvent(o.ID, order.TopicOrderPartiallyReturned)
	if ev == nil || ev["refundAmount"] != float64(177049) || ev["missingQty"] != float64(1) {
		t.Errorf("order.partially_returned: %v", ev)
	}
}

// Dòng có món thiếu một phần không làm đơn vị trả sau nhận phần tiền của món thiếu: C ×3 net 300.003; phiếu 1 trả 2
// (hoàn lúc tạo 200.002), nhận S1 M1 → 100.001; phiếu 2 trả nốt 1 → 100.001 (không phải 200.002). Tổng 200.002 ≤ net.
func TestReceiveReturnQty_MissingShareNotPaidLater(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	c := e.variant(100001, 5)
	o := e.deliveredOrder(ln(c, 3, 0))
	ic := itemOf(t, o, c)
	r1 := e.mustReturn(o.ID, custReturn(rl(ic.ID, 2)))
	if r1.RefundAmount != 200002 {
		t.Fatalf("phiếu 1 lúc tạo: %d", r1.RefundAmount)
	}
	if got := e.mustReceiveQty(r1, qty(ic.ID, 1, 0, 1)); got.RefundAmount != 100001 {
		t.Fatalf("phiếu 1 sau nhận: %d", got.RefundAmount)
	}
	r2 := e.mustReturn(o.ID, custReturn(rl(ic.ID, 1)))
	if r2.RefundAmount != 100001 {
		t.Errorf("phiếu 2: %d, muốn 100001", r2.RefundAmount)
	}
	if _, err := e.createReturn(o.ID, custReturn(rl(ic.ID, 1))); !errors.Is(err, order.ErrReturnQtyExceeds) {
		t.Errorf("phiếu 3 khi đã trả đủ 3: %v", err)
	}
}

// Phiếu đổi (exchange) nhận theo số lượng cũng hoàn theo số đã nhận: đổi 2 vB, nhận S0 D1 M1 → on_hand +1, damaged
// +1, tiền hoàn 177.049; đơn thay thế không đổi.
func TestReceiveReturnQty_ExchangePartialMissing(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	o, _, vb, ib := e.orderD1(t)
	r, _ := e.exchangeReturn(o.ID, rl(ib.ID, 2))
	n := e.get(*r.ReplacementOrderID)
	b := e.balance(vb)
	got := e.mustReceiveQty(r, qty(ib.ID, 0, 1, 1))
	if got.RefundAmount != 177049 || got.Status != order.ReturnRestocked {
		t.Fatalf("phiếu đổi: %+v", got)
	}
	if x := e.balance(vb); x.OnHand != b.OnHand+1 || x.Damaged != b.Damaged+1 || x.Available != b.Available {
		t.Errorf("vB: %+v (trước %+v)", x, b)
	}
	if ev := e.returnEvent(o.ID, order.TopicOrderPartiallyReturned); ev == nil || ev["kind"] != order.ReturnExchange ||
		ev["refundAmount"] != float64(177049) {
		t.Errorf("sự kiện: %v", ev)
	}
	if g := e.get(n.ID); g.Status != n.Status || g.Version != n.Version {
		t.Errorf("đơn thay thế đổi: %s v%d → %s v%d", n.Status, n.Version, g.Status, g.Version)
	}
}

// TestReceiveReturnQty_CustomerInvariants (rapid, 200 lượt theo TestMain): đơn đã giao 1–3 dòng (số lượng 1–5), chuỗi
// ngẫu nhiên tạo, hủy phiếu (M2), nhận theo số lượng (M3) → sau mỗi bước, mỗi dòng: Σ số trả của phiếu chưa hủy ≤ số
// mua; Σ refund_amount của dòng phiếu đã nhận ≤ net_amount; on_hand tăng đúng Σ (tốt + hỏng), damaged tăng đúng Σ hỏng;
// trả hết không món thiếu → hoàn đúng net.
func TestReceiveReturnQty_CustomerInvariants(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	rapid.Check(t, func(rt *rapid.T) {
		o := e.drawDeliveredOrder(rt, 3, 5)
		base := make(map[uuid.UUID]inventory.Balance, len(o.Items))
		for _, it := range o.Items {
			base[it.VariantID] = e.balance(it.VariantID)
		}
		var rs []order.Return
		for range rapid.IntRange(1, 8).Draw(rt, "steps") {
			e.qtyPropStep(rt, o, &rs)
			if msg := e.checkQtyInvariants(o, base); msg != "" {
				rt.Fatal(msg)
			}
		}
	})
}

// qtyPropStep áp một thao tác ngẫu nhiên: tạo (số 1–5 mỗi dòng chọn), hủy, nhận theo số lượng (chia ngẫu nhiên tốt,
// hỏng, thiếu). Lỗi nghiệp vụ hợp lệ (vượt số, sai trạng thái) bị bỏ qua.
func (e *orderEnv) qtyPropStep(rt *rapid.T, o order.Order, rs *[]order.Return) {
	op := rapid.SampledFrom([]string{"create", "create", "receive", "receive", "cancel"}).Draw(rt, "op")
	var err error
	if op == "create" {
		err = e.propCreate(rt, o, rs, 5)
	} else {
		r, ok := e.propPick(rt, *rs)
		if !ok {
			return
		}
		if op == "cancel" {
			_, err = e.cancelReturn(r)
		} else {
			_, err = e.receiveQty(r, drawSplitLines(rt, r)...)
		}
	}
	propErr(rt, op, err)
}

// drawSplitLines chia ngẫu nhiên số của mỗi dòng phiếu r thành tốt, hỏng, thiếu.
func drawSplitLines(rt *rapid.T, r order.Return) []order.ReceiveQtyLine {
	lines := make([]order.ReceiveQtyLine, 0, len(r.Items))
	for _, it := range r.Items {
		q := int32(it.Quantity) //nolint:gosec // ≤ 5
		s := rapid.Int32Range(0, q).Draw(rt, "s")
		d := rapid.Int32Range(0, q-s).Draw(rt, "d")
		lines = append(lines, qty(it.OrderItemID, s, d, q-s-d))
	}
	return lines
}

// checkQtyInvariants: lineBreach theo ba số của dòng phiếu đã nhận, rồi on_hand tăng đúng Σ (tốt + hỏng), damaged tăng
// đúng Σ hỏng so với base.
func (e *orderEnv) checkQtyInvariants(o order.Order, base map[uuid.UUID]inventory.Balance) string {
	const recv = `FROM ord.return_items ri JOIN ord.returns r ON r.id = ri.return_id
		WHERE ri.order_item_id = $1 AND r.status IN ('restocked', 'closed')`
	for _, it := range o.Items {
		received := e.count(`SELECT coalesce(sum(ri.quantity), 0)::int `+recv, it.ID)
		back := e.count(`SELECT coalesce(sum(ri.sellable_qty + ri.damaged_qty), 0)::int `+recv, it.ID)
		hurt := e.count(`SELECT coalesce(sum(ri.damaged_qty), 0)::int `+recv, it.ID)
		missing := e.count(`SELECT coalesce(sum(ri.missing_qty), 0)::int `+recv, it.ID)
		refunded := int64(e.count(`SELECT coalesce(sum(ri.refund_amount), 0)::int `+recv, it.ID))
		if msg := lineBreach(it, e.returnedOf(it.ID), received, missing, refunded); msg != "" {
			return msg
		}
		b, now := base[it.VariantID], e.balance(it.VariantID)
		if int(now.OnHand-b.OnHand) != back || int(now.Damaged-b.Damaged) != hurt {
			return fmt.Sprintf("dòng %s: tồn %+v (trước %+v), nhận về %d, hỏng %d", it.ID, now, b, back, hurt)
		}
	}
	return ""
}
