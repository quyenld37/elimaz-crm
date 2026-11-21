package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Giao một phần qua HTTP (kế hoạch M3, Task M3-26): recordPartialDelivery, rồi kho nhận phiếu qua receiveReturn của M2.

const (
	partialUser = "giao-mot-phan@elimaz.test"
	receiver    = "nhan-hang@elimaz.test"
)

// partialShipment tạo đơn 2 cái (250.000 mỗi cái, phí 30.000 → COD 530.000), gửi hãng, chạy worker, đặt vận đơn đang
// giao; trả mã vận đơn và dòng đơn.
func (e *shipAPIEnv) partialShipment(key string) (shipment string, item uuid.UUID) {
	e.t.Helper()
	v := e.variant(e.shop.Seller, 250000, 5)
	orderID := fmt.Sprint(e.mustCreate("o-"+key, orderBody(30000, line(v, 2)))["id"])
	shipment = e.shipOrder(orderID, key)
	e.staff.Exec(e.t, e.shop.Tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(e.t.Context(), "UPDATE fulfillment.shipments SET status = 'shipping' WHERE id = $1",
			shipment); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(e.t.Context(), "SELECT id FROM ord.order_items WHERE order_id = $1", orderID).Scan(&item)
	})
	return shipment, item
}

// Thiếu fulfillment.correct → 403; giao đủ, giao 0 → 422 qty_mismatch; thu 700.000 > COD → 422 payment_exceeds_total;
// ghi đúng → 200 vận đơn delivered, partial, codCollectedAmount; bấm lại cùng khóa → cùng thân, một phiếu, một audit
// (lý do đã che SĐT); khóa khác → 409 invalid_transition. Kho nhận phiếu qua receiveReturn → 200 restocked.
func TestRecordPartialDeliveryAPI(t *testing.T) {
	e := newShipAPIEnv(t)
	tenant := e.shop.Tenant
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, partialUser, "active"), []string{"fulfillment.correct"}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, receiver, "active"), []string{"order.read", "order.return.receive"},
		"all", "")
	id, item := e.partialShipment("a")
	path := "/api/v1/shipments/" + id + "/partial-delivery"
	body := func(qty int, cod int64) string {
		return fmt.Sprintf(`{"items":[{"orderItemId":"%s","deliveredQty":%d}],"codCollected":%d,`+
			`"reason":"Khách nhận 1 cái, gọi 0912345678"}`, item, qty, cod)
	}
	if rec := serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, "n1", body(1, 280000)); rec.Code != http.StatusForbidden {
		t.Fatalf("thiếu fulfillment.correct = %d %s, muốn 403", rec.Code, rec.Body.String())
	}
	for i, c := range []struct {
		name string
		body string
		code string
	}{
		{"giao đủ", body(2, 530000), "qty_mismatch"},
		{"giao 0", body(0, 0), "qty_mismatch"},
		{"thu hơn COD", body(1, 700000), "payment_exceeds_total"},
	} {
		rec := serveAs(e.srv, e.keys, http.MethodPost, path, partialUser, fmt.Sprint("r", i), c.body)
		if b := decodeJSON(t, rec); rec.Code != http.StatusUnprocessableEntity || b["error"] != c.code {
			t.Errorf("%s = %d %v, muốn 422 %s", c.name, rec.Code, b, c.code)
		}
	}
	rec1 := serveAs(e.srv, e.keys, http.MethodPost, path, partialUser, "p1", body(1, 280000))
	rec2 := serveAs(e.srv, e.keys, http.MethodPost, path, partialUser, "p1", body(1, 280000))
	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK || rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("bấm hai lần cùng khóa: %d %d\n%s\n%s", rec1.Code, rec2.Code, rec1.Body.String(), rec2.Body.String())
	}
	if s := decodeJSON(t, rec1); s["status"] != "delivered" || s["partial"] != true || s["codCollectedAmount"] != float64(280000) {
		t.Fatalf("sau ghi = %v", s)
	}
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE shipment_id = $1 AND kind = 'partial_delivery'`, id); n != 1 {
		t.Fatalf("phiếu partial_delivery = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.partial_delivery'
		AND after->>'reason' NOT LIKE '%0912345678%' AND after->>'returnCode' LIKE 'TH%'
		AND (after->>'codCollected')::bigint = 280000`); n != 1 {
		t.Fatalf("audit giao một phần (đã che SĐT) = %d, muốn 1", n)
	}
	rec := serveAs(e.srv, e.keys, http.MethodPost, path, partialUser, "p2", body(1, 280000))
	if b := decodeJSON(t, rec); rec.Code != http.StatusConflict || b["error"] != "invalid_transition" {
		t.Fatalf("khóa khác = %d %v, muốn 409 invalid_transition", rec.Code, b)
	}

	var returnID string
	var version int64
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id, version FROM ord.returns WHERE shipment_id = $1`, id).
			Scan(&returnID, &version)
	})
	rec = serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/returns/"+returnID+"/receive", receiver, "rv1",
		fmt.Sprintf(`{"version":%d,"lines":[{"orderItemId":"%s","condition":"sellable"}]}`, version, item))
	if b := decodeJSON(t, rec); rec.Code != http.StatusOK || b["status"] != "restocked" || b["kind"] != "partial_delivery" {
		t.Fatalf("nhận phiếu = %d %v", rec.Code, b)
	}
}
