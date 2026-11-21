package order_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// m3ShipmentSQL chèn vận đơn @id của hãng @carrier_key ở trạng thái @status cho đơn @order_id (tài khoản hãng thử của
// người bán, kho của đơn; tạo nếu chưa có) rồi gắn làm vận đơn hiện hành của đơn. Chạy bằng chủ bảng trong ngữ cảnh
// tenant: dữ liệu đọc của danh sách, không qua đường ghi của fulfillment.
const m3ShipmentSQL = `
WITH o AS (
    SELECT tenant_id, seller_id, warehouse_id, id FROM ord.orders WHERE id = @order_id
), acc AS (
    INSERT INTO fulfillment.carrier_accounts (tenant_id, seller_id, carrier_key, name, external_account_ref, warehouse_id)
    SELECT tenant_id, seller_id, @carrier_key::text, 'Hãng thử M3-28', 'm328:' || seller_id || ':' || warehouse_id, warehouse_id
    FROM o
    ON CONFLICT (tenant_id, carrier_key, external_account_ref) DO UPDATE SET name = EXCLUDED.name
    RETURNING id
), s AS (
    INSERT INTO fulfillment.shipments
        (id, tenant_id, seller_id, code, order_id, carrier_key, carrier_account_id, tracking_code, status, cod_amount,
         stuck_since, needs_review)
    SELECT @id::uuid, o.tenant_id, o.seller_id,
           'E' || to_char(now() AT TIME ZONE 'Asia/Ho_Chi_Minh', 'YYMMDD') || upper(substr(md5(@id::uuid::text), 1, 6)),
           o.id, @carrier_key::text, acc.id,
           CASE WHEN @status::text = 'creating' THEN NULL ELSE 'TR' || upper(substr(md5(@id::uuid::text), 1, 10)) END,
           @status::text, @cod::bigint, @stuck_since::timestamptz, @needs_review::boolean
    FROM o, acc
    RETURNING id
)
UPDATE ord.orders SET current_shipment_id = (SELECT id FROM s), packing_status = @packing::text WHERE id = @order_id`

// m3PostSQL ghi một bút toán COD hai vế cho vận đơn @shipment_id: @amount từ ngăn @from sang ngăn @to (tổng bằng 0).
const m3PostSQL = `
WITH t AS (
    INSERT INTO cash.cod_transfers (tenant_id, shipment_id, kind, idempotency_key, actor_type)
    SELECT tenant_id, id, @kind::text, gen_random_uuid()::text, 'system' FROM fulfillment.shipments WHERE id = @shipment_id::uuid
    RETURNING tenant_id, id
)
INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, account, amount, shipment_id)
SELECT t.tenant_id, t.id, x.account, x.amount, @shipment_id::uuid
FROM t, (VALUES (@to::text, @amount::bigint), (@from::text, -@amount::bigint)) AS x (account, amount)`

// m3Ship là vận đơn của một đơn mẫu; COD: các bút toán (kind, từ ngăn, sang ngăn) với số tiền cod của vận đơn.
type m3Ship struct {
	carrier, status, packing string
	stuck                    *time.Time
	review                   bool
	posts                    [][3]string
}

// shipM3 gắn vận đơn sh (mã cố định theo đơn) cho đơn o, ghi bút toán COD; trả mã vận đơn.
func (e *orderEnv) shipM3(o order.Order, sh m3Ship) uuid.UUID {
	e.t.Helper()
	id := uuid.NewSHA1(uuid.NameSpaceOID, o.ID[:])
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(e.t.Context(), m3ShipmentSQL, pgx.NamedArgs{
			"order_id": o.ID, "id": id, "carrier_key": sh.carrier, "status": sh.status, "cod": o.CODAmount,
			"stuck_since": sh.stuck, "needs_review": sh.review, "packing": sh.packing,
		}); err != nil {
			return fmt.Errorf("vận đơn mẫu: %w", err)
		}
		for _, p := range sh.posts {
			if _, err := tx.Exec(e.t.Context(), m3PostSQL, pgx.NamedArgs{
				"shipment_id": id, "kind": p[0], "from": p[1], "to": p[2], "amount": o.CODAmount,
			}); err != nil {
				return fmt.Errorf("bút toán mẫu: %w", err)
			}
		}
		return nil
	})
	return id
}

// setStatusOwner đặt thẳng trạng thái đơn (chủ bảng; dữ liệu đọc của danh sách).
func (e *orderEnv) setStatusOwner(o order.Order, status string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), "UPDATE ord.orders SET status = $2 WHERE id = $1", o.ID, status)
		return err //nolint:wrapcheck // test
	})
}

// Bút toán COD mẫu (kind, từ ngăn, sang ngăn): ghi phải thu, hãng đã thu, hãng đã trả shop.
func postExpected() [3]string  { return [3]string{"expected", "sales_clearing", "to_collect"} }
func postCollected() [3]string { return [3]string{"collected", "to_collect", "carrier_holding"} }
func postRemitted() [3]string  { return [3]string{"remitted", "carrier_holding", "shop_received"} }

// listM3Fixture: 8 đơn confirmed của người bán e.seller (mới nhất trước: h … a), và một đơn đang chuyển của người bán
// kia (không được lẫn vào):
//   - a: đã gói, chưa vận đơn; b: đang lấy, vận đơn fake_a creating (chưa bút toán);
//   - c: fake_a shipping, đã ghi phải thu (to_collect > 0); d: fake_a shipping, kẹt từ stuck, cần xem;
//   - e: đơn delivered, fake_b delivered, hãng đã thu chưa có bảng kê (carrier_holding > 0);
//   - f: đơn delivered, fake_a delivered, shop đã nhận (shop_received > 0);
//   - g: fake_a awaiting_pickup; h: fake_a delivery_failed.
type listM3Fixture struct {
	a, b, c, d, e, f, g, h order.Order
	shipD, shipE           uuid.UUID
	stuck                  time.Time
}

func newListM3Fixture(e *orderEnv) listM3Fixture {
	e.t.Helper()
	v := e.variant(300000, 100)
	mk := func(i int) order.Order {
		return e.createAt(e.m2Input(0, ln(v, 1, 0)), listBase().Add(time.Duration(i)*time.Second))
	}
	x := listM3Fixture{a: mk(1), b: mk(2), c: mk(3), d: mk(4), e: mk(5), f: mk(6), g: mk(7), h: mk(8)}
	x.stuck = listBase().Add(-time.Hour)
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), "UPDATE ord.orders SET packing_status = 'packed' WHERE id = $1", x.a.ID)
		return err //nolint:wrapcheck // test
	})
	e.shipM3(x.b, m3Ship{carrier: "fake_a", status: "creating", packing: "picking"})
	e.shipM3(x.c, m3Ship{carrier: "fake_a", status: "shipping", packing: "packed", posts: [][3]string{postExpected()}})
	x.shipD = e.shipM3(x.d, m3Ship{carrier: "fake_a", status: "shipping", packing: "packed", stuck: &x.stuck, review: true})
	x.shipE = e.shipM3(x.e, m3Ship{
		carrier: "fake_b", status: "delivered", packing: "packed",
		posts: [][3]string{postExpected(), postCollected()},
	})
	e.shipM3(x.f, m3Ship{
		carrier: "fake_a", status: "delivered", packing: "packed",
		posts: [][3]string{postExpected(), postCollected(), postRemitted()},
	})
	e.shipM3(x.g, m3Ship{carrier: "fake_a", status: "awaiting_pickup", packing: "packed"})
	e.shipM3(x.h, m3Ship{carrier: "fake_a", status: "delivery_failed", packing: "packed"})
	e.setStatusOwner(x.e, order.StatusDelivered)
	e.setStatusOwner(x.f, order.StatusDelivered)

	other := *e
	other.seller = e.partner
	pv := other.variant(300000, 10)
	po := other.createAt(other.m2Input(0, ln(pv, 1, 0)), listBase().Add(9*time.Second))
	other.shipM3(po, m3Ship{carrier: "fake_b", status: "shipping", packing: "packed", posts: [][3]string{postExpected()}})
	return x
}

func (e *orderEnv) mustListM3(f queries.OrderFilter) []uuid.UUID {
	e.t.Helper()
	rows := e.listM3Rows(f)
	return rowIDs(rows)
}

// listM3Rows đọc danh sách (không q) của người bán e.seller bằng queries.OrderList với bộ lọc M3.
func (e *orderEnv) listM3Rows(f queries.OrderFilter) []queries.OrderListRow {
	e.t.Helper()
	f.SellerID = e.seller
	var rows []queries.OrderListRow
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		rows, _, err = queries.OrderList(e.t.Context(), tx, f, queries.OrderCursor{}, 100)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("OrderList(%+v): %v", f, err)
	}
	return rows
}

// listM3ByID đọc danh sách không lọc của người bán e.seller, theo mã đơn.
func (e *orderEnv) listM3ByID() map[uuid.UUID]queries.OrderListRow {
	e.t.Helper()
	byID := map[uuid.UUID]queries.OrderListRow{}
	for _, r := range e.listM3Rows(queries.OrderFilter{}) {
		byID[r.ID] = r
	}
	return byID
}

// Lọc theo vận đơn, hãng, tiền COD, đóng gói (kế hoạch M3, Task M3-28): shipmentStatus=shipping → đúng 2;
// codState=holding → đúng đơn đã giao chưa có bảng kê; carrierKey=fake_b → đúng 1; đơn của người bán kia không lẫn vào.
func TestListM3_Filters(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	x := newListM3Fixture(e)
	sameIDs(t, "mặc định (không lẫn người bán kia)", e.mustListM3(queries.OrderFilter{}),
		x.h.ID, x.g.ID, x.f.ID, x.e.ID, x.d.ID, x.c.ID, x.b.ID, x.a.ID)
	sameIDs(t, "shipmentStatus=shipping", e.mustListM3(queries.OrderFilter{ShipmentStatuses: []string{"shipping"}}), x.d.ID, x.c.ID)
	sameIDs(t, "shipmentStatus=creating,delivery_failed",
		e.mustListM3(queries.OrderFilter{ShipmentStatuses: []string{"creating", "delivery_failed"}}), x.h.ID, x.b.ID)
	sameIDs(t, "carrierKey=fake_b", e.mustListM3(queries.OrderFilter{CarrierKey: "fake_b"}), x.e.ID)
	sameIDs(t, "codState=holding", e.mustListM3(queries.OrderFilter{CODState: queries.CODHolding}), x.e.ID)
	sameIDs(t, "codState=collecting", e.mustListM3(queries.OrderFilter{CODState: queries.CODCollecting}), x.c.ID)
	sameIDs(t, "codState=received", e.mustListM3(queries.OrderFilter{CODState: queries.CODReceived}), x.f.ID)
	sameIDs(t, "codState=none", e.mustListM3(queries.OrderFilter{CODState: queries.CODNone}),
		x.h.ID, x.g.ID, x.d.ID, x.b.ID, x.a.ID)
	sameIDs(t, "packingStatus=picking", e.mustListM3(queries.OrderFilter{PackingStatus: "picking"}), x.b.ID)
	sameIDs(t, "hãng và trạng thái", e.mustListM3(queries.OrderFilter{
		CarrierKey:       "fake_a",
		ShipmentStatuses: []string{"delivered"},
	}), x.f.ID)
	if err := e.app(func(tx pgx.Tx) error {
		_, _, err := queries.OrderList(t.Context(), tx, queries.OrderFilter{SellerID: e.seller, CODState: "khac"},
			queries.OrderCursor{}, 10)
		return err //nolint:wrapcheck // test
	}); err == nil {
		t.Error("codState lạ phải lỗi")
	}

	other := *e
	other.seller = e.partner
	if ids := other.mustListM3(queries.OrderFilter{ShipmentStatuses: []string{"shipping"}}); len(ids) != 1 {
		t.Errorf("người bán kia: %d đơn đang chuyển, muốn 1", len(ids))
	}
}

// Mỗi dòng mang vận đơn hiện hành (mã, hãng, mã hãng, trạng thái, kẹt, cần xem) và số dư COD ba ngăn; đơn không vận đơn
// → cả hai nil; vận đơn chưa bút toán → cod nil. Nhãn dòng lấy từ orderview.Label có trục vận đơn.
func TestListM3_RowShipmentAndCOD(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	x := newListM3Fixture(e)
	byID := e.listM3ByID()
	if r := byID[x.a.ID]; r.Shipment != nil || r.COD != nil || r.Label != orderview.LabelPacked {
		t.Errorf("a: %+v %+v %s", r.Shipment, r.COD, r.Label)
	}
	if r := byID[x.b.ID]; r.Shipment == nil || r.Shipment.Status != "creating" || r.Shipment.TrackingCode != "" ||
		r.COD != nil || r.Label != orderview.LabelSending {
		t.Errorf("b: %+v %+v %s", r.Shipment, r.COD, r.Label)
	}
	d := byID[x.d.ID]
	if d.Shipment == nil || d.Shipment.ID != x.shipD || d.Shipment.CarrierKey != "fake_a" || d.Shipment.Code == "" ||
		d.Shipment.TrackingCode == "" || d.Shipment.StuckSince == nil || !d.Shipment.StuckSince.Equal(x.stuck) ||
		!d.Shipment.NeedsReview || d.Label != orderview.LabelShipping {
		t.Errorf("d: %+v %s", d.Shipment, d.Label)
	}
	ev := byID[x.e.ID]
	if ev.Shipment == nil || ev.Shipment.ID != x.shipE || ev.Shipment.CarrierKey != "fake_b" ||
		ev.COD == nil || *ev.COD != (queries.OrderCOD{ToCollect: 0, CarrierHolding: x.e.CODAmount, ShopReceived: 0}) ||
		ev.Label != orderview.LabelDelivered {
		t.Errorf("e: %+v %+v %s", ev.Shipment, ev.COD, ev.Label)
	}
	if r := byID[x.c.ID]; r.COD == nil || r.COD.ToCollect != x.c.CODAmount || r.COD.CarrierHolding != 0 {
		t.Errorf("c: %+v", r.COD)
	}
	if r := byID[x.f.ID]; r.COD == nil || r.COD.ShopReceived != x.f.CODAmount || r.COD.ToCollect != 0 {
		t.Errorf("f: %+v", r.COD)
	}
	for id, want := range map[uuid.UUID]orderview.LabelKey{
		x.g.ID: orderview.LabelAwaitingPickup, x.h.ID: orderview.LabelDeliveryFailed, x.c.ID: orderview.LabelShipping,
	} {
		if byID[id].Label != want {
			t.Errorf("đơn %s nhãn %s, muốn %s", byID[id].Code, byID[id].Label, want)
		}
	}
}

// Mọi nhãn (cả nhãn vận đơn, trước là ErrLabelNotFilterable) lọc được: mỗi nhãn chỉ trả đơn có đúng nhãn đó và mọi
// đơn có trong đúng một nhãn.
func TestListM3_EveryLabelFilterable(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	x := newListM3Fixture(e)
	checkEachLabelOnce(t, e.mustListM3(queries.OrderFilter{}), func(l orderview.LabelKey) []queries.OrderListRow {
		return e.listM3Rows(queries.OrderFilter{Labels: []orderview.LabelKey{l}})
	})
	sameIDs(t, "nhãn sending", e.mustListM3(queries.OrderFilter{Labels: []orderview.LabelKey{orderview.LabelSending}}), x.b.ID)
	sameIDs(t, "nhãn packed", e.mustListM3(queries.OrderFilter{Labels: []orderview.LabelKey{orderview.LabelPacked}}), x.a.ID)
	sameIDs(t, "nhãn chờ thu gom, thất bại", e.mustListM3(queries.OrderFilter{Labels: []orderview.LabelKey{
		orderview.LabelAwaitingPickup, orderview.LabelDeliveryFailed,
	}}), x.h.ID, x.g.ID)
}

// Trường của màn Đóng gói (Task 136): mỗi dòng có trạng thái đóng gói; vận đơn có mã tài khoản hãng, cách nối (mode)
// và giờ in nhãn (nil khi chưa in). Lọc labelPrinted (đã/chưa in nhãn, chỉ đơn có vận đơn) và warehouseId (kho của
// đơn; kho khác → rỗng).
func TestListM3_PackFields(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	x := newListM3Fixture(e)
	printed := listBase().Add(-30 * time.Minute)
	var warehouse uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE fulfillment.shipments SET label_printed_at = $2, mode = 'manual'
			WHERE id = $1`, x.shipD, printed); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(t.Context(), "SELECT warehouse_id FROM ord.orders WHERE id = $1", x.d.ID).Scan(&warehouse)
	})
	byID := e.listM3ByID()
	if r := byID[x.a.ID]; r.PackingStatus != "packed" || r.Shipment != nil {
		t.Errorf("a: packing %q", r.PackingStatus)
	}
	if r := byID[x.b.ID]; r.PackingStatus != "picking" || r.Shipment == nil || r.Shipment.Mode != "api" ||
		r.Shipment.LabelPrintedAt != nil || r.Shipment.CarrierAccountID == uuid.Nil {
		t.Errorf("b: packing %q, vận đơn %+v", r.PackingStatus, r.Shipment)
	}
	if r := byID[x.d.ID]; r.Shipment == nil || r.Shipment.Mode != "manual" || r.Shipment.LabelPrintedAt == nil ||
		!r.Shipment.LabelPrintedAt.Equal(printed) {
		t.Errorf("d: vận đơn %+v", r.Shipment)
	}
	yes, no := true, false
	sameIDs(t, "đã in nhãn", e.mustListM3(queries.OrderFilter{LabelPrinted: &yes}), x.d.ID)
	sameIDs(t, "chưa in nhãn, chờ thu gom", e.mustListM3(queries.OrderFilter{
		LabelPrinted: &no, ShipmentStatuses: []string{"awaiting_pickup"},
	}), x.g.ID)
	sameIDs(t, "chưa in nhãn (chỉ đơn có vận đơn)", e.mustListM3(queries.OrderFilter{LabelPrinted: &no}),
		x.h.ID, x.g.ID, x.f.ID, x.e.ID, x.c.ID, x.b.ID)
	sameIDs(t, "kho của đơn", e.mustListM3(queries.OrderFilter{WarehouseID: &warehouse}),
		x.h.ID, x.g.ID, x.f.ID, x.e.ID, x.d.ID, x.c.ID, x.b.ID, x.a.ID)
	other := uuid.New()
	sameIDs(t, "kho khác", e.mustListM3(queries.OrderFilter{WarehouseID: &other}))
}
