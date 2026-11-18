package order_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order"
)

// TestCreateManual_NoOversell (rapid, 200 lượt): 1–3 biến thể có tồn ngẫu nhiên 0–5, 2–8 đơn song song với số lượng
// ngẫu nhiên → (1) tổng số lượng các đơn tạo được của từng biến thể không vượt tồn ban đầu và bằng held; (2) mỗi đơn
// hoặc có đủ dòng giữ hàng (một dòng held cho mỗi dòng đơn, đúng số lượng) hoặc không tồn tại; (3) đơn bị từ chối chỉ
// vì thiếu hàng.
func TestCreateManual_NoOversell(t *testing.T) {
	e := newOrderEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		vs := make([]uuid.UUID, rapid.IntRange(1, 3).Draw(rt, "variants"))
		stock := make(map[uuid.UUID]int32, len(vs))
		for i := range vs {
			n := rapid.Int32Range(0, 5).Draw(rt, "stock")
			vs[i] = e.variant(1000, n)
			stock[vs[i]] = n
		}
		inputs := make([]order.CreateManualInput, rapid.IntRange(2, 8).Draw(rt, "orders"))
		for i := range inputs {
			lines := rapid.SliceOfN(rapid.Custom(func(rt *rapid.T) order.ItemInput {
				return item(vs[rapid.IntRange(0, len(vs)-1).Draw(rt, "variant")], rapid.Int32Range(1, 3).Draw(rt, "qty"))
			}), 1, 3).Draw(rt, "lines")
			inputs[i] = e.input(0, lines...)
		}
		created := make([]order.Order, len(inputs))
		errs := make([]error, len(inputs))
		var wg sync.WaitGroup
		for i, in := range inputs {
			wg.Go(func() { created[i], errs[i] = e.create(in) })
		}
		wg.Wait()

		if msg := e.checkNoOversell(vs, stock, inputs, created, errs); msg != "" {
			rt.Fatal(msg)
		}
		if n := e.count(`SELECT count(*) FROM ord.orders o WHERE NOT EXISTS (SELECT 1 FROM ord.order_items oi
			WHERE oi.order_id = o.id)`); n != 0 {
			rt.Fatalf("%d đơn không có dòng", n)
		}
	})
}

// checkNoOversell kiểm quy luật (1)–(3) của TestCreateManual_NoOversell; trả mô tả vi phạm đầu tiên (rỗng nếu đúng).
func (e *orderEnv) checkNoOversell(vs []uuid.UUID, stock map[uuid.UUID]int32, inputs []order.CreateManualInput,
	created []order.Order, errs []error,
) string {
	used := map[uuid.UUID]int32{}
	for i, err := range errs {
		switch {
		case err == nil:
			for _, it := range inputs[i].Items {
				used[it.VariantID] += it.Qty
			}
			if n := e.count(`SELECT count(*) FROM ord.order_items oi JOIN inventory.stock_reservations r
				ON r.order_item_id = oi.id AND r.quantity = oi.quantity AND r.state = 'held'
				WHERE oi.order_id = $1`, created[i].ID); n != len(inputs[i].Items) {
				return fmt.Sprintf("đơn %s: %d/%d dòng có giữ hàng", created[i].Code, n, len(inputs[i].Items))
			}
		case !errors.Is(err, order.ErrOutOfStock):
			return fmt.Sprintf("lỗi không mong đợi: %v", err)
		}
	}
	for _, v := range vs {
		b := e.balance(v)
		if used[v] > stock[v] || b.Held != used[v] || b.Available < 0 {
			return fmt.Sprintf("biến thể %s: tồn %d, đơn giữ %d, held %d, có thể bán %d", v, stock[v], used[v], b.Held, b.Available)
		}
	}
	return ""
}
