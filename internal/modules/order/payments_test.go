package order_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
)

// pay đẩy tổng đã thanh toán paid về đơn o bằng Writer.ApplyPaymentTotals.
func (e *orderEnv) pay(o uuid.UUID, paid int64) error {
	return e.app(func(tx pgx.Tx) error {
		return e.writer().ApplyPaymentTotals(e.t.Context(), tx, e.seller, o, paid, e.actor, e.now())
	})
}

// DH01 (690.000): trả 200.000 → cod 490.000 partial; 690.000 → cod 0 paid; 700.000 → cod 0 paid; −1 → ErrInvalidInput;
// mỗi lần đổi: version +1, order.updated seq = version; cùng tổng lần hai → không đổi gì.
func TestApplyPaymentTotals(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.confirmedDH01()
	version := o.Version
	for _, c := range []struct {
		paid, cod int64
		status    string
	}{
		{200000, 490000, "partial"},
		{690000, 0, "paid"},
		{700000, 0, "paid"},
		{0, 690000, "unpaid"},
	} {
		if err := e.pay(o.ID, c.paid); err != nil {
			t.Fatalf("trả %d: %v", c.paid, err)
		}
		g := e.get(o.ID)
		version++
		if g.PrepaidTotal != c.paid || g.CODAmount != c.cod || g.PaymentStatus != c.status || g.Version != version ||
			!g.UpdatedAt.Equal(e.now()) {
			t.Errorf("trả %d: prepaid %d, cod %d, %s, version %d; muốn cod %d, %s, version %d", c.paid,
				g.PrepaidTotal, g.CODAmount, g.PaymentStatus, g.Version, c.cod, c.status, version)
		}
		if n := e.events(o.ID, order.TopicOrderUpdated, version); n != 1 {
			t.Errorf("trả %d: order.updated seq %d: %d", c.paid, version, n)
		}
	}
	if err := e.pay(o.ID, 0); err != nil || e.get(o.ID).Version != version {
		t.Errorf("cùng tổng lần hai: %v, version %d; muốn %d", err, e.get(o.ID).Version, version)
	}
	for _, bad := range []int64{-1, 9007199254740992} {
		if err := e.pay(o.ID, bad); !errors.Is(err, order.ErrInvalidInput) {
			t.Errorf("trả %d: %v; muốn ErrInvalidInput", bad, err)
		}
	}
	if err := e.app(func(tx pgx.Tx) error {
		return e.writer().ApplyPaymentTotals(t.Context(), tx, e.partner, o.ID, 1, e.actor, e.now())
	}); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v; muốn ErrNotFound", err)
	}
}

// Đơn có vận đơn hiện hành: đổi COD → ErrLockedByShipment, không đổi gì; đổi tổng mà COD giữ 0 → được.
func TestApplyPaymentTotals_LockedByShipment(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.confirmedDH01()
	if err := e.attachBy(o.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	attached := e.get(o.ID)
	if err := e.pay(o.ID, 200000); !errors.Is(err, order.ErrLockedByShipment) {
		t.Errorf("trả 200.000 khi đã gửi hãng: %v; muốn ErrLockedByShipment", err)
	}
	if g := e.get(o.ID); g.CODAmount != 690000 || g.PrepaidTotal != 0 || g.Version != attached.Version {
		t.Errorf("đơn bị đổi: %+v", g)
	}

	paid, _, _ := e.confirmedDH01()
	if err := e.pay(paid.ID, 690000); err != nil {
		t.Fatal(err)
	}
	if err := e.attachBy(paid.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err := e.pay(paid.ID, 700000); err != nil {
		t.Errorf("trả thêm khi COD giữ 0: %v", err)
	}
	if err := e.pay(paid.ID, 600000); !errors.Is(err, order.ErrLockedByShipment) {
		t.Errorf("trả bớt làm COD đổi: %v; muốn ErrLockedByShipment", err)
	}
}

// Quy luật (vét cạn mọi cặp chuyển giữa 6 mức trả, một đơn 690.000): sau mỗi lần trả, cod = max(grand − paid, 0),
// trạng thái theo ngưỡng, version tăng đúng khi tổng thật sự đổi (cùng tổng → không đổi).
func TestApplyPaymentTotals_AllTransitions(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.confirmedDH01()
	levels := []int64{0, 1, 200000, 689999, 690000, 700000}
	version, prev := o.Version, int64(0)
	for _, from := range levels {
		for _, to := range levels {
			for _, p := range []int64{from, to} {
				if err := e.pay(o.ID, p); err != nil {
					t.Fatalf("trả %d: %v", p, err)
				}
				if p != prev {
					version++
				}
				prev = p
				if msg := paidState(e.get(o.ID), p, version); msg != "" {
					t.Fatalf("%d → %d (trả %d): %s", from, to, p, msg)
				}
			}
		}
	}
}

// paidState trả "" khi đơn g khớp tổng đã trả p (cod = max(grand − p, 0), trạng thái theo ngưỡng) và version; ngược
// lại là mô tả chỗ lệch.
func paidState(g order.Order, p, version int64) string {
	want := order.PaymentPartial
	switch {
	case p == 0:
		want = order.PaymentUnpaid
	case p >= g.GrandTotal:
		want = order.PaymentPaid
	}
	if g.CODAmount != max(g.GrandTotal-p, 0) || g.PrepaidTotal != p || g.PaymentStatus != want || g.Version != version {
		return fmt.Sprintf("cod %d, prepaid %d, %s, version %d; muốn %s, version %d", g.CODAmount, g.PrepaidTotal,
			g.PaymentStatus, g.Version, want, version)
	}
	return ""
}

// delivered chạy song song với ApplyPaymentTotals cùng đơn (100 đơn, 10 luồng mỗi luồng 10 đơn): không deadlock, cả
// hai được áp, version +2, tồn trừ đúng một lần. Đơn đã trả đủ trước khi gửi hãng để lần trả thêm không đổi COD.
func TestApplyPaymentTotals_ConcurrentWithDelivered(t *testing.T) {
	e := newOrderEnv(t)
	const workers, perWorker, price = 10, 10, 100000
	v := e.variant(price, workers*perWorker)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range perWorker {
				if err := e.payDeliverRace(v, price); err != nil {
					t.Errorf("luồng %d lần %d: %v", w, i, err)
					return
				}
			}
		})
	}
	wg.Wait()
	balanceIs(t, e.balance(v), 0, 0, 0)
}

// payDeliverRace tạo một đơn giá price (1 cái biến thể v), trả đủ, gắn vận đơn, rồi cho delivered và trả thêm chạy
// cùng lúc; trả lỗi khi một bên lỗi hay kết quả cuối không nhất quán.
func (e *orderEnv) payDeliverRace(v uuid.UUID, price int64) error {
	o, err := e.create(e.input(0, item(v, 1)))
	if err != nil {
		return err
	}
	if err := e.pay(o.ID, price); err != nil {
		return err
	}
	s := uuid.New()
	if err := e.attachBy(o.ID, s); err != nil {
		return err
	}
	before := o.Version + 2 // trả đủ, gắn vận đơn
	var wg sync.WaitGroup
	var delivered bool
	var errDelivered, errPay error
	wg.Go(func() { delivered, errDelivered = e.onEvent(o.ID, s, order.StepDelivered) })
	wg.Go(func() { errPay = e.pay(o.ID, price+1) })
	wg.Wait()
	if errDelivered != nil || !delivered || errPay != nil {
		return fmt.Errorf("delivered %v %w, trả thêm %w", delivered, errDelivered, errPay)
	}
	var g order.Order
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		g, err = order.Get(e.t.Context(), tx, e.seller, o.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		return err
	}
	if g.Version != before+2 || g.Status != order.StatusDelivered || g.PrepaidTotal != price+1 || g.CODAmount != 0 ||
		g.PaymentStatus != order.PaymentPaid {
		return fmt.Errorf("đơn cuối %s, version %d (muốn %d), prepaid %d, cod %d, %s", g.Status, g.Version, before+2,
			g.PrepaidTotal, g.CODAmount, g.PaymentStatus)
	}
	return nil
}
