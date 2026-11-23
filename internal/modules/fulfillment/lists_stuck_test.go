package fulfillment_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/testkit"
)

// textNotifier ghi khóa và văn bản mọi cảnh báo.
type textNotifier struct {
	mu    sync.Mutex
	keys  []string
	texts []string
}

func (n *textNotifier) Notify(_ context.Context, key, severity, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if severity != "normal" {
		return nil
	}
	n.keys, n.texts = append(n.keys, key), append(n.texts, text)
	return nil
}

func (n *textNotifier) sent() ([]string, []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.keys), slices.Clone(n.texts)
}

// stuckRunner chạy lịch fulfillment.stuck của một Module dựng trên database của e với đồng hồ clock (như Ticker ghi
// việc: payload {slot}, tenant A), rồi lệnh cảnh báo ngày bằng worker của e.
type stuckRunner struct {
	e     *shipEnv
	clock *testClock
	mod   *fulfillment.Module
	notes *textNotifier
}

func newStuckRunner(e *shipEnv, clock *testClock) *stuckRunner {
	r := &stuckRunner{e: e, clock: clock, notes: &textNotifier{}}
	r.mod = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &realtimetest.RecordingBus{}, Events: events.NewRegistry(), Now: clock.Now,
	})
	e.useCommands(append(slices.Clone(e.cmds), fulfillment.NewStuckAlertCommand(e.staff.App, r.notes, nil)))
	return r
}

// schedule trả lịch đơn kẹt của module (3.600 s, theo tenant).
func (r *stuckRunner) schedule() schedules.Schedule {
	r.e.t.Helper()
	for _, s := range r.mod.Schedules() {
		if s.Name == fulfillment.StuckScheduleName {
			if s.Every != time.Hour || !s.PerTenant {
				r.e.t.Fatalf("lịch đơn kẹt = %v, theo tenant %v; muốn 3.600 s theo tenant", s.Every, s.PerTenant)
			}
			return s
		}
	}
	r.e.t.Fatalf("module không có lịch %s", fulfillment.StuckScheduleName)
	return schedules.Schedule{}
}

// run chạy lệnh của lịch ở giờ của clock, rồi worker (lệnh cảnh báo nếu có); trả kết quả {marked, cleared}.
func (r *stuckRunner) run() (marked, cleared int) {
	r.e.t.Helper()
	s := r.schedule()
	var h commands.Handler
	for _, c := range r.mod.CommandHandlers() {
		if c.Kind().Name == s.Kind.Name {
			h = c
		}
	}
	if h == nil {
		r.e.t.Fatalf("không có lệnh cho lịch %s", s.Name)
	}
	payload, err := json.Marshal(map[string]time.Time{"slot": r.clock.Now().UTC().Truncate(time.Hour)})
	if err != nil {
		r.e.t.Fatal(err)
	}
	out, err := h.Execute(r.e.t.Context(), queue.Job{ID: uuid.New(), Kind: s.Kind.Name, TenantID: &r.e.tenant, Payload: payload})
	var res struct {
		Marked  int `json:"marked"`
		Cleared int `json:"cleared"`
	}
	if err != nil || out.Status != commands.StatusDone || json.Unmarshal(out.Result, &res) != nil {
		r.e.t.Fatalf("lệnh đơn kẹt = %+v, %v", out, err)
	}
	r.e.drain()
	return res.Marked, res.Cleared
}

func (r *stuckRunner) list(f fulfillment.StuckFilter) ([]fulfillment.StuckShipment, string) {
	r.e.t.Helper()
	type page struct {
		rows []fulfillment.StuckShipment
		next string
	}
	p := readAs(r.e, func(ctx context.Context, tx pgx.Tx) (page, error) {
		rows, next, err := r.mod.Service.ListStuck(ctx, tx, f)
		return page{rows, next}, err //nolint:wrapcheck // test
	})
	return p.rows, p.next
}

func stuckIDs(rows []fulfillment.StuckShipment) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ShipmentID)
	}
	return out
}

// setLastEvent đặt trạng thái và mốc sự kiện cuối của hãng (dựng tình huống bằng chủ bảng).
func (e *shipEnv) setLastEvent(id uuid.UUID, status string, at time.Time) {
	e.t.Helper()
	e.ownerExec(`UPDATE fulfillment.shipments SET status = $2, last_carrier_event_at = $3, last_status_at = $3
		WHERE id = $1`, id, status, at)
}

func (e *shipEnv) stuckSince(id uuid.UUID) *time.Time {
	e.t.Helper()
	return readAs(e, func(ctx context.Context, tx pgx.Tx) (*time.Time, error) {
		var at *time.Time
		err := tx.QueryRow(ctx, `SELECT stuck_since FROM fulfillment.shipments_v WHERE id = $1`, id).Scan(&at)
		return at, err //nolint:wrapcheck // test
	})
}

// vnClock: đồng hồ ở 09:00 giờ Việt Nam của ngày hôm nay (giờ máy), micro giây.
func vnClock() *testClock {
	ict := time.FixedZone("ICT", 7*60*60)
	n := time.Now().In(ict)
	return &testClock{t: time.Date(n.Year(), n.Month(), n.Day(), 9, 0, 0, 0, ict).UTC()}
}

// expectStuck: mọi vận đơn của stuck có stuck_since = at, mọi vận đơn của fresh không có.
func (e *shipEnv) expectStuck(at time.Time, stuck, fresh map[string]uuid.UUID) {
	e.t.Helper()
	for name, id := range stuck {
		if got := e.stuckSince(id); got == nil || !got.Equal(at) {
			e.t.Errorf("%s: stuck_since = %v, muốn %v", name, got, at)
		}
	}
	for name, id := range fresh {
		if got := e.stuckSince(id); got != nil {
			e.t.Errorf("%s: stuck_since = %v, muốn rỗng", name, got)
		}
	}
}

// checkFirstList: danh sách sau lượt đầu là V1, V3 (cùng stuck_since: theo id) với đủ trường; lọc trạng thái, tài
// khoản, kho.
func (r *stuckRunner) checkFirstList(v1, v3 fulfillment.Shipment, now time.Time) {
	t, e := r.e.t, r.e
	t.Helper()
	rows, next := r.list(fulfillment.StuckFilter{AllWarehouses: true})
	if !slices.Equal(stuckIDs(rows), []uuid.UUID{v1.ID, v3.ID}) || next != "" {
		t.Fatalf("danh sách = %v next %q, muốn V1, V3", stuckIDs(rows), next)
	}
	got := rows[0]
	if got.Status != "shipping" || got.HoursSinceUpdate != 73 || got.OrderCode == "" || got.TrackingCode != v1.TrackingCode ||
		got.CarrierKey == "" || got.AccountName == "" || got.Label == "" || got.LastCarrierEventAt == nil ||
		!got.StuckSince.Equal(now) {
		t.Fatalf("dòng V1 = %+v", got)
	}
	if rows[1].HoursSinceUpdate != 49 {
		t.Fatalf("V3 giờ từ cập nhật = %d, muốn 49", rows[1].HoursSinceUpdate)
	}
	other := uuid.New()
	for _, c := range []struct {
		name string
		f    fulfillment.StuckFilter
		want []uuid.UUID
	}{
		{"lọc awaiting_pickup", fulfillment.StuckFilter{AllWarehouses: true, Status: "awaiting_pickup"}, []uuid.UUID{v3.ID}},
		{"tài khoản khác", fulfillment.StuckFilter{AllWarehouses: true, CarrierAccountID: &other}, []uuid.UUID{}},
		{"kho ngoài phạm vi", fulfillment.StuckFilter{WarehouseIDs: []uuid.UUID{uuid.New()}}, []uuid.UUID{}},
		{"không kho nào", fulfillment.StuckFilter{}, []uuid.UUID{}},
		{"kho MAIN", fulfillment.StuckFilter{WarehouseIDs: []uuid.UUID{e.shop.Warehouse}}, []uuid.UUID{v1.ID, v3.ID}},
	} {
		if rows, _ := r.list(c.f); !slices.Equal(stuckIDs(rows), c.want) {
			t.Errorf("%s = %v, muốn %v", c.name, stuckIDs(rows), c.want)
		}
	}
}

// checkAlertOnce: đúng một cảnh báo khóa stuck-shipments:<ngày>:<tenant>, chỉ số lượng, không mã của vận đơn nào.
func (r *stuckRunner) checkAlertOnce(vs []fulfillment.Shipment, now time.Time) {
	t, e := r.e.t, r.e
	t.Helper()
	keys, texts := r.notes.sent()
	want := "stuck-shipments:" + now.In(time.FixedZone("ICT", 7*3600)).Format("20060102") + ":" + e.tenant.String()
	if len(texts) != 1 || keys[0] != want {
		t.Fatalf("cảnh báo = %v %v, muốn một tin khóa %s", keys, texts, want)
	}
	if !strings.Contains(texts[0], "awaiting_pickup: 1, shipping: 1") {
		t.Fatalf("văn bản = %q", texts[0])
	}
	for _, s := range vs {
		o := e.getOrder(s.OrderID)
		for _, secret := range []string{s.Code, s.TrackingCode, o.Code, s.ID.String()} {
			if strings.Contains(texts[0], secret) {
				t.Fatalf("cảnh báo có %q: %q", secret, texts[0])
			}
		}
	}
}

// VC18: V1 shipping sự kiện cuối 73 giờ trước → kẹt; V2 71 giờ → không; V3 awaiting_pickup 49 giờ → kẹt; V4 mode
// manual 100 giờ → không. Cảnh báo ngày: một tin "awaiting_pickup: 1, shipping: 1" không có mã vận đơn, mã đơn, mã
// hãng; lượt thứ hai trong ngày không gửi thêm. Sự kiện mới cho V1 → stuck_since rỗng, rời danh sách.
func TestStuck_ThresholdsAlertAndClear(t *testing.T) {
	t.Parallel()
	te := newTrackEnv(t)
	e := te.shipEnv
	clock := vnClock()
	r := newStuckRunner(e, clock)
	now := clock.Now()
	var vs []fulfillment.Shipment
	for range 4 {
		_, _, s := te.shipped(1)
		vs = append(vs, s)
	}
	v1, v2, v3, v4 := vs[0], vs[1], vs[2], vs[3]
	e.setLastEvent(v1.ID, "shipping", now.Add(-73*time.Hour))
	e.setLastEvent(v2.ID, "shipping", now.Add(-71*time.Hour))
	e.setLastEvent(v3.ID, "awaiting_pickup", now.Add(-49*time.Hour))
	e.setLastEvent(v4.ID, "shipping", now.Add(-100*time.Hour))
	e.ownerExec(`UPDATE fulfillment.shipments SET mode = 'manual' WHERE id = $1`, v4.ID)

	if marked, cleared := r.run(); marked != 2 || cleared != 0 {
		t.Fatalf("lượt đầu đánh dấu %d, bỏ dấu %d; muốn 2, 0", marked, cleared)
	}
	e.expectStuck(now, map[string]uuid.UUID{"V1 73 giờ": v1.ID, "V3 49 giờ": v3.ID}, map[string]uuid.UUID{"V2 71 giờ": v2.ID, "V4 manual": v4.ID})
	r.checkFirstList(v1, v3, now)
	r.checkAlertOnce(vs, now)
	// Lượt thứ hai cùng ngày: không đánh dấu lại, không gửi thêm.
	clock.add(time.Hour)
	if marked, cleared := r.run(); marked != 0 || cleared != 0 {
		t.Fatalf("lượt hai đánh dấu %d, bỏ dấu %d", marked, cleared)
	}
	if _, texts := r.notes.sent(); len(texts) != 1 {
		t.Fatalf("lượt hai gửi thêm: %v", texts)
	}
	if at := e.stuckSince(v1.ID); at == nil || !at.Equal(now) {
		t.Fatalf("lượt hai đổi stuck_since của V1: %v", at)
	}

	// Sự kiện mới cho V1 (hãng báo đang giao, tra lại) → stuck_since rỗng, rời danh sách; lượt sau không đánh dấu lại.
	te.srv.Push(v1.TrackingCode, fulfillment.CarrierEvent{RawStatus: fake.RawInTransit, OccurredAt: clock.Now()})
	te.sync(v1.ID)
	te.drain()
	if at := e.stuckSince(v1.ID); at != nil {
		t.Fatalf("sau sự kiện mới stuck_since = %v", at)
	}
	if rows, _ := r.list(fulfillment.StuckFilter{AllWarehouses: true}); !slices.Equal(stuckIDs(rows), []uuid.UUID{v3.ID}) {
		t.Fatalf("sau sự kiện mới danh sách = %v", stuckIDs(rows))
	}
	// Hai giờ sau lượt đầu: V1 (sự kiện mới) không bị đánh dấu lại; V2 giờ đã 73 giờ không có tin → kẹt.
	clock.add(time.Hour)
	if marked, _ := r.run(); marked != 1 || e.stuckSince(v1.ID) != nil || e.stuckSince(v2.ID) == nil {
		t.Fatalf("lượt ba: đánh dấu %d, V1 %v, V2 %v; muốn 1, V1 rỗng, V2 kẹt", marked, e.stuckSince(v1.ID), e.stuckSince(v2.ID))
	}

	// getShipment có mốc để màn ghi "cập nhật lần cuối lúc…".
	v := readAs(e, func(ctx context.Context, tx pgx.Tx) (fulfillment.ShipmentView, error) {
		return r.mod.Service.ShipmentView(ctx, tx, v1.ID)
	})
	if v.LastCarrierEventAt == nil || v.LastSyncedAt == nil || v.StuckSince != nil {
		t.Fatalf("ShipmentView V1: sự kiện %v, đồng bộ %v, kẹt %v", v.LastCarrierEventAt, v.LastSyncedAt, v.StuckSince)
	}

	// V3 giao xong (đường khác không xóa dấu) → lượt sau bỏ dấu.
	e.ownerExec(`UPDATE fulfillment.shipments SET status = 'delivered' WHERE id = $1`, v3.ID)
	clock.add(time.Hour)
	if marked, cleared := r.run(); marked != 0 || cleared != 1 || e.stuckSince(v3.ID) != nil {
		t.Fatalf("vận đơn đã giao: đánh dấu %d, bỏ dấu %d, stuck_since %v", marked, cleared, e.stuckSince(v3.ID))
	}
}

// Đơn kẹt gồm vận đơn creating có lệnh tạo đã kết thúc mà không qua OnTerminal (người bỏ lệnh in_doubt bằng
// queue.AbandonJob → expired, Task 408) và vận đơn creating chắc chưa gửi (not_sent, chờ "Gửi lại"); lệnh tạo còn
// chờ → không. Vận đơn có anomaly stock_short mở → kẹt; đóng anomaly → lượt sau bỏ dấu. Số đếm vào cảnh báo ngày.
func TestStuck_CreatingAndStockShort(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	clock := vnClock()
	r := newStuckRunner(e, clock)
	_, abandoned := e.createTimedOut()
	jobID, st := e.createJob(abandoned.ID)
	if st != queue.StatusInDoubt {
		t.Fatalf("lệnh tạo = %s, muốn in_doubt", st)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, _, err := queue.AbandonJob(t.Context(), tx, e.tenant, jobID)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	o2, _ := e.order(1)
	pending := e.mustCreateShipment(o2.ID) // lệnh tạo còn chờ (chưa chạy worker)
	o3, _ := e.order(1)
	notSent := e.mustCreateShipment(o3.ID)
	e.ownerExec(`UPDATE fulfillment.shipments SET create_state = 'not_sent' WHERE id = $1`, notSent.ID)
	o4, _ := e.order(1)
	short := e.mustCreateShipment(o4.ID)
	e.ownerExec(`UPDATE fulfillment.shipments SET status = 'shipping', tracking_code = 'SS1', create_state = 'created',
		last_carrier_event_at = $2 WHERE id = $1`, short.ID, clock.Now().Add(-time.Hour))
	e.ownerExec(`INSERT INTO fulfillment.shipment_anomalies (tenant_id, shipment_id, kind) VALUES ($1, $2, 'stock_short')`,
		e.tenant, short.ID)

	if marked, _ := r.run(); marked != 3 {
		t.Fatalf("đánh dấu %d, muốn 3", marked)
	}
	for _, c := range []struct {
		name  string
		id    uuid.UUID
		stuck bool
	}{
		{"bỏ lệnh in_doubt", abandoned.ID, true},
		{"lệnh còn chờ", pending.ID, false},
		{"not_sent", notSent.ID, true},
		{"stock_short", short.ID, true},
	} {
		if got := e.stuckSince(c.id); (got != nil) != c.stuck {
			t.Errorf("%s: stuck_since = %v, muốn kẹt %v", c.name, got, c.stuck)
		}
	}
	if _, texts := r.notes.sent(); len(texts) != 1 || !strings.Contains(texts[0], "creating: 2, shipping: 1") {
		t.Fatalf("cảnh báo = %v", texts)
	}
	// Đóng anomaly stock_short → lượt sau bỏ dấu (vận đơn không còn lý do kẹt).
	if _, err := resolveAnomaly(e, r.mod, anomalyOf(e, short.ID, "stock_short"), "đã điều chỉnh tồn"); err != nil {
		t.Fatal(err)
	}
	clock.add(time.Hour)
	if _, cleared := r.run(); cleared != 1 || e.stuckSince(short.ID) != nil {
		t.Fatalf("bỏ dấu %d, stuck_since %v", cleared, e.stuckSince(short.ID))
	}
}

// Trước 08:00 giờ Việt Nam: đánh dấu nhưng không gửi cảnh báo; lượt 08:00 gửi. Không có vận đơn kẹt → không gửi.
func TestStuck_AlertWindow(t *testing.T) {
	t.Parallel()
	te := newTrackEnv(t)
	e := te.shipEnv
	clock := vnClock()
	clock.add(-2 * time.Hour) // 07:00
	r := newStuckRunner(e, clock)
	r.run()
	if _, texts := r.notes.sent(); len(texts) != 0 {
		t.Fatalf("không có vận đơn kẹt mà vẫn gửi: %v", texts)
	}
	_, _, s := te.shipped(1)
	e.setLastEvent(s.ID, "shipping", clock.Now().Add(-80*time.Hour))
	if marked, _ := r.run(); marked != 1 {
		t.Fatal("07:00 phải vẫn đánh dấu")
	}
	if _, texts := r.notes.sent(); len(texts) != 0 {
		t.Fatalf("07:00 đã gửi: %v", texts)
	}
	clock.add(time.Hour)
	r.run()
	if _, texts := r.notes.sent(); len(texts) != 1 || !strings.Contains(texts[0], "shipping: 1") {
		t.Fatalf("08:00 cảnh báo = %v", texts)
	}
}

// Danh sách đơn kẹt: phân trang theo con trỏ, lâu nhất trước, không lặp; con trỏ hỏng → ErrInvalidPage.
func TestStuck_ListPages(t *testing.T) {
	t.Parallel()
	te := newTrackEnv(t)
	e := te.shipEnv
	clock := vnClock()
	r := newStuckRunner(e, clock)
	var want []uuid.UUID
	for i := range 5 {
		_, _, s := te.shipped(1)
		e.setLastEvent(s.ID, "shipping", clock.Now().Add(-100*time.Hour))
		e.ownerExec(`UPDATE fulfillment.shipments SET stuck_since = $2 WHERE id = $1`, s.ID,
			clock.Now().Add(-time.Duration(10-i)*time.Hour))
		want = append(want, s.ID)
	}
	var got []uuid.UUID
	cursor := ""
	for range 4 {
		rows, next := r.list(fulfillment.StuckFilter{AllWarehouses: true, Limit: 2, Cursor: cursor})
		got = append(got, stuckIDs(rows)...)
		if next == "" {
			break
		}
		cursor = next
	}
	if !slices.Equal(got, want) {
		t.Fatalf("các trang = %v, muốn %v", got, want)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, _, err := r.mod.Service.ListStuck(t.Context(), tx, fulfillment.StuckFilter{AllWarehouses: true, Cursor: "hỏng"})
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrInvalidPage) {
		t.Fatalf("con trỏ hỏng = %v", err)
	}
}

// Câu đơn kẹt (danh sách, đếm, đánh dấu) và câu Cần xem (danh sách, số đếm) với 50.000 vận đơn đã kết thúc và 500
// vận đơn mở → không Seq Scan.
func TestStuck_PlanUsesIndexes(t *testing.T) {
	t.Parallel()
	te := newTrackEnv(t)
	e := te.shipEnv
	_, _, tmpl := te.shipped(1)
	old := time.Now().Add(-200 * time.Hour)
	// Nạp dữ liệu thử bằng câu INSERT … SELECT generate_series trong ownerBulk (giới hạn thời gian riêng của bước nạp:
	// lúc máy chung bận, nạp 6.250 dòng từng vượt 30 s của ứng dụng); câu đang được kiểm (EXPLAIN bên dưới) vẫn chạy
	// bằng crm_app với giới hạn 30 s.
	for p := 1; p <= 8; p++ { // 8 × 6.250 = 50.000 vận đơn đã kết thúc
		st := "delivered"
		if p%2 == 0 {
			st = "returned"
		}
		te.clone(tmpl, p, 6250, st, &old, &old)
	}
	te.clone(tmpl, 9, 500, "shipping", &old, nil)
	e.ownerBulk(func(tx pgx.Tx) error {
		for _, q := range []string{
			`UPDATE fulfillment.shipments SET stuck_since = now() WHERE code LIKE 'E9%' AND code < 'E900000000050'`,
			// Việc Cần xem đã đóng của vận đơn đã kết thúc (bảng không rỗng như khi chạy thật), vài việc mở của vận đơn mở.
			`INSERT INTO fulfillment.shipment_anomalies (tenant_id, shipment_id, kind, status, resolved_at)
				SELECT tenant_id, id, 'missing_items', 'resolved', now() FROM fulfillment.shipments WHERE code LIKE 'E1%'`,
			`INSERT INTO fulfillment.shipment_anomalies (tenant_id, shipment_id, kind)
				SELECT tenant_id, id, 'stock_short' FROM fulfillment.shipments WHERE code LIKE 'E9%' AND code < 'E900000000020'`,
			// Việc gọi lại đã xong của vận đơn đã kết thúc, vài việc mở của vận đơn mở.
			`INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at, status)
				SELECT tenant_id, id, order_id, 1, now(), now(), 'done' FROM fulfillment.shipments WHERE code LIKE 'E2%'`,
			`INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at)
				SELECT tenant_id, id, order_id, 1, now(), now() FROM fulfillment.shipments WHERE code LIKE 'E9%' AND code < 'E900000000010'`,
		} {
			if _, err := tx.Exec(t.Context(), q); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	e.ownerBulk(func(tx pgx.Tx) error { // thống kê sau khi dữ liệu đã commit
		_, err := tx.Exec(t.Context(), `ANALYZE fulfillment.callback_tasks, fulfillment.shipments, fulfillment.shipment_anomalies`)
		return err //nolint:wrapcheck // test
	})
	hours, err := json.Marshal(fulfillment.DefaultSettings().StuckHours)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		args []any
	}{
		{"listStuckShipments", []any{nil, nil, true, []uuid.UUID{}, nil, nil, int32(51)}},
		{"countStuckByStatus", nil},
		{"listOpenAnomalies", []any{true, []uuid.UUID{}, nil, nil, int32(51)}},
		{"reviewCounts", []any{true, []uuid.UUID{}, time.Now(), []string{"in_doubt_create", "in_doubt_cancel"}}},
		{"syncStuckShipments", []any{time.Now(), hours, []uuid.UUID{}}}, // chạy thật: để cuối
	} {
		nodes, err := testkit.ExplainAsApp(t.Context(), e.staff.App, e.tenant, storeSQL(t, c.name), c.args...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		testkit.RequireNoSeqScan(t, nodes)
	}
}

// storeSQL đọc hằng câu SQL name do sqlc sinh trong internal/store/lists.sql.go (chuỗi thô giữa hai dấu `).
func storeSQL(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile("internal/store/lists.sql.go")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(src), "const "+name+" = `")
	sql, _, closed := strings.Cut(rest, "`")
	if !ok || !closed {
		t.Fatalf("không thấy hằng %s", name)
	}
	return sql
}
