package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Tài khoản hãng gửi được cho đơn (Task 449, M3-x2): listOrderShippingAccounts theo fulfillment.ship ở kho của đơn.

const (
	saleMain = "ban-hang-kho-chinh@elimaz.test"
	saleW2   = "ban-hang-kho-phu@elimaz.test"
	noShip   = "chi-xem-don@elimaz.test"
)

// seedShipAccounts thêm vào kho MAIN tài khoản api hãng "ghost" (không có adapter), nhập tay ghn, tự giao, nhập tay
// best đang tạm dừng; tạo kho W2 có tài khoản nhập tay spx. Trả id theo tên và id kho W2.
func seedShipAccounts(e *shipAPIEnv) (map[string]uuid.UUID, uuid.UUID) {
	e.t.Helper()
	t := e.t
	var w2 uuid.UUID
	ids := map[string]uuid.UUID{}
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, 'W2', 'Kho phụ') RETURNING id`, e.shop.Tenant, e.shop.Seller).Scan(&w2); err != nil {
			return err //nolint:wrapcheck // test
		}
		ghost, err := fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: "ghost", Name: "API ghost", ExternalAccountRef: "ghost-1",
		})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		ids["ghost"] = ghost
		ids["ghn"] = shiptest.ManualAccount(t, tx, e.shop.Seller, e.shop.Warehouse, "ghn", "ghn-tay")
		ids["self"] = shiptest.ManualAccount(t, tx, e.shop.Seller, e.shop.Warehouse, fulfillment.SelfCarrierKey, "tu-giao")
		ids["paused"] = shiptest.ManualAccount(t, tx, e.shop.Seller, e.shop.Warehouse, "best", "best-dung")
		ids["w2"] = shiptest.ManualAccount(t, tx, e.shop.Seller, w2, "spx", "spx-kho-phu")
		_, err = tx.Exec(t.Context(), `UPDATE fulfillment.carrier_accounts SET status = 'paused' WHERE id = $1`, ids["paused"])
		return err //nolint:wrapcheck // test
	})
	return ids, w2
}

// shipAccountRows đọc listOrderShippingAccounts của user: 200, mỗi dòng chỉ id, name, carrierKey, mode, status
// (status active); trả các dòng theo thứ tự máy chủ.
func shipAccountRows(t *testing.T, e *shipAPIEnv, path, user string) []map[string]any {
	t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodGet, path, user, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d %s, muốn 200", user, rec.Code, rec.Body.String())
	}
	items, _ := decodeJSON(t, rec)["items"].([]any)
	out := make([]map[string]any, 0, len(items))
	allowed := []string{"id", "name", "carrierKey", "mode", "status"}
	for _, it := range items {
		m, _ := it.(map[string]any)
		for k := range m {
			if !slices.Contains(allowed, k) {
				t.Fatalf("dòng có trường %q ngoài %v: %v", k, allowed, m)
			}
		}
		if m["status"] != "active" {
			t.Fatalf("dòng không active: %v", m)
		}
		out = append(out, m)
	}
	return out
}

// Bán hàng (vai mẫu sale, phạm vi kho MAIN; không có fulfillment.carrier_accounts.read, listCarrierAccounts → 403) đọc
// tài khoản của đơn ở kho MAIN → 200 đúng các tài khoản gửi được của kho MAIN (hai tài khoản api hãng fake có adapter,
// nhập tay ghn, tự giao; cùng web_sort nên xếp theo id), không có tài khoản kho W2, tài khoản tạm dừng, tài khoản api
// không có adapter; mỗi dòng chỉ id, name, carrierKey, mode, status. Bán hàng chỉ ở kho W2 → 403; không có
// fulfillment.ship → 403; đơn lạ → 404.
func TestOrderShippingAccountsAPI(t *testing.T) {
	e := newShipCancelAPIEnv(t)
	tenant := e.shop.Tenant
	ids, w2 := seedShipAccounts(e)
	sale := rolePerms(t, "sale")
	if slices.Contains(sale, "fulfillment.carrier_accounts.read") {
		t.Fatal("vai sale có fulfillment.carrier_accounts.read: test không còn kiểm đúng ca Bán hàng")
	}
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, saleMain, "active"), sale, "warehouse", e.shop.Warehouse.String())
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, saleW2, "active"), sale, "warehouse", w2.String())
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, noShip, "active"), []string{"order.read", "fulfillment.read"}, "all", "")
	path := "/api/v1/orders/" + e.confirmedOrder("sa1") + "/shipping-accounts"

	if rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/carrier-accounts", saleMain, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("Bán hàng đọc listCarrierAccounts = %d, muốn 403 (không cấp thêm quyền)", rec.Code)
	}
	rows := shipAccountRows(t, e, path, saleMain)
	got := make([]string, 0, len(rows))
	for _, m := range rows {
		got = append(got, fmt.Sprint(m["id"]))
	}
	want := []string{e.accounts[0].String(), e.accounts[1].String(), ids["ghn"].String(), ids["self"].String()}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("tài khoản = %v, muốn %v (fake ×2, ghn, self theo id; không ghost, paused, w2)", got, want)
	}
	i := slices.Index(got, ids["ghn"].String())
	if m := rows[i]; m["mode"] != "manual" || m["carrierKey"] != "ghn" || m["name"] != "Tay ghn-tay" {
		t.Fatalf("dòng ghn = %v", m)
	}
	if m := rows[slices.Index(got, e.accounts[0].String())]; m["mode"] != "api" || m["carrierKey"] != "fake" {
		t.Fatalf("dòng api = %v", m)
	}
	for _, who := range []string{saleW2, noShip} {
		if rec := serveAs(e.srv, e.keys, http.MethodGet, path, who, "", ""); rec.Code != http.StatusForbidden ||
			decodeJSON(t, rec)["error"] != "forbidden" {
			t.Fatalf("%s = %d %s, muốn 403 forbidden", who, rec.Code, rec.Body.String())
		}
	}
	other := "/api/v1/orders/" + uuid.NewString() + "/shipping-accounts"
	if rec := serveAs(e.srv, e.keys, http.MethodGet, other, saleMain, "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("đơn lạ = %d, muốn 404", rec.Code)
	}
}
