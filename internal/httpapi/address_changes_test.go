package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
)

// Đổi địa chỉ sau khi đã gửi hãng qua HTTP (kế hoạch M3, Task M3-17, DH15). Service của server thử không có adapter
// hãng → yêu cầu manual (nhân viên gọi hãng rồi xác nhận).

const (
	addrUser   = "doi-dia-chi@elimaz.test"
	addrViewer = "xem-van-don@elimaz.test"
)

func (e *shipAPIEnv) addrPost(user, path, key, body string) (int, map[string]any) {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodPost, path, user, key, body)
	return rec.Code, decodeJSON(e.t, rec)
}

// addrShipment tạo đơn đã gửi hãng (awaiting_pickup, awaitingShipment) và trả mã đơn, mã vận đơn.
func (e *shipAPIEnv) addrShipment(key string) (orderID, shipmentID string) {
	e.t.Helper()
	shipmentID = e.awaitingShipment(key)
	code, s := e.get("/api/v1/shipments/" + shipmentID)
	if code != http.StatusOK {
		e.t.Fatalf("đọc vận đơn = %d %v", code, s)
	}
	return fmt.Sprint(s["orderId"]), shipmentID
}

func addrJSON(ward string) string {
	return fmt.Sprintf(`{"name":"Chị Lan","phone":"0912345678","line":"45 Trần Hưng Đạo","provinceCode":"%s","wardCode":"%s"}`,
		testkit.SampleProvinceCode, ward)
}

// DH15 qua HTTP: updateOrder đổi địa chỉ khi có vận đơn → 409 order_locked_by_shipment; thiếu fulfillment.ship → 403;
// thiếu xã → 400 (hợp đồng); requestAddressChange → 201 pending manual, cùng khóa → cùng thân, địa chỉ đơn vẫn
// cũ, audit yêu cầu không có SĐT; yêu cầu thứ hai → 409 already_exists; resolve confirmed → 200, địa chỉ đơn mới, audit
// order.address_change có changed_fields ship_address mà giá trị bị che; resolve lần hai → 409 invalid_transition; yêu
// cầu lạ → 404.
func TestAddressChangeAPI_DH15(t *testing.T) {
	e := newShipAPIEnv(t)
	tenant := e.shop.Tenant
	e.staff.SeedAdminUnits(t)
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, addrUser, "active"),
		[]string{"fulfillment.ship", "fulfillment.read", "order.read", "order.update"}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, addrViewer, "active"), []string{"fulfillment.read"}, "all", "")
	orderID, id := e.addrShipment("dh15")

	o := decodeJSON(t, serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/orders/"+orderID, addrUser, "", ""))
	patch := e.patchOrder(addrUser, "u-1", orderID, fmt.Sprintf(`{"version":%v,"shipAddress":%s}`, o["version"],
		addrJSON(testkit.SampleWardCode)))
	if b := decodeJSON(t, patch); patch.Code != http.StatusConflict || b["error"] != "order_locked_by_shipment" {
		t.Fatalf("updateOrder đổi địa chỉ khi có vận đơn = %d %v", patch.Code, b)
	}

	path := "/api/v1/shipments/" + id + "/address-changes"
	body := fmt.Sprintf(`{"address":%s,"note":"Khách gọi 0987654321"}`, addrJSON(testkit.SampleWardCode))
	if code, b := e.addrPost(addrViewer, path, "a-0", body); code != http.StatusForbidden {
		t.Fatalf("thiếu fulfillment.ship = %d %v, muốn 403", code, b)
	}
	if code, b := e.addrPost(addrUser, path, "a-1", fmt.Sprintf(`{"address":%s}`, addrJSON(""))); code != http.StatusBadRequest {
		t.Fatalf("thiếu xã = %d %v, muốn 400 (hợp đồng đòi wardCode)", code, b)
	}
	rec1 := serveAs(e.srv, e.keys, http.MethodPost, path, addrUser, "a-2", body)
	rec2 := serveAs(e.srv, e.keys, http.MethodPost, path, addrUser, "a-2", body)
	if rec1.Code != http.StatusCreated || rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("yêu cầu hai lần cùng khóa: %d %d\n%s\n%s", rec1.Code, rec2.Code, rec1.Body.String(), rec2.Body.String())
	}
	c := decodeJSON(t, rec1)
	if c["status"] != "pending" || c["mode"] != "manual" || c["shipmentId"] != id || c["resolvedAt"] != nil {
		t.Fatalf("yêu cầu = %v", c)
	}
	changeID := fmt.Sprint(c["id"])
	if n := e.count("SELECT count(*) FROM ord.orders WHERE id = $1 AND ship_address->>'wardCode' = $2", orderID,
		testkit.SampleWardCode); n != 0 {
		t.Fatal("địa chỉ đơn đổi trước khi hãng nhận")
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.address_change.request'
		AND entity_id = $1 AND after::text NOT LIKE '%0987654321%' AND after::text NOT LIKE '%Trần%'`, changeID); n != 1 {
		t.Fatalf("audit yêu cầu (không SĐT, địa chỉ) = %d, muốn 1", n)
	}
	if code, b := e.addrPost(addrUser, path, "a-3", body); code != http.StatusConflict || b["error"] != "already_exists" {
		t.Fatalf("yêu cầu thứ hai = %d %v, muốn 409 already_exists", code, b)
	}

	resolve := "/api/v1/address-changes/" + changeID + "/resolve"
	if code, b := e.addrPost(addrViewer, resolve, "r-0", `{"result":"confirmed"}`); code != http.StatusForbidden {
		t.Fatalf("xác nhận thiếu fulfillment.ship = %d %v, muốn 403", code, b)
	}
	code, b := e.addrPost(addrUser, resolve, "r-1", `{"result":"confirmed","note":"Đã gọi hãng"}`)
	if code != http.StatusOK || b["status"] != "confirmed" || b["resolvedAt"] == nil || b["note"] != "Đã gọi hãng" {
		t.Fatalf("xác nhận = %d %v", code, b)
	}
	if n := e.count(`SELECT count(*) FROM ord.orders WHERE id = $1 AND ship_address->>'wardCode' = $2
		AND ship_address->>'line' = '45 Trần Hưng Đạo'`, orderID, testkit.SampleWardCode); n != 1 {
		t.Fatal("địa chỉ đơn chưa đổi sau khi xác nhận")
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'order.address_change' AND entity_type = 'order'
		AND entity_id = $1 AND actor_type = 'staff' AND 'ship_address' = ANY(changed_fields)
		AND after::text NOT LIKE '%Trần%' AND after::text NOT LIKE '%0912345678%'
		AND before::text NOT LIKE '%Lê Lợi%'`, orderID); n != 1 {
		t.Fatalf("audit order.address_change (ship_address, không giá trị) = %d, muốn 1", n)
	}
	if code, b := e.addrPost(addrUser, resolve, "r-2", `{"result":"rejected"}`); code != http.StatusConflict ||
		b["error"] != "invalid_transition" {
		t.Fatalf("xác nhận lần hai = %d %v, muốn 409 invalid_transition", code, b)
	}
	if code, b := e.addrPost(addrUser, "/api/v1/address-changes/"+uuid.NewString()+"/resolve", "r-3",
		`{"result":"confirmed"}`); code != http.StatusNotFound {
		t.Fatalf("yêu cầu lạ = %d %v, muốn 404", code, b)
	}
}

// Vận đơn đã giao (hàng đã tới khách) → 409 invalid_transition; vận đơn lạ → 404; xã không có trong danh mục → 422.
func TestAddressChangeAPI_Refusals(t *testing.T) {
	e := newShipAPIEnv(t)
	e.staff.SeedAdminUnits(t)
	_, id := e.addrShipment("r1")
	body := fmt.Sprintf(`{"address":%s}`, addrJSON(testkit.SampleWardCode))
	if code, b := e.addrPost(shipAll, "/api/v1/shipments/"+uuid.NewString()+"/address-changes", "x-1", body); code != http.StatusNotFound {
		t.Fatalf("vận đơn lạ = %d %v, muốn 404", code, b)
	}
	if code, b := e.addrPost(shipAll, "/api/v1/shipments/"+id+"/address-changes", "x-2",
		fmt.Sprintf(`{"address":%s}`, addrJSON("T01999"))); code != http.StatusUnprocessableEntity || b["error"] != "unknown_admin_unit" {
		t.Fatalf("xã lạ = %d %v, muốn 422 unknown_admin_unit", code, b)
	}
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.shipments SET status = 'delivered', delivered_at = now() WHERE id = $1", id)
		return err //nolint:wrapcheck // test
	})
	if code, b := e.addrPost(shipAll, "/api/v1/shipments/"+id+"/address-changes", "x-3", body); code != http.StatusConflict ||
		b["error"] != "invalid_transition" {
		t.Fatalf("đã giao = %d %v, muốn 409 invalid_transition", code, b)
	}
}

// listShipmentAddressChanges (Task 134): chỉ yêu cầu đang mở (pending, sent, unknown), không có địa chỉ; chưa có → rỗng;
// sau khi xác nhận → rỗng; fulfillment.read ở kho khác → 404; chỉ order.read → 403; vận đơn lạ → 404.
func TestAddressChangeAPI_ListOpen(t *testing.T) {
	e := newShipAPIEnv(t)
	e.staff.SeedAdminUnits(t)
	e.staff.Grant(t, e.shop.Tenant, e.staff.User(t, e.shop.Tenant, shipW2, "active"), []string{"fulfillment.read"},
		"warehouse", uuid.NewString())
	_, id := e.addrShipment("lo")
	path := "/api/v1/shipments/" + id + "/address-changes"
	list := func(user string) (int, []any) {
		t.Helper()
		rec := serveAs(e.srv, e.keys, http.MethodGet, path, user, "", "")
		if rec.Code != http.StatusOK {
			return rec.Code, nil
		}
		items, ok := decodeJSON(t, rec)["items"].([]any)
		if !ok {
			t.Fatalf("thân không có items: %s", rec.Body.String())
		}
		return rec.Code, items
	}
	if code, items := list(shipAll); code != http.StatusOK || len(items) != 0 {
		t.Fatalf("chưa có yêu cầu = %d %v, muốn 200 rỗng", code, items)
	}
	if code, _ := list(shipW2); code != http.StatusNotFound {
		t.Fatalf("fulfillment.read kho khác = %d, muốn 404", code)
	}
	if code, _ := list(orderViewer); code != http.StatusForbidden {
		t.Fatalf("chỉ order.read = %d, muốn 403", code)
	}
	rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/shipments/"+uuid.NewString()+"/address-changes", shipAll, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("vận đơn lạ = %d, muốn 404", rec.Code)
	}

	code, c := e.addrPost(shipAll, path, "lo-1", fmt.Sprintf(`{"address":%s,"note":"Khách nhờ"}`,
		addrJSON(testkit.SampleWardCode)))
	if code != http.StatusCreated {
		t.Fatalf("yêu cầu = %d %v", code, c)
	}
	code, items := list(shipAll)
	if code != http.StatusOK || len(items) != 1 {
		t.Fatalf("có một yêu cầu mở = %d %v", code, items)
	}
	got, _ := items[0].(map[string]any)
	if got["id"] != c["id"] || got["status"] != "pending" || got["mode"] != "manual" || got["shipmentId"] != id ||
		got["note"] != "Khách nhờ" {
		t.Fatalf("yêu cầu mở = %v", got)
	}
	for _, k := range []string{"address", "newAddress", "line", "phone"} {
		if _, ok := got[k]; ok {
			t.Fatalf("yêu cầu mở lộ khóa %q: %v", k, got)
		}
	}
	if code, b := e.addrPost(shipAll, "/api/v1/address-changes/"+fmt.Sprint(c["id"])+"/resolve", "lo-2",
		`{"result":"rejected"}`); code != http.StatusOK {
		t.Fatalf("hãng không đổi được = %d %v", code, b)
	}
	if code, items := list(shipAll); code != http.StatusOK || len(items) != 0 {
		t.Fatalf("sau khi đóng = %d %v, muốn rỗng", code, items)
	}
}
