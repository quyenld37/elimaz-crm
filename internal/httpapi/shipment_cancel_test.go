package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// Đổi hãng qua HTTP (kế hoạch M3, Task M3-14): cancelShipment.

// cancelMemCarrier là memCarrier hủy được qua API (mọi lần hủy: CancelDone).
type cancelMemCarrier struct{ *memCarrier }

func (cancelMemCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Create: true, LookupByRef: true, Cancel: true}
}

func (cancelMemCarrier) Cancel(context.Context, fulfillment.Account, string) (fulfillment.CancelResult, error) {
	return fulfillment.CancelDone, nil
}

const cancelUser = "huy-van-don@elimaz.test"

// newShipCancelAPIEnv: shipAPIEnv mà máy chủ có bộ hãng (hãng giả hủy được); cancelUser có gửi, xem, hủy (shipAll chỉ
// có gửi và xem).
func newShipCancelAPIEnv(t *testing.T) *shipAPIEnv {
	t.Helper()
	e := newShipAPIEnv(t)
	reg, err := fulfillment.NewRegistry(cancelMemCarrier{e.carrier})
	if err != nil {
		t.Fatal(err)
	}
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Fulfillment = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Events: events.NewRegistry(), Carriers: reg,
	}).Service
	e.srv = NewServer(d)
	tenant := e.shop.Tenant
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, cancelUser, "active"),
		[]string{"fulfillment.ship", "fulfillment.read", "fulfillment.cancel"}, "all", "")
	return e
}

// cancel gọi cancelShipment bằng cancelUser.
func (e *shipAPIEnv) cancel(path, key, body string) (int, map[string]any) {
	e.t.Helper()
	rec := serveAs(e.srv, e.keys, http.MethodPost, path, cancelUser, key, body)
	return rec.Code, decodeJSON(e.t, rec)
}

// awaitingShipment tạo đơn, gửi hãng, chạy worker; trả mã vận đơn (chờ thu gom).
func (e *shipAPIEnv) awaitingShipment(key string) string {
	e.t.Helper()
	return e.shipOrder(e.confirmedOrder("o-"+key), key)
}

// shipOrder gửi hãng đơn orderID bằng tài khoản đầu (khóa s-<key>), chạy worker; trả mã vận đơn (chờ thu gom).
func (e *shipAPIEnv) shipOrder(orderID, key string) string {
	e.t.Helper()
	code, s := e.post("/api/v1/orders/"+orderID+"/shipments", "s-"+key, fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[0]))
	if code != http.StatusAccepted {
		e.t.Fatalf("gửi hãng = %d %v", code, s)
	}
	e.drain()
	return fmt.Sprint(s["id"])
}

// Bấm hủy hai lần cùng khóa → cùng thân, một lệnh hủy, một audit; khóa khác → 409 shipment_not_cancellable; thiếu
// fulfillment.cancel → 403; vận đơn đã đi → 409 shipment_not_cancellable; "không rõ" → 409 create_in_progress, có
// carrierChecked → 200 cancelled staff_after_unknown.
func TestCancelShipmentAPI(t *testing.T) {
	t.Parallel()
	e := newShipCancelAPIEnv(t)
	id := e.awaitingShipment("a")
	path := "/api/v1/shipments/" + id + "/cancel"
	body := `{"reason":"Đổi sang hãng khác, gọi 0912345678"}`
	if rec := serveAs(e.srv, e.keys, http.MethodPost, path, shipAll, "n1", body); rec.Code != http.StatusForbidden {
		t.Fatalf("thiếu fulfillment.cancel = %d %s, muốn 403", rec.Code, rec.Body.String())
	}
	rec1 := serveAs(e.srv, e.keys, http.MethodPost, path, cancelUser, "c1", body)
	rec2 := serveAs(e.srv, e.keys, http.MethodPost, path, cancelUser, "c1", body)
	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK || rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("bấm hai lần cùng khóa: %d %d\n%s\n%s", rec1.Code, rec2.Code, rec1.Body.String(), rec2.Body.String())
	}
	if s := decodeJSON(t, rec1); s["status"] != "cancel_requested" {
		t.Fatalf("sau hủy = %v", s)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE dedupe_key = $1", "cancel:"+id); n != 1 {
		t.Fatalf("lệnh hủy = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.cancel'
		AND after->>'reason' NOT LIKE '%0912345678%'`); n != 1 {
		t.Fatalf("audit cancel (đã che SĐT) = %d, muốn 1", n)
	}
	if code, b := e.cancel(path, "c2", body); code != http.StatusConflict || b["error"] != "shipment_not_cancellable" {
		t.Fatalf("khóa khác = %d %v, muốn 409 shipment_not_cancellable", code, b)
	}
	if code, b := e.cancel(path, "c3", `{"reason":""}`); code != http.StatusBadRequest {
		t.Fatalf("lý do rỗng = %d %v, muốn 400", code, b)
	}

	// Hàng đã đi: đổi hãng → 409.
	shipped := e.awaitingShipment("b")
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.shipments SET status = 'shipping' WHERE id = $1", shipped)
		return err //nolint:wrapcheck // test
	})
	if code, b := e.cancel("/api/v1/shipments/"+shipped+"/cancel", "c4", body); code != http.StatusConflict ||
		b["error"] != "shipment_not_cancellable" {
		t.Fatalf("hủy khi shipping = %d %v", code, b)
	}

	// "Không rõ": hãng quá giờ chưa tạo, tra không chắc.
	e.carrier.set("timeout_no_create", true)
	orderID := e.confirmedOrder("o-c")
	code, s := e.post("/api/v1/orders/"+orderID+"/shipments", "s-c", fmt.Sprintf(`{"carrierAccountId":"%s"}`, e.accounts[0]))
	if code != http.StatusAccepted {
		t.Fatalf("gửi hãng = %d %v", code, s)
	}
	e.drain()
	unknown := "/api/v1/shipments/" + fmt.Sprint(s["id"]) + "/cancel"
	if code, b := e.cancel(unknown, "c5", body); code != http.StatusConflict || b["error"] != "create_in_progress" {
		t.Fatalf("đổi hãng khi không rõ = %d %v", code, b)
	}
	code, b := e.cancel(unknown, "c6", `{"reason":"Đã tra trang hãng, không có","carrierChecked":true}`)
	if code != http.StatusOK || b["status"] != "cancelled" || b["cancelKind"] != "staff_after_unknown" {
		t.Fatalf("đổi hãng sau khi tra = %d %v", code, b)
	}
}
