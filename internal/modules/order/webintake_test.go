package order_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// getIntake đọc trạng thái nhận đơn web của tenant của e.
func (e *orderEnv) getIntake() order.WebIntake {
	e.t.Helper()
	var wi order.WebIntake
	if err := e.app(func(tx pgx.Tx) (err error) {
		wi, err = order.GetWebIntake(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("GetWebIntake: %v", err)
	}
	return wi
}

// setIntake đổi trạng thái nhận đơn web bằng Writer w trong một transaction crm_app.
func (e *orderEnv) setIntake(w order.Writer, paused bool, reason string, version int, now time.Time,
) (order.WebIntakeChange, error) {
	var ch order.WebIntakeChange
	err := e.app(func(tx pgx.Tx) (err error) {
		ch, err = w.SetWebIntake(e.t.Context(), tx, paused, reason, version, e.actor, now)
		return err //nolint:wrapcheck // test
	})
	return ch, err
}

// Chưa có dòng: đang nhận đơn, version 1, chưa ai đổi.
func TestWebIntake_DefaultOpen(t *testing.T) {
	t.Parallel()
	e := newOrderEnv(t)
	wi := e.getIntake()
	if wi.Paused || wi.Reason != "" || wi.ChangedBy != nil || wi.ChangedAt != nil || wi.Version != 1 {
		t.Fatalf("mặc định: %+v", wi)
	}
	if n := e.count(`SELECT count(*) FROM ord.web_intake`); n != 0 {
		t.Errorf("đọc không được chèn dòng: %d", n)
	}
}

// Tạm dừng: lý do rỗng (kể cả chỉ khoảng trắng) → ErrReasonRequired; 301 ký tự → ErrInvalidInput; 300 ký tự có dấu
// được. Không đổi gì khi lỗi.
func TestWebIntake_PauseReasonRules(t *testing.T) {
	t.Parallel()
	e := newOrderEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, reason := range []string{"", "   \t "} {
		if _, err := e.setIntake(e.writer(), true, reason, 1, now); !errors.Is(err, order.ErrReasonRequired) {
			t.Fatalf("lý do %q: %v, mong ErrReasonRequired", reason, err)
		}
	}
	if _, err := e.setIntake(e.writer(), true, strings.Repeat("ạ", 301), 1, now); !errors.Is(err, order.ErrInvalidInput) {
		t.Fatalf("301 ký tự: %v, mong ErrInvalidInput", err)
	}
	if wi := e.getIntake(); wi.Paused || wi.Version != 1 {
		t.Fatalf("lỗi mà vẫn đổi: %+v", wi)
	}
	ch, err := e.setIntake(e.writer(), true, "  "+strings.Repeat("ạ", 300)+" ", 1, now)
	if err != nil {
		t.Fatalf("300 ký tự: %v", err)
	}
	if ch.After.Reason != strings.Repeat("ạ", 300) || !ch.After.Paused {
		t.Errorf("lý do sau khi cắt khoảng trắng: %d ký tự, paused %v", len([]rune(ch.After.Reason)), ch.After.Paused)
	}
}

// Tạm dừng rồi mở lại: lưu lý do, người, giờ, version tăng; mỗi lần một sự kiện order.web_intake.changed (seq =
// version mới) và một báo hiệu màn hình; version cũ → ErrVersionConflict; mở lại không cần lý do.
func TestWebIntake_PauseAndReopen(t *testing.T) {
	t.Parallel()
	e := newOrderEnv(t)
	bus := &realtimetest.RecordingBus{}
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: bus}
	t1 := time.Now().UTC().Truncate(time.Second)
	ch, err := e.setIntake(w, true, "Nghỉ Tết", 1, t1)
	if err != nil {
		t.Fatalf("tạm dừng: %v", err)
	}
	if ch.Before.Paused || ch.Before.Version != 1 {
		t.Errorf("trước: %+v", ch.Before)
	}
	got := e.getIntake()
	if !got.Paused || got.Reason != "Nghỉ Tết" || got.ChangedBy == nil || *got.ChangedBy != e.actor.UserID ||
		got.ChangedAt == nil || !got.ChangedAt.Equal(t1) || got.Version != 2 {
		t.Fatalf("sau tạm dừng: %+v", got)
	}
	if ch.After.Version != got.Version || ch.After.Reason != got.Reason || *ch.After.ChangedBy != *got.ChangedBy {
		t.Errorf("After %+v khác bản đọc lại %+v", ch.After, got)
	}
	if _, err := e.setIntake(w, false, "", 1, t1); !errors.Is(err, order.ErrVersionConflict) {
		t.Fatalf("version cũ: %v, mong ErrVersionConflict", err)
	}
	t2 := t1.Add(time.Hour)
	if _, err := e.setIntake(w, false, "", 2, t2); err != nil {
		t.Fatalf("mở lại: %v", err)
	}
	got = e.getIntake()
	if got.Paused || got.Reason != "" || got.Version != 3 || got.ChangedAt == nil || !got.ChangedAt.Equal(t2) {
		t.Fatalf("sau mở lại: %+v", got)
	}
	for seq := 2; seq <= 3; seq++ {
		if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = $1 AND aggregate_id = $2 AND seq = $3`,
			order.TopicWebIntakeChanged, e.tenant.String(), seq); n != 1 {
			t.Errorf("sự kiện seq %d: %d dòng", seq, n)
		}
	}
	if n := len(bus.Signals()); n != 2 {
		t.Errorf("báo hiệu màn hình: %d, mong 2", n)
	}
}

// Hai lần ghi đầu song song cùng version 1 (chưa có dòng): đúng một lần được, lần kia ErrVersionConflict; một dòng.
func TestWebIntake_ConcurrentFirstWrite(t *testing.T) {
	t.Parallel()
	e := newOrderEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			_, errs[i] = e.setIntake(e.writer(), true, "Kiểm kho", 1, now)
		})
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, order.ErrVersionConflict):
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d lần thành công, mong 1", ok)
	}
	if c := e.count(`SELECT count(*) FROM ord.web_intake`); c != 1 {
		t.Errorf("%d dòng, mong 1", c)
	}
	if wi := e.getIntake(); wi.Version != 2 {
		t.Errorf("version %d, mong 2", wi.Version)
	}
}

// Tenant khác không thấy, không bị ảnh hưởng bởi trạng thái của tenant A.
func TestWebIntake_OtherTenant(t *testing.T) {
	t.Parallel()
	e := newOrderEnv(t)
	if _, err := e.setIntake(e.writer(), true, "Bảo trì", 1, time.Now().UTC()); err != nil {
		t.Fatalf("tạm dừng: %v", err)
	}
	b := *e
	b.tenant = e.staff.Tenant(t, "B")
	b.actor.TenantID = b.tenant
	if wi := b.getIntake(); wi.Paused || wi.Version != 1 {
		t.Fatalf("tenant B thấy %+v", wi)
	}
}

// Đang tạm dừng: đơn sync mới → ErrWebIntakePaused, không đơn, không giữ hàng; queued vẫn nhận; gửi lại đơn đã có
// cùng ExternalRef → trả đơn đó (Replayed). Mở lại → sync nhận.
func TestCreateFromWeb_IntakePaused(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	_, v := e.product(5, 200000)
	before := e.webInput("0903000201", "", wl(v[0], 1, 200000))
	created := e.mustCreateWeb(before)
	if _, err := e.setIntake(e.writer(), true, "Nghỉ lễ", 1, e.now); err != nil {
		t.Fatalf("tạm dừng: %v", err)
	}
	orders := func() int { return e.count(`SELECT count(*) FROM ord.orders`) }
	n0 := orders()

	sync1 := e.webInput("0903000202", "", wl(v[0], 1, 200000))
	if _, err := e.createWeb(sync1); !errors.Is(err, order.ErrWebIntakePaused) {
		t.Fatalf("sync khi tạm dừng: %v, mong ErrWebIntakePaused", err)
	}
	if n := orders(); n != n0 {
		t.Fatalf("có đơn mới khi bị chặn: %d → %d", n0, n)
	}
	replay, err := e.createWeb(before)
	if err != nil || !replay.Replayed || replay.OrderID != created.OrderID {
		t.Fatalf("gửi lại đơn đã có: %+v, %v", replay, err)
	}
	queued := e.webInput("0903000203", "", wl(v[0], 1, 200000))
	queued.SubmittedVia = order.SubmittedQueued
	if res, err := e.createWeb(queued); err != nil || res.Replayed {
		t.Fatalf("queued khi tạm dừng: %+v, %v", res, err)
	}

	if _, err := e.setIntake(e.writer(), false, "", 2, e.now); err != nil {
		t.Fatalf("mở lại: %v", err)
	}
	if res, err := e.createWeb(sync1); err != nil || res.Replayed {
		t.Fatalf("sync sau khi mở lại: %+v, %v", res, err)
	}
	if n := orders(); n != n0+2 {
		t.Errorf("số đơn %d, mong %d", n, n0+2)
	}
}
