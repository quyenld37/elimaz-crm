package inventory_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// invParallel là số test chạy song song mặc định của gói (Task 470, như order ở Task 469): mỗi test có database
// riêng và tới 9 kết nối PostgreSQL (2 pool × MaxConns 4 + 1 kết nối quản trị lúc tạo database); máy chủ thử dùng
// chung nhiều phiên có max_connections 300.
const invParallel = 6

// TestMain đặt số lượt rapid 300 (kế hoạch Task M1-28: TestInventoryInvariants 300 lượt), trừ khi người chạy đã đặt
// -rapid.checks hay RAPID_CHECKS; đặt -parallel thành min(invParallel, GOMAXPROCS), trừ khi người chạy đã đặt
// -parallel.
func TestMain(m *testing.M) {
	flag.Parse()
	testkit.SetRapidChecks(300)
	testkit.SetParallel(invParallel)
	os.Exit(m.Run())
}

// invEnv: một database thử, tenant A có hai người bán ELIMAZ và PARTNER1 (mỗi người một mẫu AO01), kho MAIN và các
// biến thể tạo bằng variants. seller là người bán của biến thể trong test (mặc định ELIMAZ; eachSeller chạy với từng
// người), product là mẫu AO01 của người bán đó.
type invEnv struct {
	t         *testing.T
	staff     *testkit.Staff
	tenant    uuid.UUID
	seller    uuid.UUID
	warehouse uuid.UUID
	bus       *realtimetest.RecordingBus
	product   uuid.UUID
	products  map[uuid.UUID]uuid.UUID
	nVariants int
}

func newInvEnv(t *testing.T) *invEnv {
	t.Helper()
	return newInvEnvOn(t, testkit.OpenStaff(t), "A")
}

// newInvEnvOn dựng tenant name trên staff có sẵn (nhiều tenant chung một database).
func newInvEnvOn(t *testing.T, staff *testkit.Staff, name string) *invEnv {
	t.Helper()
	e := &invEnv{t: t, staff: staff, bus: &realtimetest.RecordingBus{}}
	e.tenant = staff.Tenant(t, name)
	e.products = map[uuid.UUID]uuid.UUID{}
	staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		elimaz, partner := catalogtest.Sellers(t, tx, e.tenant)
		var err error
		if e.warehouse, err = inventory.EnsureDefaultWarehouse(t.Context(), tx, e.tenant, elimaz); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, s := range []uuid.UUID{elimaz, partner} {
			var id uuid.UUID
			if err := tx.QueryRow(t.Context(), `INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status)
				VALUES ($1, $2, 'AO01', 'Áo thử', 'ao thu', 'active') RETURNING id`, e.tenant, s).Scan(&id); err != nil {
				return err //nolint:wrapcheck // test
			}
			e.products[s] = id
		}
		e.seller, e.product = elimaz, e.products[elimaz]
		return nil
	})
	return e
}

// sellers trả hai người bán của tenant: ELIMAZ (người bán mặc định) rồi PARTNER1.
func (e *invEnv) sellers() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(e.products))
	for s := range e.products {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }) // uuidv7: ELIMAZ tạo trước
	return out
}

// eachSeller chạy body trên cùng một database lần lượt với từng người bán làm người bán của biến thể (KH14, ADR-29);
// mỗi lượt một bộ ghi báo hiệu riêng.
func eachSeller(t *testing.T, body func(t *testing.T, e *invEnv)) {
	t.Helper()
	base := newInvEnv(t)
	testkit.ForEachSeller(t, base.sellers(), func(t *testing.T, s uuid.UUID) {
		e := *base
		e.t, e.seller, e.product, e.bus = t, s, base.products[s], &realtimetest.RecordingBus{}
		body(t, &e)
	})
}

// variants tạo n biến thể mới (đang bán) của mẫu thử và trả mã của chúng.
func (e *invEnv) variants(n int) []uuid.UUID {
	e.t.Helper()
	ids := make([]uuid.UUID, n)
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		for i := range ids {
			e.nVariants++
			if err := tx.QueryRow(e.t.Context(), `INSERT INTO catalog.variants
				(tenant_id, seller_id, product_id, sku, barcode, color, size, price)
				VALUES ($1, $2, $3, $4, $5, 'Đỏ', $6, 100000) RETURNING id`,
				e.tenant, e.seller, e.product, fmt.Sprintf("AO01-%d", e.nVariants), fmt.Sprintf("BC%06d", e.nVariants),
				fmt.Sprintf("S%d", e.nVariants)).Scan(&ids[i]); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return ids
}

// secondWarehouse tạo kho W2 của người bán mặc định (chủ bảng).
func (e *invEnv) secondWarehouse() uuid.UUID {
	e.t.Helper()
	var w2 uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, 'W2', 'Kho phụ') RETURNING id`, e.tenant, e.seller).Scan(&w2)
	})
	return w2
}

// stock tạo một biến thể có on_hand = n ở kho MAIN.
func (e *invEnv) stock(n int32) uuid.UUID {
	e.t.Helper()
	v := e.variants(1)[0]
	e.seed(v, n)
	return v
}

func (e *invEnv) seed(v uuid.UUID, n int32) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		invtest.SeedStock(e.t, tx, v, e.warehouse, n)
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *invEnv) writer() inventory.Writer {
	return inventory.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: e.bus}
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant của e.
func (e *invEnv) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

// line trả dòng giữ hàng q cái của biến thể v cho một dòng đơn thật mới (khóa ngoại stock_reservations → ord.order_items,
// Task M1-30). Gọi được từ goroutine: lỗi ghi bằng t.Errorf (không t.Fatal) và Reserve sau đó báo lỗi khóa ngoại.
func (e *invEnv) line(v uuid.UUID, q int32) inventory.ReserveLine {
	item, err := e.orderItem(v)
	if err != nil {
		e.t.Errorf("tạo dòng đơn thử: %v", err)
	}
	return inventory.ReserveLine{OrderItemID: item, VariantID: v, WarehouseID: e.warehouse, Qty: q}
}

// orderItem chèn một khách, một đơn và một dòng đơn (1 cái biến thể v, giá 0) bằng crm_app, chỉ để giữ hàng có dòng
// đơn thật; inventory không nhập module order nên dùng SQL trong test.
func (e *invEnv) orderItem(v uuid.UUID) (uuid.UUID, error) {
	var item uuid.UUID
	err := e.app(func(tx pgx.Tx) error {
		ctx := e.t.Context()
		customer, order := uuid.New(), uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO customer.customers (id, tenant_id, root_id, full_name)
			VALUES ($1, $2, $1, 'Khách thử')`, customer, e.tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ord.orders (id, tenant_id, seller_id, code, customer_id, channel,
			idempotency_key, status, ship_address, subtotal, cod_amount, grand_total, warehouse_id)
			VALUES ($1, $2, $3, 'DH' || lpad(nextval('ord.order_code_seq')::text, 7, '0'), $4, 'manual', $1,
			'confirmed', '{}', 0, 0, 0, $5)`, order, e.tenant, e.seller, customer, e.warehouse); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO ord.order_items (tenant_id, seller_id, order_id, position, variant_id,
			sku_snapshot, name_snapshot, quantity, unit_price, net_amount)
			VALUES ($1, $2, $3, 1, $4, 'SKU', 'Dòng thử', 1, 0, 0) RETURNING id`,
			e.tenant, e.seller, order, v).Scan(&item)
	})
	return item, err
}

func (e *invEnv) reserve(a inventory.Actor, lines ...inventory.ReserveLine) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := e.app(func(tx pgx.Tx) error {
		var err error
		ids, err = e.writer().Reserve(e.t.Context(), tx, lines, a)
		return err //nolint:wrapcheck // test
	})
	return ids, err
}

func (e *invEnv) do(fn func(ctx context.Context, tx pgx.Tx, w inventory.Writer) error) error {
	return e.app(func(tx pgx.Tx) error { return fn(e.t.Context(), tx, e.writer()) })
}

// balance đọc số dư của v ở kho MAIN qua inventory.stock_available_v (crm_app).
func (e *invEnv) balance(v uuid.UUID) inventory.Balance {
	e.t.Helper()
	var b inventory.Balance
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT variant_id, warehouse_id, on_hand, held, in_transit, damaged, available
			FROM inventory.stock_available_v WHERE variant_id = $1 AND warehouse_id = $2`, v, e.warehouse).
			Scan(&b.VariantID, &b.WarehouseID, &b.OnHand, &b.Held, &b.InTransit, &b.Damaged, &b.Available)
	}); err != nil {
		e.t.Fatalf("đọc số dư: %v", err)
	}
	return b
}

// count đếm bằng chủ bảng trong ngữ cảnh tenant (crm_app không đọc domain_events).
func (e *invEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

func (e *invEnv) reservationState(id uuid.UUID) string {
	e.t.Helper()
	var s string
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT state FROM inventory.stock_reservations WHERE id = $1", id).Scan(&s)
	}); err != nil {
		e.t.Fatalf("đọc trạng thái giữ hàng: %v", err)
	}
	return s
}

// sysActor là tác nhân hệ thống (actor_id NULL).
func sysActor() inventory.Actor { return inventory.Actor{Type: "system"} }

func want(t *testing.T, got, exp inventory.Balance) {
	t.Helper()
	if got.OnHand != exp.OnHand || got.Held != exp.Held || got.InTransit != exp.InTransit || got.Damaged != exp.Damaged ||
		got.Available != exp.Available {
		t.Fatalf("số dư sai: có %+v, mong on_hand %d held %d in_transit %d damaged %d available %d", got,
			exp.OnHand, exp.Held, exp.InTransit, exp.Damaged, exp.Available)
	}
}

// uuidLess so như PostgreSQL so uuid (16 byte theo thứ tự).
func uuidLess(a, b uuid.UUID) bool { return bytes.Compare(a[:], b[:]) < 0 }

// tenancyApp chạy fn bằng crm_app trong ngữ cảnh tenant khác của cùng database.
func tenancyApp(e *invEnv, tenant uuid.UUID, fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, tenant, fn) //nolint:wrapcheck // test
}
