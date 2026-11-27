package order_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// errAbort là lỗi người mua tự trả sau lượt mua để lùi cả transaction (hủy giữa chừng).
var errAbort = errors.New("người mua hủy giữa chừng")

// TestFlashBuy_Invariants (rapid): 1–20 khách, giới hạn 1–3, 1–30 suất, qty 1–3, người mua đồng thời, một số lùi
// transaction ngay sau lượt mua (hủy giữa chừng) → (1) tổng đơn vị held+sold = tổng số lượng dòng flash của đơn
// draft/new; (2) không khách nào có claims vượt giới hạn; (3) tổng claims = tổng đơn vị held+sold; (4) tổng số đơn vị
// không đổi; (5) mọi dòng flash của đơn có đúng quantity đơn vị held/sold. Lượt không bị hủy mà sold_out thì không còn
// suất trống đủ cho nó lúc kết thúc (bất biến hết suất).
func TestFlashBuy_Invariants(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		slots := rapid.IntRange(1, 30).Draw(rt, "slots")
		limit := rapid.IntRange(1, 3).Draw(rt, "limit")
		s := f.schedule(slots, limit, 10)
		cs := f.customers(rapid.IntRange(1, 20).Draw(rt, "customers"))
		type buyer struct {
			in    order.FlashBuyInput
			abort bool
		}
		buyers := make([]buyer, rapid.IntRange(1, 25).Draw(rt, "buyers"))
		for i := range buyers {
			c := rapid.SampledFrom(cs).Draw(rt, "customer")
			buyers[i] = buyer{
				in:    f.in(s, c, rapid.IntRange(1, 3).Draw(rt, "qty")),
				abort: rapid.IntRange(0, 4).Draw(rt, "abort") == 0,
			}
		}
		res := make([]order.FlashBuyResult, len(buyers))
		errs := make([]error, len(buyers))
		var wg sync.WaitGroup
		for i, b := range buyers {
			wg.Go(func() { res[i], errs[i] = f.buyMaybeAbort(b.in, b.abort) })
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				rt.Fatalf("lượt %d: %v", i, err)
			}
		}
		if msg := f.checkFlash(s, slots, limit); msg != "" {
			rt.Fatal(msg)
		}
		free := f.units(s.item, "available")
		for i, b := range buyers {
			if !b.abort && res[i].Result == order.FlashSoldOut && free >= b.in.Qty {
				rt.Fatalf("lượt %d sold_out qty %d mà còn %d suất trống", i, b.in.Qty, free)
			}
		}
	})
}

// buyMaybeAbort chạy lượt mua như điểm cuối (abort false), hay một lần rồi lùi transaction ngay sau lượt mua (abort
// true: hủy giữa chừng; lỗi lùi, thử lại không tính là lỗi).
func (f *flashEnv) buyMaybeAbort(in order.FlashBuyInput, abort bool) (order.FlashBuyResult, error) {
	if !abort {
		return f.buy(in)
	}
	err := tenancy.WithTenant(f.t.Context(), f.staff.App, f.tenant, func(tx pgx.Tx) error {
		if _, err := f.writer().BuyFlash(f.t.Context(), tx, in, f.service); err != nil {
			return err //nolint:wrapcheck // test
		}
		return errAbort
	}, db.TxFlashBuy)
	if errors.Is(err, errAbort) || errors.Is(err, order.ErrFlashRetry) || errors.Is(err, db.ErrRetryLater) {
		return order.FlashBuyResult{}, nil
	}
	return order.FlashBuyResult{}, err //nolint:wrapcheck // test
}

// checkFlash kiểm năm bất biến của món s; trả mô tả vi phạm đầu tiên.
func (f *flashEnv) checkFlash(s flashSale, slots, limit int) string {
	taken := f.count(`SELECT count(*) FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1
		AND status IN ('held', 'sold')`, s.item)
	lines := f.count(`SELECT coalesce(sum(i.quantity), 0) FROM ord.order_items AS i JOIN ord.orders AS o ON o.id = i.order_id
		WHERE i.flash_sale_item_id = $1 AND o.status IN ('draft', 'new')`, s.item)
	if taken != lines {
		return fmt.Sprintf("(1) đơn vị held+sold %d, dòng flash %d", taken, lines)
	}
	if n := f.count(`SELECT count(*) FROM promotion.flash_sale_claims WHERE flash_sale_item_id = $1 AND quantity > $2`,
		s.item, limit); n != 0 {
		return fmt.Sprintf("(2) %d khách vượt giới hạn %d", n, limit)
	}
	if c := f.claimed(s.item); c != taken {
		return fmt.Sprintf("(3) tổng claims %d, đơn vị held+sold %d", c, taken)
	}
	if n := f.count(`SELECT count(*) FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1`, s.item); n != slots {
		return fmt.Sprintf("(4) %d đơn vị, muốn %d", n, slots)
	}
	if n := f.count(`SELECT count(*) FROM ord.order_items AS i WHERE i.flash_sale_item_id = $1 AND i.quantity <>
		(SELECT count(*) FROM inventory.flash_sale_units AS u WHERE u.order_item_id = i.id AND u.status IN ('held', 'sold'))`,
		s.item); n != 0 {
		return fmt.Sprintf("(5) %d dòng đơn thiếu đơn vị", n)
	}
	if n := f.count(`SELECT count(*) FROM inventory.stock_reservations AS r JOIN ord.order_items AS i ON i.id = r.order_item_id
		WHERE i.flash_sale_item_id = $1 AND r.state = 'held' AND r.quantity = i.quantity`, s.item); n !=
		f.count(`SELECT count(*) FROM ord.order_items WHERE flash_sale_item_id = $1`, s.item) {
		return "dòng giữ hàng không khớp dòng đơn flash"
	}
	return ""
}
