package fulfillment_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/tools/fakecarrier/server"
)

const trackSecret = "track-test-fake-secret-0123456789abcdef"

// testClock là đồng hồ giả của Service: đứng yên tới khi test dời.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// trackEnv: shipEnv (tenant A, tài khoản hãng fake-1 active, mode api) cộng máy chủ hãng giả thật
// (tools/fakecarrier/server qua httptest; client bỏ sau 1 s, máy chủ ở chế độ treo giữ 30 s), Module với adapter fake
// HTTP và đồng hồ giả, worker chạy mọi lệnh của module cộng lệnh cảnh báo đồng bộ.
type trackEnv struct {
	*shipEnv
	srv     *server.Server
	client  *fake.Client
	mod     *fulfillment.Module
	clock   *testClock
	metrics *metrics.Registry
	notes   *recNotifier
}

func newTrackEnv(t *testing.T) *trackEnv {
	t.Helper()
	e := &trackEnv{
		shipEnv: newShipEnv(t), srv: server.New(server.Options{Secret: trackSecret}),
		clock: &testClock{t: time.Now().UTC().Truncate(time.Microsecond)}, metrics: metrics.NewRegistry(),
		notes: &recNotifier{},
	}
	hs := httptest.NewServer(e.srv)
	t.Cleanup(hs.Close)
	hc := hs.Client()
	hc.Timeout = time.Second
	e.client = fake.NewClient(hs.URL, trackSecret, hc)
	reg, err := fulfillment.NewRegistry(e.client)
	if err != nil {
		t.Fatal(err)
	}
	e.mod = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Bus: &testkit.RecordingBus{}, Events: events.NewRegistry(),
		Carriers: reg, Now: e.clock.Now, Metrics: e.metrics,
	})
	e.useCommands(append(e.mod.CommandHandlers(), fulfillment.NewTrackAlertCommand(e.staff.App, e.notes, nil)))
	return e
}

// sync gọi Service.SyncShipment (như nút "Tra lại") trong tx crm_app; trả mã việc.
func (e *trackEnv) sync(id uuid.UUID) uuid.UUID {
	e.t.Helper()
	return readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
		return e.mod.Service.SyncShipment(ctx, tx, id, e.actor)
	})
}

func (e *trackEnv) setSynced(id uuid.UUID, at time.Time) {
	e.t.Helper()
	e.ownerExec(`UPDATE fulfillment.shipments SET last_synced_at = $2 WHERE id = $1`, id, at)
}

// syncState đọc last_synced_at (nil: chưa đồng bộ) và last_sync_error của vận đơn qua view công bố.
func (e *trackEnv) syncState(id uuid.UUID) (*time.Time, string) {
	e.t.Helper()
	var at *time.Time
	var code *string
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT last_synced_at, last_sync_error FROM fulfillment.shipments_v
			WHERE id = $1`, id).Scan(&at, &code)
	}); err != nil {
		e.t.Fatal(err)
	}
	if code == nil {
		return at, ""
	}
	return at, *code
}

// job đọc trạng thái, lỗi cuối của việc id.
func (e *trackEnv) job(id uuid.UUID) (status, lastError string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status, coalesce(last_error, '') FROM platform.jobs WHERE id = $1`,
			id).Scan(&status, &lastError)
	})
	return status, lastError
}

func (e *trackEnv) push(tracking, raw string) {
	e.srv.Push(tracking, fulfillment.CarrierEvent{RawStatus: raw})
}

// VC17: hãng (API) nói InTransit; webhook nói Delivered → sau mọi việc V1 shipping, đơn confirmed (trạng thái lấy
// từ API, webhook chỉ là tín hiệu tra lại); last_synced_at = giờ tra.
func TestTrack_WebhookOnlySignals(t *testing.T) {
	e := newTrackEnv(t)
	o, v, s := e.shipped(2)
	e.push(s.TrackingCode, fake.RawInTransit)
	e.signal(s.TrackingCode, "webhook-says-delivered")
	e.drain()
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusShipping {
		t.Fatalf("vận đơn = %s, muốn shipping", got.Status)
	}
	if got := e.getOrder(o.ID); got.Status != "confirmed" {
		t.Fatalf("đơn = %s, muốn confirmed", got.Status)
	}
	e.stockIs(v, [3]int32{5, 0, 2})
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1 AND status = 'delivered'`,
		s.ID); n != 0 {
		t.Fatalf("sự kiện delivered = %d, muốn 0", n)
	}
	at, code := e.syncState(s.ID)
	if at == nil || !at.Equal(e.clock.Now()) || code != "" {
		t.Fatalf("mốc đồng bộ = %v %q, muốn %v và không lỗi", at, code, e.clock.Now())
	}
}

// VC18: máy chủ hãng treo 30 s → lệnh not_sent, chờ lượt sau (pending, chưa tới giờ); last_sync_error = timeout;
// last_synced_at trong fulfillment.shipments_v giữ giờ cũ.
func TestTrack_CarrierHangs(t *testing.T) {
	e := newTrackEnv(t)
	_, _, s := e.shipped(1)
	old := e.clock.Now().Add(-2 * time.Hour)
	e.setSynced(s.ID, old)
	e.srv.SetMode("track", "timeout_after_send")
	job := e.sync(s.ID)
	e.drain()
	if st, last := e.job(job); st != "pending" || !strings.Contains(last, "not_sent: timeout") {
		t.Fatalf("việc tra = %s %q, muốn pending not_sent: timeout", st, last)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE id = $1 AND run_at > now() AND attempts = 1`, job); n != 1 {
		t.Fatal("việc tra phải hẹn lượt sau, đã tính một lượt")
	}
	at, code := e.syncState(s.ID)
	if code != "timeout" || at == nil || !at.Equal(old) {
		t.Fatalf("mốc đồng bộ = %v %q, muốn %v timeout", at, code, old)
	}
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusAwaitingPickup {
		t.Fatalf("vận đơn = %s", got.Status)
	}
}

// Hãng trả 429 Retry-After: 7 → việc pending, run_at cách 7 s (±1 s), không đổi vận đơn; last_sync_error rate_limited.
func TestTrack_RateLimited(t *testing.T) {
	e := newTrackEnv(t)
	_, _, s := e.shipped(1)
	e.srv.SetMode("track", "rate_limited")
	job := e.sync(s.ID)
	e.drain()
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE id = $1 AND status = 'pending'
		AND run_at BETWEEN now() + interval '6 seconds' AND now() + interval '8 seconds'`, job); n != 1 {
		st, last := e.job(job)
		t.Fatalf("việc tra phải chờ 7 s: %s %q", st, last)
	}
	if _, code := e.syncState(s.ID); code != "rate_limited" {
		t.Fatalf("last_sync_error = %q", code)
	}
}

// Hãng từ chối khóa (401) → việc dead (rejected), tài khoản auth_failed, có lệnh cảnh báo carrier-auth;
// last_sync_error auth.
func TestTrack_AuthRevoked(t *testing.T) {
	e := newTrackEnv(t)
	_, _, s := e.shipped(1)
	e.srv.SetMode("track", "auth_revoked")
	job := e.sync(s.ID)
	e.drain()
	if st, last := e.job(job); st != "dead" || !strings.Contains(last, "rejected: auth") {
		t.Fatalf("việc tra = %s %q", st, last)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.carrier_accounts WHERE id = $1 AND status = 'auth_failed'`,
		e.account); n != 1 {
		t.Fatal("tài khoản phải sang auth_failed")
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, fulfillment.AuthAlertKind); n != 1 {
		t.Fatalf("lệnh cảnh báo khóa = %d, muốn 1", n)
	}
	if _, code := e.syncState(s.ID); code != "auth" {
		t.Fatalf("last_sync_error = %q", code)
	}
}

// Tài khoản không active → việc dead lý do account_unavailable, không gọi hãng; vận đơn đã kết thúc (giao 8 ngày
// trước, đã hủy) → việc done, không gọi hãng, không đổi gì.
func TestTrack_SkipsWithoutCalling(t *testing.T) {
	e := newTrackEnv(t)
	_, _, s := e.shipped(1)
	_, _, s2 := e.shipped(1)
	e.ownerExec(`UPDATE fulfillment.shipments SET status = 'delivered', delivered_at = $2 WHERE id = $1`, s2.ID,
		e.clock.Now().Add(-8*24*time.Hour))
	calls := e.srv.Calls("track")
	closed := e.sync(s2.ID)
	e.drain()
	if st, _ := e.job(closed); st != "done" || e.srv.Calls("track") != calls {
		t.Fatalf("vận đơn đã kết thúc: việc %s, gọi hãng %d lần", st, e.srv.Calls("track")-calls)
	}
	e.ownerExec(`UPDATE fulfillment.carrier_accounts SET status = 'paused' WHERE id = $1`, e.account)
	job := e.sync(s.ID)
	e.drain()
	if st, last := e.job(job); st != "dead" || !strings.Contains(last, "rejected: account_unavailable") {
		t.Fatalf("việc tra = %s %q", st, last)
	}
	if e.srv.Calls("track") != calls {
		t.Fatal("tài khoản paused không được gọi hãng")
	}
	if _, code := e.syncState(s.ID); code != "account_unavailable" {
		t.Fatalf("last_sync_error = %q", code)
	}
}

// VC10: V4 creating (lệnh tạo in_doubt), hãng đã có kiện theo mã của V4 và PickedUp → tra theo mã vận đơn → V4 có
// mã hãng, shipping; lệnh tạo sau đó tra lại thấy đã tạo (done) → không đổi gì thêm; việc "không rõ đã tạo" mở lúc
// in_doubt (Task M3-12, VC16) được đóng, không còn anomaly mở.
func TestTrack_InDoubtCreateFoundByCode(t *testing.T) {
	e := newTrackEnv(t)
	o, v := e.order(1)
	e.srv.SetMode("create", "timeout_after_create")
	s := e.mustCreateShipment(o.ID)
	e.drain()
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment' AND status = 'in_doubt'`); n != 1 {
		t.Fatal("lệnh tạo phải in_doubt")
	}
	e.srv.SetMode("create", "ok")
	lr, err := e.client.LookupByCode(t.Context(), fulfillment.Account{ExternalAccountRef: e.accountRef}, s.Code)
	if err != nil || !lr.Found {
		t.Fatalf("hãng giả phải có kiện của %s: %v", s.Code, err)
	}
	e.push(lr.TrackingCode, fake.RawPickedUp)
	e.sync(s.ID)
	e.drain()
	got := e.shipment(s.ID)
	if got.TrackingCode != lr.TrackingCode || got.Status != fulfillment.StatusShipping {
		t.Fatalf("V4 = %q %s, muốn %q shipping", got.TrackingCode, got.Status, lr.TrackingCode)
	}
	e.stockIs(v, [3]int32{5, 0, 1})
	events := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1`, s.ID)
	done, _, _, err := commands.ResolveInDoubt(context.Background(), e.staff.App, e.cmds, 10)
	if err != nil || done != 1 {
		t.Fatalf("tra lại lệnh tạo: done %d, %v", done, err)
	}
	e.drain()
	after := e.shipment(s.ID)
	if after.Version != got.Version || after.Status != fulfillment.StatusShipping {
		t.Fatalf("sau lệnh tạo xong: v%d %s, muốn v%d shipping", after.Version, after.Status, got.Version)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1`, s.ID); n != events {
		t.Fatalf("sự kiện = %d, muốn %d", n, events)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1 AND status = 'open'`, s.ID); n != 0 {
		t.Fatalf("anomaly mở = %d, muốn 0", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND needs_review`, s.ID); n != 0 {
		t.Fatal("vận đơn còn cần xem sau khi lệnh tạo xong")
	}
	e.stockIs(v, [3]int32{5, 0, 1})
}

// Bấm "Tra lại" 5 lần trong một phút → 1 việc (cùng mã); phút sau → việc mới.
func TestTrack_ManualSyncOncePerMinute(t *testing.T) {
	e := newTrackEnv(t)
	_, _, s := e.shipped(1)
	first := e.sync(s.ID)
	for range 4 {
		if id := e.sync(s.ID); id != first {
			t.Fatalf("lần bấm sau trả việc %s, muốn %s", id, first)
		}
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.refresh_status'
		AND dedupe_key LIKE 'manual:%'`); n != 1 {
		t.Fatalf("việc tra tay = %d, muốn 1", n)
	}
	e.clock.add(time.Minute)
	if id := e.sync(s.ID); id == first {
		t.Fatal("phút sau phải ra việc mới")
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.mod.Service.SyncShipment(e.t.Context(), tx, uuid.New(), e.actor)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("vận đơn lạ: %v, muốn ErrNotFound", err)
	}
}

// Q-T91-11: kiểm kho ghi thấp hơn phần giữ, hãng báo PickedUp → ApplyTracking trả ErrStockShort: việc tra vẫn done
// (không thử lại vô ích), vận đơn giữ awaiting_pickup, last_sync_error stock_short, một anomaly stock_short mở; cảnh
// báo stock-short:<tenant> một lần trong ngày (lần tra sau cùng ngày không thêm).
func TestTrack_StockShort(t *testing.T) {
	e := newTrackEnv(t)
	_, v, s := e.shipped(2)
	e.ownerExec(`UPDATE inventory.stock_balances SET on_hand = 1 WHERE variant_id = $1`, v)
	e.push(s.TrackingCode, fake.RawPickedUp)
	job := e.sync(s.ID)
	e.drain()
	if st, last := e.job(job); st != "done" {
		t.Fatalf("việc tra = %s %q, muốn done", st, last)
	}
	if got := e.shipment(s.ID); got.Status != fulfillment.StatusAwaitingPickup {
		t.Fatalf("vận đơn = %s, muốn awaiting_pickup", got.Status)
	}
	if _, code := e.syncState(s.ID); code != "stock_short" {
		t.Fatalf("last_sync_error = %q", code)
	}
	e.clock.add(time.Minute)
	e.sync(s.ID)
	e.drain()
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1 AND kind = 'stock_short'
		AND status = 'open'`, s.ID); n != 1 {
		t.Fatalf("anomaly stock_short = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'done'`,
		fulfillment.TrackAlertKind); n != 1 {
		t.Fatalf("lệnh cảnh báo = %d, muốn 1", n)
	}
	if !e.notes.has("stock-short:" + e.tenant.String()) {
		t.Fatalf("thiếu cảnh báo stock-short: %v", e.notes.keys)
	}
}

// Kết quả lệnh tra (platform.jobs.result) chỉ có mã hãng, trạng thái, khóa sự kiện, giờ, số tiền: không SĐT, tên,
// địa chỉ của đơn.
func TestTrack_ResultHasNoPersonalData(t *testing.T) {
	e := newTrackEnv(t)
	_, _, s := e.shipped(1)
	e.srv.Push(s.TrackingCode, fulfillment.CarrierEvent{
		RawStatus: fake.RawDeliveryFailed, Key: "df:1",
		OccurredAt: e.clock.Now(),
	})
	job := e.sync(s.ID)
	e.drain()
	var raw json.RawMessage
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT result FROM platform.jobs WHERE id = $1`, job).Scan(&raw)
	})
	testkit.AssertNoPersonalData(t, "kết quả lệnh tra", string(raw))
	if !strings.Contains(string(raw), s.TrackingCode) {
		t.Fatalf("kết quả = %s", raw)
	}
}

// Lỗi tạm của database khi áp (OnDone trả lên để Runner chạy lại) và lỗi lặp lại (ghi last_sync_error, việc done).
func TestTrack_TransientDBError(t *testing.T) {
	t.Parallel()
	pg := func(code string) error { return fmt.Errorf("áp: %w", &pgconn.PgError{Code: code}) }
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"deadlock 40P01", pg("40P01"), true},
		{"chờ khóa 55P03", pg("55P03"), true},
		{"tuần tự 40001", pg("40001"), true},
		{"quá hạn câu lệnh 57014", pg("57014"), true},
		{"đang tắt 57P01", pg("57P01"), true},
		{"đang khởi động 57P03", pg("57P03"), true},
		{"quá nhiều kết nối 53300", pg("53300"), true},
		{"mất kết nối 08006", pg("08006"), true},
		{"lỗi lúc nối", fmt.Errorf("áp: %w", &pgconn.ConnectError{}), true},
		{"ctx hết hạn", fmt.Errorf("áp: %w", context.DeadlineExceeded), true},
		{"ctx bị hủy", context.Canceled, true},
		{"CHECK 23514", pg("23514"), false},
		{"trùng 23505", pg("23505"), false},
		{"tồn thiếu", fmt.Errorf("áp: %w", fulfillment.ErrStockShort), false},
		{"kết quả sai dạng", fulfillment.ErrInvalidTracking, false},
		{"lỗi khác", errors.New("x"), false},
	}
	for _, c := range cases {
		if got := fulfillment.TransientDBError(c.err); got != c.want {
			t.Errorf("%s: %v, muốn %v", c.name, got, c.want)
		}
	}
}

// Phân loại lỗi gọi hãng của lệnh tra: trạng thái lệnh, lý do (last_sync_error), Retry-After, mã cho MarkAuthFailed.
func TestTrack_ClassifyTrackError(t *testing.T) {
	t.Parallel()
	ce := func(c fulfillment.ErrorClass, code string, wrapped error) error {
		return fmt.Errorf("đọc hành trình: %w", &fulfillment.CarrierError{Class: c, Code: code, RetryAfter: 7 * time.Second, Err: wrapped})
	}
	cases := []struct {
		name, status, reason, auth string
		err                        error
		retry                      time.Duration
	}{
		{"429", commands.StatusNotSent, "rate_limited", "", ce(fulfillment.ClassRateLimited, "http_429", fulfillment.ErrNotSent), 7 * time.Second},
		{"401", commands.StatusRejected, "auth", "http_401", ce(fulfillment.ClassAuth, "http_401", fulfillment.ErrRejected), 0},
		{"403 không mã", commands.StatusRejected, "auth", "auth", ce(fulfillment.ClassAuth, "", fulfillment.ErrRejected), 0},
		{"quá hạn", commands.StatusNotSent, "timeout", "", fmt.Errorf("gọi: %w", context.DeadlineExceeded), 0},
		{"503", commands.StatusNotSent, "carrier_error", "", ce(fulfillment.ClassNotSentRetry, "http_503", fulfillment.ErrNotSent), 0},
		{"không rõ", commands.StatusNotSent, "carrier_error", "", ce(fulfillment.ClassUnknownOutcome, "http_500", errors.New("x")), 0},
		{"404", commands.StatusRejected, "carrier_rejected", "", ce(fulfillment.ClassRejected, "http_404", fulfillment.ErrRejected), 0},
		{"vĩnh viễn", commands.StatusRejected, "carrier_rejected", "", ce(fulfillment.ClassPermanent, "x", errors.New("x")), 0},
		{"M1 từ chối", commands.StatusRejected, "carrier_rejected", "", fmt.Errorf("đọc: %w", fulfillment.ErrRejected), 0},
		{"không hỗ trợ", commands.StatusRejected, "not_supported", "", fulfillment.ErrNotSupported, 0},
		{"lỗi mạng", commands.StatusNotSent, "carrier_error", "", errors.New("connection reset"), 0},
	}
	for _, c := range cases {
		out, auth := fulfillment.ClassifyTrackError(c.err)
		if out.Status != c.status || out.Reason != c.reason || auth != c.auth || out.RetryAfter != c.retry {
			t.Errorf("%s: %+v auth %q, muốn %s %s %v auth %q", c.name, out, auth, c.status, c.reason, c.retry, c.auth)
		}
	}
}
