package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// listShipmentEvents: không có fulfillment.read ở kho của tài khoản hãng → 404; có quyền → mới nhất trước, đủ trường,
// không có khóa raw; phân trang theo con trỏ; vận đơn lạ → 404.
func TestShipmentEventsAPI(t *testing.T) {
	e := newOrderAPIEnv(t)
	shipmentID := seedShipmentEvents(t, e)
	path := fmt.Sprintf("/api/v1/shipments/%s/events", shipmentID)
	if rec := serveAs(e.srv, e.keys, http.MethodGet, path, shipW2, "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("fulfillment.read kho w2 = %d %s, muốn 404", rec.Code, rec.Body.String())
	}
	if rec := serveAs(e.srv, e.keys, http.MethodGet, path, orderViewer, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("chỉ order.read = %d, muốn 403", rec.Code)
	}
	if rec := serveAs(e.srv, e.keys, http.MethodGet, "/api/v1/shipments/"+uuid.NewString()+"/events", shipAll, "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("vận đơn lạ = %d, muốn 404", rec.Code)
	}
	rec := serveAs(e.srv, e.keys, http.MethodGet, path, shipAll, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("listShipmentEvents = %d %s", rec.Code, rec.Body.String())
	}
	items, _ := decodeJSON(t, rec)["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %v", items)
	}
	wantRaw := []string{"DONE", "WEIRD", "PICKED"}
	for i, it := range items {
		m, _ := it.(map[string]any)
		if _, ok := m["raw"]; ok {
			t.Fatalf("item %d có khóa raw: %v", i, m)
		}
		if m["rawStatus"] != wantRaw[i] || m["source"] != "carrier_api" || m["occurredAt"] == nil || m["recordedAt"] == nil {
			t.Fatalf("item %d = %v", i, m)
		}
	}
	first, _ := items[0].(map[string]any)
	weird, _ := items[1].(map[string]any)
	if first["applied"] != true || first["status"] != "delivered" || first["label"] != "Thành công" {
		t.Fatalf("sự kiện giao xong = %v", first)
	}
	if weird["applied"] != false || weird["ignoredReason"] != "unmapped" {
		t.Fatalf("sự kiện lạ = %v", weird)
	}

	rec = serveAs(e.srv, e.keys, http.MethodGet, path+"?limit=2", shipAll, "", "")
	page := decodeJSON(t, rec)
	items, _ = page["items"].([]any)
	next, _ := page["nextCursor"].(string)
	if len(items) != 2 || next == "" {
		t.Fatalf("trang 1 = %v", page)
	}
	rec = serveAs(e.srv, e.keys, http.MethodGet, path+"?limit=2&cursor="+next, shipAll, "", "")
	page = decodeJSON(t, rec)
	items, _ = page["items"].([]any)
	last, _ := items[0].(map[string]any)
	if len(items) != 1 || last["rawStatus"] != "PICKED" || page["nextCursor"] != nil {
		t.Fatalf("trang 2 = %v", page)
	}
	if rec := serveAs(e.srv, e.keys, http.MethodGet, path+"?cursor=xyz", shipAll, "", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("con trỏ sai = %d, muốn 400", rec.Code)
	}
}

// seedShipmentEvents dựng tài khoản hãng kho MAIN, kho W2, quyền (shipAll mọi kho, shipW2 chỉ W2), đơn, vận đơn có
// ba sự kiện (lấy hàng 08:00, mã lạ 09:00, giao xong 10:00); trả mã vận đơn.
func seedShipmentEvents(t *testing.T, e *orderAPIEnv) uuid.UUID {
	t.Helper()
	tenant := e.shop.Tenant
	// Tài khoản hãng ở kho MAIN; shipW2 chỉ có fulfillment.read ở một kho khác (W2).
	account := seedCarrierAccount(t, e)
	w2 := uuid.New()
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipW2, "active"), []string{"fulfillment.read"}, "warehouse", w2.String())
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipAll, "active"), []string{"fulfillment.ship", "fulfillment.read"}, "all", "")

	v := e.variant(e.shop.Seller, 250000, 5)
	orderID := uuid.MustParse(fmt.Sprint(e.mustCreate("o1", orderBody(30000, line(v, 2)))["id"]))
	svc := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Events: events.NewRegistry(),
	}).Service
	at := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	var shipmentID uuid.UUID
	if err := tenancy.WithTenant(t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		s, err := fulfillment.CreateForOrder(t.Context(), tx, orderID, account, fulfillment.Actor{TenantID: tenant})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		shipmentID = s.ID
		if _, err := svc.AttachTrackingCode(t.Context(), tx, s.ID, "FKAPI0001", nil); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = svc.ApplyTracking(t.Context(), tx, fulfillment.ApplyInput{
			ShipmentID: s.ID, Source: fulfillment.SrcCarrierAPI, SyncedAt: at,
			Actor: order.Actor{TenantID: tenant, Type: "carrier"},
			Tracking: fulfillment.Tracking{Events: []fulfillment.CarrierEvent{
				{Key: "a", RawStatus: "PICKED", Canonical: fulfillment.CanonPickedUp, OccurredAt: at},
				{Key: "b", RawStatus: "WEIRD", Canonical: fulfillment.CanonUnknown, OccurredAt: at.Add(time.Hour)},
				{Key: "c", RawStatus: "DONE", Canonical: fulfillment.CanonDelivered, OccurredAt: at.Add(2 * time.Hour)},
			}},
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}

	return shipmentID
}

// seedCarrierAccount tạo tài khoản hãng giả fake-1 của người bán ELIMAZ ở kho MAIN (chủ bảng).
func seedCarrierAccount(t *testing.T, e *orderAPIEnv) uuid.UUID {
	t.Helper()
	in := fulfillment.SeedAccountInput{
		SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: "fake", Name: "Hãng giả",
		ExternalAccountRef: "fake-1",
	}
	var id uuid.UUID
	e.staff.Exec(t, e.shop.Tenant, func(tx pgx.Tx) (err error) {
		id, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, in)
		return err //nolint:wrapcheck // test
	})
	return id
}
