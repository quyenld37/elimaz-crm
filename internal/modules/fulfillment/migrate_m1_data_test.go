package fulfillment_test

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// m1Ship là một vận đơn ghi bằng đúng các cột của M1 (0118, 0130) trước 0300.
type m1Ship struct {
	id, order, seller uuid.UUID
	status, tracking  string
}

// m1Seeded là dữ liệu M1 dựng trước 0300: tài khoản tắt, ba vận đơn.
type m1Seeded struct {
	tenant, disabled uuid.UUID
	ships            []m1Ship
}

// Dữ liệu M1 (tài khoản fake active và disabled không có khóa; vận đơn creating chưa có mã hãng, delivered có mã,
// cancelled; mã sinh bằng NewShipmentCode; vận đơn hiện hành của đơn) chạy 0300–0302 không lỗi và được điền đúng.
func TestMigrateM1Data(t *testing.T) {
	t.Parallel()
	_, ownerURL, appURL := testkit.CreateDatabase(t)
	testkit.MigrateBefore(t, ownerURL, "0300")
	owner, app := testkit.OpenDB(t, ownerURL), testkit.OpenDB(t, appURL)
	s := seedM1Shipments(t, owner, app)
	if _, err := db.Migrate(t.Context(), ownerURL); err != nil {
		t.Fatalf("migrate 0300–0302 trên dữ liệu M1: %v", err)
	}
	checkM1Accounts(t, owner, s.tenant, s.disabled)
	checkM1Shipments(t, owner, s)
	testkit.CheckSchemaRulesClean(t, owner)
}

// seedM1Shipments dựng tenant A (shiptest.Shop: fake-1 của ELIMAZ, fake-2 của PARTNER1), tài khoản fake-off disabled,
// ba đơn và ba vận đơn ghi bằng đúng các cột của M1; hai vận đơn chưa hủy là vận đơn hiện hành của đơn.
func seedM1Shipments(t *testing.T, owner, app *db.DB) m1Seeded {
	t.Helper()
	var s m1Seeded
	if err := owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('A') RETURNING id").
		Scan(&s.tenant); err != nil {
		t.Fatal(err)
	}
	var shop *shiptest.Shop
	var v1, v2 uuid.UUID
	testkit.InTenant(t, owner, s.tenant, func(tx pgx.Tx) error {
		shop = shiptest.NewShop(t, tx, s.tenant)
		v1 = shop.Variant(t, tx, shop.Seller, 100000, 10)
		v2 = shop.Variant(t, tx, shop.Partner, 100000, 10)
		return tx.QueryRow(t.Context(), `INSERT INTO fulfillment.carrier_accounts
			(tenant_id, seller_id, carrier_key, name, external_account_ref, warehouse_id, status)
			VALUES ($1, $2, 'fake', 'Hãng giả tắt', 'fake-off', $3, 'disabled') RETURNING id`,
			s.tenant, shop.Seller, shop.Warehouse).Scan(&s.disabled)
	})
	newOrder := func(seller, v uuid.UUID) uuid.UUID {
		var o order.Order
		testkit.InTenant(t, app, s.tenant, func(tx pgx.Tx) (err error) {
			o, err = shop.Order(t.Context(), tx, seller, uuid.Nil, 30000, order.ItemInput{VariantID: v, Qty: 1})
			return err //nolint:wrapcheck // test
		})
		return o.ID
	}
	s.ships = []m1Ship{
		{order: newOrder(shop.Seller, v1), seller: shop.Seller, status: "creating"},
		{order: newOrder(shop.Partner, v2), seller: shop.Partner, status: "delivered", tracking: "M1DELIVERED"},
		{order: newOrder(shop.Seller, v1), seller: shop.Seller, status: "cancelled"},
	}
	testkit.InTenant(t, owner, s.tenant, func(tx pgx.Tx) error {
		for i := range s.ships {
			if err := insertM1Shipment(t, tx, s.tenant, shop.AccountOf(s.ships[i].seller), &s.ships[i]); err != nil {
				return err
			}
		}
		return nil
	})
	return s
}

// insertM1Shipment chèn vận đơn sh bằng các cột của M1 (chủ bảng); vận đơn chưa hủy gắn làm vận đơn hiện hành.
func insertM1Shipment(t *testing.T, tx pgx.Tx, tenant, account uuid.UUID, sh *m1Ship) error {
	t.Helper()
	if err := tx.QueryRow(t.Context(), `INSERT INTO fulfillment.shipments
		(tenant_id, code, order_id, carrier_key, carrier_account_id, tracking_code, status, cod_amount)
		VALUES ($1, $2, $3, 'fake', $4, NULLIF($5, ''), $6, 0) RETURNING id`, tenant,
		fulfillment.NewShipmentCode(time.Now(), rand.Reader), sh.order, account, sh.tracking, sh.status).
		Scan(&sh.id); err != nil {
		return err //nolint:wrapcheck // test
	}
	if sh.status == "cancelled" {
		return nil
	}
	_, err := tx.Exec(t.Context(), "UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1", sh.order, sh.id)
	return err //nolint:wrapcheck // test
}

// checkM1Shipments: trạng thái giữ (creating vẫn creating); seller_id là người bán của đơn; có mã hãng → created;
// cancelled → cancel_kind manual; last_status_at được điền.
func checkM1Shipments(t *testing.T, owner *db.DB, s m1Seeded) {
	t.Helper()
	want := []struct{ createState, cancelKind string }{{"pending", ""}, {"created", ""}, {"pending", "manual"}}
	testkit.InTenant(t, owner, s.tenant, func(tx pgx.Tx) error {
		for i, sh := range s.ships {
			var status, createState, cancelKind string
			var seller uuid.UUID
			var lastStatus *time.Time
			if err := tx.QueryRow(t.Context(), `SELECT status, seller_id, create_state, COALESCE(cancel_kind, ''),
				last_status_at FROM fulfillment.shipments WHERE id = $1`, sh.id).
				Scan(&status, &seller, &createState, &cancelKind, &lastStatus); err != nil {
				return err //nolint:wrapcheck // test
			}
			if status != sh.status || seller != sh.seller || createState != want[i].createState ||
				cancelKind != want[i].cancelKind || lastStatus == nil {
				t.Errorf("vận đơn %s: status %s, seller %v (muốn %v), create_state %s, cancel_kind %q, last_status_at %v",
					sh.status, status, seller, sh.seller, createState, cancelKind, lastStatus)
			}
		}
		return nil
	})
}

// checkM1Accounts: mọi tài khoản có route_key 32 ký tự, khác nhau; disabled → paused; active giữ.
func checkM1Accounts(t *testing.T, owner *db.DB, tenant, disabled uuid.UUID) {
	t.Helper()
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
		var n, distinct, active int
		var status string
		if err := tx.QueryRow(t.Context(), `SELECT count(*), count(DISTINCT route_key),
			count(*) FILTER (WHERE status = 'active'), max(status) FILTER (WHERE id = $1)
			FROM fulfillment.carrier_accounts WHERE route_key ~ '^[A-Za-z0-9]{32}$'`, disabled).
			Scan(&n, &distinct, &active, &status); err != nil {
			return err //nolint:wrapcheck // test
		}
		if n != 3 || distinct != 3 || active != 2 || status != "paused" {
			t.Errorf("tài khoản sau migrate: %d có route_key hợp lệ, %d khác nhau, %d active, tắt → %q", n, distinct,
				active, status)
		}
		return nil
	})
}
