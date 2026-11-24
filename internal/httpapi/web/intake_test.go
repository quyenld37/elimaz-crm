package web_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

const intakePath = "/api/v1/web/intake"

// setIntake đổi trạng thái nhận đơn web của tenant bằng chủ shop (đường của nhân viên là handlers_web_intake_test).
func (o *orderEnv) setIntake(paused bool, reason string, version int) {
	o.t.Helper()
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue()}
	a := order.Actor{TenantID: o.tenant, UserID: o.owner.UserID}
	o.staff.Exec(o.t, o.tenant, func(tx pgx.Tx) error {
		_, err := w.SetWebIntake(o.t.Context(), tx, paused, reason, version, a, time.Now())
		return err //nolint:wrapcheck // test
	})
}

// intakeStatus đọc /web/intake bằng khóa web.catalog.read; trả thân dạng map (để kiểm không có khóa thừa).
func (o *orderEnv) intakeStatus() map[string]any {
	o.t.Helper()
	rec := o.get(o.env.key, intakePath)
	var m map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &m) != nil {
		o.t.Fatalf("webIntakeStatus: %d %s", rec.Code, rec.Body)
	}
	return m
}

// [WB01] Tạm dừng nhận đơn web: trạng thái cho website chỉ có paused (không lý do, người, giờ); đơn gửi thẳng → 503
// web_orders_paused, không Retry-After, không đơn, không lưu theo khóa; đơn từ hàng đợi → 201. Mở lại → gửi lại cùng
// khóa → 201. Khóa không có web.catalog.read đọc trạng thái → 403.
func TestWebIntake_PauseBlocksSyncOrders(t *testing.T) {
	t.Parallel()
	o := newOrderEnv(t, nil)
	p := o.product("PIN", "[]")
	v := p.Variants[0].ID
	o.stock(v, 5)
	if m := o.intakeStatus(); len(m) != 1 || m["paused"] != false {
		t.Fatalf("mặc định: %v", m)
	}
	o.setIntake(true, "Nghỉ Tết Nguyên Đán", 1)
	if m := o.intakeStatus(); len(m) != 1 || m["paused"] != true {
		t.Fatalf("sau tạm dừng (chỉ paused): %v", m)
	}
	if rec := o.get(o.key, intakePath); rec.Code != http.StatusForbidden {
		t.Errorf("khóa web.orders.create đọc trạng thái: %d", rec.Code)
	}

	w := o.order(phone(301), "", quoteLine(v, 1, 350000))
	rec := o.place(w.ref, w)
	refused(t, rec, http.StatusServiceUnavailable, "web_orders_paused")
	if rec.Header().Get("Retry-After") != "" || o.ordersOf(w.ref) != 0 || o.storedKeys(w.ref) != 0 {
		t.Fatalf("bị chặn: Retry-After %q, %d đơn, %d khóa lưu", rec.Header().Get("Retry-After"), o.ordersOf(w.ref),
			o.storedKeys(w.ref))
	}
	q := o.order(phone(302), "", quoteLine(v, 1, 350000))
	q.via = order.SubmittedQueued
	rq := o.place(q.ref, q)
	created(t, rq.Code, rq.Body.Bytes())

	o.setIntake(false, "", 2)
	if m := o.intakeStatus(); m["paused"] != false {
		t.Fatalf("sau mở lại: %v", m)
	}
	again := o.place(w.ref, w)
	created(t, again.Code, again.Body.Bytes())
	if again.Header().Get("Idempotent-Replayed") == "true" || o.ordersOf(w.ref) != 1 {
		t.Fatalf("gửi lại sau mở lại: %v, %d đơn", again.Header(), o.ordersOf(w.ref))
	}
}
