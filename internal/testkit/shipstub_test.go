package testkit_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// stubEnv là một shop có một đơn thật (tạo qua order) và n đơn chép từ đơn đó bằng SQL, để gắn vận đơn giả.
type stubEnv struct {
	staff  *testkit.Staff
	tenant uuid.UUID
	orders []uuid.UUID
}

func newStubEnv(t *testing.T, n int) *stubEnv {
	t.Helper()
	e := &stubEnv{staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "Shop vận đơn giả")
	var shop *shiptest.Shop
	var variant uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		shop = shiptest.NewShop(t, tx, e.tenant)
		variant = shop.Variant(t, tx, shop.Seller, 100000, 10)
		return nil
	})
	var tmpl uuid.UUID
	testkit.InTenant(t, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		o, err := shop.Order(t.Context(), tx, shop.Seller, uuid.Nil, 0, order.ItemInput{VariantID: variant, Qty: 1})
		tmpl = o.ID
		return err //nolint:wrapcheck // test
	})
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		ctx := t.Context()
		var cols string
		// Đơn chép không có dòng hàng: mọi tổng tiền 0 (CHECK tổng dòng = subtotal).
		if err := tx.QueryRow(ctx, `SELECT string_agg(column_name, ', ' ORDER BY ordinal_position)
			FROM information_schema.columns WHERE table_schema = 'ord' AND table_name = 'orders' AND is_generated = 'NEVER'
			AND column_name <> ALL ('{id,code,idempotency_key,current_shipment_id,subtotal,discount_total,shipping_fee_charged,grand_total,cod_amount}')`,
		).Scan(&cols); err != nil {
			return err //nolint:wrapcheck // test
		}
		rows, err := tx.Query(ctx, fmt.Sprintf(`INSERT INTO ord.orders (id, code, idempotency_key, subtotal, discount_total,
				shipping_fee_charged, grand_total, cod_amount, %[1]s)
			SELECT gen_random_uuid(), 'DH9' || lpad(g::text, 7, '0'), gen_random_uuid(), 0, 0, 0, 0, 0, %[1]s
			FROM ord.orders, generate_series(1, $2) g WHERE id = $1
			RETURNING id`, cols), tmpl, n)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		e.orders, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err //nolint:wrapcheck // test
	})
	if len(e.orders) != n {
		t.Fatalf("đơn chép = %d, muốn %d", len(e.orders), n)
	}
	return e
}

// stub gắn vận đơn giả s cho đơn o (một transaction).
func (e *stubEnv) stub(t *testing.T, o, s uuid.UUID) {
	t.Helper()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		testkit.StubShipment(t, tx, o, s)
		return nil
	})
}

// code trả mã của vận đơn s ("" nếu không có).
func (e *stubEnv) code(t *testing.T, s uuid.UUID) string {
	t.Helper()
	var c string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		err := tx.QueryRow(t.Context(), `SELECT coalesce(max(code), '') FROM fulfillment.shipments WHERE id = $1`, s).Scan(&c)
		return err //nolint:wrapcheck // test
	})
	return c
}

// 20.000 vận đơn giả trong một database (nhiều hơn số vận đơn mọi test của order, customer, httpapi tạo cộng lại) →
// không lỗi trùng shipments_code_key, 20.000 mã khác nhau, đều đúng dạng CHECK của 0118.
func TestStubShipment_ManyCodesUnique(t *testing.T) {
	const n = 20000
	e := newStubEnv(t, n)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		for _, o := range e.orders {
			testkit.StubShipment(t, tx, o, uuid.New())
		}
		return nil
	})
	var rows, codes int
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		err := tx.QueryRow(t.Context(), `SELECT count(*), count(DISTINCT code) FROM fulfillment.shipments
			WHERE code ~ '^E[0-9]{6}[0-9A-Z]{6}$'`).Scan(&rows, &codes)
		return err //nolint:wrapcheck // test
	})
	if rows != n || codes != n {
		t.Fatalf("vận đơn = %d, mã khác nhau = %d; muốn %d", rows, codes, n)
	}
}

// Mã ứng viên đầu (và cả mã thứ hai) đã có vận đơn khác giữ → vận đơn mới lấy mã ứng viên kế tiếp, không lỗi 23505.
func TestStubShipment_CodeTakenTriesNext(t *testing.T) {
	e := newStubEnv(t, 4)
	s := uuid.New()
	for i, attempt := range []int{0, 1} {
		holder := uuid.New()
		e.stub(t, e.orders[i], holder)
		prefix := e.code(t, holder)[:7]
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `UPDATE fulfillment.shipments SET code = $2 WHERE id = $1`,
				holder, prefix+testkit.StubCodeSuffix(s, attempt))
			return err //nolint:wrapcheck // test
		})
	}
	e.stub(t, e.orders[2], s)
	got := e.code(t, s)
	if want := got[:7] + testkit.StubCodeSuffix(s, 2); got != want {
		t.Fatalf("mã = %q, muốn %q (ứng viên thứ ba)", got, want)
	}
}

// Gọi lại cùng mã vận đơn không đổi gì; đơn không có → không chèn.
func TestStubShipment_IdempotentAndMissingOrder(t *testing.T) {
	e := newStubEnv(t, 1)
	s := uuid.New()
	e.stub(t, e.orders[0], s)
	first := e.code(t, s)
	e.stub(t, e.orders[0], s)
	if got := e.code(t, s); first == "" || got != first {
		t.Fatalf("mã lần đầu %q, lần hai %q", first, got)
	}
	missing := uuid.New()
	e.stub(t, uuid.New(), missing)
	if got := e.code(t, missing); got != "" {
		t.Fatalf("đơn không có mà vẫn chèn vận đơn %q", got)
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM fulfillment.shipments`); n != 1 {
		t.Fatalf("số vận đơn = %d, muốn 1", n)
	}
}
