package fulfillment_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Đường thường: tạo vận đơn (creating, gắn vào đơn, một lệnh) → worker gọi hãng → awaiting_pickup có mã hãng, phí;
// hãng giả có đúng 1 kiện; picked_up → shipping, giữ hàng in_transit; delivered → đơn delivered, on_hand giảm.
//
// Chạy với từng người bán (KH14): đơn của người bán nào đi bằng tài khoản hãng của người bán đó.
func TestShipment_HappyPath(t *testing.T) {
	eachSeller(t, testShipmentHappyPath)
}

func testShipmentHappyPath(t *testing.T, e *shipEnv) {
	parcels := e.carrier.Parcels()
	o, v := e.order(2)
	s := e.mustCreateShipment(o.ID)
	if s.Status != fulfillment.StatusCreating || s.CODAmount != 530000 || s.TrackingCode != "" ||
		!codePattern.MatchString(s.Code) || s.CarrierKey != fake.Key || s.CarrierAccountID != e.account {
		t.Fatalf("vận đơn mới = %+v", s)
	}
	if got := e.getOrder(o.ID); got.CurrentShipmentID == nil || *got.CurrentShipmentID != s.ID {
		t.Fatalf("đơn chưa gắn vận đơn: %+v", got.CurrentShipmentID)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment'
		AND payload->>'shipmentId' = $1`, s.ID.String()); n != 1 {
		t.Fatalf("lệnh tạo = %d, muốn 1", n)
	}

	e.drain()
	s = e.shipment(s.ID)
	if s.Status != fulfillment.StatusAwaitingPickup || s.TrackingCode == "" || s.Fee == nil || *s.Fee != 25000 {
		t.Fatalf("sau lệnh tạo = %+v", s)
	}
	if e.carrier.Parcels() != parcels+1 {
		t.Fatalf("hãng giả có %d kiện, muốn %d", e.carrier.Parcels(), parcels+1)
	}

	e.carrier.Advance(s.TrackingCode, fake.RawPickedUp)
	e.signal(s.TrackingCode, "ev-1")
	e.drain()
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusShipping {
		t.Fatalf("sau picked_up = %s", got.Status)
	}
	e.stockIs(v, [3]int32{5, 0, 2})

	e.carrier.Advance(s.TrackingCode, fake.RawDelivered)
	e.signal(s.TrackingCode, "ev-2")
	e.drain()
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusDelivered {
		t.Fatalf("sau delivered = %s", got.Status)
	}
	if got := e.getOrder(o.ID); got.Status != "delivered" {
		t.Fatalf("đơn = %s, muốn delivered", got.Status)
	}
	e.stockIs(v, [3]int32{3, 0, 0})
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1", s.ID); n != 2 {
		t.Fatalf("sự kiện vận đơn = %d, muốn 2", n)
	}
	// awaiting_pickup (lệnh tạo), shipping, delivered: mỗi lần tiến một sự kiện shipment.status_changed, seq = version.
	if n := e.count(`SELECT count(DISTINCT seq) FROM platform.domain_events
		WHERE topic = 'shipment.status_changed' AND aggregate_id = $1`, s.ID.String()); n != 3 {
		t.Fatalf("shipment.status_changed = %d, muốn 3", n)
	}
}

// timeout_after_create: hãng đã tạo kiện nhưng trả lời quá giờ → lệnh in_doubt, không gửi lại; ResolveInDoubt tra theo
// mã → done; hãng vẫn 1 kiện; vận đơn được điền mã hãng qua lần tra trạng thái sau đó.
func TestShipment_TimeoutAfterCreate(t *testing.T) {
	e := newShipEnv(t)
	_, s := e.createTimedOut()
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment' AND status = 'in_doubt'"); n != 1 {
		t.Fatalf("lệnh in_doubt = %d, muốn 1", n)
	}
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusCreating {
		t.Fatalf("vận đơn = %s, muốn creating", got.Status)
	}
	e.carrier.SetMode(fake.ModeOK)
	done, requeued, still, err := commands.ResolveInDoubt(t.Context(), e.staff.App, e.cmds, 10)
	if err != nil || done != 1 || requeued != 0 || still != 0 {
		t.Fatalf("ResolveInDoubt = %d %d %d %v", done, requeued, still, err)
	}
	e.drain()
	if e.carrier.Parcels() != 1 {
		t.Fatalf("hãng giả có %d kiện, muốn 1", e.carrier.Parcels())
	}
	got := e.shipment(s.ID)
	if got.Status != fulfillment.StatusAwaitingPickup || got.TrackingCode != e.carrier.Tracking(s.Code) {
		t.Fatalf("sau tra lại = %+v", got)
	}
}

// fail_before_create: chắc chắn chưa tạo → not_sent, tự gửi lại lần sau; không có kiện nào cho tới khi hãng nhận.
func TestShipment_FailBeforeCreate(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.carrier.SetMode(fake.ModeFailBeforeCreate)
	e.drain()
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment' AND status = 'pending'"); n != 1 {
		t.Fatalf("lệnh chờ gửi lại = %d, muốn 1", n)
	}
	if e.carrier.Parcels() != 0 || e.shipment(s.ID).Status != fulfillment.StatusCreating {
		t.Fatalf("hãng có %d kiện, vận đơn %s", e.carrier.Parcels(), e.shipment(s.ID).Status)
	}
}

// Vận đơn thứ hai cho cùng đơn khi vận đơn đầu chưa hủy → ErrShipmentExists, không thêm dòng, không thêm lệnh.
func TestShipment_SecondForSameOrder(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	e.mustCreateShipment(o.ID)
	if _, err := e.createShipment(o.ID); !errors.Is(err, fulfillment.ErrShipmentExists) {
		t.Fatalf("vận đơn thứ hai: %v, muốn ErrShipmentExists", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o.ID); n != 1 {
		t.Fatalf("vận đơn = %d, muốn 1", n)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment'"); n != 1 {
		t.Fatalf("lệnh tạo = %d, muốn 1", n)
	}
}

// Unique một phần (tenant_id, order_id) WHERE status <> 'cancelled' là chốt của database: chèn thẳng vận đơn thứ hai
// bằng crm_app → lỗi; mã sai mẫu → lỗi CHECK.
func TestShipment_DatabaseGuards(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	ins := `INSERT INTO fulfillment.shipments
		(tenant_id, seller_id, code, order_id, carrier_key, carrier_account_id, status, cod_amount)
		VALUES ($1, $5, $2, $3, 'fake', $4, 'creating', 0)`
	if err := e.appExec(ins, e.tenant, "E261118ABCDEF", o.ID, e.account, e.seller); err == nil {
		t.Fatal("vận đơn thứ hai cho cùng đơn chèn được")
	}
	o2, _ := e.order(1)
	if err := e.appExec(ins, e.tenant, "X261118ABCDEF", o2.ID, e.account, e.seller); err == nil {
		t.Fatal("mã sai mẫu chèn được")
	}
	if err := e.appExec("UPDATE fulfillment.shipments SET status = 'cancelled', cancel_kind = 'manual' WHERE id = $1",
		s.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.appExec(ins, e.tenant, "E261118ABCDEF", o.ID, e.account, e.seller); err != nil {
		t.Fatalf("vận đơn đầu đã hủy mà không chèn được vận đơn mới: %v", err)
	}
	if err := e.appExec("UPDATE fulfillment.shipment_events SET raw_status = 'x'"); err == nil {
		t.Fatal("shipment_events sửa được bằng crm_app")
	}
}

// Vận đơn của đơn người bán này bằng tài khoản hãng của người bán kia (ADR-29) → ErrOrderNotShippable; đơn không
// đổi, không có vận đơn, không có lệnh.
func TestShipment_AccountOfOtherSeller(t *testing.T) {
	eachSeller(t, func(_ *testing.T, e *shipEnv) {
		o, _ := e.order(1)
		other := e.shop.Account
		if other == e.account {
			other = e.shop.PartnerAccount
		}
		e.expectNotShippable(o, other)
	})
}

// expectNotShippable: tạo vận đơn cho o bằng tài khoản account → ErrOrderNotShippable; đơn không đổi, không có vận đơn.
func (e *shipEnv) expectNotShippable(o order.Order, account uuid.UUID) {
	e.t.Helper()
	err := e.app(func(tx pgx.Tx) error {
		_, err := fulfillment.CreateForOrder(e.t.Context(), tx, o.ID, account, e.actor)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, fulfillment.ErrOrderNotShippable) {
		e.t.Fatalf("tạo vận đơn: %v, muốn ErrOrderNotShippable", err)
	}
	if got := e.getOrder(o.ID); got.CurrentShipmentID != nil || got.Version != o.Version {
		e.t.Errorf("đơn bị đổi: %v, version %d", got.CurrentShipmentID, got.Version)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o.ID); n != 0 {
		e.t.Errorf("%d vận đơn, muốn 0", n)
	}
}

// D-01 (review độc lập M1): tài khoản hãng của kho khác kho giữ hàng của đơn → ErrOrderNotShippable; đơn, tồn không
// đổi, không có vận đơn (hãng lấy ở kho này mà tồn trừ ở kho kia là sai sổ cả hai kho, ADR-18).
func TestShipment_AccountOfOtherWarehouse(t *testing.T) {
	e := newShipEnv(t)
	o, v := e.order(1)
	var other uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		var w2 uuid.UUID
		if err := tx.QueryRow(t.Context(), `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, 'W2', 'Kho phụ') RETURNING id`, e.tenant, e.seller).Scan(&w2); err != nil {
			return err //nolint:wrapcheck // test
		}
		var err error
		other, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.AccountInput{
			SellerID: e.seller, WarehouseID: w2, CarrierKey: shiptest.CarrierKey, Name: "Hãng giả W2", ExternalAccountRef: "w2-ref",
		})
		return err //nolint:wrapcheck // test
	})
	before := e.stock(v)
	e.expectNotShippable(o, other)
	e.stockIs(v, before)
}
