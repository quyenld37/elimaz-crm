package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const shipFixer = "sua-van-don@elimaz.test"

// Đơn kẹt, Cần xem, đóng việc qua HTTP: fulfillment.read toàn shop thấy vận đơn kẹt và việc Cần xem (câu hướng dẫn,
// counts); fulfillment.read kho w2 → rỗng, counts 0; getShipment có lastCarrierEventAt, lastSyncedAt, stuckSince.
// Đóng in_doubt_create → 409 invalid_transition; đóng missing_items → 200, needsReview về false, có audit (không chép
// lý do); không có fulfillment.correct → 403; có correct nhưng không thấy kho → 404; thiếu resolution → 400.
func TestShipmentListsAPI(t *testing.T) {
	e, srv, stuck, review, inDoubt, missing := newListsAPI(t)
	checkStuckAPI(t, e, srv, stuck)
	checkReviewAPI(t, e, srv)
	checkResolveAPI(t, e, srv, review, inDoubt, missing)
}

// newListsAPI dựng hai vận đơn: stuck (shipping, sự kiện cuối 80 giờ trước, đã kẹt) và review (hai việc mở:
// in_doubt_create, missing_items); người dùng shipAll (read toàn shop), shipW2 (read + correct kho w2), shipFixer
// (read + correct toàn shop).
func newListsAPI(t *testing.T) (e *orderAPIEnv, srv http.Handler, stuck, review, inDoubt, missing uuid.UUID) {
	t.Helper()
	e = newOrderAPIEnv(t)
	tenant := e.shop.Tenant
	account := seedCarrierAccount(t, e)
	w2 := uuid.New()
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipAll, "active"), []string{"fulfillment.read"}, "all", "")
	e.staff.Grant(t, tenant, e.staff.User(t, tenant, shipW2, "active"), []string{"fulfillment.read", "fulfillment.correct"}, "warehouse", w2.String())
	fixer := e.staff.User(t, tenant, shipFixer, "active")
	e.staff.Grant(t, tenant, fixer, []string{"fulfillment.read", "fulfillment.correct"}, "all", "")
	now := time.Now().UTC().Truncate(time.Second)
	ids := make([]uuid.UUID, 2)
	for i := range ids {
		v := e.variant(e.shop.Seller, 250000, 5)
		orderID := uuid.MustParse(fmt.Sprint(e.mustCreate(fmt.Sprintf("o%d", i), orderBody(30000, line(v, 1)))["id"]))
		if err := tenancy.WithTenant(t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
			s, err := fulfillment.CreateForOrder(t.Context(), tx, orderID, account, fulfillment.Actor{TenantID: tenant})
			ids[i] = s.ID
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
	}
	stuck, review = ids[0], ids[1]
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		ctx := t.Context()
		if _, err := tx.Exec(ctx, `UPDATE fulfillment.shipments SET status = 'shipping', tracking_code = 'T' || left(id::text, 8),
			create_state = 'created', last_carrier_event_at = $2, last_synced_at = $3, last_status_at = $2, stuck_since = $3
			WHERE id = $1`, stuck, now.Add(-80*time.Hour), now); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, `UPDATE fulfillment.shipments SET needs_review = true WHERE id = $1`, review); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO fulfillment.shipment_anomalies (tenant_id, shipment_id, kind)
			VALUES ($1, $2, 'in_doubt_create') RETURNING id`, tenant, review).Scan(&inDoubt); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO fulfillment.shipment_anomalies (tenant_id, shipment_id, kind, detail)
			VALUES ($1, $2, 'missing_items', '{"missingQty": "1"}') RETURNING id`, tenant, review).Scan(&missing)
	})
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Fulfillment = fulfillment.New(fulfillment.Deps{DB: e.staff.App, Queue: queue.NewQueue()}).Service // đồng hồ thật
	return e, NewServer(d), stuck, review, inDoubt, missing
}

func checkStuckAPI(t *testing.T, e *orderAPIEnv, srv http.Handler, stuck uuid.UUID) {
	t.Helper()
	rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/shipments/stuck", shipAll, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("listStuckShipments = %d %s", rec.Code, rec.Body.String())
	}
	items, _ := decodeJSON(t, rec)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("đơn kẹt = %v", items)
	}
	row, _ := items[0].(map[string]any)
	if row["shipmentId"] != stuck.String() || row["status"] != "shipping" || row["hoursSinceUpdate"] != float64(80) ||
		row["orderCode"] == "" || row["lastSyncedAt"] == nil || row["lastCarrierEventAt"] == nil || row["stuckSince"] == nil {
		t.Fatalf("dòng kẹt = %v", row)
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/shipments/stuck?status=returning", shipAll, "", ""); !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("lọc returning = %s", rec.Body.String())
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/shipments/stuck?cursor=hong", shipAll, "", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("con trỏ hỏng = %d", rec.Code)
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/shipments/stuck", shipW2, "", ""); !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("kho w2 = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeJSON(t, serveAs(srv, e.keys, http.MethodGet, "/api/v1/shipments/"+stuck.String(), shipAll, "", ""))
	if got["lastCarrierEventAt"] == nil || got["lastSyncedAt"] == nil || got["stuckSince"] == nil {
		t.Fatalf("getShipment thiếu mốc cập nhật: %v", got)
	}
}

func checkReviewAPI(t *testing.T, e *orderAPIEnv, srv http.Handler) {
	t.Helper()
	rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/shipments/review", shipAll, "", "")
	body := decodeJSON(t, rec)
	counts, _ := body["counts"].(map[string]any)
	if rec.Code != http.StatusOK || counts["review"] != float64(2) || counts["stuck"] != float64(1) ||
		counts["inDoubt"] != float64(1) || counts["callbacksOpen"] != float64(0) || counts["callbacksOverdue"] != float64(0) {
		t.Fatalf("listShipmentsForReview = %d %v", rec.Code, body)
	}
	ritems, _ := body["items"].([]any)
	first, _ := ritems[0].(map[string]any)
	if len(ritems) != 2 || first["kind"] != "in_doubt_create" || !strings.Contains(fmt.Sprint(first["label"]), "Đã tạo / Chưa tạo") {
		t.Fatalf("việc Cần xem = %v", ritems)
	}
	if second, _ := ritems[1].(map[string]any); fmt.Sprint(second["detail"]) != "map[missingQty:1]" {
		t.Fatalf("detail = %v", second)
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/shipments/review", shipW2, "", ""); !strings.Contains(rec.Body.String(), `"items":[]`) ||
		!strings.Contains(rec.Body.String(), `"review":0`) {
		t.Fatalf("Cần xem kho w2 = %s", rec.Body.String())
	}
}

func checkResolveAPI(t *testing.T, e *orderAPIEnv, srv http.Handler, review, inDoubt, missing uuid.UUID) {
	t.Helper()
	resolve := func(id uuid.UUID, email, key, body string) int {
		rec := serveAs(srv, e.keys, http.MethodPost, "/api/v1/shipment-anomalies/"+id.String()+"/resolve", email, key, body)
		return rec.Code
	}
	ok := `{"resolution": "đã gọi khách 0912345678"}`
	if c := resolve(inDoubt, shipFixer, "r1", ok); c != http.StatusConflict {
		t.Fatalf("đóng in_doubt_create = %d, muốn 409", c)
	}
	if c := resolve(missing, shipAll, "r2", ok); c != http.StatusForbidden {
		t.Fatalf("không có fulfillment.correct = %d, muốn 403", c)
	}
	if c := resolve(missing, shipW2, "r3", ok); c != http.StatusNotFound {
		t.Fatalf("không thấy kho = %d, muốn 404", c)
	}
	if c := resolve(missing, shipFixer, "r4", `{}`); c != http.StatusBadRequest {
		t.Fatalf("thiếu resolution = %d, muốn 400", c)
	}
	if c := resolve(uuid.New(), shipFixer, "r5", ok); c != http.StatusNotFound {
		t.Fatalf("anomaly lạ = %d, muốn 404", c)
	}
	rec := serveAs(srv, e.keys, http.MethodPost, "/api/v1/shipment-anomalies/"+missing.String()+"/resolve", shipFixer, "r6", ok)
	if out := decodeJSON(t, rec); rec.Code != http.StatusOK || out["status"] != "resolved" || out["kind"] != "missing_items" {
		t.Fatalf("đóng missing_items = %d %v", rec.Code, out)
	}
	// Cùng khóa → trả lại kết quả cũ; khóa mới → 409 (đã đóng).
	if c := resolve(missing, shipFixer, "r6", ok); c != http.StatusOK {
		t.Fatalf("gửi lại cùng khóa = %d", c)
	}
	if c := resolve(missing, shipFixer, "r7", ok); c != http.StatusConflict {
		t.Fatalf("đóng lần hai = %d, muốn 409", c)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND needs_review`, review); n != 1 {
		t.Fatal("còn in_doubt_create mở: needs_review phải true")
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.anomaly.resolve'
		AND entity_id = $1 AND NOT (coalesce(after::text, '') LIKE '%0912345678%')`, missing); n != 1 {
		t.Fatalf("audit = %d, muốn 1 (không chép lý do)", n)
	}
}
