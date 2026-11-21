package order_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/background"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// expire chạy ExpireDrafts(now, limit) trong một transaction crm_app của tenant e.
func (e *orderEnv) expire(now time.Time, limit int) (int, error) {
	var n int
	err := e.app(func(tx pgx.Tx) error {
		var err error
		n, err = e.writer().ExpireDrafts(e.t.Context(), tx, now, limit)
		return err //nolint:wrapcheck // test
	})
	return n, err
}

// draftWith tạo đơn nháp (địa chỉ đủ) V ×qty với mã code ("" → không mã) lúc e.now().
func (e *orderEnv) draftWith(v uuid.UUID, qty int32, code string) order.Order {
	e.t.Helper()
	in := e.m2Input(0, ln(v, qty, 0))
	in.Intent, in.CouponCode = order.IntentDraft, code
	o := e.mustCreateM2(in).Order
	if o.Status != order.StatusDraft || o.DraftExpiresAt == nil || !o.DraftExpiresAt.Equal(e.now().Add(24*time.Hour)) {
		e.t.Fatalf("đơn nháp: %+v", o)
	}
	return o
}

// Đơn nháp tạo lúc T, hạn 24 giờ: ExpireDrafts(T + 23h) → 0; ExpireDrafts(T + 24h + 1m) → cancelled, cancel_reason
// draft_expired, người làm system, hàng và lượt mã được trả; đơn nháp đã chốt trước hạn không bị đụng.
func TestExpireDrafts(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	cid := e.coupon(sale10("SALE10", 0))
	v := e.variant(250000, 10)
	d := e.draftWith(v, 2, "SALE10")
	kept := e.draftWith(v, 1, "")
	if err := e.transition(&kept, order.StatusConfirmed, "", e.now().Add(time.Hour)); err != nil {
		t.Fatalf("chốt đơn nháp: %v", err)
	}
	if n, err := e.expire(e.now().Add(23*time.Hour), 200); err != nil || n != 0 {
		t.Fatalf("chưa tới hạn: %d, %v", n, err)
	}
	at := e.now().Add(24*time.Hour + time.Minute)
	if n, err := e.expire(at, 200); err != nil || n != 1 {
		t.Fatalf("quá hạn: %d, %v; mong 1", n, err)
	}
	got := e.get(d.ID)
	if got.Status != order.StatusCancelled || got.CancelReason == nil || *got.CancelReason != order.CancelDraftExpired ||
		got.DraftExpiresAt != nil || got.CancelledAt == nil || !got.CancelledAt.Equal(at) {
		t.Errorf("đơn nháp hết hạn: %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM ord.order_status_history WHERE order_id = $1 AND from_status = 'draft'
		AND to_status = 'cancelled' AND actor_type = 'system' AND actor_id IS NULL AND reason = 'draft_expired'`,
		d.ID); n != 1 {
		t.Errorf("lịch sử hết hạn của system: %d", n)
	}
	if e.heldOf(d.ID, inventory.StateHeld) != 0 || e.usedCount(cid) != 0 || e.redemption(d.ID) != "released" {
		t.Errorf("đơn nháp hết hạn chưa trả hàng, lượt mã")
	}
	if b := e.balance(v); b.Held != 1 {
		t.Errorf("held V = %d, mong 1 (đơn đã chốt)", b.Held)
	}
	if g := e.get(kept.ID); g.Status != order.StatusConfirmed || g.Version != kept.Version {
		t.Errorf("đơn đã chốt bị đụng: %+v", g)
	}
	if n, err := e.expire(at, 200); err != nil || n != 0 {
		t.Errorf("chạy lại: %d, %v; mong 0", n, err)
	}
}

// Mỗi lần tối đa limit đơn (theo mã đơn tăng dần); limit ≤ 0 → ErrInvalidInput.
func TestExpireDrafts_Limit(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	v := e.variant(1000, 10)
	for range 3 {
		e.draftWith(v, 1, "")
	}
	at := e.now().Add(25 * time.Hour)
	if n, err := e.expire(at, 2); err != nil || n != 2 {
		t.Errorf("lần 1: %d, %v; mong 2", n, err)
	}
	if n, err := e.expire(at, 2); err != nil || n != 1 {
		t.Errorf("lần 2: %d, %v; mong 1", n, err)
	}
	if _, err := e.expire(at, 0); !errors.Is(err, order.ErrInvalidInput) {
		t.Errorf("limit 0: %v", err)
	}
	if b := e.balance(v); b.Held != 0 {
		t.Errorf("held = %d", b.Held)
	}
}

// Background của order khai lịch order.expire_drafts mỗi 60 giây theo tenant và handler cùng Kind; handler chạy
// ExpireDrafts với giờ của background.Deps.Now (B-05, Task 436: không gọi time.Now trong module) trong tx của việc và trả
// {"expired": n}; dòng lịch sử hủy mang đúng giờ đó (một đồng hồ cho lịch sử đơn).
func TestExpireDraftsSchedule(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	created := time.Now().UTC().AddDate(1, 0, 0).Truncate(time.Microsecond) // năm sau: theo giờ thật chưa quá hạn
	clock := created.Add(48 * time.Hour)
	parts := order.Background(background.Deps{
		Queue: queue.NewQueue(), Events: events.NewRegistry(),
		Bus: &realtimetest.RecordingBus{}, Now: func() time.Time { return clock },
	})
	if len(parts.Schedules) != 1 || len(parts.JobHandlers) != 1 {
		t.Fatalf("lịch, việc nền: %d, %d", len(parts.Schedules), len(parts.JobHandlers))
	}
	s, h := parts.Schedules[0], parts.JobHandlers[0]
	if order.ExpireDraftsSchedule != "order.expire_drafts" || s.Name != order.ExpireDraftsSchedule || s.Every != time.Minute ||
		!s.PerTenant || s.Kind.Name != h.Kind().Name {
		t.Fatalf("lịch: %+v, handler %s", s, h.Kind().Name)
	}
	v := e.variant(1000, 5)
	in := e.m2Input(0, ln(v, 1, 0))
	in.Intent = order.IntentDraft
	var d order.Order
	if err := e.app(func(tx pgx.Tx) error { // tạo lúc clock − 2 ngày: quá hạn theo đồng hồ của Deps
		res, err := e.writer().Create(t.Context(), tx, in, e.actor, created)
		d = res.Order
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	var out json.RawMessage
	if err := tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var err error
		out, err = h.Handle(t.Context(), queue.Job{TenantID: &e.tenant, Kind: h.Kind().Name}, tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if string(out) != `{"expired":1}` {
		t.Errorf("kết quả %s", out)
	}
	if g := e.get(d.ID); g.Status != order.StatusCancelled || g.CancelledAt == nil || !g.CancelledAt.Equal(clock) {
		t.Errorf("đơn nháp sau việc nền: %s, hủy lúc %v, muốn %v", g.Status, g.CancelledAt, clock)
	}
}
