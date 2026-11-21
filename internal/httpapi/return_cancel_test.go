package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// Hủy phiếu hoàn của hãng qua HTTP (Task 442): vận đơn tay ghi shipping rồi returning → phiếu carrier_return requested;
// cancelReturn → 422 return_not_allowed, phiếu vẫn requested cùng version.

const returnCanceller = "huy-phieu@elimaz.test"

func TestCancelCarrierReturnAPI(t *testing.T) {
	e := newShipAPIEnv(t)
	tenant := e.shop.Tenant
	e.grantManual()
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, returnCanceller, "active"), []string{"order.read", "order.return"},
		"all", "")
	acc := e.manualAccount("best", "best-tay-huy")
	orderID := e.confirmedOrder("o-h")
	code, s := e.post("/api/v1/orders/"+orderID+"/shipments", "s-h",
		fmt.Sprintf(`{"carrierAccountId":"%s","trackingCode":"BEST900001","feeActual":32000}`, acc))
	if code != http.StatusAccepted {
		t.Fatalf("tạo vận đơn tay = %d %v", code, s)
	}
	id := fmt.Sprint(s["id"])
	at := func(ago time.Duration) string { return time.Now().UTC().Add(-ago).Format(time.RFC3339) }
	for i, to := range []struct {
		status string
		ago    time.Duration
	}{{"shipping", time.Hour}, {"returning", time.Minute}} {
		if code, b := e.manualStatus(manualUser, id, fmt.Sprint("h", i),
			fmt.Sprintf(`{"to":"%s","at":"%s","reason":"Best báo"}`, to.status, at(to.ago))); code != http.StatusOK ||
			b["status"] != to.status {
			t.Fatalf("ghi %s = %d %v", to.status, code, b)
		}
	}
	returnID, version := e.returnOf(id, "carrier_return")
	rec := serveAs(e.srv, e.keys, http.MethodPost, "/api/v1/returns/"+returnID+"/cancel", returnCanceller, "c1",
		fmt.Sprintf(`{"version":%d}`, version))
	if b := decodeJSON(t, rec); rec.Code != http.StatusUnprocessableEntity || b["error"] != "return_not_allowed" {
		t.Fatalf("hủy phiếu hoàn của hãng = %d %v, muốn 422 return_not_allowed", rec.Code, b)
	}
	if n := e.count(`SELECT count(*) FROM ord.returns WHERE id = $1 AND status = 'requested' AND version = $2`,
		returnID, version); n != 1 {
		t.Fatal("phiếu hoàn của hãng đổi sau lần hủy bị chặn")
	}
}
