package order_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// confirmedDH01 tạo đơn DH01 không mã (đầu vào dh01 của create_coupon_test.go): confirmed, V1 ×2 (giảm 20.000),
// V2 ×1, phí 30.000 → tổng 690.000. Trả đơn và hai biến thể (V1 on_hand 5, V2 on_hand 1).
func (e *orderEnv) confirmedDH01() (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	v1, v2 := e.variant(250000, 5), e.variant(180000, 1)
	o := e.mustCreateM2(e.dh01(v1, v2, "")).Order
	if o.GrandTotal != 690000 || o.Status != order.StatusConfirmed {
		e.t.Fatalf("DH01: %+v", o)
	}
	return o, v1, v2
}

// attachBy gắn vận đơn s vào đơn o bằng Writer.AttachShipmentBy (người làm e.actor, giờ e.now()); trước đó tạo dòng
// vận đơn s (khóa ngoại của current_shipment_id).
func (e *orderEnv) attachBy(o, s uuid.UUID) error {
	e.stubShipment(o, s)
	return e.attachOnly(o, s)
}

// attachOnly là attachBy không tạo dòng vận đơn (gọi được từ goroutine; bên gọi tạo trước bằng stubShipment).
func (e *orderEnv) attachOnly(o, s uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		return e.writer().AttachShipmentBy(e.t.Context(), tx, e.seller, o, s, e.actor, e.now())
	})
}

func (e *orderEnv) detachBy(seller, o, s uuid.UUID) (bool, error) {
	var applied bool
	err := e.app(func(tx pgx.Tx) error {
		var err error
		applied, err = e.writer().DetachShipment(e.t.Context(), tx, seller, o, s, e.actor, e.now())
		return err //nolint:wrapcheck // test
	})
	return applied, err
}

func (e *orderEnv) onEvent(o, s uuid.UUID, step order.ShipmentStep) (bool, error) {
	var applied bool
	err := e.app(func(tx pgx.Tx) error {
		var err error
		carrier := order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier}
		applied, err = e.writer().OnShipmentEvent(e.t.Context(), tx, e.seller, o, s, step, carrier, e.now())
		return err //nolint:wrapcheck // test
	})
	return applied, err
}

func (e *orderEnv) mustOnEvent(o, s uuid.UUID, step order.ShipmentStep, want bool) {
	e.t.Helper()
	if applied, err := e.onEvent(o, s, step); err != nil || applied != want {
		e.t.Fatalf("%s: applied=%v, err=%v; muốn applied=%v", step, applied, err, want)
	}
}

// events đếm sự kiện topic của đơn o có seq.
func (e *orderEnv) events(o uuid.UUID, topic string, seq int64) int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_type = 'order' AND aggregate_id = $1
		AND topic = $2 AND seq = $3`, o.String(), topic, seq)
}

// AttachShipmentBy: gắn S1 → current S1, version +1, order.updated (seq = version); gắn lại S1 → không đổi; S2 →
// ErrShipmentAlreadyAttached; đơn không có hay người bán khác → ErrNotFound.
func TestAttachShipmentBy(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.confirmedDH01()
	s1, s2 := uuid.New(), uuid.New()
	if err := e.attachBy(o.ID, s1); err != nil {
		t.Fatalf("gắn S1: %v", err)
	}
	got := e.get(o.ID)
	if got.CurrentShipmentID == nil || *got.CurrentShipmentID != s1 || got.Version != o.Version+1 ||
		!got.UpdatedAt.Equal(e.now()) || got.Status != order.StatusConfirmed {
		t.Fatalf("sau gắn: %v, version %d (trước %d), updated %v", got.CurrentShipmentID, got.Version, o.Version,
			got.UpdatedAt)
	}
	if n := e.events(o.ID, order.TopicOrderUpdated, got.Version); n != 1 {
		t.Errorf("order.updated seq %d: %d; muốn 1", got.Version, n)
	}
	if err := e.attachBy(o.ID, s1); err != nil {
		t.Errorf("gắn lại S1: %v; muốn nil", err)
	}
	if err := e.attachBy(o.ID, s2); !errors.Is(err, order.ErrShipmentAlreadyAttached) {
		t.Errorf("gắn S2: %v; muốn ErrShipmentAlreadyAttached", err)
	}
	if g := e.get(o.ID); *g.CurrentShipmentID != s1 || g.Version != got.Version {
		t.Errorf("gắn lại/gắn S2 đổi đơn: %v, %d", g.CurrentShipmentID, g.Version)
	}
	if err := e.attachBy(uuid.New(), s1); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v; muốn ErrNotFound", err)
	}
	o2, _, _ := e.confirmedDH01()
	if err := e.app(func(tx pgx.Tx) error {
		return e.writer().AttachShipmentBy(t.Context(), tx, e.partner, o2.ID, s2, e.actor, e.now())
	}); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v; muốn ErrNotFound", err)
	}
}

// Đơn new (hay draft) → ErrInvalidTransition, không đổi gì.
func TestAttachShipmentBy_NotConfirmed(t *testing.T) {
	e := newM2Env(t)
	v := e.variant(100000, 5)
	for _, intent := range []string{order.IntentNew, order.IntentDraft} {
		in := e.m2Input(0, ln(v, 1, 0))
		in.Intent = intent
		o := e.mustCreateM2(in).Order
		if err := e.attachBy(o.ID, uuid.New()); !errors.Is(err, order.ErrInvalidTransition) {
			t.Errorf("%s: %v; muốn ErrInvalidTransition", intent, err)
		}
		if g := e.get(o.ID); g.CurrentShipmentID != nil || g.Version != o.Version {
			t.Errorf("%s: đơn bị đổi: %v, %d", intent, g.CurrentShipmentID, g.Version)
		}
	}
}

// picked_up → giữ hàng in_transit, Available không đổi; delivered → delivered, nhãn delivered, on_hand −, order.delivered
// seq = version; delivered lần hai → applied=false, on_hand không giảm thêm (AT47).
func TestOnShipmentEvent_PickedUpThenDelivered(t *testing.T) {
	e := newM2Env(t)
	o, v1, v2 := e.confirmedDH01()
	s1 := uuid.New()
	if err := e.attachBy(o.ID, s1); err != nil {
		t.Fatal(err)
	}
	before := e.balance(v1)
	e.mustOnEvent(o.ID, s1, order.StepPickedUp, true)
	if b := e.balance(v1); b.InTransit != 2 || b.Held != 0 || b.Available != before.Available || b.OnHand != 5 {
		t.Errorf("V1 sau lấy hàng: %+v (trước %+v)", b, before)
	}
	attached := e.get(o.ID)
	e.mustOnEvent(o.ID, s1, order.StepDelivered, true)
	got := e.get(o.ID)
	label := orderview.Label(orderview.Axes{
		Status: got.Status, PendingReason: got.PendingReason,
		PackingStatus: got.PackingStatus,
	})
	if got.Status != order.StatusDelivered || label != orderview.LabelDelivered || got.Version != attached.Version+1 {
		t.Errorf("sau giao: %s, nhãn %s, version %d", got.Status, label, got.Version)
	}
	balanceIs(t, e.balance(v1), 3, 0, 0)
	balanceIs(t, e.balance(v2), 0, 0, 0)
	if n := e.events(o.ID, order.TopicOrderDelivered, got.Version); n != 1 {
		t.Errorf("order.delivered seq %d: %d", got.Version, n)
	}
	if n := e.count(`SELECT count(*) FROM inventory.stock_reservations WHERE order_item_id = ANY($1)
		AND state = 'consumed'`, []uuid.UUID{o.Items[0].ID, o.Items[1].ID}); n != 2 {
		t.Errorf("giữ hàng consumed: %d; muốn 2", n)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND to_status = 'delivered'
		AND actor_type = 'carrier' AND occurred_at = $2`, o.ID, e.now()); n != 1 {
		t.Errorf("lịch sử delivered (carrier, giờ của bên gọi): %d", n)
	}
	e.mustOnEvent(o.ID, s1, order.StepDelivered, false)
	balanceIs(t, e.balance(v1), 3, 0, 0)
	if g := e.get(o.ID); g.Version != got.Version {
		t.Errorf("delivered lần hai đổi version: %d → %d", got.Version, g.Version)
	}
}

// B1: gắn S1, gỡ S1, gắn S2; delivered(S1) tới muộn → applied=false, đơn và tồn không đổi; delivered(S2) → applied.
func TestOnShipmentEvent_StaleShipment(t *testing.T) {
	e := newM2Env(t)
	o, v1, _ := e.confirmedDH01()
	s1, s2 := uuid.New(), uuid.New()
	if err := e.attachBy(o.ID, s1); err != nil {
		t.Fatal(err)
	}
	if applied, err := e.detachBy(e.seller, o.ID, s1); err != nil || !applied {
		t.Fatalf("gỡ S1: %v, %v", applied, err)
	}
	if err := e.attachBy(o.ID, s2); err != nil {
		t.Fatalf("gắn S2: %v", err)
	}
	cur := e.get(o.ID)
	e.mustOnEvent(o.ID, s1, order.StepPickedUp, false)
	e.mustOnEvent(o.ID, s1, order.StepDelivered, false)
	if g := e.get(o.ID); g.Status != order.StatusConfirmed || g.Version != cur.Version {
		t.Errorf("vận đơn cũ đổi đơn: %s, %d", g.Status, g.Version)
	}
	balanceIs(t, e.balance(v1), 5, 2, 0)
	e.mustOnEvent(o.ID, s2, order.StepDelivered, true)
	balanceIs(t, e.balance(v1), 3, 0, 0)
}

// delivered khi chưa picked_up → held → in_transit → consumed trong một transaction.
func TestOnShipmentEvent_DeliveredWithoutPickup(t *testing.T) {
	e := newM2Env(t)
	o, v1, v2 := e.confirmedDH01()
	s := uuid.New()
	if err := e.attachBy(o.ID, s); err != nil {
		t.Fatal(err)
	}
	e.mustOnEvent(o.ID, s, order.StepDelivered, true)
	balanceIs(t, e.balance(v1), 3, 0, 0)
	balanceIs(t, e.balance(v2), 0, 0, 0)
	if g := e.get(o.ID); g.Status != order.StatusDelivered {
		t.Errorf("trạng thái %s", g.Status)
	}
}

// DetachShipment: chỉ khi đúng vận đơn hiện hành; đơn giữ confirmed, giữ hàng không đổi, order.updated; gỡ lần hai,
// vận đơn khác → applied=false không đổi; người bán khác hay đơn không có → ErrNotFound.
func TestWriterDetachShipment(t *testing.T) {
	e := newM2Env(t)
	o, v1, _ := e.confirmedDH01()
	s := uuid.New()
	if err := e.attachBy(o.ID, s); err != nil {
		t.Fatal(err)
	}
	attached := e.get(o.ID)
	if applied, err := e.detachBy(e.seller, o.ID, uuid.New()); err != nil || applied {
		t.Errorf("gỡ vận đơn khác: %v, %v", applied, err)
	}
	if _, err := e.detachBy(e.partner, o.ID, s); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v; muốn ErrNotFound", err)
	}
	if _, err := e.detachBy(e.seller, uuid.New(), s); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v; muốn ErrNotFound", err)
	}
	if g := e.get(o.ID); g.Version != attached.Version {
		t.Fatalf("gỡ không đúng đổi đơn: %d", g.Version)
	}
	if applied, err := e.detachBy(e.seller, o.ID, s); err != nil || !applied {
		t.Fatalf("gỡ: %v, %v", applied, err)
	}
	got := e.get(o.ID)
	if got.CurrentShipmentID != nil || got.Status != order.StatusConfirmed || got.Version != attached.Version+1 {
		t.Errorf("sau gỡ: %v, %s, %d", got.CurrentShipmentID, got.Status, got.Version)
	}
	if n := e.events(o.ID, order.TopicOrderUpdated, got.Version); n != 1 {
		t.Errorf("order.updated seq %d: %d", got.Version, n)
	}
	balanceIs(t, e.balance(v1), 5, 2, 0)
	if applied, err := e.detachBy(e.seller, o.ID, s); err != nil || applied || e.get(o.ID).Version != got.Version {
		t.Errorf("gỡ lần hai: %v, %v", applied, err)
	}
}

// Lớp bọc M1 (Q30): AttachShipment, ApplyShipmentProgress(picked_up, delivered) cho cùng kết quả như hai hàm mới;
// lịch sử ghi người làm carrier.
func TestShipmentWrappers_SameAsNew(t *testing.T) {
	e := newM2Env(t)
	type result struct {
		status            string
		attachedVersion   int64
		deliveredVersion  int64
		onHand, inTransit int32
	}
	run := func(wrapper bool) result {
		o, v1, _ := e.confirmedDH01()
		s := uuid.New()
		var err error
		if wrapper {
			err = e.attach(o.ID, s)
		} else {
			err = e.attachBy(o.ID, s)
		}
		if err != nil {
			t.Fatal(err)
		}
		var r result
		r.attachedVersion = e.get(o.ID).Version - o.Version
		for _, step := range []order.ShipmentStep{order.StepPickedUp, order.StepDelivered} {
			if wrapper {
				e.mustProgress(o.ID, s, step, true)
			} else {
				e.mustOnEvent(o.ID, s, step, true)
			}
		}
		g := e.get(o.ID)
		r.status, r.deliveredVersion = g.Status, g.Version-o.Version
		b := e.balance(v1)
		r.onHand, r.inTransit = b.OnHand, b.InTransit
		if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND to_status = 'delivered'
			AND actor_type = 'carrier'`, o.ID); n != 1 {
			t.Errorf("lớp bọc %v: lịch sử delivered carrier %d", wrapper, n)
		}
		return r
	}
	if a, b := run(true), run(false); a != b {
		t.Errorf("lớp bọc %+v khác hàm mới %+v", a, b)
	}
}
