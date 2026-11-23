package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// syncShipment: 5 lần bấm trong một phút (mỗi lần một Idempotency-Key) → 202 cùng jobId, 1 việc tra; không có
// fulfillment.read ở kho của tài khoản hãng → 403; chỉ order.read → 403; vận đơn lạ → 404; có audit; vận đơn nhập
// tay → 422 (Task 448).
func TestSyncShipmentAPI(t *testing.T) {
	t.Parallel()
	e := newOrderAPIEnv(t)
	tenant := e.shop.Tenant
	account := seedCarrierAccount(t, e)
	w2 := uuid.New()
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipW2, "active"), []string{"fulfillment.read"}, "warehouse", w2.String())
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipAll, "active"), []string{"fulfillment.read"}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipReader, "active"), []string{"order.read"}, "all", "")
	v := e.variant(e.shop.Seller, 250000, 5)
	orderID := uuid.MustParse(fmt.Sprint(e.mustCreate("o1", orderBody(30000, line(v, 1)))["id"]))
	var shipmentID uuid.UUID
	if err := tenancy.WithTenant(t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		s, err := fulfillment.CreateForOrder(t.Context(), tx, orderID, account, fulfillment.Actor{TenantID: tenant})
		shipmentID = s.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 21, 9, 30, 15, 0, time.UTC)
	m := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Events: events.NewRegistry(),
		Now: func() time.Time { return now },
	})
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Fulfillment = m.Service
	srv := NewServer(d)
	path := fmt.Sprintf("/api/v1/shipments/%s/sync", shipmentID)

	if rec := serveAs(srv, e.keys, http.MethodPost, path, shipW2, "y0", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("fulfillment.read kho w2 = %d %s, muốn 403", rec.Code, rec.Body.String())
	}
	if rec := serveAs(srv, e.keys, http.MethodPost, path, shipReader, "y0", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("chỉ order.read = %d, muốn 403", rec.Code)
	}
	var first any
	for i := range 5 {
		rec := serveAs(srv, e.keys, http.MethodPost, path, shipAll, fmt.Sprintf("y%d", i+1), "")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("syncShipment lần %d = %d %s", i+1, rec.Code, rec.Body.String())
		}
		job := decodeJSON(t, rec)["jobId"]
		if i == 0 {
			first = job
		} else if job != first {
			t.Fatalf("lần %d jobId %v, muốn %v", i+1, job, first)
		}
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'
		AND payload->>'shipmentId' = $1 AND dedupe_key LIKE 'manual:%'`, shipmentID.String()); n != 1 {
		t.Fatalf("việc tra = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.shipment.sync'`); n != 5 {
		t.Fatalf("audit = %d, muốn 5", n)
	}
	other := "/api/v1/shipments/" + uuid.NewString() + "/sync"
	if rec := serveAs(srv, e.keys, http.MethodPost, other, shipAll, "y9", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("vận đơn lạ = %d, muốn 404", rec.Code)
	}
	// Task 448: vận đơn của tài khoản nhập tay (mode manual) không có nguồn tra ở hãng → 422 capability_not_supported,
	// không thêm việc.
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.carrier_accounts SET mode = 'manual' WHERE id = $1", account)
		return err //nolint:wrapcheck // test
	})
	rec := serveAs(srv, e.keys, http.MethodPost, path, shipAll, "y10", "")
	if rec.Code != http.StatusUnprocessableEntity || decodeJSON(t, rec)["error"] != "capability_not_supported" {
		t.Fatalf("vận đơn tay = %d %s, muốn 422 capability_not_supported", rec.Code, rec.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'`); n != 1 {
		t.Fatalf("việc tra sau vận đơn tay = %d, muốn 1", n)
	}
}
