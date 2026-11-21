package order_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// shipS1, shipS2 là vận đơn giả cố định của test M3-06 (kế hoạch: "S1, S2 là UUID cố định").
func shipS1() uuid.UUID { return uuid.MustParse("0197a000-0000-7000-8000-000000000051") }

func shipS2() uuid.UUID { return uuid.MustParse("0197a000-0000-7000-8000-000000000052") }

// d1 tạo đơn D1 của kế hoạch M3-06: confirmed, vA ×1 (on_hand 5), vB ×2 (on_hand 10), mỗi cái 210.000, phí 0 →
// grand_total = cod_amount = 630.000. Trả đơn và hai biến thể.
func (e *orderEnv) d1() (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	va, vb := e.variant(210000, 5), e.variant(210000, 10)
	o := e.mustCreateM2(e.m2Input(0, ln(va, 1, 0), ln(vb, 2, 0))).Order
	if o.Status != order.StatusConfirmed || o.GrandTotal != 630000 || o.CODAmount != 630000 {
		e.t.Fatalf("D1: %s, %d, %d", o.Status, o.GrandTotal, o.CODAmount)
	}
	return o, va, vb
}

// attachedD1 là D1 gắn shipS1 (M2 AttachShipmentBy).
func (e *orderEnv) attachedD1() (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	o, va, vb := e.d1()
	if err := e.attachBy(o.ID, shipS1()); err != nil {
		e.t.Fatal(err)
	}
	return e.get(o.ID), va, vb
}

// pickedUpD1 là attachedD1, hãng đã lấy hàng (M2 OnShipmentEvent: vA in_transit 1, vB in_transit 2).
func (e *orderEnv) pickedUpD1() (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	o, va, vb := e.attachedD1()
	e.mustOnEvent(o.ID, shipS1(), order.StepPickedUp, true)
	return e.get(o.ID), va, vb
}

// deliveredD1 là pickedUpD1, hãng đã giao (M2 OnShipmentEvent).
func (e *orderEnv) deliveredD1() (order.Order, uuid.UUID, uuid.UUID) {
	e.t.Helper()
	o, va, vb := e.pickedUpD1()
	e.mustOnEvent(o.ID, shipS1(), order.StepDelivered, true)
	return e.get(o.ID), va, vb
}

// asCarrier chạy fn (một hàm ghi của Writer với người làm carrier) trong transaction crm_app; trả applied và lỗi.
func (e *orderEnv) asCarrier(fn func(tx pgx.Tx, w order.Writer, a order.Actor) (bool, error)) (bool, error) {
	var applied bool
	err := e.app(func(tx pgx.Tx) error {
		var err error
		applied, err = fn(tx, e.writer(), order.Actor{TenantID: e.tenant, Type: inventory.ActorCarrier})
		return err
	})
	return applied, err
}

func (e *orderEnv) reverse(o, s, ref uuid.UUID, reason string) (bool, error) {
	return e.asCarrier(func(tx pgx.Tx, w order.Writer, a order.Actor) (bool, error) {
		return w.ReverseDelivery(e.t.Context(), tx, e.seller, o, s, ref, reason, a, e.now())
	})
}

func (e *orderEnv) changeAddress(o, s uuid.UUID, addr order.ShipAddress) (bool, error) {
	var changed bool
	err := e.app(func(tx pgx.Tx) error {
		var err error
		changed, err = e.writer().ApplyAddressChange(e.t.Context(), tx, e.seller, o, s, addr, e.actor, e.now())
		return err //nolint:wrapcheck // test
	})
	return changed, err
}

// TD10, Q-1: D1 delivered (S1) → ReverseDelivery(S1, R1, carrier_corrected) → confirmed, vA on_hand 5, in_transit 1;
// một order.delivery_reversed (Seq = version, payload Changed); lịch sử delivered → confirmed lý do; gọi lại R1 →
// false; S2 (không hiện hành) → false, không đổi gì; thiếu lý do → ErrInvalidInput.
func TestReverseDelivery(t *testing.T) {
	e := newM2Env(t)
	o, va, vb := e.deliveredD1()
	balanceIs(t, e.balance(va), 4, 0, 0)
	r1 := uuid.New()
	if _, err := e.reverse(o.ID, shipS1(), r1, ""); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("thiếu lý do: %v; muốn ErrInvalidInput", err)
	}
	if _, err := e.reverse(o.ID, shipS1(), r1, "Sai Mẫu"); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("lý do sai mẫu: %v; muốn ErrInvalidInput", err)
	}
	if applied, err := e.reverse(o.ID, shipS2(), r1, "carrier_corrected"); err != nil || applied {
		t.Errorf("S2 không hiện hành: %v, %v; muốn false, nil", applied, err)
	}
	if g := e.get(o.ID); g.Status != order.StatusDelivered || g.Version != o.Version {
		t.Fatalf("S2 đổi đơn: %s, %d", g.Status, g.Version)
	}
	if applied, err := e.reverse(o.ID, shipS1(), r1, "carrier_corrected"); err != nil || !applied {
		t.Fatalf("đảo S1: %v, %v", applied, err)
	}
	got := e.get(o.ID)
	if got.Status != order.StatusConfirmed || got.Version != o.Version+1 || got.CurrentShipmentID == nil ||
		*got.CurrentShipmentID != shipS1() || got.GrandTotal != 630000 || got.CODAmount != 630000 {
		t.Fatalf("sau đảo: %s, version %d (trước %d), vận đơn %v, %d/%d", got.Status, got.Version, o.Version,
			got.CurrentShipmentID, got.GrandTotal, got.CODAmount)
	}
	balanceIs(t, e.balance(va), 5, 0, 1)
	balanceIs(t, e.balance(vb), 10, 0, 2)
	if n := e.events(o.ID, order.TopicOrderDeliveryReversed, got.Version); n != 1 {
		t.Errorf("order.delivery_reversed seq %d: %d; muốn 1", got.Version, n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_type = 'order' AND aggregate_id = $1
		AND seq = $2`, o.ID.String(), got.Version); n != 1 {
		t.Errorf("sự kiện seq %d: %d; muốn đúng 1 (không thêm order.confirmed)", got.Version, n)
	}
	p := e.returnEvent(o.ID, order.TopicOrderDeliveryReversed)
	if p["orderId"] != o.ID.String() || p["sellerId"] != e.seller.String() || p["status"] != order.StatusConfirmed ||
		p["customerId"] != o.CustomerID.String() || p["version"] != float64(got.Version) {
		t.Errorf("payload order.delivery_reversed: %v", p)
	}
	if n := e.history(o.ID, `from_status = 'delivered' AND to_status = 'confirmed' AND reason = 'carrier_corrected'
		AND actor_type = 'carrier'`); n != 1 {
		t.Errorf("lịch sử delivered → confirmed: %d; muốn 1", n)
	}
	if applied, err := e.reverse(o.ID, shipS1(), r1, "carrier_corrected"); err != nil || applied {
		t.Errorf("gọi lại R1: %v, %v; muốn false, nil", applied, err)
	}
	balanceIs(t, e.balance(va), 5, 0, 1)
	if g := e.get(o.ID); g.Version != got.Version {
		t.Errorf("gọi lại đổi version: %d → %d", got.Version, g.Version)
	}
}

// Đơn chưa giao (confirmed) hay không có vận đơn → false; đơn của người bán khác → ErrNotFound.
func TestReverseDelivery_NotDelivered(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.d1()
	if applied, err := e.reverse(o.ID, shipS1(), uuid.New(), "carrier_corrected"); err != nil || applied {
		t.Errorf("chưa gắn vận đơn: %v, %v", applied, err)
	}
	if err := e.attachBy(o.ID, shipS1()); err != nil {
		t.Fatal(err)
	}
	if applied, err := e.reverse(o.ID, shipS1(), uuid.New(), "carrier_corrected"); err != nil || applied {
		t.Errorf("confirmed chưa giao: %v, %v", applied, err)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.writer().ReverseDelivery(t.Context(), tx, e.partner, o.ID, shipS1(), uuid.New(), "carrier_corrected",
			e.actor, e.now())
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v; muốn ErrNotFound", err)
	}
}

// Sau đảo, đơn đi đường hoàn của M2 như đơn confirmed đang giao: OnShipmentReturning (M2) tạo phiếu hoàn.
func TestReverseDelivery_ThenReturning(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.deliveredD1()
	if applied, err := e.reverse(o.ID, shipS1(), uuid.New(), "carrier_corrected"); err != nil || !applied {
		t.Fatalf("đảo: %v, %v", applied, err)
	}
	if applied, err := e.returning(o.ID, shipS1()); err != nil || !applied {
		t.Fatalf("hoàn sau đảo: %v, %v", applied, err)
	}
	if r := e.openCarrierReturn(o.ID, shipS1()); r.Kind != order.ReturnCarrier {
		t.Errorf("phiếu hoàn: %+v", r)
	}
}

// LockSnapshot: đọc dưới khóa đúng các trường; không có, người bán khác → ErrNotFound.
func TestLockSnapshot(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.attachedD1()
	var s order.Snapshot
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		s, err = order.LockSnapshot(t.Context(), tx, e.seller, o.ID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	s1 := shipS1()
	want := order.Snapshot{
		ID: o.ID, SellerID: e.seller, Status: order.StatusConfirmed, PackingStatus: order.PackingNone,
		PaymentStatus: o.PaymentStatus, OrderType: o.OrderType, CurrentShipmentID: &s1, CODAmount: 630000,
		GrandTotal: 630000, WarehouseID: e.warehouse, Version: o.Version,
	}
	if s.CurrentShipmentID == nil || *s.CurrentShipmentID != shipS1() {
		t.Fatalf("vận đơn: %v", s.CurrentShipmentID)
	}
	s.CurrentShipmentID = want.CurrentShipmentID
	if s != want {
		t.Errorf("snapshot:\n%+v\nmuốn\n%+v", s, want)
	}
	for _, seller := range []uuid.UUID{e.partner, e.seller} {
		id := o.ID
		if seller == e.seller {
			id = uuid.New()
		}
		if err := e.app(func(tx pgx.Tx) error {
			_, err := order.LockSnapshot(t.Context(), tx, seller, id)
			return err //nolint:wrapcheck // test
		}); !errors.Is(err, order.ErrNotFound) {
			t.Errorf("không thấy (%s): %v; muốn ErrNotFound", id, err)
		}
	}
}

// DH15: ApplyAddressChange(S1) ghi địa chỉ mới (SĐT chuẩn hóa, address_complete), version + 1, order.updated; cùng địa
// chỉ lần hai → false; Edit (M2) đổi địa chỉ khi có S1 vẫn ErrLockedByShipment; vận đơn khác → ErrInvalidTransition;
// địa chỉ thiếu xã → ErrAddressIncomplete; sai dạng → ErrInvalidInput.
func TestApplyAddressChange(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.pickedUpD1()
	before := e.get(o.ID)
	addr := order.ShipAddress{
		Name: "Anh Minh", Phone: logging.Phone("0912345678"), Line: "99 Hai Bà Trưng", ProvinceCode: "T01",
		WardCode: "T01001",
	}
	edit := order.EditInput{Version: before.Version, ShipAddress: &addr}
	if _, err := e.editOrder(o.ID, edit, e.actor); !errors.Is(err, order.ErrLockedByShipment) {
		t.Fatalf("Edit địa chỉ khi có vận đơn: %v; muốn ErrLockedByShipment", err)
	}
	if _, err := e.changeAddress(o.ID, shipS2(), addr); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("vận đơn không hiện hành: %v; muốn ErrInvalidTransition", err)
	}
	bad := addr
	bad.WardCode = ""
	if _, err := e.changeAddress(o.ID, shipS1(), bad); !errors.Is(err, order.ErrAddressIncomplete) {
		t.Errorf("thiếu xã: %v; muốn ErrAddressIncomplete", err)
	}
	bad = addr
	bad.Phone = "abc"
	if _, err := e.changeAddress(o.ID, shipS1(), bad); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("SĐT sai: %v; muốn ErrInvalidInput", err)
	}
	if g := e.get(o.ID); g.Version != before.Version {
		t.Fatalf("lần bị từ chối đổi đơn: %d → %d", before.Version, g.Version)
	}
	if changed, err := e.changeAddress(o.ID, shipS1(), addr); err != nil || !changed {
		t.Fatalf("đổi địa chỉ: %v, %v", changed, err)
	}
	got := e.get(o.ID)
	if got.ShipAddress.Name != "Anh Minh" || got.ShipAddress.Line != "99 Hai Bà Trưng" || got.ShipPhone != "+84912345678" ||
		!got.AddressComplete || got.Version != before.Version+1 || got.Status != order.StatusConfirmed {
		t.Errorf("sau đổi: %+v, SĐT %s, đủ %v, version %d", got.ShipAddress.Name, got.ShipPhone, got.AddressComplete,
			got.Version)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND ship_phone_reversed = $2`, o.ID,
		customer.ReverseDigits("+84912345678")); n != 1 {
		t.Errorf("ship_phone_reversed chưa ghi theo SĐT mới")
	}
	if n := e.events(o.ID, order.TopicOrderUpdated, got.Version); n != 1 {
		t.Errorf("order.updated seq %d: %d", got.Version, n)
	}
	if changed, err := e.changeAddress(o.ID, shipS1(), addr); err != nil || changed {
		t.Errorf("cùng địa chỉ lần hai: %v, %v; muốn false, nil", changed, err)
	}
	edit.Version = got.Version
	addr.Line = "khác"
	if _, err := e.editOrder(o.ID, edit, e.actor); !errors.Is(err, order.ErrLockedByShipment) {
		t.Errorf("Edit sau đổi địa chỉ vẫn phải khóa: %v", err)
	}
}

// Đơn đã giao không đổi địa chỉ được (ErrInvalidTransition), dù S1 vẫn là vận đơn hiện hành.
func TestApplyAddressChange_Delivered(t *testing.T) {
	e := newM2Env(t)
	o, _, _ := e.deliveredD1()
	if _, err := e.changeAddress(o.ID, shipS1(), *fullAddress()); !errors.Is(err, order.ErrInvalidTransition) {
		t.Errorf("đơn delivered: %v; muốn ErrInvalidTransition", err)
	}
}

// CheckShipAddress (Task M3-17: fulfillment kiểm địa chỉ trước khi gửi hãng): chuẩn hóa (bỏ khoảng trắng) như
// ApplyAddressChange; thiếu xã → ErrAddressIncomplete; SĐT sai → ErrInvalidInput; xã lạ → customer.ErrUnknownAdminUnit;
// không đổi đơn nào.
func TestCheckShipAddress(t *testing.T) {
	e := newM2Env(t)
	ok := order.ShipAddress{
		Name: " Anh Minh ", Phone: logging.Phone(" 0912345678 "), Line: " 99 Hai Bà Trưng ", ProvinceCode: "T01",
		WardCode: "T01001",
	}
	check := func(a order.ShipAddress) (order.ShipAddress, error) {
		var out order.ShipAddress
		err := e.app(func(tx pgx.Tx) (err error) {
			out, err = order.CheckShipAddress(e.t.Context(), tx, a)
			return err //nolint:wrapcheck // test
		})
		return out, err
	}
	got, err := check(ok)
	if err != nil || got.Name != "Anh Minh" || got.Phone != "0912345678" || got.Line != "99 Hai Bà Trưng" {
		t.Fatalf("địa chỉ hợp lệ: %+v, %v", got, err)
	}
	for _, c := range []struct {
		name string
		edit func(*order.ShipAddress)
		want error
	}{
		{"thiếu xã", func(a *order.ShipAddress) { a.WardCode = "" }, order.ErrAddressIncomplete},
		{"SĐT sai", func(a *order.ShipAddress) { a.Phone = "abc" }, order.ErrInvalidInput},
		{"xã lạ", func(a *order.ShipAddress) { a.WardCode = "T01999" }, customer.ErrUnknownAdminUnit},
	} {
		bad := ok
		c.edit(&bad)
		if _, err := check(bad); !errors.Is(err, c.want) {
			t.Errorf("%s: %v; muốn %v", c.name, err, c.want)
		}
	}
}
