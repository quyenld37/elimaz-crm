package cash_test

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// TestMain đặt số lượt rapid 300 (kế hoạch: quy luật sổ COD 300 lượt), trừ khi người chạy đã đặt.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(300)
	os.Exit(m.Run())
}

// codEnv: database thử, tenant A (invtest.Shop), tài khoản hãng ở kho MAIN, một khách và một biến thể tồn nhiều để tạo
// đơn, vận đơn thật (khóa ngoại của sổ COD tới fulfillment.shipments).
type codEnv struct {
	t        testing.TB
	staff    *testkit.Staff
	tenant   uuid.UUID
	seller   uuid.UUID
	account  uuid.UUID
	variant  uuid.UUID
	customer uuid.UUID
	user     uuid.UUID
}

func newCODEnv(t testing.TB) *codEnv {
	t.Helper()
	e := &codEnv{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.user = e.staff.User(t, e.tenant, "kho@elimaz.test", "active")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		shop := invtest.NewShop(t, tx, e.tenant)
		e.seller = shop.Seller
		e.variant = shop.Variant(t, tx, shop.Seller, 250000, 30000)
		in := fulfillment.AccountInput{CarrierKey: "fake", ExternalAccountRef: "fake-1", Name: "Hãng giả"}
		in.SellerID, in.WarehouseID = shop.Seller, shop.Warehouse
		var err error
		if e.account, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, in); err != nil {
			return err //nolint:wrapcheck // test
		}
		e.customer, err = customer.Create(t.Context(), tx, "Chị Lan")
		return err //nolint:wrapcheck // test
	})
	return e
}

func (e *codEnv) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

// execApp chạy câu ghi q bằng crm_app trong tenant, trả lỗi của cả transaction.
func (e *codEnv) execApp(q string) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), q)
		return err //nolint:wrapcheck // test
	})
}

// pgCode là SQLSTATE của err ("" nếu không phải lỗi PostgreSQL).
func pgCode(err error) string {
	if pg, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pg.Code
	}
	return ""
}

// shipment tạo đơn 1 cái và vận đơn thật cho đơn đó (crm_app), rồi (chủ bảng) đặt cod_amount = cod; trả mã vận đơn.
func (e *codEnv) shipment(cod int64) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.app(func(tx pgx.Tx) error {
		orderID, err := e.order(tx)
		if err != nil {
			return err
		}
		s, err := fulfillment.CreateForOrder(e.t.Context(), tx, orderID, e.account,
			fulfillment.Actor{TenantID: e.tenant, UserID: e.user})
		id = s.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("tạo vận đơn: %v", err)
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), "UPDATE fulfillment.shipments SET cod_amount = $2 WHERE id = $1", id, cod)
		return err //nolint:wrapcheck // test
	})
	return id
}

// order tạo đơn tay 1 cái của khách e.customer trong tx; trả mã đơn.
func (e *codEnv) order(tx pgx.Tx) (uuid.UUID, error) {
	in := order.CreateManualInput{IdempotencyKey: uuid.New(), SellerID: e.seller, CustomerID: e.customer}
	in.Items = []order.ItemInput{{VariantID: e.variant, Qty: 1}}
	in.ShipAddress = order.ShipAddress{Name: "Chị Lan", Phone: logging.Phone("0901234567"), Line: "12 Lê Lợi"}
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
	o, err := w.CreateManual(e.t.Context(), tx, in, order.Actor{TenantID: e.tenant, UserID: e.user})
	return o.ID, err //nolint:wrapcheck // test
}

// event dựng sự kiện shipment.status_changed của vận đơn id với trạng thái st.
func (e *codEnv) event(eventID, id uuid.UUID, st fulfillment.Status) events.StoredEvent {
	e.t.Helper()
	p, err := json.Marshal(fulfillment.StatusChanged{ShipmentID: id, Status: st, CODAmount: 1})
	if err != nil {
		e.t.Fatal(err)
	}
	return events.StoredEvent{
		ID: eventID, TenantID: e.tenant, AggregateType: "shipment", AggregateID: id.String(), Seq: 2,
		Topic: fulfillment.TopicShipmentStatusChanged, Payload: p,
	}
}

// handle giao sự kiện cho consumer cash trong một transaction crm_app của tenant.
func (e *codEnv) handle(ev events.StoredEvent) error {
	return e.app(func(tx pgx.Tx) error { return cash.Consumer{}.Handle(e.t.Context(), ev, tx) })
}

func (e *codEnv) mustHandle(ev events.StoredEvent) {
	e.t.Helper()
	if err := e.handle(ev); err != nil {
		e.t.Fatalf("Handle %s: %v", ev.ID, err)
	}
}

// balances đọc số dư từng ngăn của vận đơn id qua cash.cod_balances_v (crm_app); ngăn số dư 0 vẫn có mặt.
func (e *codEnv) balances(id uuid.UUID) map[string]int64 {
	e.t.Helper()
	out := map[string]int64{}
	if err := e.app(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), "SELECT account, balance FROM cash.cod_balances_v WHERE shipment_id = $1", id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var a string
			var b int64
			if err := rows.Scan(&a, &b); err != nil {
				return err //nolint:wrapcheck // test
			}
			out[a] = b
		}
		return rows.Err()
	}); err != nil {
		e.t.Fatalf("đọc số dư: %v", err)
	}
	return out
}

// count chạy câu đếm q bằng crm_app trong tenant.
func (e *codEnv) count(q string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.app(func(tx pgx.Tx) error { return tx.QueryRow(e.t.Context(), q, args...).Scan(&n) }); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	return n
}

// total là tổng mọi dòng sổ COD của tenant.
func (e *codEnv) total() int64 {
	e.t.Helper()
	return e.count("SELECT coalesce(sum(amount), 0)::bigint FROM cash.cod_ledger_entries")
}
