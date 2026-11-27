package order_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// Vòng đời suất flash sale (kế hoạch M5, Task 202 / M5-19; Review Focus 1, 2): chuỗi ngẫu nhiên các thao tác mua, hoàn
// tất, hết hạn, hủy, thêm suất, đóng đợt; sau mỗi bước bốn bất biến luôn đúng:
//
//  1. Đơn vị held + sold gắn đơn còn hiệu lực = tổng số lượng dòng flash của các đơn đó; đơn vị gắn đơn đã hủy chỉ có
//     thể là sold của đợt đã đóng (05 mục 7.4).
//  2. Không đơn vị available nào trong đợt đã đóng.
//  3. held trên số dư = dòng giữ hàng held + đơn vị available của đợt đang mở, = tổng sổ held (đối chiếu đêm không
//     lệch mục nào: kiểm cuối mỗi chuỗi; sau mỗi bước kiểm riêng biến thể của món cho nhanh).
//  4. Không khách nào có claims vượt giới hạn; khi đợt còn mở, tổng claims = đơn vị held + sold.
func TestFlashLifecycle_Invariants(t *testing.T) {
	t.Parallel()
	f := newFlashWebEnv(t)
	var phoneSeq int
	rapid.Check(t, func(rt *rapid.T) {
		r := lifeRun{f: f, limit: rapid.IntRange(1, 3).Draw(rt, "limit")}
		r.s = f.schedule(rapid.IntRange(1, 6).Draw(rt, "slots"), r.limit, 5)
		r.buyers = make([]lifeBuyer, 3)
		for i := range r.buyers {
			phoneSeq++
			r.buyers[i].phone = fmt.Sprintf("097%07d", phoneSeq)
			r.buyers[i].id = f.phoneCustomer(r.buyers[i].phone)
		}
		r.clock = f.start.Add(time.Minute)
		steps := rapid.IntRange(1, 8).Draw(rt, "steps")
		for step := range steps {
			op := rapid.SampledFrom([]string{"buy", "buy", "complete", "expire", "cancel", "add", "close"}).Draw(rt, "op")
			if err := r.step(rt, op); err != nil {
				rt.Fatalf("bước %d %s: %v", step, op, err)
			}
			if msg := f.lifecycleInvariants(r.s, r.limit, r.closed); msg != "" {
				rt.Fatalf("sau bước %d %s: %s", step, op, msg)
			}
		}
		if ms := f.reconcileFlash(); len(ms) != 0 {
			rt.Fatalf("cuối chuỗi: đối chiếu lệch %+v", ms)
		}
	})
}

// lifeBuyer: một khách của chuỗi (mã khách, SĐT để hoàn tất đơn).
type lifeBuyer struct {
	id    uuid.UUID
	phone string
}

// lifeBought: một lượt mua thành công của chuỗi và SĐT người mua.
type lifeBought struct {
	res   order.FlashBuyResult
	phone string
}

// lifeRun: trạng thái một chuỗi của TestFlashLifecycle_Invariants.
type lifeRun struct {
	f      *flashEnv
	s      flashSale
	limit  int
	buyers []lifeBuyer
	clock  time.Time
	bought []lifeBought
	closed bool
}

// step chạy một thao tác op; lỗi được chờ đợi của thao tác (hết hạn, đơn đã hủy, đợt đã đóng, hết hàng) trả nil.
func (r *lifeRun) step(rt *rapid.T, op string) error {
	f := r.f
	switch op {
	case "buy":
		b := rapid.SampledFrom(r.buyers).Draw(rt, "buyer")
		in := f.in(r.s, b.id, rapid.IntRange(1, r.limit).Draw(rt, "qty"))
		in.Now = r.clock
		res, err := f.buy(in)
		if err == nil && res.Result == order.FlashBought {
			r.bought = append(r.bought, lifeBought{res, b.phone})
		}
		return err
	case "complete":
		if len(r.bought) == 0 {
			return nil
		}
		b := rapid.SampledFrom(r.bought).Draw(rt, "order")
		_, err := f.complete(f.completeIn(b.res, b.phone), r.clock)
		if errors.Is(err, order.ErrHoldExpired) || errors.Is(err, order.ErrInvalidTransition) {
			return nil // đã hết hạn, hay đơn đã bị hủy tay
		}
		return err
	case "expire":
		r.clock = r.clock.Add(6 * time.Minute)
		_, err := f.expireAt(r.clock)
		return err
	case "cancel":
		if len(r.bought) == 0 {
			return nil
		}
		id := rapid.SampledFrom(r.bought).Draw(rt, "order").res.OrderID
		if o := f.getOrder(id); o.Status != order.StatusCancelled {
			return f.cancelOrder(id)
		}
		return nil
	case "add":
		err := f.addSlots(r.s, rapid.IntRange(1, 3).Draw(rt, "add"))
		if (r.closed && errors.Is(err, promotion.ErrFlashNotActive)) || errors.Is(err, promotion.ErrFlashInsufficient) {
			return nil
		}
		return err
	case "close":
		if r.closed {
			return nil
		}
		r.closed = true
		return f.closeSale(r.s)
	}
	return nil
}

// addSlots thêm add suất cho món của s.
func (f *flashEnv) addSlots(s flashSale, add int) error {
	return f.app(func(tx pgx.Tx) error {
		pw := promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
		_, err := pw.AddSlots(f.t.Context(), tx, s.sale, s.item, add, promotion.Actor{TenantID: f.tenant, UserID: f.actor.UserID})
		return err //nolint:wrapcheck // test
	})
}

// lifecycleInvariants kiểm bốn bất biến của TestFlashLifecycle_Invariants cho món của s; trả mô tả vi phạm đầu tiên.
func (f *flashEnv) lifecycleInvariants(s flashSale, limit int, closed bool) string {
	live := f.count(`SELECT count(*) FROM inventory.flash_sale_units AS u JOIN ord.order_items AS i ON i.id = u.order_item_id
		JOIN ord.orders AS o ON o.id = i.order_id WHERE u.flash_sale_item_id = $1 AND o.status <> 'cancelled'
		AND u.status IN ('held', 'sold')`, s.item)
	lines := f.count(`SELECT coalesce(sum(i.quantity), 0) FROM ord.order_items AS i JOIN ord.orders AS o ON o.id = i.order_id
		WHERE i.flash_sale_item_id = $1 AND o.status <> 'cancelled'`, s.item)
	if live != lines {
		return fmt.Sprintf("(1) đơn vị của đơn còn hiệu lực %d, dòng flash %d", live, lines)
	}
	dead := f.count(`SELECT count(*) FROM inventory.flash_sale_units AS u JOIN ord.order_items AS i ON i.id = u.order_item_id
		JOIN ord.orders AS o ON o.id = i.order_id WHERE u.flash_sale_item_id = $1 AND o.status = 'cancelled'
		AND (u.status <> 'sold' OR NOT $2)`, s.item, closed)
	if dead != 0 {
		return fmt.Sprintf("(1) %d đơn vị còn gắn đơn đã hủy", dead)
	}
	if closed && f.units(s.item, "available") != 0 {
		return fmt.Sprintf("(2) %d đơn vị trống trong đợt đã đóng", f.units(s.item, "available"))
	}
	if msg := f.heldBalanced(s); msg != "" {
		return msg
	}
	if n := f.count(`SELECT count(*) FROM promotion.flash_sale_claims WHERE flash_sale_item_id = $1 AND quantity > $2`,
		s.item, limit); n != 0 {
		return fmt.Sprintf("(4) %d khách vượt giới hạn", n)
	}
	if taken := f.units(s.item, "held") + f.units(s.item, "sold"); !closed && f.claimed(s.item) != taken {
		return fmt.Sprintf("(4) claims %d, đơn vị held + sold %d", f.claimed(s.item), taken)
	}
	return ""
}

// heldBalanced: held của biến thể của s = giữ hàng held + đơn vị trống của đợt chưa đóng = tổng sổ ngăn held.
func (f *flashEnv) heldBalanced(s flashSale) string {
	held := int(f.balance(s.variant).Held)
	reserved := f.count(`SELECT coalesce(sum(quantity), 0) FROM inventory.stock_reservations
		WHERE variant_id = $1 AND state = 'held'`, s.variant)
	free := f.count(`SELECT count(*) FROM inventory.flash_sale_units AS u
		JOIN promotion.flash_sale_items AS i ON i.id = u.flash_sale_item_id
		JOIN promotion.flash_sales AS fs ON fs.id = i.flash_sale_id
		WHERE i.variant_id = $1 AND u.status = 'available' AND fs.status IN ('scheduled', 'open')`, s.variant)
	ledger := f.count(`SELECT coalesce(sum(delta), 0) FROM inventory.stock_ledger WHERE variant_id = $1
		AND bucket = 'held'`, s.variant)
	if held != reserved+free || held != ledger {
		return fmt.Sprintf("(3) held %d, giữ hàng %d + đơn vị trống %d, sổ %d", held, reserved, free, ledger)
	}
	return ""
}
