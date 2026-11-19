package order_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
)

// attach gắn vận đơn s vào đơn o bằng crm_app.
func (e *orderEnv) attach(o, s uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		return order.AttachShipment(e.t.Context(), tx, e.seller, o, s)
	})
}

// progress áp bước step của vận đơn s lên đơn o bằng crm_app.
func (e *orderEnv) progress(o, s uuid.UUID, step order.ShipmentStep) (bool, error) {
	var applied bool
	err := e.app(func(tx pgx.Tx) error {
		var err error
		applied, err = e.writer().ApplyShipmentProgress(e.t.Context(), tx, e.seller, o, s, step)
		return err //nolint:wrapcheck // test
	})
	return applied, err
}

func (e *orderEnv) mustProgress(o, s uuid.UUID, step order.ShipmentStep, want bool) {
	e.t.Helper()
	applied, err := e.progress(o, s, step)
	if err != nil || applied != want {
		e.t.Fatalf("%s: applied=%v, err=%v; muốn applied=%v", step, applied, err, want)
	}
}

func (e *orderEnv) get(id uuid.UUID) order.Order {
	e.t.Helper()
	var o order.Order
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		o, err = order.Get(e.t.Context(), tx, e.seller, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return o
}

// attachedOrder tạo biến thể on_hand 5, đơn qty cái và gắn một vận đơn mới; trả đơn, biến thể, vận đơn.
func (e *orderEnv) attachedOrder(qty int32) (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	v := e.variant(100000, 5)
	o := e.mustCreate(e.input(0, item(v, qty)))
	s := uuid.New()
	if err := e.attach(o.ID, s); err != nil {
		e.t.Fatal(err)
	}
	return o, v, s
}

func balanceIs(t *testing.T, b inventory.Balance, onHand, held, inTransit int32) {
	t.Helper()
	if b.OnHand != onHand || b.Held != held || b.InTransit != inTransit {
		t.Errorf("số dư %+v; muốn on_hand %d, held %d, in_transit %d", b, onHand, held, inTransit)
	}
}

func TestAttachShipment(t *testing.T) {
	eachSeller(t, testAttachShipment)
}

func testAttachShipment(t *testing.T, e *orderEnv) {
	v := e.variant(100000, 5)
	o := e.mustCreate(e.input(0, item(v, 2)))
	s1, s2 := uuid.New(), uuid.New()

	if err := e.attach(o.ID, s1); err != nil {
		t.Fatalf("gắn lần đầu: %v", err)
	}
	got := e.get(o.ID)
	if got.CurrentShipmentID == nil || *got.CurrentShipmentID != s1 || got.Version != o.Version+1 {
		t.Fatalf("sau gắn: current %v, version %d (trước %d)", got.CurrentShipmentID, got.Version, o.Version)
	}
	if err := e.attach(o.ID, s1); err != nil {
		t.Errorf("gắn lại cùng vận đơn phải không lỗi: %v", err)
	}
	if err := e.attach(o.ID, s2); !errors.Is(err, order.ErrShipmentAlreadyAttached) {
		t.Errorf("gắn vận đơn khác: %v; muốn ErrShipmentAlreadyAttached", err)
	}
	if got := e.get(o.ID); *got.CurrentShipmentID != s1 || got.Version != o.Version+1 {
		t.Errorf("vận đơn hiện hành phải giữ %s, version giữ: %v, %d", s1, got.CurrentShipmentID, got.Version)
	}
	if err := e.attach(uuid.New(), s1); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v; muốn ErrNotFound", err)
	}
	// Đơn của người bán này, gọi bằng người bán kia (ADR-29) → ErrNotFound, không đổi gì.
	o2 := e.mustCreate(e.input(0, item(v, 1)))
	if err := e.app(func(tx pgx.Tx) error {
		return order.AttachShipment(t.Context(), tx, e.partner, o2.ID, s2)
	}); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("gắn bằng người bán khác: %v; muốn ErrNotFound", err)
	}
	if got := e.get(o2.ID); got.CurrentShipmentID != nil || got.Version != o2.Version {
		t.Errorf("đơn bị đổi khi gắn bằng người bán khác: %v, %d", got.CurrentShipmentID, got.Version)
	}
}

func TestApplyShipmentProgress_Flow(t *testing.T) {
	eachSeller(t, testApplyShipmentProgressFlow)
}

func testApplyShipmentProgressFlow(t *testing.T, e *orderEnv) {
	v := e.variant(100000, 5)
	o := e.mustCreate(e.input(0, item(v, 2)))
	cur, other := uuid.New(), uuid.New()

	// Chưa gắn vận đơn: không vận đơn nào là hiện hành.
	e.mustProgress(o.ID, cur, order.StepPickedUp, false)
	if err := e.attach(o.ID, cur); err != nil {
		t.Fatal(err)
	}
	e.mustProgress(o.ID, other, order.StepPickedUp, false)
	e.mustProgress(o.ID, other, order.StepDelivered, false)
	balanceIs(t, e.balance(v), 5, 2, 0)

	// Người bán khác (ADR-29): không thấy đơn, không đổi tồn.
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.writer().ApplyShipmentProgress(t.Context(), tx, e.partner, o.ID, cur, order.StepPickedUp)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("áp bước bằng người bán khác: %v; muốn ErrNotFound", err)
	}
	balanceIs(t, e.balance(v), 5, 2, 0)

	e.mustProgress(o.ID, cur, order.StepPickedUp, true)
	balanceIs(t, e.balance(v), 5, 0, 2)
	e.mustProgress(o.ID, cur, order.StepPickedUp, false)
	balanceIs(t, e.balance(v), 5, 0, 2)
	e.mustProgress(o.ID, other, order.StepDelivered, false)
	balanceIs(t, e.balance(v), 5, 0, 2)

	e.mustProgress(o.ID, cur, order.StepDelivered, true)
	balanceIs(t, e.balance(v), 3, 0, 0)
	got := e.get(o.ID)
	if got.Status != order.StatusDelivered {
		t.Errorf("trạng thái %q; muốn delivered", got.Status)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1`, o.ID); n != 2 {
		t.Errorf("lịch sử: %d dòng; muốn 2", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status = 'confirmed'
		AND to_status = 'delivered' AND actor_type = 'carrier'`, o.ID); n != 1 {
		t.Errorf("dòng lịch sử confirmed → delivered: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_type = 'order' AND aggregate_id = $1
		AND topic = 'order.delivered' AND seq = $2`, o.ID.String(), got.Version); n != 1 {
		t.Errorf("sự kiện order.delivered seq %d: %d", got.Version, n)
	}

	// Gọi lại sau khi giao: không đổi gì.
	e.mustProgress(o.ID, cur, order.StepDelivered, false)
	e.mustProgress(o.ID, cur, order.StepPickedUp, false)
	balanceIs(t, e.balance(v), 3, 0, 0)
	if g := e.get(o.ID); g.Version != got.Version {
		t.Errorf("version đổi sau gọi lại: %d → %d", got.Version, g.Version)
	}
	if err := e.attach(o.ID, other); !errors.Is(err, order.ErrShipmentAlreadyAttached) &&
		!errors.Is(err, order.ErrStatusConflict) {
		t.Errorf("gắn vào đơn đã giao: %v", err)
	}
}

// Hãng báo giao mà chưa báo lấy hàng (Quyết định 3): dòng held qua in_transit rồi consumed trong cùng lần.
func TestApplyShipmentProgress_DeliveredWithoutPickup(t *testing.T) {
	eachSeller(t, testDeliveredWithoutPickup)
}

func testDeliveredWithoutPickup(t *testing.T, e *orderEnv) {
	v1, v2 := e.variant(100000, 5), e.variant(50000, 4)
	o := e.mustCreate(e.input(0, item(v1, 2), item(v2, 1)))
	s := uuid.New()
	if err := e.attach(o.ID, s); err != nil {
		t.Fatal(err)
	}
	e.mustProgress(o.ID, s, order.StepDelivered, true)
	balanceIs(t, e.balance(v1), 3, 0, 0)
	balanceIs(t, e.balance(v2), 3, 0, 0)
	if g := e.get(o.ID); g.Status != order.StatusDelivered {
		t.Errorf("trạng thái %q", g.Status)
	}
}

// Bất biến hỏng (đơn không còn giữ hàng nào) → lỗi, không applied=false im lặng, không đổi gì (lưu ý reviewer Task
// 30: không nuốt ErrStateConflict).
func TestApplyShipmentProgress_BrokenReservationsIsError(t *testing.T) {
	e := newOrderEnv(t)
	o, v, s := e.attachedOrder(2)
	// Nhả giữ hàng bằng đường của inventory (không qua đơn) để tạo trạng thái hỏng.
	if err := e.app(func(tx pgx.Tx) error {
		w := e.writer()
		return inventory.Writer{Events: w.Events, Queue: w.Queue, Realtime: w.Realtime}.ReleaseHeldByItems(
			t.Context(), tx, []uuid.UUID{o.Items[0].ID}, "test_broken", inventory.Actor{Type: inventory.ActorSystem})
	}); err != nil {
		t.Fatal(err)
	}
	for _, step := range []order.ShipmentStep{order.StepPickedUp, order.StepDelivered} {
		applied, err := e.progress(o.ID, s, step)
		if err == nil || applied {
			t.Errorf("%s trên đơn không còn giữ hàng: applied=%v, err=%v; muốn lỗi", step, applied, err)
		}
	}
	if g := e.get(o.ID); g.Status != order.StatusConfirmed {
		t.Errorf("trạng thái đổi: %q", g.Status)
	}
	balanceIs(t, e.balance(v), 5, 0, 0)
}

func TestApplyShipmentProgress_UnknownStep(t *testing.T) {
	e := newOrderEnv(t)
	o, _, s := e.attachedOrder(1)
	if _, err := e.progress(o.ID, s, order.ShipmentStep("returned")); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("bước lạ: %v; muốn ErrInvalidInput", err)
	}
}

// Quy luật (rapid): chuỗi bước ngẫu nhiên của vận đơn hiện hành và vận đơn khác — bước của vận đơn khác không bao giờ
// được áp; số dư luôn khớp trạng thái đơn (held trước lấy hàng, in_transit sau lấy hàng, on_hand giảm đúng một lần
// khi giao); mỗi bước được áp nhiều nhất một lần.
func TestApplyShipmentProgress_OnlyCurrentShipment(t *testing.T) {
	e := newOrderEnv(t)
	rapid.Check(t, func(rt *rapid.T) {
		qty := rapid.Int32Range(1, 3).Draw(rt, "qty")
		v := e.variant(10000, 3)
		o := e.mustCreate(e.input(0, item(v, qty)))
		cur, other := uuid.New(), uuid.New()
		if err := e.attach(o.ID, cur); err != nil {
			rt.Fatal(err)
		}
		var m shipModel
		steps := rapid.SliceOfN(rapid.SampledFrom([]order.ShipmentStep{order.StepPickedUp, order.StepDelivered}),
			1, 6).Draw(rt, "steps")
		currents := rapid.SliceOfN(rapid.Bool(), len(steps), len(steps)).Draw(rt, "current")
		for i, step := range steps {
			s := other
			if currents[i] {
				s = cur
			}
			applied, err := e.progress(o.ID, s, step)
			if err != nil {
				rt.Fatalf("bước %d %s: %v", i, step, err)
			}
			if want := m.step(currents[i], step); applied != want {
				rt.Fatalf("bước %d %s (hiện hành %v): applied=%v; muốn %v", i, step, currents[i], applied, want)
			}
			if b, want := e.balance(v), m.balance(qty); [3]int32{b.OnHand, b.Held, b.InTransit} != want {
				rt.Fatalf("bước %d: số dư %+v; muốn %+v", i, b, want)
			}
		}
	})
}

// Hai tín hiệu giao cùng lúc (webhook và hỏi trạng thái) → đúng một lần áp, on_hand trừ một lần (khóa đơn trước).
func TestApplyShipmentProgress_ConcurrentDelivered(t *testing.T) {
	e := newOrderEnv(t)
	o, v, s := e.attachedOrder(2)
	const n = 8
	var wg sync.WaitGroup
	results := make([]bool, n)
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() { results[i], errs[i] = e.progress(o.ID, s, order.StepDelivered) })
	}
	wg.Wait()
	applied := 0
	for i := range n {
		if errs[i] != nil {
			t.Errorf("lần %d: %v", i, errs[i])
		}
		if results[i] {
			applied++
		}
	}
	if applied != 1 {
		t.Errorf("áp %d lần; muốn 1", applied)
	}
	balanceIs(t, e.balance(v), 3, 0, 0)
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_type = 'order' AND aggregate_id = $1
		AND topic = 'order.delivered'`, o.ID.String()); n != 1 {
		t.Errorf("order.delivered: %d", n)
	}
}

// shipModel là mô hình của bước vận đơn hiện hành đã áp lên đơn.
type shipModel struct{ pickedUp, delivered bool }

// step trả bước có được áp không (chỉ vận đơn hiện hành, mỗi bước một lần) và cập nhật mô hình.
func (m *shipModel) step(current bool, s order.ShipmentStep) bool {
	ok := current && !m.delivered && (s == order.StepDelivered || !m.pickedUp)
	if ok {
		m.pickedUp = true
		m.delivered = s == order.StepDelivered
	}
	return ok
}

// balance là (on_hand, held, in_transit) của biến thể (on_hand 3, một đơn qty cái) theo các bước đã áp.
func (m shipModel) balance(qty int32) [3]int32 {
	switch {
	case m.delivered:
		return [3]int32{3 - qty, 0, 0}
	case m.pickedUp:
		return [3]int32{3, 0, qty}
	default:
		return [3]int32{3, qty, 0}
	}
}

// Lock: đơn có → khóa được; không có (hay tenant khác, người bán khác) → ErrNotFound.
func TestLock(t *testing.T) {
	e := newOrderEnv(t)
	o := e.mustCreate(e.input(0, item(e.variant(100000, 5), 1)))
	if err := e.app(func(tx pgx.Tx) error { return order.Lock(e.t.Context(), tx, e.seller, o.ID) }); err != nil {
		t.Fatalf("Lock đơn có: %v", err)
	}
	if err := e.app(func(tx pgx.Tx) error { return order.Lock(e.t.Context(), tx, e.seller, uuid.New()) }); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("Lock đơn lạ: %v, muốn ErrNotFound", err)
	}
	if err := e.app(func(tx pgx.Tx) error { return order.Lock(e.t.Context(), tx, e.partner, o.ID) }); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("Lock bằng người bán khác: %v, muốn ErrNotFound", err)
	}
}

// detach gỡ vận đơn s khỏi đơn o của người bán seller bằng crm_app.
func (e *orderEnv) detach(seller, o, s uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		return order.DetachShipment(e.t.Context(), tx, seller, o, s)
	})
}

// D-03: gỡ vận đơn hiện hành chỉ khi đúng vận đơn đó (version +1); vận đơn khác, gỡ lần hai → không đổi, không lỗi;
// đơn không có hay của người bán khác → ErrNotFound; gỡ xong gắn lại vận đơn mới được.
func TestDetachShipment(t *testing.T) {
	eachSeller(t, testDetachShipment)
}

func testDetachShipment(t *testing.T, e *orderEnv) {
	o, _, s := e.attachedOrder(1)
	attached := e.get(o.ID)
	if err := e.detach(e.seller, o.ID, uuid.New()); err != nil {
		t.Fatalf("gỡ vận đơn khác: %v", err)
	}
	if got := e.get(o.ID); got.CurrentShipmentID == nil || *got.CurrentShipmentID != s || got.Version != attached.Version {
		t.Fatalf("gỡ vận đơn khác đổi đơn: %v, %d", got.CurrentShipmentID, got.Version)
	}
	other := e.partner
	if other == e.seller {
		other = e.shop.Seller
	}
	if err := e.detach(other, o.ID, s); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("gỡ bằng người bán khác: %v; muốn ErrNotFound", err)
	}
	if err := e.detach(e.seller, uuid.New(), s); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("đơn không có: %v; muốn ErrNotFound", err)
	}
	if err := e.detach(e.seller, o.ID, s); err != nil {
		t.Fatalf("gỡ: %v", err)
	}
	got := e.get(o.ID)
	if got.CurrentShipmentID != nil || got.Version != attached.Version+1 || got.Status != order.StatusConfirmed {
		t.Fatalf("sau gỡ: %v, version %d (trước %d), %s", got.CurrentShipmentID, got.Version, attached.Version, got.Status)
	}
	if err := e.detach(e.seller, o.ID, s); err != nil || e.get(o.ID).Version != got.Version {
		t.Fatalf("gỡ lần hai: %v, version %d", err, e.get(o.ID).Version)
	}
	s2 := uuid.New()
	if err := e.attach(o.ID, s2); err != nil {
		t.Fatalf("gắn lại sau gỡ: %v", err)
	}
}

// countDown áp một phiên kiểm kho ở kho của e ghi số đếm counted cho v (đường của inventory, như nhân viên kho).
func (e *orderEnv) countDown(v uuid.UUID, counted int32) {
	e.t.Helper()
	a := inventory.Actor{Type: inventory.ActorSystem}
	if err := e.app(func(tx pgx.Tx) error {
		ctx := e.t.Context()
		s, err := inventory.StartStocktake(ctx, tx, e.warehouse, a)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := inventory.RecordCount(ctx, tx, s.ID, v, counted, nil, a); err != nil {
			return err //nolint:wrapcheck // test
		}
		w := e.writer()
		inv := inventory.Writer{Events: w.Events, Queue: w.Queue, Realtime: w.Realtime}
		_, err = inv.ApplyStocktake(ctx, tx, s.ID, s.Version+1, a)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("kiểm kho: %v", err)
	}
}

// A-01 (Task 406): on_hand 4, hai đơn giữ 2 + 2, kiểm kho đếm 2. Đơn 1 giao xong; đơn 2 lấy hàng hay giao →
// lỗi Is inventory.ErrStockShort (không phải lỗi CHECK), applied=false, đơn vẫn confirmed, giữ hàng vẫn held, số dư
// không đổi; nhập thêm (nhân viên điều chỉnh) thì đơn 2 giao xong.
func TestApplyShipmentProgress_StockShortAfterStocktake(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(100000, 4)
	o1, o2 := e.mustCreate(e.input(0, item(v, 2))), e.mustCreate(e.input(0, item(v, 2)))
	s1, s2 := uuid.New(), uuid.New()
	for _, p := range [][2]uuid.UUID{{o1.ID, s1}, {o2.ID, s2}} {
		if err := e.attach(p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	e.countDown(v, 2)
	balanceIs(t, e.balance(v), 2, 4, 0)
	e.mustProgress(o1.ID, s1, order.StepDelivered, true)
	balanceIs(t, e.balance(v), 0, 2, 0)
	for _, step := range []order.ShipmentStep{order.StepPickedUp, order.StepDelivered} {
		applied, err := e.progress(o2.ID, s2, step)
		if applied || !errors.Is(err, inventory.ErrStockShort) {
			t.Fatalf("%s đơn 2: applied=%v, err=%v; muốn ErrStockShort", step, applied, err)
		}
	}
	if g := e.get(o2.ID); g.Status != order.StatusConfirmed {
		t.Fatalf("đơn 2 thành %q", g.Status)
	}
	balanceIs(t, e.balance(v), 0, 2, 0)
	if err := e.app(func(tx pgx.Tx) error {
		invtest.SeedStock(t, tx, v, e.warehouse, 2)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.mustProgress(o2.ID, s2, order.StepDelivered, true)
	balanceIs(t, e.balance(v), 0, 0, 0)
}
