package order_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit"
)

// qtyOrder là một đơn hai dòng (mỗi dòng 2 cái) của tenant mới, tạo bằng Writer.CreateManual.
type qtyOrder struct {
	tenant, order uuid.UUID
	items         [2]uuid.UUID
}

func seedQtyOrder(t *testing.T, owner, app *db.DB) qtyOrder {
	t.Helper()
	ctx := t.Context()
	var q qtyOrder
	if err := owner.Pool.QueryRow(ctx, "INSERT INTO platform.tenants (name) VALUES ('A') RETURNING id").
		Scan(&q.tenant); err != nil {
		t.Fatal(err)
	}
	var shop *invtest.Shop
	var v1, v2 uuid.UUID
	testkit.InTenant(t, owner, q.tenant, func(tx pgx.Tx) error {
		shop = invtest.NewShop(t, tx, q.tenant)
		v1 = shop.Variant(t, tx, shop.Seller, 100000, 10)
		v2 = shop.Variant(t, tx, shop.Seller, 200000, 10)
		return nil
	})
	testkit.InTenant(t, app, q.tenant, func(tx pgx.Tx) error {
		c, err := customer.Create(ctx, tx, "Chị Lan")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
		o, err := w.CreateManual(ctx, tx, order.CreateManualInput{
			IdempotencyKey: uuid.New(), SellerID: shop.Seller, CustomerID: c,
			Items:       []order.ItemInput{{VariantID: v1, Qty: 2}, {VariantID: v2, Qty: 2}},
			ShipAddress: order.ShipAddress{Name: "Chị Lan", Phone: logging.Phone("0901234567"), Line: "12 Lê Lợi"},
		}, order.Actor{TenantID: q.tenant})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		q.order = o.ID
		q.items = [2]uuid.UUID{o.Items[0].ID, o.Items[1].ID}
		return nil
	})
	return q
}

// insertReturn chèn thẳng (chủ bảng) phiếu trả trạng thái status với các dòng (dòng đơn, số lượng, tình trạng).
func insertReturn(t *testing.T, owner *db.DB, q qtyOrder, code, status string, lines map[uuid.UUID]qtyLine,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	testkit.InTenant(t, owner, q.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO ord.returns (tenant_id, code, order_id, kind, status)
			VALUES ($1, $2, $3, 'customer_return', $4) RETURNING id`, q.tenant, code, q.order, status).Scan(&id); err != nil {
			return err //nolint:wrapcheck // test
		}
		for item, l := range lines {
			if _, err := tx.Exec(t.Context(), `INSERT INTO ord.return_items (tenant_id, return_id, order_item_id, quantity,
				condition) VALUES ($1, $2, $3, $4, $5)`, q.tenant, id, item, l.qty, l.cond); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return id
}

// qtyLine là một dòng phiếu trả chèn thẳng: số lượng, tình trạng.
type qtyLine struct {
	qty  int
	cond string
}

// 0302 trên dữ liệu M2: dòng của phiếu đã nhận điền số lượng theo condition; phiếu chưa nhận giữ 0/0/0.
func TestReturnItemsQty_BackfillM2(t *testing.T) {
	_, ownerURL, appURL := testkit.CreateDatabase(t)
	testkit.MigrateBefore(t, ownerURL, "0302")
	owner, app := testkit.OpenDB(t, ownerURL), testkit.OpenDB(t, appURL)
	q := seedQtyOrder(t, owner, app)
	received := insertReturn(t, owner, q, "TH0000001", "received", map[uuid.UUID]qtyLine{
		q.items[0]: {2, "sellable"}, q.items[1]: {1, "damaged"},
	})
	requested := insertReturn(t, owner, q, "TH0000002", "requested", map[uuid.UUID]qtyLine{q.items[1]: {1, "missing"}})

	if _, err := db.Migrate(t.Context(), ownerURL); err != nil {
		t.Fatalf("migrate 0302: %v", err)
	}
	type split struct{ s, d, m int }
	got := map[[2]uuid.UUID]split{}
	testkit.InTenant(t, owner, q.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT return_id, order_item_id, sellable_qty, damaged_qty, missing_qty
			FROM ord.return_items`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		var r, i uuid.UUID
		var v split
		_, err = pgx.ForEachRow(rows, []any{&r, &i, &v.s, &v.d, &v.m}, func() error {
			got[[2]uuid.UUID{r, i}] = v
			return nil
		})
		return err //nolint:wrapcheck // test
	})
	want := map[[2]uuid.UUID]split{
		{received, q.items[0]}:  {2, 0, 0},
		{received, q.items[1]}:  {0, 1, 0},
		{requested, q.items[1]}: {0, 0, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("dòng phiếu trả: %v", got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("dòng %v: %+v, muốn %+v", k, got[k], w)
		}
	}
}

// CHECK: ba số lượng cộng lại là 0 hoặc đúng quantity; mỗi số ≥ 0; condition thêm mixed.
func TestReturnItemsQty_Checks(t *testing.T) {
	s := testkit.OpenStaff(t)
	q := seedQtyOrder(t, s.Owner, s.App)
	ret := insertReturn(t, s.Owner, q, "TH0000001", "received", nil)
	cases := []struct {
		name    string
		qty     int
		s, d, m int
		cond    string
		want    string
	}{
		{"2 = 1+0+0", 2, 1, 0, 0, "sellable", "23514"},
		{"2 = 1+1+0", 2, 1, 1, 0, "mixed", "ok"},
		{"chưa nhận 0+0+0", 2, 0, 0, 0, "sellable", "ok"},
		{"vượt 2+1+0", 2, 2, 1, 0, "mixed", "23514"},
		{"số âm", 1, 2, -1, 0, "mixed", "23514"},
		{"condition lạ", 1, 1, 0, 0, "oops", "23514"},
	}
	for i, c := range cases {
		err := s.TryExec(t.Context(), q.tenant, func(tx pgx.Tx) error {
			// Mỗi ca một dòng đơn riêng không có (unique dòng phiếu theo dòng đơn): dùng lần lượt hai dòng, rồi xóa.
			_, err := tx.Exec(t.Context(), `INSERT INTO ord.return_items (tenant_id, return_id, order_item_id, quantity,
				condition, sellable_qty, damaged_qty, missing_qty) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				q.tenant, ret, q.items[i%2], c.qty, c.cond, c.s, c.d, c.m)
			if err != nil {
				return err //nolint:wrapcheck // test
			}
			_, err = tx.Exec(t.Context(), "DELETE FROM ord.return_items WHERE return_id = $1", ret)
			return err //nolint:wrapcheck // test
		})
		got := "ok"
		if err != nil {
			got = pgCode(err)
		}
		if got != c.want {
			t.Errorf("%s: %s (%v), muốn %s", c.name, got, err, c.want)
		}
	}
	if n := s.CountIn(t, q.tenant, "SELECT count(*) FROM ord.return_items_v WHERE sellable_qty + damaged_qty "+
		"+ missing_qty >= 0"); n != 0 {
		t.Errorf("còn %d dòng sau các ca", n)
	}
}
