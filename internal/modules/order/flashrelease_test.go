package order_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// Nhả suất theo chuyển trạng thái của đơn (kế hoạch M5, Task 202 / M5-19; 05 mục 7.4; ca FS06, FS08 khi còn giữ, FS17).

// expireAt chạy ExpireDrafts(now, 500) như lịch order.expire_drafts; trả số đơn đã hủy.
func (f *flashEnv) expireAt(now time.Time) (int, error) {
	var n int
	err := f.app(func(tx pgx.Tx) error {
		var err error
		n, err = f.writer().ExpireDrafts(f.t.Context(), tx, now, 500)
		return err //nolint:wrapcheck // test
	})
	return n, err
}

// closeSale đóng tay đợt của s.
func (f *flashEnv) closeSale(s flashSale) error {
	return f.app(func(tx pgx.Tx) error {
		pw := promotion.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
		_, err := pw.CloseFlashSale(f.t.Context(), tx, s.sale, promotion.FlashClosedManual,
			promotion.Actor{TenantID: f.tenant, UserID: f.actor.UserID})
		return err //nolint:wrapcheck // test
	})
}

func (f *flashEnv) mustClose(s flashSale) {
	f.t.Helper()
	if err := f.closeSale(s); err != nil {
		f.t.Fatalf("đóng đợt: %v", err)
	}
}

// getOrder đọc đơn id của người bán của món flash.
func (f *flashEnv) getOrder(id uuid.UUID) order.Order {
	f.t.Helper()
	var o order.Order
	f.mustApp(func(tx pgx.Tx) error {
		var err error
		o, err = order.Get(f.t.Context(), tx, f.seller, id)
		return err //nolint:wrapcheck // test
	})
	return o
}

// cancelOrder hủy tay đơn id (lý do khách đổi ý) bằng nhân viên.
func (f *flashEnv) cancelOrder(id uuid.UUID) error {
	o := f.getOrder(id)
	return f.app(func(tx pgx.Tx) error {
		_, err := f.writer().Cancel(f.t.Context(), tx, f.seller, id, o.Version, order.CancelCustomerChangedMind, "", f.actor,
			f.start.Add(2*time.Minute))
		return err //nolint:wrapcheck // test
	})
}

// phoneCustomer tạo (hay tìm) khách có danh tính SĐT phone như điểm cuối mua của website.
func (f *flashEnv) phoneCustomer(phone string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	f.mustApp(func(tx pgx.Tx) error {
		cw := customer.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
		c, err := cw.FindOrCreateForOrder(f.t.Context(), tx, customer.OrderCustomerInput{
			FullName: "Khách website", Phone: phone, PhoneIsCustomers: true, Source: customer.SourceWeb,
		}, customer.Actor{Type: customer.ActorService, ID: f.service.UserID}, f.start)
		id = c.CustomerID
		return err //nolint:wrapcheck // test
	})
	return id
}

// reconcileFlash chạy đối chiếu đêm và trả mọi mục lệch.
func (f *flashEnv) reconcileFlash() []inventory.Mismatch {
	f.t.Helper()
	var run inventory.Run
	f.mustApp(func(tx pgx.Tx) error {
		var err error
		run, err = inventory.Reconcile(f.t.Context(), tx, 0)
		return err //nolint:wrapcheck // test
	})
	return run.Mismatches
}

func (f *flashEnv) mustReconcile() {
	f.t.Helper()
	if ms := f.reconcileFlash(); len(ms) != 0 {
		f.t.Fatalf("đối chiếu lệch: %+v", ms)
	}
}

// reservationState là trạng thái dòng giữ hàng của dòng đơn flash của đơn id.
func (f *flashEnv) reservationState(id uuid.UUID) string {
	f.t.Helper()
	var st string
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(f.t.Context(), `SELECT r.state FROM inventory.stock_reservations AS r
			JOIN ord.order_items AS i ON i.id = r.order_item_id WHERE i.order_id = $1`, id).Scan(&st)
	})
	return st
}

// ownerExec chạy các câu (mỗi câu cùng args) bằng chủ bảng trong một transaction ngữ cảnh tenant (sửa tay dữ liệu cho
// ca hỏng; trigger tổng tiền kiểm lúc commit).
func (f *flashEnv) ownerExec(args []any, qs ...string) {
	f.t.Helper()
	f.staff.Exec(f.t, f.tenant, func(tx pgx.Tx) error {
		for _, q := range qs {
			if _, err := tx.Exec(f.t.Context(), q, args...); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
}

// [FS06] Mua rồi tua đồng hồ quá hạn giữ, chạy order.expire_drafts → đơn cancelled lý do draft_expired; đơn vị về
// available (bỏ dòng đơn); claims về 0; dòng giữ hàng released; held của số dư không đổi; đối chiếu không lệch.
func TestFlashRelease_ExpireWhileOpen(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(3, 1, 10)
	heldBefore := f.balance(s.variant).Held
	c := f.customers(1)[0]
	res := f.mustBuy(f.in(s, c, 1))
	if n, err := f.expireAt(f.start.Add(5 * time.Minute)); err != nil || n != 0 {
		t.Fatalf("trong hạn: hủy %d, %v", n, err)
	}
	if n, err := f.expireAt(f.start.Add(12 * time.Minute)); err != nil || n != 1 {
		t.Fatalf("quá hạn: hủy %d, %v; mong 1", n, err)
	}
	o := f.getOrder(res.OrderID)
	if o.Status != order.StatusCancelled || o.CancelReason == nil || *o.CancelReason != order.CancelDraftExpired {
		t.Fatalf("đơn %s lý do %v", o.Status, o.CancelReason)
	}
	if f.units(s.item, "available") != 3 || f.units(s.item, "held") != 0 {
		t.Errorf("đơn vị trống %d, giữ %d; mong 3, 0", f.units(s.item, "available"), f.units(s.item, "held"))
	}
	if n := f.count(`SELECT count(*) FROM inventory.flash_sale_units WHERE flash_sale_item_id = $1
		AND order_item_id IS NOT NULL`, s.item); n != 0 {
		t.Errorf("%d đơn vị còn gắn dòng đơn", n)
	}
	if f.claimed(s.item) != 0 {
		t.Errorf("claims = %d", f.claimed(s.item))
	}
	if st := f.reservationState(res.OrderID); st != inventory.StateReleased {
		t.Errorf("giữ hàng %s", st)
	}
	if b := f.balance(s.variant); b.Held != heldBefore {
		t.Errorf("held %d, mong không đổi %d", b.Held, heldBefore)
	}
	f.mustReconcile()
	// Suất đã trả mua lại được (khách cũ: giới hạn 1 đã trả).
	if r := f.mustBuy(f.in(s, c, 1)); r.Result != order.FlashBought {
		t.Errorf("mua lại: %s", r.Result)
	}
}

// [FS17] Đợt đóng trước khi đơn nháp hết hạn → đơn vị released, held −1, có thể bán thường +1; đối chiếu không lệch.
func TestFlashRelease_ExpireAfterClose(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(3, 1, 10)
	res := f.mustBuy(f.in(s, f.customers(1)[0], 1))
	f.mustClose(s) // 2 đơn vị trống → released, held −2
	before := f.balance(s.variant)
	if n, err := f.expireAt(f.start.Add(12 * time.Minute)); err != nil || n != 1 {
		t.Fatalf("hủy %d, %v", n, err)
	}
	if o := f.getOrder(res.OrderID); o.Status != order.StatusCancelled {
		t.Fatalf("đơn %s", o.Status)
	}
	if f.units(s.item, "released") != 3 || f.units(s.item, "available") != 0 || f.units(s.item, "held") != 0 {
		t.Errorf("đơn vị: released %d, available %d, held %d", f.units(s.item, "released"),
			f.units(s.item, "available"), f.units(s.item, "held"))
	}
	after := f.balance(s.variant)
	if after.Held != before.Held-1 || after.Available != before.Available+1 {
		t.Errorf("held %d → %d, có thể bán %d → %d; mong −1, +1", before.Held, after.Held, before.Available, after.Available)
	}
	if f.claimed(s.item) != 1 {
		t.Errorf("claims = %d; đợt đã đóng không trừ lại", f.claimed(s.item))
	}
	f.mustReconcile()
}

// 05 mục 7.4 "Đơn đã sold bị hủy khi đợt còn mở": hủy tay đơn đã hoàn tất → đơn vị available, claims −1, held không
// đổi. Đơn nháp (held) hủy tay khi đợt mở: như hết hạn.
func TestFlashRelease_CancelWhileOpen(t *testing.T) {
	t.Parallel()
	f := newFlashWebEnv(t)
	s := f.schedule(3, 1, 10)
	heldBefore := f.balance(s.variant).Held
	sold := f.mustBuy(f.in(s, f.phoneCustomer("0911000001"), 1))
	if _, err := f.complete(f.completeIn(sold, "0911000001"), f.start.Add(2*time.Minute)); err != nil {
		t.Fatalf("hoàn tất: %v", err)
	}
	draft := f.mustBuy(f.in(s, f.customers(1)[0], 1))
	for _, id := range []uuid.UUID{sold.OrderID, draft.OrderID} {
		if err := f.cancelOrder(id); err != nil {
			t.Fatalf("hủy %s: %v", id, err)
		}
	}
	if f.units(s.item, "available") != 3 || f.claimed(s.item) != 0 {
		t.Errorf("đơn vị trống %d, claims %d; mong 3, 0", f.units(s.item, "available"), f.claimed(s.item))
	}
	if b := f.balance(s.variant); b.Held != heldBefore {
		t.Errorf("held %d, mong %d", b.Held, heldBefore)
	}
	f.mustReconcile()
}

// Đơn flash có hai dòng flash (chèn bằng owner) → hủy trả ErrInvalidTransition, không đổi gì; hết hạn bỏ qua đơn đó
// (không chặn đơn khác của lô).
func TestFlashRelease_TwoFlashLines(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(4, 2, 10)
	bad := f.mustBuy(f.in(s, f.customers(1)[0], 1))
	good := f.mustBuy(f.in(s, f.customers(1)[0], 1))
	other := f.schedule(2, 1, 10) // dòng flash thứ hai: món của biến thể khác (unique biến thể trong đơn)
	f.ownerExec([]any{bad.OrderID, other.variant, other.item}, `INSERT INTO ord.order_items (id, tenant_id, seller_id, order_id, position, variant_id, sku_snapshot,
		name_snapshot, quantity, unit_price, line_discount, allocated_order_discount, net_amount, flash_sale_item_id)
		SELECT uuidv7(), tenant_id, seller_id, order_id, 2, $2, sku_snapshot, name_snapshot, 1, unit_price, 0, 0,
		       unit_price, $3
		FROM ord.order_items WHERE order_id = $1`, `UPDATE ord.orders SET subtotal = subtotal * 2,
		grand_total = grand_total * 2, cod_amount = cod_amount * 2 WHERE id = $1 AND $2::uuid IS NOT NULL
		AND $3::uuid IS NOT NULL`)
	if err := f.cancelOrder(bad.OrderID); !errors.Is(err, order.ErrInvalidTransition) {
		t.Fatalf("hủy đơn hai dòng flash: %v; mong ErrInvalidTransition", err)
	}
	if o := f.getOrder(bad.OrderID); o.Status != order.StatusDraft {
		t.Errorf("đơn %s", o.Status)
	}
	f.expireSkipping(good.OrderID, bad.OrderID)
	f.wantHeldClaims(s, 1, 1)
}

// expireSkipping chạy hết hạn sau 12 phút và đòi đúng một đơn bị hủy (good), đơn bad bị bỏ qua, vẫn draft.
func (f *flashEnv) expireSkipping(good, bad uuid.UUID) {
	f.t.Helper()
	if n, err := f.expireAt(f.start.Add(12 * time.Minute)); err != nil || n != 1 {
		f.t.Fatalf("hết hạn: %d, %v; mong 1 (bỏ qua đơn hỏng)", n, err)
	}
	if o := f.getOrder(good); o.Status != order.StatusCancelled {
		f.t.Errorf("đơn tốt %s", o.Status)
	}
	if o := f.getOrder(bad); o.Status != order.StatusDraft {
		f.t.Errorf("đơn hỏng %s", o.Status)
	}
}

// wantHeldClaims đòi số đơn vị held và tổng claims của món của s.
func (f *flashEnv) wantHeldClaims(s flashSale, held, claims int) {
	f.t.Helper()
	if f.units(s.item, "held") != held || f.claimed(s.item) != claims {
		f.t.Errorf("held %d, claims %d; mong %d, %d", f.units(s.item, "held"), f.claimed(s.item), held, claims)
	}
}

// checkRace: held ≥ 0, không đơn vị available trong đợt đã đóng, đối chiếu không lệch.
func (f *flashEnv) checkRace(i int, s flashSale) {
	f.t.Helper()
	if b := f.balance(s.variant); b.Held < 0 {
		f.t.Fatalf("lần %d: held %d", i, b.Held)
	}
	if f.units(s.item, "available") != 0 {
		f.t.Fatalf("lần %d: còn %d đơn vị trống trong đợt đã đóng", i, f.units(s.item, "available"))
	}
	if ms := f.reconcileFlash(); len(ms) != 0 {
		f.t.Fatalf("lần %d: đối chiếu lệch %+v", i, ms)
	}
}

// retryLater chạy fn tới khi không còn lỗi tạm của RetryTx (hết lượt chạy lại không phải deadlock vĩnh viễn).
func retryLater(fn func() error) error {
	for range 50 {
		err := fn()
		if !errors.Is(err, db.ErrRetryLater) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return errors.New("hết lượt thử lại")
}

// Tranh, 50 lần: đóng đợt cùng lúc với hết hạn đơn nháp → không deadlock (không lỗi), held ≥ 0, không đơn vị
// available trong đợt đóng, đối chiếu không lệch.
func TestFlashRelease_CloseRacesExpire(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	for i := range 50 {
		s := f.schedule(3, 1, 10)
		f.mustBuy(f.in(s, f.customers(1)[0], 1))
		f.mustBuy(f.in(s, f.customers(1)[0], 1))
		var wg sync.WaitGroup
		var closeErr, expErr error
		wg.Go(func() { closeErr = retryLater(func() error { return f.closeSale(s) }) })
		wg.Go(func() {
			expErr = retryLater(func() error { _, err := f.expireAt(f.start.Add(12 * time.Minute)); return err })
		})
		wg.Wait()
		if closeErr != nil || expErr != nil {
			t.Fatalf("lần %d: đóng %v, hết hạn %v", i, closeErr, expErr)
		}
		if n := f.count(`SELECT count(*) FROM ord.orders AS o JOIN ord.order_items AS i ON i.order_id = o.id
			WHERE i.flash_sale_item_id = $1 AND o.status = 'draft'`, s.item); n != 0 {
			t.Fatalf("lần %d: còn %d đơn nháp", i, n)
		}
		f.checkRace(i, s)
	}
}

// Tranh, 50 lần: lượt mua cùng lúc với đóng đợt → như trên (lượt mua bought hay not_open/sold_out đều được).
func TestFlashRelease_BuyRacesClose(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	for i := range 50 {
		s := f.schedule(2, 1, 10)
		cs := f.customers(2)
		var wg sync.WaitGroup
		errs := make([]error, 3)
		for j, c := range cs {
			wg.Go(func() { _, errs[j] = f.buy(f.in(s, c, 1)) })
		}
		wg.Go(func() { errs[2] = retryLater(func() error { return f.closeSale(s) }) })
		wg.Wait()
		for j, err := range errs {
			if err != nil {
				t.Fatalf("lần %d, lượt %d: %v", i, j, err)
			}
		}
		f.checkRace(i, s)
		if n, err := f.expireAt(f.start.Add(12 * time.Minute)); err != nil {
			t.Fatalf("lần %d: hết hạn %d, %v", i, n, err)
		}
		f.checkRace(i, s)
	}
}

// FlashItemOf: đơn flash → món của dòng flash; đơn thường → ok false.
func TestFlashItemOf(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(2, 1, 10)
	res := f.mustBuy(f.in(s, f.customers(1)[0], 1))
	plain := f.mustCreate(f.input(0, item(s.variant, 1)))
	f.mustApp(func(tx pgx.Tx) error {
		got, ok, err := order.FlashItemOf(f.t.Context(), tx, res.OrderID)
		if err != nil || !ok || got != s.item {
			return fmt.Errorf("đơn flash: %v %v %w", got, ok, err)
		}
		if _, ok, err := order.FlashItemOf(f.t.Context(), tx, plain.ID); err != nil || ok {
			return fmt.Errorf("đơn thường: %v %w", ok, err)
		}
		return nil
	})
}

// Đơn có dòng flash: sửa dòng (đổi số lượng) → ErrInvalidTransition, không đổi gì (đơn vị suất, claims gắn đúng số
// lượng dòng); sửa trường khác (phí ship) vẫn được.
func TestFlashRelease_EditLinesBlocked(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(4, 2, 10)
	res := f.mustBuy(f.in(s, f.customers(1)[0], 1))
	o := f.getOrder(res.OrderID)
	it := o.Items[0]
	err := f.app(func(tx pgx.Tx) error {
		_, err := f.writer().Edit(f.t.Context(), tx, f.seller, o.ID, order.EditInput{
			Version: o.Version,
			Lines:   &[]order.EditLine{{ItemID: &it.ID, VariantID: it.VariantID, Quantity: 2}},
		}, f.actor, f.start.Add(time.Minute))
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, order.ErrInvalidTransition) {
		t.Fatalf("sửa dòng flash: %v; mong ErrInvalidTransition", err)
	}
	if got := f.getOrder(o.ID); got.Version != o.Version || got.Items[0].Qty != 1 {
		t.Errorf("đơn v%d, số lượng %d sau lần bị chặn", got.Version, got.Items[0].Qty)
	}
	fee := int64(25000)
	if err := f.app(func(tx pgx.Tx) error {
		_, err := f.writer().Edit(f.t.Context(), tx, f.seller, o.ID, order.EditInput{Version: o.Version, ShippingFee: &fee},
			f.actor, f.start.Add(time.Minute))
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("sửa phí ship đơn flash: %v", err)
	}
	f.wantHeldClaims(s, 1, 1)
}

// Lô hết hạn có một đơn flash lệch claims (sửa tay bằng owner: claims 0, Unclaim lỗi ở pha hủy) → lô lùi về savepoint,
// hủy từng đơn: đơn tốt của khách khác vẫn hủy, đơn lệch bị bỏ qua, vẫn draft (lỗi một đơn không chặn
// đơn khác, kế hoạch M5 Task 202).
func TestFlashRelease_ExpireSkipsBrokenOrder(t *testing.T) {
	t.Parallel()
	f := newFlashEnv(t)
	s := f.schedule(4, 1, 10)
	cs := f.customers(2)
	bad := f.mustBuy(f.in(s, cs[0], 1))
	good := f.mustBuy(f.in(s, cs[1], 1))
	f.ownerExec([]any{s.item, cs[0]}, `UPDATE promotion.flash_sale_claims SET quantity = 0
		WHERE flash_sale_item_id = $1 AND customer_id = $2`)
	f.expireSkipping(good.OrderID, bad.OrderID)
	if f.units(s.item, "held") != 1 || f.units(s.item, "available") != 3 {
		t.Errorf("held %d, available %d; mong 1, 3", f.units(s.item, "held"), f.units(s.item, "available"))
	}
}
