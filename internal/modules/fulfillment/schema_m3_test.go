package fulfillment_test

import (
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// pgCode trả SQLSTATE của lỗi PostgreSQL trong err ("" khi không phải lỗi PostgreSQL, "ok" khi err rỗng).
func pgCode(err error) string {
	if err == nil {
		return "ok"
	}
	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pe.Code
	}
	return err.Error()
}

// wantCode đòi err có SQLSTATE want ("ok": không lỗi).
func wantCode(t *testing.T, what string, err error, want string) {
	t.Helper()
	if got := pgCode(err); got != want {
		t.Errorf("%s: %s (%v), muốn %s", what, got, err, want)
	}
}

// shipRow là một vận đơn chèn thẳng bằng crm_app (ca ràng buộc).
type shipRow struct {
	seller, order, account         uuid.UUID
	carrierKey, status, cancelKind string
	tracking, code                 string
}

const insertShipSQL = `INSERT INTO fulfillment.shipments
	(tenant_id, seller_id, code, order_id, carrier_key, carrier_account_id, status, tracking_code, cod_amount, cancel_kind)
	VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), 0, NULLIF($9, '')) RETURNING id`

func (e *shipEnv) insertShip(r shipRow) (uuid.UUID, error) {
	if r.code == "" {
		r.code = fulfillment.NewShipmentCode(time.Now(), rand.Reader)
	}
	if r.carrierKey == "" {
		r.carrierKey = "fake"
	}
	var id uuid.UUID
	err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), insertShipSQL, e.tenant, r.seller, r.code, r.order, r.carrierKey, r.account,
			r.status, r.tracking, r.cancelKind).Scan(&id)
	})
	return id, err
}

// Sau 0300–0302 database theo đủ luật schema (RLS, unique ghép tenant, luật người bán (g)).
func TestSchemaM3_Rules(t *testing.T) {
	testkit.CheckSchemaRulesClean(t, testkit.OpenStaff(t).Owner)
}

// Một đơn một vận đơn chưa hủy (unique M1 còn); hủy (replace) rồi tạo lại được.
func TestSchemaM3_OneActiveShipmentPerOrder(t *testing.T) {
	eachSeller(t, func(t *testing.T, e *shipEnv) {
		o, _ := e.order(1)
		first, err := e.insertShip(shipRow{seller: e.seller, order: o.ID, account: e.account, status: "creating"})
		wantCode(t, "vận đơn đầu", err, "ok")
		_, err = e.insertShip(shipRow{seller: e.seller, order: o.ID, account: e.account, status: "creating"})
		wantCode(t, "vận đơn thứ hai chưa hủy", err, "23505")
		wantCode(t, "hủy vận đơn đầu", e.appExec(
			"UPDATE fulfillment.shipments SET status = 'cancelled', cancel_kind = 'replace' WHERE id = $1", first), "ok")
		_, err = e.insertShip(shipRow{seller: e.seller, order: o.ID, account: e.account, status: "creating"})
		wantCode(t, "vận đơn mới sau khi hủy", err, "ok")
	})
}

// KH14 cho vận đơn: người bán của vận đơn phải là người bán của đơn và của tài khoản hãng.
func TestSchemaM3_SellerMustMatch(t *testing.T) {
	eachSeller(t, func(t *testing.T, e *shipEnv) {
		o, _ := e.order(1)
		other := e.shop.Partner
		if e.seller == e.shop.Partner {
			other = e.shop.Seller
		}
		_, err := e.insertShip(shipRow{seller: e.seller, order: o.ID, account: e.shop.AccountOf(other), status: "creating"})
		wantCode(t, "tài khoản hãng của người bán khác", err, "23503")
		_, err = e.insertShip(shipRow{seller: other, order: o.ID, account: e.shop.AccountOf(other), status: "creating"})
		wantCode(t, "đơn của người bán khác", err, "23503")
	})
}

// VC13 (ràng buộc): mã hãng duy nhất theo hãng; hai hãng cùng mã được.
func TestSchemaM3_TrackingUniquePerCarrier(t *testing.T) {
	e := newShipEnv(t)
	o1, _ := e.order(1)
	o2, _ := e.order(1)
	o3, _ := e.order(1)
	ins := func(o uuid.UUID, key string) error {
		_, err := e.insertShip(shipRow{
			seller: e.seller, order: o, account: e.account, carrierKey: key,
			status: "awaiting_pickup", tracking: "123456789",
		})
		return err
	}
	wantCode(t, "vtp lần đầu", ins(o1.ID, "vtp"), "ok")
	wantCode(t, "vtp cùng mã", ins(o2.ID, "vtp"), "23505")
	wantCode(t, "best cùng mã", ins(o3.ID, "best"), "ok")
}

// CHECK trạng thái: đã qua creating thì phải có mã hãng; hủy phải có loại hủy; giá trị lạ, mã sai mẫu bị chặn.
func TestSchemaM3_StatusChecks(t *testing.T) {
	e := newShipEnv(t)
	cases := []struct {
		name string
		row  shipRow
		want string
	}{
		{"awaiting_pickup không mã hãng", shipRow{status: "awaiting_pickup"}, "23514"},
		{"cancelled không loại hủy", shipRow{status: "cancelled"}, "23514"},
		{"cancelled có loại hủy", shipRow{status: "cancelled", cancelKind: "manual"}, "ok"},
		{"delivery_failed có mã", shipRow{status: "delivery_failed", tracking: "DF1"}, "ok"},
		{"creating không mã (M1)", shipRow{status: "creating"}, "ok"},
		{"status requested", shipRow{status: "requested", tracking: "RQ1"}, "23514"},
		{"mã VD0001", shipRow{status: "creating", code: "VD0001"}, "23514"},
		{"cancel_kind lạ", shipRow{status: "cancelled", cancelKind: "oops"}, "23514"},
	}
	for _, c := range cases {
		o, _ := e.order(1)
		c.row.seller, c.row.order, c.row.account = e.seller, o.ID, e.account
		_, err := e.insertShip(c.row)
		wantCode(t, c.name, err, c.want)
	}
}

// Đường tạo vận đơn của M1 vẫn chạy sau migration: seller_id lấy từ tài khoản, create_state pending, last_status_at có.
func TestSchemaM3_CreateForOrderStillWorks(t *testing.T) {
	eachSeller(t, func(t *testing.T, e *shipEnv) {
		o, _ := e.order(1)
		s := e.mustCreateShipment(o.ID)
		if n := e.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND seller_id = $2
			AND create_state = 'pending' AND last_status_at IS NOT NULL AND status = 'creating'`, s.ID, e.seller); n != 1 {
			t.Fatalf("vận đơn M1 sau migration không đúng (seller_id, create_state, last_status_at)")
		}
		e.drain()
		if n := e.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND status = 'awaiting_pickup'
			AND create_state = 'created' AND tracking_code IS NOT NULL`, s.ID); n != 1 {
			t.Fatalf("vận đơn sau lệnh tạo: muốn awaiting_pickup, created, có mã hãng")
		}
	})
}

// shipment_events vẫn chỉ ghi thêm với crm_app; callback_task_logs mới cũng vậy (cả chủ bảng).
func TestSchemaM3_AppendOnly(t *testing.T) {
	e := newShipEnv(t)
	_, _, s := e.shipped(1)
	wantCode(t, "UPDATE shipment_events", e.appExec("UPDATE fulfillment.shipment_events SET raw_status = 'x'"), "42501")
	var task uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `INSERT INTO fulfillment.callback_tasks
			(tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at) VALUES ($1, $2, $3, 1, now(), now())
			RETURNING id`, e.tenant, s.ID, s.OrderID).Scan(&task)
	})
	wantCode(t, "crm_app ghi nhật ký gọi lại", e.appExec(`INSERT INTO fulfillment.callback_task_logs
		(tenant_id, task_id, outcome, actor_id) VALUES ($1, $2, 'no_answer', $3)`, e.tenant, task, e.actor.UserID), "ok")
	wantCode(t, "crm_app UPDATE nhật ký", e.appExec("UPDATE fulfillment.callback_task_logs SET note = 'x'"), "42501")
	wantCode(t, "crm_app DELETE nhật ký", e.appExec("DELETE FROM fulfillment.callback_task_logs"), "42501")
	e.staff.CheckOwnerAppendOnly(t, e.tenant, "UPDATE fulfillment.callback_task_logs SET note = 'x'",
		"DELETE FROM fulfillment.callback_task_logs")
}

// Tài khoản hãng: route_key 32 ký tự, duy nhất trong shop; một tài khoản mặc định mỗi (người bán, kho, hãng);
// trạng thái mới hợp lệ, lạ thì chặn.
func TestSchemaM3_CarrierAccounts(t *testing.T) {
	e := newShipEnv(t)
	if n := e.count(`SELECT count(*) FROM fulfillment.carrier_accounts WHERE route_key ~ '^[A-Za-z0-9]{32}$'`); n != 2 {
		t.Fatalf("tài khoản có route_key hợp lệ = %d, muốn 2", n)
	}
	owner := func(sql string, args ...any) error {
		return e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), sql, args...)
			return err //nolint:wrapcheck // test
		})
	}
	wantCode(t, "route_key trùng", owner(`UPDATE fulfillment.carrier_accounts SET route_key =
		(SELECT route_key FROM fulfillment.carrier_accounts WHERE id = $2) WHERE id = $1`, e.shop.Account,
		e.shop.PartnerAccount), "23505")
	wantCode(t, "route_key sai mẫu", owner(`UPDATE fulfillment.carrier_accounts SET route_key = 'ngan' WHERE id = $1`,
		e.shop.Account), "23514")
	wantCode(t, "trạng thái paused", owner(`UPDATE fulfillment.carrier_accounts SET status = 'paused' WHERE id = $1`,
		e.shop.Account), "ok")
	wantCode(t, "trạng thái lạ", owner(`UPDATE fulfillment.carrier_accounts SET status = 'oops' WHERE id = $1`,
		e.shop.Account), "23514")
	wantCode(t, "mặc định của người bán 1", owner(`UPDATE fulfillment.carrier_accounts SET is_default_for_warehouse = true
		WHERE id = $1`, e.shop.Account), "ok")
	wantCode(t, "mặc định của người bán 2 cùng kho, hãng", owner(`UPDATE fulfillment.carrier_accounts
		SET is_default_for_warehouse = true WHERE id = $1`, e.shop.PartnerAccount), "ok")
	second, err := fulfillmentAccount(e, e.shop.Seller, "fake-3")
	wantCode(t, "tài khoản thứ hai của người bán 1", err, "ok")
	wantCode(t, "hai tài khoản mặc định cùng người bán, kho, hãng", owner(`UPDATE fulfillment.carrier_accounts
		SET is_default_for_warehouse = true WHERE id = $1`, second), "23505")
	wantCode(t, "crm_app không sửa route_key", e.appExec(`UPDATE fulfillment.carrier_accounts SET route_key = $2
		WHERE id = $1`, e.shop.Account, "abcdefghijabcdefghijabcdefghij12"), "42501")
}

func fulfillmentAccount(e *shipEnv, seller uuid.UUID, ref string) (uuid.UUID, error) {
	var id uuid.UUID
	err := e.staff.TryExec(e.t.Context(), e.tenant, func(tx pgx.Tx) (err error) {
		id, err = fulfillment.EnsureCarrierAccount(e.t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: seller, WarehouseID: e.shop.Warehouse, CarrierKey: "fake", Name: "Hãng giả 3",
			ExternalAccountRef: ref,
		})
		return err //nolint:wrapcheck // test
	})
	return id, err //nolint:wrapcheck // test
}

// View công bố: đúng cột theo thứ tự (không cột bí mật, dữ liệu cá nhân), security_invoker.
func TestSchemaM3_Views(t *testing.T) {
	owner := testkit.OpenStaff(t).Owner
	testkit.CheckViews(t, owner, "fulfillment", map[string]string{
		"shipments_v": "id, order_id, code, carrier_key, carrier_account_id, tracking_code, status, cod_amount, fee, " +
			"tenant_id, seller_id, mode, service, sub_status, cancel_kind, create_state, cod_collected_amount, " +
			"declared_value, fee_actual, weight_grams, partial, picked_up_at, delivered_at, " +
			"carrier_reported_returned_at, returned_at, last_status_at, last_carrier_event_at, last_synced_at, " +
			"last_sync_error, stuck_since, needs_review, label_file_id, label_printed_at, created_at, province_code",
		"carrier_accounts_v": "tenant_id, id, seller_id, carrier_key, mode, environment, name, warehouse_id, status, " +
			"is_default_for_warehouse, show_on_web, web_sort, last_check_at, last_check_result, created_at, updated_at",
		"shipment_events_v": "tenant_id, id, shipment_id, source, raw_status, canonical, status, sub_status, " +
			"occurred_at, recorded_at, applied, ignored_reason, actor_id",
		"locations_v": "tenant_id, carrier_key, level, our_code, carrier_code, carrier_name, synced_at",
	})
	testkit.CheckViews(t, owner, "inventory", map[string]string{
		"warehouses_v": "warehouse_id, operated_by_seller_id, code, name, status, pickup_address, pickup_phone",
	})
}

// Tenant B (crm_app) không đọc được dòng nào của tenant A qua bốn view của fulfillment; tenant A đọc được.
func TestSchemaM3_ViewsTenantIsolation(t *testing.T) {
	e := newShipEnv(t)
	_, _, s := e.shipped(1)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `INSERT INTO fulfillment.shipment_events
			(tenant_id, shipment_id, event_key, raw_status, status, occurred_at, raw)
			VALUES ($1, $2, 'k1', 'picked', 'shipping', now(), '{}')`, e.tenant, s.ID); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO fulfillment.carrier_locations
			(tenant_id, carrier_key, level, our_code, carrier_code, carrier_name, synced_at)
			VALUES ($1, 'fake', 'province', '79', 'HCM', 'Hồ Chí Minh', now())`, e.tenant)
		return err //nolint:wrapcheck // test
	})
	b := e.staff.Tenant(t, "B")
	views := []string{"shipments_v", "carrier_accounts_v", "shipment_events_v", "locations_v"}
	for _, tc := range []struct {
		tenant uuid.UUID
		want   bool
	}{{e.tenant, true}, {b, false}} {
		for _, v := range views {
			var n int
			if err := tenancy.WithTenant(t.Context(), e.staff.App, tc.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(t.Context(), "SELECT count(*) FROM fulfillment."+v).Scan(&n)
			}); err != nil {
				t.Fatal(err)
			}
			if (n > 0) != tc.want {
				t.Errorf("tenant %v đọc %s: %d dòng, muốn có=%v", tc.tenant, v, n, tc.want)
			}
		}
	}
}

// Khóa ngoại M2 để lại: vận đơn hiện hành của đơn phải là vận đơn có thật (cả crm_app lẫn chủ bảng).
func TestSchemaM3_CurrentShipmentFK(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	wantCode(t, "crm_app trỏ vận đơn không có", e.appExec(
		"UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1", o.ID, uuid.New()), "23503")
	wantCode(t, "chủ bảng trỏ vận đơn không có", e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1", o.ID, uuid.New())
		return err //nolint:wrapcheck // test
	}), "23503")
	s := e.mustCreateShipment(o.ID)
	if got := e.getOrder(o.ID); got.CurrentShipmentID == nil || *got.CurrentShipmentID != s.ID {
		t.Fatalf("CreateForOrder không gắn vận đơn hiện hành: %v", got.CurrentShipmentID)
	}
}
