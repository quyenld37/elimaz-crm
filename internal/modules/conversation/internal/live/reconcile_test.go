package live_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/inbound"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/webhooks"
	"elimaz.com.vn/crm/internal/testkit/fakegraph"
	"elimaz.com.vn/crm/internal/testkit/fbtest"
)

// reconcileRun là kết quả một lần đối chiếu: số Meta đếm đã ghi, số cảnh báo live-missing, payload cảnh báo cuối.
type reconcileRun struct {
	meta, missing int
	alert         map[string]any
}

// reconcile chạy lệnh đối chiếu của phiên e.s như Runner (đúng một lệnh đã được ghi khi phiên kết thúc): Execute ngoài
// transaction, OnDone trong tx tenant; đọc lại meta_comment_count và cảnh báo live-missing.
func (e *fbEnv) reconcile() reconcileRun {
	e.t.Helper()
	var j queue.Job
	e.mustTx(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, payload FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2`,
			live.ReconcileCommandName, "live-reconcile:"+e.s.ID.String()).Scan(&j.ID, &j.Payload)
	})
	j.TenantID, j.Kind = &e.tenant, live.ReconcileCommandName
	c := live.NewReconcileCommand(e.d)
	out, err := c.Execute(e.t.Context(), j)
	if err != nil || out.Status != commands.StatusDone {
		e.t.Fatalf("Execute: %+v %v", out, err)
	}
	if strings.Contains(string(out.Result), "chốt") || strings.Contains(string(out.Result), fbLiveID+"_c") {
		e.t.Errorf("kết quả lệnh chứa nội dung hay mã comment: %s", out.Result)
	}
	e.mustTx(func(tx pgx.Tx) error { return c.OnDone(e.t.Context(), j, out.Result, tx) })
	var r reconcileRun
	e.mustTx(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT coalesce(meta_comment_count, -1) FROM conversation.live_sessions
			WHERE id = $1 AND reconciled_at IS NOT NULL`, e.s.ID).Scan(&r.meta)
	})
	r.missing = e.alerts("live-missing:" + e.s.ID.String())
	if r.missing > 0 {
		var raw []byte
		e.mustTx(func(tx pgx.Tx) error {
			return tx.QueryRow(e.t.Context(), `SELECT payload FROM platform.jobs WHERE kind = $1 AND dedupe_key LIKE 'live-missing:%'
				ORDER BY created_at DESC LIMIT 1`, live.AlertCommandName).Scan(&raw)
		})
		_ = json.Unmarshal(raw, &r.alert)
	}
	return r
}

// processInbound chạy đường vào Facebook thật (Task 153, 154) trên mọi việc vào đang chờ: comment thành tin is_live.
func (e *fbEnv) processInbound() {
	e.t.Helper()
	d := e.d
	d.Bus, d.Events = &realtimetest.RecordingBus{}, events.NewRegistry()
	d.Config = config.Config{Meta: config.MetaConfig{AppID: fbtest.AppID}}
	hs, err := webhooks.InboundJobHandlers([]webhooks.Source{e.fb}, []webhooks.InboundHandler{inbound.NewHandler(d, facebook.Key)})
	if err != nil {
		e.t.Fatal(err)
	}
	w := queue.NewWorker(e.staff.App, hs, queue.WorkerOptions{WorkerID: "w-in", Logger: slog.New(slog.DiscardHandler), Concurrency: 4})
	for {
		n, err := w.RunOnce(e.t.Context(), 50)
		if err != nil {
			e.t.Fatalf("đường vào: %v", err)
		}
		if n == 0 {
			return
		}
	}
}

// Đối chiếu cuối phiên: 10 comment đã thành tin is_live (webhook), Meta giữ lại 5 comment không phát qua luồng lẫn
// webhook → lệnh lấy đủ (5 việc vào mới, trùng thì bỏ); total_count = 17 mà CRM có 15 → meta_comment_count = 17, đúng
// 1 cảnh báo live-missing nêu 15/17 (chỉ mã phiên và con số); kết thúc phiên ghi đúng một lệnh đối chiếu (bấm tay kết
// thúc lại không thêm).
func TestReconcileFillsMissingAndAlerts(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	for i := range 10 {
		e.fg.PushLiveComment(fbLiveID, fbComment(i))
		e.webhookComment(i)
	}
	e.processInbound()
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE is_live`); n != 10 {
		t.Fatalf("%d tin is_live sau đường vào, muốn 10", n)
	}
	for i := 10; i < 15; i++ {
		e.fg.AddLiveComment(fbLiveID, fbComment(i))
	}
	e.fg.SetLiveCommentTotal(fbLiveID, 17)
	e.observe(fbLiveID, live.StatusEnded, live.DetectedWebhook, time.Now())
	e.observe(fbLiveID, live.StatusEnded, live.DetectedPoll, time.Now()) // đã kết thúc: không ghi lệnh thứ hai
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, live.ReconcileCommandName); n != 1 {
		t.Fatalf("%d lệnh đối chiếu, muốn 1", n)
	}
	r := e.reconcile()
	if total, d := e.jobs(); total != 15 || d != 15 {
		t.Errorf("sau đối chiếu: %d việc, %d khóa; muốn 15", total, d)
	}
	if r.meta != 17 || r.missing != 1 || r.alert["got"] != float64(15) || r.alert["meta"] != float64(17) {
		t.Errorf("đối chiếu: %+v; muốn meta 17, 1 cảnh báo 15/17", r)
	}
	// Chạy lại lệnh (Runner chạy lại sau lỗi): không thêm việc vào; sau khi đường vào xử lý, CRM có 15 tin.
	e.processInbound()
	if n := e.count(`SELECT count(*) FROM conversation.messages WHERE is_live`); n != 15 {
		t.Errorf("%d tin is_live sau đối chiếu, muốn 15", n)
	}
}

// Bấm tay kết thúc phiên ghi lệnh đối chiếu (một lần); bấm lại không thêm.
func TestEndManualEnqueuesReconcileOnce(t *testing.T) {
	t.Parallel()
	e := newFBEnv(t)
	u := operator(e.tenant, e.page, "live.operate")
	for range 2 {
		e.mustTx(func(tx pgx.Tx) error {
			_, err := live.EndManual(t.Context(), tx, u, e.s.ID, time.Now())
			return err //nolint:wrapcheck // test
		})
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2`,
		live.ReconcileCommandName, "live-reconcile:"+e.s.ID.String()); n != 1 {
		t.Errorf("%d lệnh đối chiếu, muốn 1", n)
	}
	// Phiên không còn → rejected; kênh tạm lỗi → not_sent (tự chạy lại).
	c := live.NewReconcileCommand(e.d)
	body, _ := json.Marshal(map[string]any{"sessionId": uuid.New()})
	if out, err := c.Execute(t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant, Payload: body}); err != nil ||
		out.Status != commands.StatusRejected {
		t.Errorf("phiên không có: %+v %v", out, err)
	}
	e.fg.FailNext(fbLiveID+"/comments", fakegraph.GraphErrorSpec{HTTPStatus: 500, Code: 2, Type: "OAuthException", Message: "tạm lỗi"})
	body, _ = json.Marshal(map[string]any{"sessionId": e.s.ID})
	if out, err := c.Execute(t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant, Payload: body}); err != nil ||
		out.Status != commands.StatusNotSent {
		t.Errorf("Meta lỗi: %+v %v", out, err)
	}
	if !c.AutoRetryNotSent() {
		t.Error("lệnh chỉ đọc phải tự chạy lại")
	}
	if st, err := c.Lookup(t.Context(), queue.Job{}); err != nil || st != commands.StatusNotSent {
		t.Errorf("Lookup = %q %v", st, err)
	}
}

// recordingAlerter ghi các cảnh báo đã gửi.
type recordingAlerter struct {
	mu   sync.Mutex
	sent []string
}

func (a *recordingAlerter) Notify(_ context.Context, key, severity, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, key+"|"+severity+"|"+text)
	return nil
}

// Lệnh cảnh báo: live-source và live-missing, mức thường, văn bản chỉ có mã phiên và con số; payload hỏng → rejected.
func TestAlertCommand(t *testing.T) {
	t.Parallel()
	a := &recordingAlerter{}
	c := live.NewAlertCommand(a, slog.New(slog.DiscardHandler))
	id := uuid.New()
	for _, p := range []map[string]any{
		{"kind": "source", "sessionId": id},
		{"kind": "missing", "sessionId": id, "got": 600, "meta": 605},
	} {
		body, _ := json.Marshal(p)
		if out, err := c.Execute(t.Context(), queue.Job{Payload: body}); err != nil || out.Status != commands.StatusDone {
			t.Fatalf("Execute %v: %+v %v", p, out, err)
		}
	}
	if len(a.sent) != 2 || !strings.HasPrefix(a.sent[0], "live-source:"+id.String()+"|normal|") ||
		!strings.HasPrefix(a.sent[1], "live-missing:"+id.String()+"|normal|") || !strings.Contains(a.sent[1], "600/605") {
		t.Errorf("cảnh báo: %q", a.sent)
	}
	if out, _ := c.Execute(t.Context(), queue.Job{Payload: []byte(`{"kind":"x"}`)}); out.Status != commands.StatusRejected {
		t.Errorf("payload hỏng: %+v", out)
	}
	if c.Kind().Name != live.AlertCommandName || !c.AutoRetryNotSent() {
		t.Errorf("Kind %+v", c.Kind())
	}
}

// Page của kênh không đọc được comment live (kênh giả): lệnh đối chiếu từ chối (rejected no_live_source), không treo.
func TestReconcileRejectsChannelWithoutLiveSource(t *testing.T) {
	t.Parallel()
	e := newLiveEnv(t)
	s := e.observe("lv-fake", live.StatusLive, live.DetectedManual, time.Now())
	e.observe("lv-fake", live.StatusEnded, live.DetectedManual, time.Now())
	body, _ := json.Marshal(map[string]any{"sessionId": s.ID})
	out, err := live.NewReconcileCommand(e.d).Execute(t.Context(), queue.Job{ID: uuid.New(), TenantID: &e.tenant, Payload: body})
	if err != nil || out.Status != commands.StatusRejected || out.Reason != "no_live_source" {
		t.Errorf("kênh không đọc được live: %+v %v", out, err)
	}
}
