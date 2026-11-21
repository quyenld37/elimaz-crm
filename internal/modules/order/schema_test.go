package order_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// m1Order là một đơn ghi bằng đúng các cột câu InsertOrder, InsertOrderItem, InsertStatusHistory của M1 (Task M1-30)
// ghi trước 0204 (Writer.CreateManual của bản này ghi thêm warehouse_id nên không chạy được trên schema của M1).
type m1Order struct {
	status, phone string
	fee           int64
	lines         []m1Line
	reserveAt     uuid.UUID // uuid.Nil: không có dòng giữ hàng
}

type m1Line struct {
	variant uuid.UUID
	qty     int32
	price   int64
}

// insertM1Order ghi đơn kiểu M1 (mã từ ord.order_code_seq như order.nextCode) bằng chủ bảng; trả mã đơn và mã.
func insertM1Order(t *testing.T, tx pgx.Tx, seller, cust uuid.UUID, o m1Order) (uuid.UUID, string) {
	t.Helper()
	ctx := t.Context()
	var sub int64
	for _, l := range o.lines {
		sub += int64(l.qty) * l.price
	}
	id := uuid.Must(uuid.NewV7())
	var code string
	if err := tx.QueryRow(ctx, `SELECT ('DH' || lpad(nextval('ord.order_code_seq')::text, 7, '0'))::text`).
		Scan(&code); err != nil {
		t.Fatal(err)
	}
	addr := `{"name": "Chị Lan", "phone": "` + o.phone + `", "line": "12 Lê Lợi"}`
	var created time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO ord.orders
		(id, tenant_id, seller_id, code, customer_id, channel, idempotency_key, status, ship_address, subtotal,
		 discount_total, shipping_fee_charged, cod_amount, grand_total, created_by, version)
		VALUES ($1, current_setting('app.tenant_id')::uuid, $2, $3, $4, 'manual', $5, $6, $7, $8::bigint, 0, $9::bigint,
		        $8::bigint + $9::bigint, $8::bigint + $9::bigint, NULL, 2) RETURNING created_at`,
		id, seller, code, cust, uuid.New(), o.status, addr, sub, o.fee).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ord.order_status_history
		(tenant_id, order_id, from_status, to_status, actor_type, occurred_at)
		VALUES (current_setting('app.tenant_id')::uuid, $1, NULL, $2, 'staff', $3)`, id, o.status, created); err != nil {
		t.Fatal(err)
	}
	for i, l := range o.lines {
		itemID := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO ord.order_items
			(id, tenant_id, seller_id, order_id, position, variant_id, sku_snapshot, name_snapshot, quantity, unit_price,
			 line_discount, allocated_order_discount, net_amount)
			VALUES ($1, current_setting('app.tenant_id')::uuid, $2, $3, $4, $5, 'SKU', 'Áo', $6, $7, 0, 0, $6::int * $7::bigint)`,
			itemID, seller, id, i+1, l.variant, l.qty, l.price); err != nil {
			t.Fatal(err)
		}
		if o.reserveAt != uuid.Nil {
			if _, err := tx.Exec(ctx, `INSERT INTO inventory.stock_reservations
				(tenant_id, order_item_id, variant_id, warehouse_id, quantity)
				VALUES (current_setting('app.tenant_id')::uuid, $1, $2, $3, $4)`,
				itemID, l.variant, o.reserveAt, l.qty); err != nil {
				t.Fatal(err)
			}
		}
	}
	return id, code
}

type m1Seeded struct {
	shop               *invtest.Shop
	w2                 uuid.UUID
	o1, o2             uuid.UUID
	code1, code2       string
	positions          map[uuid.UUID][]int32
	created1, created2 time.Time
}

// seedM1 dựng (trên schema trước 0204) shop, kho thứ hai W2, hai đơn kiểu M1: o1 confirmed giữ hàng ở W2 (SĐT hợp lệ có
// dấu chấm), o2 delivered không có dòng giữ hàng (SĐT không chuẩn hóa được).
func seedM1(t *testing.T, owner, app *db.DB, tenant uuid.UUID) m1Seeded {
	t.Helper()
	ctx := t.Context()
	var s m1Seeded
	var v1, v2, v3 uuid.UUID
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		s.shop = invtest.NewShop(t, tx, tenant)
		v1 = s.shop.Variant(t, tx, s.shop.Seller, 250000, 0)
		v2 = s.shop.Variant(t, tx, s.shop.Seller, 180000, 0)
		v3 = s.shop.Variant(t, tx, s.shop.Seller, 320000, 5)
		if err := tx.QueryRow(ctx, `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, 'W2', 'Kho hai') RETURNING id`, tenant, s.shop.Seller).Scan(&s.w2); err != nil {
			return err //nolint:wrapcheck // test
		}
		invtest.SeedStock(t, tx, v1, s.w2, 10)
		invtest.SeedStock(t, tx, v2, s.w2, 10)
		return nil
	})
	var cust uuid.UUID
	testkit.InTenant(t, app, tenant, func(tx pgx.Tx) error {
		var err error
		cust, err = customer.Create(ctx, tx, "Chị Lan")
		return err //nolint:wrapcheck // test
	})
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		s.o1, s.code1 = insertM1Order(t, tx, s.shop.Seller, cust, m1Order{
			status: "confirmed", phone: "0901.234.567", fee: 30000, reserveAt: s.w2,
			lines: []m1Line{{v1, 2, 250000}, {v2, 1, 180000}},
		})
		s.o2, s.code2 = insertM1Order(t, tx, s.shop.Seller, cust, m1Order{
			status: "delivered", phone: "12345678", lines: []m1Line{{v3, 1, 320000}},
		})
		return nil
	})
	s.positions = map[uuid.UUID][]int32{}
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		for _, id := range []uuid.UUID{s.o1, s.o2} {
			rows, err := tx.Query(ctx, "SELECT position FROM ord.order_items WHERE order_id = $1 ORDER BY id", id)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			if s.positions[id], err = pgx.CollectRows(rows, pgx.RowTo[int32]); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		if err := tx.QueryRow(ctx, "SELECT created_at FROM ord.orders WHERE id = $1", s.o1).Scan(&s.created1); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, "SELECT created_at FROM ord.orders WHERE id = $1", s.o2).Scan(&s.created2)
	})
	return s
}

// Mở rộng bảng của M1: đơn có sẵn trước 0204 giữ mã, vị trí dòng; được điền kho, SĐT, confirmed_at; ràng buộc mới và
// trigger tổng tiền đạt; orders_v giữ 11 cột đầu; sequence mã đơn không bị đặt lại; CreateManual sau migrate ra mã kế.
func TestSchemaExtendsM1Orders(t *testing.T) {
	_, ownerURL, appURL := testkit.CreateDatabase(t)
	testkit.MigrateBefore(t, ownerURL, "0204")
	owner, app := testkit.OpenDB(t, ownerURL), testkit.OpenDB(t, appURL)
	ctx := t.Context()

	var tenant uuid.UUID
	if err := owner.Pool.QueryRow(ctx, "INSERT INTO platform.tenants (name) VALUES ('A') RETURNING id").
		Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	s := seedM1(t, owner, app, tenant)
	if s.code1 != "DH0000001" || s.code2 != "DH0000002" {
		t.Fatalf("mã đơn M1 %q, %q", s.code1, s.code2)
	}
	before := testkit.Columns(t, owner, "ord", "orders_v")

	if _, err := db.Migrate(ctx, ownerURL); err != nil {
		t.Fatalf("migrate 0204, 0205: %v", err)
	}

	checkBackfilledM1(t, owner, tenant, s)
	checkPositionsAndTotals(t, owner, tenant, s)
	checkOrdersViewAndSeq(t, owner, tenant, s, before)
	checkCreateAfter(t, app, tenant, s)
}

// m1Got là các cột M2 của một đơn M1 sau migrate.
type m1Got struct {
	code, pending, phone, reversed string
	warehouse                      uuid.UUID
	confirmed                      *time.Time
	lineDisc, orderDisc            int64
}

// checkBackfilledM1: mã giữ; o1 kho W2 (dòng giữ hàng), SĐT chuẩn hóa như customer.NormalizePhone; o2 kho MAIN, SĐT
// không chuẩn hóa được → ”; confirmed_at = created_at; pending_reason ”.
func checkBackfilledM1(t *testing.T, owner *db.DB, tenant uuid.UUID, s m1Seeded) {
	t.Helper()
	read := func(id uuid.UUID) m1Got {
		var g m1Got
		testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `SELECT code, pending_reason, ship_phone, ship_phone_reversed, warehouse_id,
				confirmed_at, line_discount_total, order_discount FROM ord.orders WHERE id = $1`, id).
				Scan(&g.code, &g.pending, &g.phone, &g.reversed, &g.warehouse, &g.confirmed, &g.lineDisc, &g.orderDisc)
		})
		return g
	}
	wantPhone, err := customer.NormalizePhone("0901.234.567")
	if err != nil {
		t.Fatal(err)
	}
	g1, g2 := read(s.o1), read(s.o2)
	if g1.code != s.code1 || g1.warehouse != s.w2 || g1.pending != "" || g1.phone != string(wantPhone) ||
		g1.reversed != customer.ReverseDigits(wantPhone) || g1.confirmed == nil || !g1.confirmed.Equal(s.created1) ||
		g1.lineDisc != 0 || g1.orderDisc != 0 {
		t.Errorf("đơn 1 sau migrate: %+v (W2 %s)", g1, s.w2)
	}
	if g2.code != s.code2 || g2.warehouse != s.shop.Warehouse || g2.phone != "" || g2.reversed != "" ||
		g2.confirmed == nil || !g2.confirmed.Equal(s.created2) || g2.pending != "" {
		t.Errorf("đơn 2 sau migrate: %+v (MAIN %s)", g2, s.shop.Warehouse)
	}
}

// checkPositionsAndTotals: position của dòng đơn giữ nguyên; chạm mọi đơn và commit (trigger tổng tiền đạt); không còn
// ràng buộc NOT VALID ở ord, inventory.
func checkPositionsAndTotals(t *testing.T, owner *db.DB, tenant uuid.UUID, s m1Seeded) {
	t.Helper()
	ctx := t.Context()
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		for id, want := range s.positions {
			rows, err := tx.Query(ctx, "SELECT position FROM ord.order_items WHERE order_id = $1 ORDER BY id", id)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			pos, err := pgx.CollectRows(rows, pgx.RowTo[int32])
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			if !slices.Equal(pos, want) {
				t.Errorf("position của đơn %s đổi: %v → %v", id, want, pos)
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE ord.order_items SET updated_at = now()"); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, "UPDATE ord.orders SET updated_at = now()")
		return err //nolint:wrapcheck // test
	})
	if n := countOwner(t, owner, `SELECT count(*) FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace
		WHERE n.nspname IN ('ord', 'inventory') AND NOT c.convalidated`); n != 0 {
		t.Errorf("còn %d ràng buộc NOT VALID", n)
	}
}

// checkOrdersViewAndSeq: orders_v giữ 11 cột của M1 ở vị trí 1–11 cùng tên, kiểu, tổng 38 cột (M3 Task 119 nối
// ship_phone_reversed); order_code_seq không bị đặt lại; dòng tạo đơn M1 (from_status NULL) đọc qua view là ”.
func checkOrdersViewAndSeq(t *testing.T, owner *db.DB, tenant uuid.UUID, s m1Seeded, before []testkit.Column) {
	t.Helper()
	after := testkit.Columns(t, owner, "ord", "orders_v")
	if len(before) != 11 || len(after) != 38 {
		t.Fatalf("orders_v trước %d cột, sau %d cột", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("cột %d của orders_v đổi: %v → %v", i+1, before[i], after[i])
		}
	}
	if n := countOwner(t, owner, "SELECT last_value::int FROM ord.order_code_seq"); n != 2 {
		t.Errorf("order_code_seq last_value %d, mong 2", n)
	}
	var from string
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT from_status FROM ord.order_status_history_v WHERE order_id = $1", s.o1).
			Scan(&from)
	})
	if from != "" {
		t.Errorf("from_status của dòng tạo đơn M1 qua view %q, mong ''", from)
	}
}

func countOwner(t *testing.T, owner *db.DB, q string) int {
	t.Helper()
	var n int
	if err := owner.Pool.QueryRow(t.Context(), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// checkCreateAfter: Writer.CreateManual của M1 chạy sau migrate, ra DH0000003 (cùng sequence), ghi kho giữ hàng, SĐT
// chuẩn hóa, confirmed_at; order.Get đọc đủ trường M2.
func checkCreateAfter(t *testing.T, app *db.DB, tenant uuid.UUID, s m1Seeded) {
	t.Helper()
	ctx := t.Context()
	var v uuid.UUID
	testkit.InTenant(t, app, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT variant_id FROM inventory.stock_available_v WHERE available > 0 LIMIT 1").Scan(&v)
	})
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
	testkit.InTenant(t, app, tenant, func(tx pgx.Tx) error {
		c, err := customer.Create(ctx, tx, "Anh Bình")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		o, err := w.CreateManual(ctx, tx, order.CreateManualInput{
			IdempotencyKey: uuid.New(), SellerID: s.shop.Seller, CustomerID: c,
			Items:       []order.ItemInput{{VariantID: v, Qty: 1}},
			ShipAddress: order.ShipAddress{Name: "Anh Bình", Phone: logging.Phone("+84912345678"), Line: "1 Hai Bà Trưng"},
		}, order.Actor{TenantID: tenant})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if o.Code != "DH0000003" {
			t.Errorf("mã đơn sau migrate %q, mong DH0000003", o.Code)
		}
		got, err := order.Get(ctx, tx, s.shop.Seller, o.ID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if got.WarehouseID != s.shop.Warehouse || got.ShipPhone != "+84912345678" || got.ConfirmedAt == nil ||
			got.PendingReason != "" || got.PaymentStatus != "unpaid" || got.OrderType != "ship" {
			t.Errorf("đơn mới sau migrate: kho %s SĐT %q xác nhận %v chờ %q tiền %q loại %q", got.WarehouseID,
				got.ShipPhone, got.ConfirmedAt, got.PendingReason, got.PaymentStatus, got.OrderType)
		}
		return nil
	})
}

// schemaEnv: database đủ migration; tenant A có danh mục SeedCatalog (S1: V1 250.000, V2 180.000, V3 320.000; S2: W1)
// và một khách; tenant B có một khách.
type schemaEnv struct {
	staff      *testkit.Staff
	a, b       uuid.UUID
	cat        testkit.Catalog
	custA      uuid.UUID
	codeSerial int
}

func newSchemaEnv(t *testing.T) *schemaEnv {
	t.Helper()
	e := &schemaEnv{staff: testkit.OpenStaff(t), codeSerial: 9000000}
	e.a, e.b = e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	e.cat = testkit.SeedCatalog(t, e.staff.Owner, e.a, testkit.SeedOptions{Variants: []testkit.SeedVariant{
		{SKU: "V1", Price: 250000},
		{SKU: "V2", Price: 180000},
		{SKU: "V3", Price: 320000},
		{Seller: 1, SKU: "W1", Price: 100000},
	}})
	e.custA = e.newCustomer(t)
	return e
}

// draftOrder là một đơn ghi thẳng bằng SQL để thử ràng buộc của bảng (crm_app, tenant A).
type draftOrder struct {
	status, pending                                     string
	draftExpires                                        *time.Time
	subtotal, lineDisc, orderDisc, discount, fee, grand int64
	cod                                                 int64
	items                                               []draftItem
}

type draftItem struct {
	sku                         string
	qty                         int32
	price, lineDisc, alloc, net int64
}

func (e *schemaEnv) insert(t *testing.T, o draftOrder) (uuid.UUID, error) {
	t.Helper()
	ctx := t.Context()
	if o.status == "" {
		o.status = "confirmed"
	}
	e.codeSerial++
	code := fmt.Sprintf("DH%07d", e.codeSerial)
	id := uuid.Must(uuid.NewV7())
	err := tenancy.WithTenant(ctx, e.staff.App, e.a, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO ord.orders
			(id, tenant_id, seller_id, code, customer_id, channel, idempotency_key, status, pending_reason,
			 draft_expires_at, ship_address, subtotal, line_discount_total, order_discount, discount_total,
			 shipping_fee_charged, grand_total, cod_amount, warehouse_id)
			VALUES ($1, $2, $3, $4, $5, 'manual', $6, $7, $8, $9, '{}', $10, $11, $12, $13, $14, $15, $16, $17)`,
			id, e.a, e.cat.Sellers[0], code, e.custA, uuid.New(), o.status, o.pending, o.draftExpires, o.subtotal,
			o.lineDisc, o.orderDisc, o.discount, o.fee, o.grand, o.cod, e.cat.Warehouse); err != nil {
			return err //nolint:wrapcheck // test
		}
		for i, it := range o.items {
			if _, err := tx.Exec(ctx, `INSERT INTO ord.order_items
				(tenant_id, seller_id, order_id, position, variant_id, sku_snapshot, name_snapshot, quantity, unit_price,
				 line_discount, allocated_order_discount, net_amount)
				VALUES ($1, $2, $3, $4, $5, $6, 'Áo', $7, $8, $9, $10, $11)`,
				e.a, e.cat.Sellers[0], id, i+1, e.cat.Variants[it.sku], it.sku, it.qty, it.price, it.lineDisc, it.alloc,
				it.net); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return id, err //nolint:wrapcheck // test
}

// newCustomer tạo một khách của tenant A bằng customer.Create (crm_app).
func (e *schemaEnv) newCustomer(t *testing.T) (id uuid.UUID) {
	t.Helper()
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.a, func(tx pgx.Tx) (err error) {
		id, err = customer.Create(t.Context(), tx, "Khách của đơn")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *schemaEnv) mustInsert(t *testing.T, o draftOrder) uuid.UUID {
	t.Helper()
	id, err := e.insert(t, o)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// baseOrder: subtotal 680.000, giảm dòng 20.000, phí 30.000, tổng 690.000, COD 690.000.
func baseOrder() draftOrder {
	return draftOrder{
		subtotal: 680000, lineDisc: 20000, discount: 20000, fee: 30000, grand: 690000, cod: 690000,
		items: []draftItem{
			{sku: "V1", qty: 2, price: 250000, lineDisc: 20000, net: 480000},
			{sku: "V2", qty: 1, price: 180000, net: 180000},
		},
	}
}

func TestSchemaOrderMoneyChecks(t *testing.T) {
	e := newSchemaEnv(t)
	if _, err := e.insert(t, baseOrder()); err != nil {
		t.Fatalf("đơn đúng tổng: %v", err)
	}
	o := baseOrder()
	o.grand = 690001
	if _, err := e.insert(t, o); pgCode(err) != "23514" {
		t.Errorf("grand_total 690001: %v, mong 23514", err)
	}
	o = baseOrder()
	o.cod = 690000 - 1
	if _, err := e.insert(t, o); pgCode(err) != "23514" {
		t.Errorf("cod_amount khác grand_total − prepaid_total: %v, mong 23514", err)
	}
	o = baseOrder()
	o.items[0].net = 480001
	if _, err := e.insert(t, o); pgCode(err) != "23514" {
		t.Errorf("net_amount 480001: %v, mong 23514", err)
	}
	o = baseOrder()
	o.items[1].lineDisc, o.items[1].net = 180001, 0
	if _, err := e.insert(t, o); pgCode(err) != "23514" {
		t.Errorf("line_discount vượt tiền dòng: %v, mong 23514", err)
	}
	// Tổng giảm theo dòng của đơn khác Σ line_discount → lỗi lúc commit.
	o = baseOrder()
	o.lineDisc, o.orderDisc = 0, 20000
	if _, err := e.insert(t, o); pgCode(err) != "23514" || !strings.Contains(err.Error(), "line_discount_total") {
		t.Errorf("line_discount_total lệch: %v, mong 23514 nêu line_discount_total", err)
	}
	// subtotal lệch Σ quantity × unit_price → lỗi lúc commit.
	o = baseOrder()
	o.subtotal, o.grand, o.cod = 680001, 690001, 690001
	if _, err := e.insert(t, o); pgCode(err) != "23514" || !strings.Contains(err.Error(), "subtotal") {
		t.Errorf("subtotal lệch: %v, mong 23514 nêu subtotal", err)
	}
}

// Giảm cả đơn 100.001 chia ba dòng: đúng tổng thì commit được, lệch 1 đồng thì commit lỗi nêu allocated_order_discount.
func TestSchemaAllocatedDiscountAtCommit(t *testing.T) {
	e := newSchemaEnv(t)
	order3 := func(first int64) draftOrder {
		return draftOrder{
			subtotal: 750000, orderDisc: 100001, discount: 100001, grand: 649999, cod: 649999,
			items: []draftItem{
				{sku: "V1", qty: 1, price: 250000, alloc: first, net: 250000 - first},
				{sku: "V2", qty: 1, price: 180000, alloc: 18000, net: 162000},
				{sku: "V3", qty: 1, price: 320000, alloc: 32000, net: 288000},
			},
		}
	}
	if _, err := e.insert(t, order3(50001)); err != nil {
		t.Fatalf("phân bổ đúng: %v", err)
	}
	_, err := e.insert(t, order3(50000))
	if pgCode(err) != "23514" || !strings.Contains(err.Error(), "allocated_order_discount") {
		t.Errorf("phân bổ thiếu 1 đồng: %v, mong 23514 nêu allocated_order_discount", err)
	}
}

func TestSchemaOrderItemRules(t *testing.T) {
	e := newSchemaEnv(t)
	// Dòng của đơn người bán S1 trỏ biến thể của S2 → khóa ngoại ghép (KH14, ADR-29).
	o := draftOrder{
		subtotal: 100000, grand: 100000, cod: 100000,
		items: []draftItem{{sku: "W1", qty: 1, price: 100000, net: 100000}},
	}
	if _, err := e.insert(t, o); pgCode(err) != "23503" {
		t.Errorf("biến thể của người bán khác: %v, mong 23503", err)
	}
	// Hai dòng cùng biến thể → 23505.
	o = draftOrder{subtotal: 500000, grand: 500000, cod: 500000, items: []draftItem{
		{sku: "V1", qty: 1, price: 250000, net: 250000}, {sku: "V1", qty: 1, price: 250000, net: 250000},
	}}
	if _, err := e.insert(t, o); pgCode(err) != "23505" {
		t.Errorf("hai dòng cùng biến thể: %v, mong 23505", err)
	}
}

func TestSchemaStatusRules(t *testing.T) {
	e := newSchemaEnv(t)
	draft := baseOrder()
	draft.status = "draft"
	if _, err := e.insert(t, draft); pgCode(err) != "23514" {
		t.Errorf("đơn nháp không có draft_expires_at: %v, mong 23514", err)
	}
	exp := time.Now().Add(time.Hour)
	draft.draftExpires = &exp
	draft.pending = "discount_approval"
	if _, err := e.insert(t, draft); err != nil {
		t.Errorf("đơn nháp có hạn, chờ duyệt giảm: %v", err)
	}
	c := baseOrder()
	c.draftExpires = &exp
	if _, err := e.insert(t, c); pgCode(err) != "23514" {
		t.Errorf("đơn confirmed có draft_expires_at: %v, mong 23514", err)
	}
	c = baseOrder()
	c.pending = "awaiting_customer"
	if _, err := e.insert(t, c); pgCode(err) != "23514" {
		t.Errorf("đơn confirmed có pending_reason: %v, mong 23514", err)
	}
}

func TestSchemaAppendOnly(t *testing.T) {
	e := newSchemaEnv(t)
	ctx := t.Context()
	id := e.mustInsert(t, baseOrder())
	testkit.InTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO ord.order_status_history (tenant_id, order_id, from_status, to_status,
			actor_type, occurred_at) VALUES ($1, $2, '', 'confirmed', 'service', now())`, e.a, id); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, `INSERT INTO ord.order_notes (tenant_id, order_id, body, author_id)
			VALUES ($1, $2, 'gọi lại chiều', $3)`, e.a, id, uuid.New())
		return err //nolint:wrapcheck // test
	})
	e.staff.CheckOwnerAppendOnly(t, e.a, "UPDATE ord.order_status_history SET to_status = 'cancelled'",
		"DELETE FROM ord.order_notes", "UPDATE ord.order_notes SET body = 'sửa'")
}

func TestSchemaReturns(t *testing.T) {
	e := newSchemaEnv(t)
	ctx := t.Context()
	id := e.mustInsert(t, baseOrder())
	ship := uuid.New()
	ret := func(code, status string) error {
		return tenancy.WithTenant(ctx, e.staff.App, e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO ord.returns (tenant_id, code, order_id, shipment_id, kind, status)
				VALUES ($1, $2, $3, $4, 'carrier_return', $5)`, e.a, code, id, ship, status)
			return err //nolint:wrapcheck // test
		})
	}
	if err := ret("TH9000001", "cancelled"); err != nil {
		t.Fatal(err)
	}
	if err := ret("TH9000002", "requested"); err != nil {
		t.Fatalf("phiếu hoàn thứ nhất còn hiệu lực: %v", err)
	}
	if err := ret("TH9000003", "requested"); pgCode(err) != "23505" {
		t.Errorf("hai phiếu hoàn cùng vận đơn còn hiệu lực: %v, mong 23505", err)
	}
	if err := ret("XX1", "requested"); pgCode(err) != "23514" {
		t.Errorf("mã phiếu sai mẫu: %v, mong 23514", err)
	}
}

// Hằng Go của loại partial_delivery, trạng thái received (Q-T91-4) khớp CHECK của ord.returns.
func TestSchemaReturnM3Constants(t *testing.T) {
	e := newSchemaEnv(t)
	ctx := t.Context()
	id := e.mustInsert(t, baseOrder())
	if err := tenancy.WithTenant(ctx, e.staff.App, e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO ord.returns (tenant_id, code, order_id, shipment_id, kind, status)
			VALUES ($1, 'TH9100001', $2, $3, $4, $5)`, e.a, id, uuid.New(), order.ReturnPartialDelivery,
			order.ReturnStatusReceived)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("phiếu %s/%s: %v", order.ReturnPartialDelivery, order.ReturnStatusReceived, err)
	}
}

func TestNextReturnCode(t *testing.T) {
	e := newSchemaEnv(t)
	var codes []string
	for range 2 {
		testkit.InTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
			c, err := order.NextReturnCode(t.Context(), tx)
			codes = append(codes, c)
			return err //nolint:wrapcheck // test
		})
	}
	if strings.Join(codes, ",") != "TH0000001,TH0000002" {
		t.Errorf("mã phiếu trả %v, mong TH0000001, TH0000002", codes)
	}
}

// Không câu SQL nào của ứng dụng ngoài queries/ord/code.sql (mã sqlc sinh từ nó, chú thích ở code.go) gọi nextval của sequence mã đơn,
// mã phiếu trả (R2: một chỗ sinh mã).
func TestCodeSequenceSingleSource(t *testing.T) {
	root, err := os.OpenRoot(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	allowed := map[string]bool{
		"queries/ord/code.sql": true, "internal/modules/order/internal/store/code.sql.go": true,
		"internal/modules/order/code.go": true, // chú thích của nextCode
	}
	var bad []string
	fsys := root.FS()
	for _, dir := range []string{"queries", "internal", "cmd"} {
		err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasSuffix(p, "_test.go") ||
				(!strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".sql")) {
				return err
			}
			b, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			if (strings.Contains(string(b), "nextval('ord.order_code_seq')") ||
				strings.Contains(string(b), "nextval('ord.return_code_seq')")) && !allowed[p] {
				bad = append(bad, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(bad) != 0 {
		t.Errorf("gọi nextval mã đơn, mã phiếu trả ngoài queries/ord/code.sql: %v", bad)
	}
}

func TestSchemaTenantIsolation(t *testing.T) {
	e := newSchemaEnv(t)
	id, ctx := e.mustInsert(t, baseOrder()), t.Context()
	testkit.InTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
		if err := order.EnsureDefaults(ctx, tx, e.a); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ord.order_notes (tenant_id, order_id, body, author_id)
			VALUES ($1, $2, 'n', $3)`, e.a, id, uuid.New()); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ord.returns (tenant_id, code, order_id, kind)
			VALUES ($1, 'TH9000009', $2, 'customer_return')`, e.a, id); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, `INSERT INTO ord.order_status_history (tenant_id, order_id, to_status, actor_type,
			occurred_at) VALUES ($1, $2, 'confirmed', 'staff', now())`, e.a, id)
		return err //nolint:wrapcheck // test
	})
	rels := []string{
		"ord.orders_v", "ord.order_items_v", "ord.order_status_history_v", "ord.returns_v", "ord.traffic_sources_v",
		"ord.order_notes", "ord.order_settings", "ord.returns",
	}
	for _, rel := range rels {
		for _, c := range []struct {
			tenant uuid.UUID
			want   bool
		}{{e.a, true}, {e.b, false}} {
			var n int
			testkit.InTenant(t, e.staff.App, c.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, "SELECT count(*) FROM "+rel+" WHERE tenant_id = $1", e.a).Scan(&n)
			})
			if (n > 0) != c.want {
				t.Errorf("%s nhìn từ %s: %d dòng của A", rel, c.tenant, n)
			}
		}
	}
}

func TestSchemaViewsAndRules(t *testing.T) {
	e := newSchemaEnv(t)
	owner := e.staff.Owner
	want := map[string]string{
		"orders_v": "id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, " +
			"current_shipment_id, created_at, pending_reason, packing_status, payment_status, channel_account_id, " +
			"traffic_source_id, conversation_id, external_ref, order_type, warehouse_id, ship_phone, address_complete, " +
			"subtotal, line_discount_total, order_discount, coupon_discount, discount_total, shipping_fee_charged, " +
			"prepaid_total, created_by, care_by, confirmed_at, cancelled_at, cancel_reason, draft_expires_at, " +
			"duplicate_suspect, version, ship_phone_reversed",
		"order_items_v": "id, tenant_id, seller_id, order_id, variant_id, position, sku_snapshot, name_snapshot, " +
			"quantity, unit_price, line_discount, allocated_order_discount, net_amount",
		"order_status_history_v": "id, tenant_id, order_id, from_status, to_status, from_pending, to_pending, " +
			"actor_type, actor_id, reason, occurred_at, recorded_at",
		"returns_v": "id, tenant_id, code, order_id, shipment_id, kind, status, refund_amount, replacement_order_id, " +
			"received_at, created_at",
		"return_items_v": "id, tenant_id, return_id, order_item_id, quantity, condition, refund_amount, " +
			"sellable_qty, damaged_qty, missing_qty",
		"traffic_sources_v": "id, tenant_id, key, name, channel_account_id, active, position",
	}
	testkit.CheckViews(t, owner, "ord", want)
	testkit.CheckSchemaRulesClean(t, owner)
}

// Dòng lịch sử tạo đơn của Writer.CreateManual đọc qua view có from_status ”.
func TestSchemaHistoryViewCreateRow(t *testing.T) {
	e := newOrderEnv(t)
	v := e.variant(250000, 5)
	o := e.mustCreate(e.input(0, item(v, 1)))
	var from, to string
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT from_status, to_status FROM ord.order_status_history_v
			WHERE order_id = $1`, o.ID).Scan(&from, &to)
	}); err != nil {
		t.Fatal(err)
	}
	if from != "" || to != "confirmed" {
		t.Errorf("dòng tạo đơn qua view: %q → %q", from, to)
	}
}

func TestEnsureDefaults(t *testing.T) {
	e := newSchemaEnv(t)
	ctx := t.Context()
	for range 2 {
		testkit.InTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error { return order.EnsureDefaults(ctx, tx, e.a) })
	}
	checkSettings(t, e)
	checkSourceByKey(t, e)
	// Gọi lại sau khi đổi tên: không chèn lại "Website".
	testkit.InTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error { return order.EnsureDefaults(ctx, tx, e.a) })
	if n := e.staff.CountIn(t, e.a, "SELECT count(*) FROM ord.traffic_sources"); n != 10 {
		t.Errorf("gọi lại sau đổi tên: %d nguồn, mong 10", n)
	}
}

// checkSettings: một dòng cài đặt (1440, 1440, 1000, 7) và 10 nguồn đơn.
func checkSettings(t *testing.T, e *schemaEnv) {
	t.Helper()
	testkit.InTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
		var n, ttl, dup, bp, days int
		if err := tx.QueryRow(t.Context(), `SELECT count(*) OVER (), draft_ttl_minutes, duplicate_window_minutes,
			discount_approval_threshold_bp, return_warning_days FROM ord.order_settings`).
			Scan(&n, &ttl, &dup, &bp, &days); err != nil {
			return err //nolint:wrapcheck // test
		}
		if n != 1 || ttl != 1440 || dup != 1440 || bp != 1000 || days != 7 {
			t.Errorf("cài đặt đơn: %d dòng (%d, %d, %d, %d)", n, ttl, dup, bp, days)
		}
		return nil
	})
	if n := e.staff.CountIn(t, e.a, "SELECT count(*) FROM ord.traffic_sources"); n != 10 {
		t.Errorf("%d nguồn đơn, mong 10", n)
	}
}

// checkSourceByKey: SourceByKey("web") là nguồn "Website"; đổi tên vẫn tìm ra; khóa không có → ErrTrafficSourceNotFound.
func checkSourceByKey(t *testing.T, e *schemaEnv) {
	t.Helper()
	ctx := t.Context()
	testkit.InTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
		web, err := order.SourceByKey(ctx, tx, order.SourceKeyWeb)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		var name string
		if err := tx.QueryRow(ctx, "SELECT name FROM ord.traffic_sources_v WHERE id = $1", web).Scan(&name); err != nil {
			return err //nolint:wrapcheck // test
		}
		if name != "Website" {
			t.Errorf("nguồn web tên %q", name)
		}
		if _, err := tx.Exec(ctx, "UPDATE ord.traffic_sources SET name = 'Web ELIMAZ' WHERE id = $1", web); err != nil {
			return err //nolint:wrapcheck // test
		}
		again, err := order.SourceByKey(ctx, tx, order.SourceKeyWeb)
		if err != nil || again != web {
			t.Errorf("sau đổi tên: %s, %v; mong %s", again, err, web)
		}
		if _, err := order.SourceByKey(ctx, tx, "khong_co"); !errors.Is(err, order.ErrTrafficSourceNotFound) {
			t.Errorf("khóa không có: %v, mong ErrTrafficSourceNotFound", err)
		}
		return nil
	})
}

// Writer.CreateManual gộp dòng nhập cùng biến thể thành một dòng (cộng số lượng, giữ vị trí đầu) để đúng ràng buộc một
// biến thể một dòng.
func TestCreateManual_MergesSameVariant(t *testing.T) {
	e := newOrderEnv(t)
	v1, v2 := e.variant(250000, 10), e.variant(180000, 10)
	o := e.mustCreate(e.input(0, item(v1, 1), item(v2, 2), item(v1, 3)))
	if len(o.Items) != 2 || o.Items[0].VariantID != v1 || o.Items[0].Qty != 4 || o.Items[0].Position != 1 ||
		o.Items[1].VariantID != v2 || o.Items[1].Qty != 2 || o.Subtotal != 4*250000+2*180000 {
		t.Fatalf("dòng sau gộp: %+v, subtotal %d", o.Items, o.Subtotal)
	}
	if b := e.balance(v1); b.Held != 4 {
		t.Errorf("giữ hàng v1 %d, mong 4", b.Held)
	}
}
