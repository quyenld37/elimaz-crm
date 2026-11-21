package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Nhập mã tay và cập nhật tay qua HTTP (kế hoạch M3, Task M3-15): createShipment với tài khoản tay,
// setShipmentManualStatus.

const manualUser = "giao-tay@elimaz.test"

// manualAccount tạo tài khoản tay (active) của hãng key cho ELIMAZ ở kho MAIN.
func (e *shipAPIEnv) manualAccount(key, ref string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		id = shiptest.ManualAccount(e.t, tx, e.shop.Seller, e.shop.Warehouse, key, ref)
		return nil
	})
	return id
}

// grantManual tạo nhân viên manualUser có quyền gửi hãng, xem và ghi tay trạng thái vận đơn ở mọi kho.
func (e *shipAPIEnv) grantManual() {
	e.t.Helper()
	tenant := e.shop.Tenant
	e.staff.Grant(e.t, tenant, e.staff.User(e.t, tenant, manualUser, "active"),
		[]string{"fulfillment.ship", "fulfillment.read", "fulfillment.manual_status"}, "all", "")
}

func (e *shipAPIEnv) manualStatus(user, id, key, body string) (int, map[string]any) {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/shipments/"+id+"/manual-status", user, key, body)
	return rec.Code, decodeJSON(e.t, rec)
}

// VC14 qua HTTP: tạo vận đơn tay có mã → 202 awaiting_pickup, createState manual; thiếu fulfillment.manual_status →
// 403; lý do chỉ khoảng trắng → 422; ghi shipping hai lần cùng khóa → cùng thân, một dòng lịch sử, một audit (lý do đã
// che SĐT); delivered có codCollected → 200; vận đơn api không việc hủy tay → 409 invalid_transition; trùng mã ở tài
// khoản tay khác → 409 tracking_code_conflict; vận đơn lạ → 404.
func TestShipmentManualAPI(t *testing.T) {
	e := newShipAPIEnv(t)
	e.grantManual()
	a1, a2 := e.manualAccount("best", "best-tay-1"), e.manualAccount("best", "best-tay-2")

	orderID := e.confirmedOrder("o-1")
	code, s := e.post("/api/v1/orders/"+orderID+"/shipments", "s-1",
		fmt.Sprintf(`{"carrierAccountId":"%s","trackingCode":"BEST123456","feeActual":32000}`, a1))
	if code != http.StatusAccepted || s["status"] != "awaiting_pickup" || s["createState"] != "manual" ||
		s["trackingCode"] != "BEST123456" || s["mode"] != "manual" {
		t.Fatalf("tạo vận đơn tay = %d %v", code, s)
	}
	id := fmt.Sprint(s["id"])
	at := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{"to":"shipping","at":"%s","reason":"Best đã lấy, gọi 0912345678"}`, at)

	if code, b := e.manualStatus(shipAll, id, "m0", body); code != http.StatusForbidden {
		t.Fatalf("thiếu fulfillment.manual_status = %d %v, muốn 403", code, b)
	}
	if code, b := e.manualStatus(manualUser, id, "m1", fmt.Sprintf(`{"to":"shipping","at":"%s","reason":"   "}`, at)); code != http.StatusUnprocessableEntity ||
		b["error"] != "invalid_input" {
		t.Fatalf("lý do khoảng trắng = %d %v, muốn 422 invalid_input", code, b)
	}
	rec1 := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/shipments/"+id+"/manual-status", manualUser, "m2", body)
	rec2 := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/shipments/"+id+"/manual-status", manualUser, "m2", body)
	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK || rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("ghi tay hai lần cùng khóa: %d %d\n%s\n%s", rec1.Code, rec2.Code, rec1.Body.String(), rec2.Body.String())
	}
	if b := decodeJSON(t, rec1); b["status"] != "shipping" {
		t.Fatalf("sau ghi shipping = %v", b)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1 AND source = 'staff'", id); n != 1 {
		t.Fatalf("lịch sử staff = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.manual_status'
		AND entity_id = $1 AND after->>'reason' NOT LIKE '%0912345678%' AND after->>'to' = 'shipping'`, id); n != 1 {
		t.Fatalf("audit ghi tay (đã che SĐT) = %d, muốn 1", n)
	}
	at2 := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if code, b := e.manualStatus(manualUser, id, "m3",
		fmt.Sprintf(`{"to":"delivered","at":"%s","reason":"Best báo giao xong","codCollected":280000}`, at2)); code != http.StatusOK ||
		b["status"] != "delivered" || b["codCollectedAmount"] != float64(280000) {
		t.Fatalf("ghi delivered = %d %v", code, b)
	}

	api := e.awaitingShipment("api")
	if code, b := e.manualStatus(manualUser, api, "m4", body); code != http.StatusConflict || b["error"] != "invalid_transition" {
		t.Fatalf("vận đơn api = %d %v, muốn 409 invalid_transition", code, b)
	}
	if code, b := e.manualStatus(manualUser, uuid.NewString(), "m5", body); code != http.StatusNotFound {
		t.Fatalf("vận đơn lạ = %d %v, muốn 404", code, b)
	}

	order2 := e.confirmedOrder("o-2")
	if code, b := e.post("/api/v1/orders/"+order2+"/shipments", "s-2",
		fmt.Sprintf(`{"carrierAccountId":"%s","trackingCode":"BEST123456"}`, a2)); code != http.StatusConflict ||
		b["error"] != "tracking_code_conflict" {
		t.Fatalf("trùng mã = %d %v, muốn 409 tracking_code_conflict", code, b)
	}
	if code, b := e.post("/api/v1/orders/"+order2+"/shipments", "s-3",
		fmt.Sprintf(`{"carrierAccountId":"%s"}`, a2)); code != http.StatusBadRequest || b["error"] != "invalid_input" {
		t.Fatalf("tay thiếu mã = %d %v, muốn 400 invalid_input", code, b)
	}
}
