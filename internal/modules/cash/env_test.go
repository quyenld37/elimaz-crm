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
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// cashParallel là số test chạy song song mặc định của gói (Task 475, như order, fulfillment, inventory, httpapi).
const cashParallel = 6

// TestMain đặt số lượt rapid 300 (kế hoạch: quy luật sổ COD 300 lượt) và -parallel thành min(cashParallel,
// GOMAXPROCS), trừ khi người chạy đã đặt.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(300)
	testkit.SetParallel(cashParallel)
	os.Exit(m.Run())
}

// codEnv: database thử, tenant A (shiptest.Shop: tài khoản hãng của hai người bán ở kho MAIN) và mỗi người bán một
// biến thể tồn nhiều để tạo đơn, vận đơn thật (khóa ngoại của sổ COD tới fulfillment.shipments). seller là người bán
// của đơn trong test (mặc định ELIMAZ; eachSeller chạy với từng người), variant là biến thể của người bán đó.
type codEnv struct {
	t        testing.TB
	staff    *testkit.Staff
	tenant   uuid.UUID
	shop     *shiptest.Shop
	seller   uuid.UUID
	variant  uuid.UUID
	variants map[uuid.UUID]uuid.UUID
	user     uuid.UUID
}

func newCODEnv(t testing.TB) *codEnv {
	t.Helper()
	e := &codEnv{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.user = e.staff.User(t, e.tenant, "kho@elimaz.test", "active")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.shop = shiptest.NewShop(t, tx, e.tenant)
		e.variants = map[uuid.UUID]uuid.UUID{}
		for _, s := range []uuid.UUID{e.shop.Seller, e.shop.Partner} {
			e.variants[s] = e.shop.Variant(t, tx, s, 250000, 30000)
		}
		return nil
	})
	e.seller, e.variant = e.shop.Seller, e.variants[e.shop.Seller]
	return e
}

// eachSeller chạy body trên cùng một database lần lượt với từng người bán làm người bán của đơn (KH14, ADR-29).
func eachSeller(t *testing.T, body func(t *testing.T, e *codEnv)) {
	t.Helper()
	base := newCODEnv(t)
	testkit.ForEachSeller(t, []uuid.UUID{base.shop.Seller, base.shop.Partner}, func(t *testing.T, s uuid.UUID) {
		e := *base
		e.t, e.seller, e.variant = t, s, base.variants[s]
		body(t, &e)
	})
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
		o, err := e.shop.Order(e.t.Context(), tx, e.seller, e.user, 0, order.ItemInput{VariantID: e.variant, Qty: 1})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		s, err := e.shop.Shipment(e.t.Context(), tx, e.seller, o.ID, e.user)
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
	var out map[string]int64
	if err := e.app(func(tx pgx.Tx) (err error) {
		out, err = shiptest.CODBalances(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
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
