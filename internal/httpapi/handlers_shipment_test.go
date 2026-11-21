package httpapi

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

const (
	shipAll    = "giao-hang@elimaz.test"
	shipW2     = "giao-kho2@elimaz.test"
	shipReader = "xem-van-don@elimaz.test"
)

// Tạo vận đơn (202, creating, mã E…), lần hai cùng đơn → 409 shipment_exists; có fulfillment.ship ở kho w2 mà đơn ở
// kho MAIN → 404 (Task M3-12: phạm vi theo kho của đơn, không lộ đơn; M1: 403 theo kho tài khoản); chỉ có order.read →
// getShipment 403; fulfillment.read → 200.
func TestShipmentAPI(t *testing.T) {
	e := newOrderAPIEnv(t)
	tenant := e.shop.Tenant
	var account, w2 uuid.UUID
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		var err error
		account, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: "fake", Name: "Hãng giả", ExternalAccountRef: "fake-1",
		})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(t.Context(), `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
			VALUES ($1, $2, 'W2', 'Kho phụ') RETURNING id`, tenant, e.shop.Seller).Scan(&w2)
	})
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipAll, "active"), []string{"fulfillment.ship", "fulfillment.read"}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipW2, "active"), []string{"fulfillment.ship", "fulfillment.read"}, "warehouse", w2.String())
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipReader, "active"), []string{"order.read"}, "all", "")
	e.srv = shipServer(t, e)

	v := e.variant(e.shop.Seller, 250000, 5)
	orderID := e.mustCreate("o1", orderBody(30000, line(v, 2)))["id"]
	path := fmt.Sprintf("/api/v1/orders/%v/shipments", orderID)
	body := fmt.Sprintf(`{"carrierAccountId":"%s"}`, account)

	if rec := serveAs(e.srv, e.keys, http.MethodPost, path, shipW2, "s0", body); rec.Code != http.StatusNotFound {
		t.Fatalf("kho w2 gửi đơn của kho MAIN = %d %s, muốn 404", rec.Code, rec.Body.String())
	}
	rec := serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, "s1", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("createShipment = %d %s", rec.Code, rec.Body.String())
	}
	s := decodeJSON(t, rec)
	if s["status"] != "creating" || s["codAmount"] != float64(530000) || s["orderId"] != orderID ||
		!regexp.MustCompile(`^E[0-9]{6}[0-9A-Z]{6}$`).MatchString(fmt.Sprint(s["code"])) || s["trackingCode"] != nil {
		t.Fatalf("vận đơn = %v", s)
	}
	if rec := serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, "s1", body); rec.Code != http.StatusAccepted {
		t.Fatalf("gửi lại cùng khóa = %d, muốn 202 như lần đầu", rec.Code)
	}
	rec = serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, "s2", body)
	if rec.Code != http.StatusConflict || decodeJSON(t, rec)["error"] != "shipment_exists" {
		t.Fatalf("vận đơn thứ hai = %d %s, muốn 409 shipment_exists", rec.Code, rec.Body.String())
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments"); n != 1 {
		t.Fatalf("vận đơn = %d, muốn 1", n)
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.create'"); n != 1 {
		t.Fatalf("audit = %d, muốn 1", n)
	}
	other := fmt.Sprintf("/api/v1/orders/%s/shipments", uuid.New())
	if rec := serveAs(e.srv, e.keys, http.MethodPost, other, shipAll, "s3", body); rec.Code != http.StatusNotFound {
		t.Fatalf("đơn lạ = %d, muốn 404", rec.Code)
	}

	get := fmt.Sprintf("/api/v1/shipments/%v", s["id"])
	if rec := serveAs(e.srv, e.keys, http.MethodGet, get, shipReader, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("chỉ order.read gọi getShipment = %d, muốn 403", rec.Code)
	}
	if rec := serveAs(e.srv, e.keys, http.MethodGet, get, shipW2, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("fulfillment.read kho w2 = %d, muốn 403", rec.Code)
	}
	rec = serveAs(e.srv, e.keys, http.MethodGet, get, shipAll, "", "")
	if rec.Code != http.StatusOK || decodeJSON(t, rec)["code"] != s["code"] {
		t.Fatalf("getShipment = %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/shipments/"+uuid.NewString(), shipAll, "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("vận đơn lạ = %d, muốn 404", rec.Code)
	}
}
