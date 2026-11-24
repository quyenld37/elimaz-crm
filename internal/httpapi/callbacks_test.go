package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

const (
	callbackCaller = "goi-lai@elimaz.test"
	callbackMate   = "goi-lai-2@elimaz.test"
	callbackReader = "chi-doc-van-don@elimaz.test"
)

// newCallbacksAPI dựng một vận đơn giao thất bại kèm việc gọi lại quá hạn; người dùng callbackCaller, callbackMate có
// fulfillment.callbacks.work (không fulfillment.ship), callbackReader chỉ có fulfillment.read.
func newCallbacksAPI(t *testing.T) (e *orderAPIEnv, srv http.Handler, task uuid.UUID, users map[string]uuid.UUID) {
	t.Helper()
	e = newOrderAPIEnv(t)
	tenant := e.shop.Tenant
	account := seedCarrierAccount(t, e)
	users = map[string]uuid.UUID{}
	for _, u := range []string{callbackCaller, callbackMate, callbackReader} {
		users[u] = e.staff.User(t, tenant, u, "active")
		perm := "fulfillment.callbacks.work"
		if u == callbackReader {
			perm = "fulfillment.read"
		}
		e.staff.Grant(t, tenant, users[u], []string{perm}, "all", "")
	}
	v := e.variant(e.shop.Seller, 250000, 5)
	orderID := uuid.MustParse(fmt.Sprint(e.mustCreate("cb1", orderBody(30000, line(v, 1)))["id"]))
	var shipment uuid.UUID
	if err := tenancy.WithTenant(t.Context(), e.staff.App, tenant, func(tx pgx.Tx) error {
		s, err := fulfillment.CreateForOrder(t.Context(), tx, orderID, account, fulfillment.Actor{TenantID: tenant})
		shipment = s.ID
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	failed := time.Now().UTC().Add(-30 * time.Hour)
	e.staff.Exec(t, tenant, func(tx pgx.Tx) error {
		ctx := t.Context()
		if _, err := tx.Exec(ctx, `UPDATE fulfillment.shipments SET status = 'delivery_failed', create_state = 'created',
			tracking_code = 'T' || left(id::text, 8) WHERE id = $1`, shipment); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(ctx, `INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no,
			failed_reason, failed_at, due_at) VALUES ($1, $2, $3, 1, 'Khách không nghe máy', $4::timestamptz, $4::timestamptz + interval '24 hours')
			RETURNING id`, tenant, shipment, orderID, failed).Scan(&task)
	})
	d := testServerDeps(t, e.staff, e.keys, 0)
	d.Fulfillment = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(),
	}).Service
	return e, NewServer(d), task, users
}

// Danh sách qua HTTP: quá hạn, tên khách, số nhận/hoàn, openCount; JSON không có SĐT người nhận; thiếu quyền → 403.
func TestCallbacksAPI_List(t *testing.T) {
	t.Parallel()
	e, srv, task, _ := newCallbacksAPI(t)
	rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/callback-tasks", callbackCaller, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("listCallbackTasks = %d %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "0901234567") || strings.Contains(raw, "901234567") || strings.Contains(raw, "phone") ||
		strings.Contains(raw, "Lê Lợi") {
		t.Fatalf("danh sách lộ SĐT hay địa chỉ: %s", raw)
	}
	body := decodeJSON(t, rec)
	items, _ := body["items"].([]any)
	if len(items) != 1 || body["openCount"] != float64(1) {
		t.Fatalf("danh sách = %v", body)
	}
	row, _ := items[0].(map[string]any)
	stats, _ := row["deliveryStats"].(map[string]any)
	if row["id"] != task.String() || row["overdue"] != true || row["status"] != "open" || row["customerName"] != "Chị Lan" ||
		row["attemptNo"] != float64(1) || row["failedReason"] != "Khách không nghe máy" || stats["risk"] != "none" ||
		row["orderCode"] == "" || row["trackingCode"] == nil {
		t.Fatalf("dòng gọi lại = %v", row)
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/callback-tasks?assignee=me", callbackCaller, "", ""); !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("việc của tôi = %s", rec.Body.String())
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/callback-tasks?status=done", callbackCaller, "", ""); !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("việc done = %s", rec.Body.String())
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/callback-tasks", callbackReader, "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("không quyền = %d", rec.Code)
	}
}

// Ghi kết quả qua HTTP: 50 yêu cầu cùng khóa → một dòng nhật ký, một audit (không chép ghi chú); không quyền → 403;
// giao lại địa chỉ mới thiếu fulfillment.ship → 403, không đổi gì; lệch version → 409 version_conflict.
func TestCallbacksAPI_RecordOutcome(t *testing.T) {
	t.Parallel()
	e, srv, task, _ := newCallbacksAPI(t)
	path := "/api/v1/callback-tasks/" + task.String() + "/outcomes"
	body := `{"outcome":"no_answer","note":"gọi 0912345678 không nghe","nextCallAt":"2030-01-02T08:00:00Z","version":1}`
	if rec := serveAs(srv, e.keys, http.MethodPost, path, callbackReader, "r1", body); rec.Code != http.StatusForbidden {
		t.Fatalf("không quyền = %d", rec.Code)
	}
	codes := make([]int, 50)
	bodies := make([]string, 50)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			rec := serveAs(srv, e.keys, http.MethodPost, path, callbackCaller, "same-key", body)
			codes[i], bodies[i] = rec.Code, rec.Body.String()
		})
	}
	wg.Wait()
	ok := 0
	for i, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
			if !strings.Contains(bodies[i], `"status":"in_progress"`) || !strings.Contains(bodies[i], `"version":2`) {
				t.Fatalf("câu trả lời %d = %s", i, bodies[i])
			}
		case http.StatusConflict: // khóa đang chạy
		default:
			t.Fatalf("yêu cầu %d = %d %s", i, c, bodies[i])
		}
	}
	if ok == 0 {
		t.Fatal("không yêu cầu nào 200")
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_task_logs WHERE task_id = $1`, task); n != 1 {
		t.Fatalf("nhật ký = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.callback.outcome'
		AND entity_id = $1 AND after::text NOT LIKE '%0912345678%' AND after::text NOT LIKE '%nghe%'`, task.String()); n != 1 {
		t.Fatalf("audit = %d, muốn 1", n)
	}
	addr := `{"outcome":"redeliver_new_address","version":2,"newAddress":{"name":"Chị Lan","phone":"0912345678",` +
		`"line":"45 Trần Hưng Đạo","provinceCode":"T01","wardCode":"T0101"}}`
	if rec := serveAs(srv, e.keys, http.MethodPost, path, callbackCaller, "addr", addr); rec.Code != http.StatusForbidden {
		t.Fatalf("địa chỉ mới thiếu fulfillment.ship = %d %s", rec.Code, rec.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_task_logs WHERE task_id = $1`, task); n != 1 {
		t.Fatalf("nhật ký sau 403 = %d, muốn 1", n)
	}
	stale := `{"outcome":"customer_cancel","version":1}`
	if rec := serveAs(srv, e.keys, http.MethodPost, path, callbackCaller, "stale", stale); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "version_conflict") {
		t.Fatalf("lệch version = %d %s", rec.Code, rec.Body.String())
	}
	done := `{"outcome":"customer_cancel","version":2}`
	if rec := serveAs(srv, e.keys, http.MethodPost, path, callbackCaller, "done", done); rec.Code != http.StatusOK {
		t.Fatalf("khách hủy = %d %s", rec.Code, rec.Body.String())
	}
	again := `{"outcome":"no_answer","version":3}`
	if rec := serveAs(srv, e.keys, http.MethodPost, path, callbackCaller, "again", again); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "invalid_transition") {
		t.Fatalf("ghi lên việc đã xong = %d %s", rec.Code, rec.Body.String())
	}
	missing := "/api/v1/callback-tasks/" + uuid.NewString() + "/outcomes"
	if rec := serveAs(srv, e.keys, http.MethodPost, missing, callbackCaller, "missing", again); rec.Code != http.StatusNotFound {
		t.Fatalf("việc lạ = %d", rec.Code)
	}
}

// Giao việc qua HTTP: người có fulfillment.callbacks.work → 200 kèm tên; người không có quyền đó → 422 invalid_input;
// lệch version → 409.
func TestCallbacksAPI_Assign(t *testing.T) {
	t.Parallel()
	e, srv, task, users := newCallbacksAPI(t)
	mate, reader := users[callbackMate], users[callbackReader]
	path := "/api/v1/callback-tasks/" + task.String() + "/assign"
	if rec := serveAs(srv, e.keys, http.MethodPost, path, callbackCaller, "a1", fmt.Sprintf(`{"assigneeId":"%s"}`, reader)); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "invalid_input") {
		t.Fatalf("giao cho người không có quyền = %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(srv, e.keys, http.MethodPost, path, callbackCaller, "a2", fmt.Sprintf(`{"assigneeId":"%s","version":9}`, mate)); rec.Code != http.StatusConflict {
		t.Fatalf("lệch version = %d %s", rec.Code, rec.Body.String())
	}
	rec := serveAs(srv, e.keys, http.MethodPost, path, callbackCaller, "a3", fmt.Sprintf(`{"assigneeId":"%s","version":1}`, mate))
	got := decodeJSON(t, rec)
	assignee, _ := got["assignee"].(map[string]any)
	if rec.Code != http.StatusOK || assignee["id"] != mate.String() || assignee["name"] == "" || got["version"] != float64(2) {
		t.Fatalf("giao việc = %d %v", rec.Code, got)
	}
	if rec := serveAs(srv, e.keys, http.MethodGet, "/api/v1/callback-tasks?assignee=me", callbackMate, "", ""); !strings.Contains(rec.Body.String(), task.String()) {
		t.Fatalf("việc của người được giao = %s", rec.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'fulfillment.callback.assign' AND entity_id = $1`,
		task.String()); n != 1 {
		t.Fatalf("audit giao việc = %d", n)
	}
}
