package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Mặc định của PGHub (kế hoạch M0 Task 16).
const (
	defaultMinBackoff  = 500 * time.Millisecond
	defaultMaxBackoff  = 5 * time.Second
	defaultCheckEvery  = 15 * time.Second
	defaultPingTimeout = 5 * time.Second
	closeTimeout       = 5 * time.Second
)

// Tên chỉ số.
const (
	metricListening  = "rt_listening"
	metricClients    = "sse_connections"
	metricBadPayload = "rt_bad_payload"
	metricReconnect  = "rt_reconnect"
)

// PGHubOptions cấu hình PGHub.
type PGHubOptions struct {
	// URL là chuỗi kết nối của kết nối LISTEN (role crm_app), đi thẳng tới PostgreSQL, không qua bộ gom kết nối.
	URL     string
	Metrics *metrics.Registry // nil: không ghi chỉ số
	Logger  *slog.Logger      // nil: slog.Default
	// MinBackoff, MaxBackoff: lùi khi nối lại, gấp đôi mỗi lần hỏng (mặc định 500 ms → 5 s).
	MinBackoff, MaxBackoff time.Duration
	// CheckEvery: không có thông báo nào trong khoảng này thì Ping để phát hiện kết nối chết im lặng (mặc định 15 s).
	CheckEvery time.Duration
	// PingTimeout: hạn của Ping đó; quá hạn coi như kết nối rớt (mặc định 5 s).
	PingTimeout time.Duration
}

// PGHub hiện thực Bus bằng LISTEN/NOTIFY: Publish gọi pg_notify('rt', json) trong tx của bên gọi; Run giữ một
// pgx.Conn riêng LISTEN rt, phát mỗi thông báo cho các client đúng tenant và có quyền. Kết nối rớt → listening =
// false, nối lại lùi dần; nối được → resync cho mọi client.
type PGHub struct {
	o         PGHubOptions
	listening atomic.Bool

	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

type subscriber struct {
	u    identity.AuthUser
	send func(Frame)
}

// NewPGHub dựng hub; gọi Run để bắt đầu nghe.
func NewPGHub(o PGHubOptions) *PGHub {
	if o.MinBackoff <= 0 {
		o.MinBackoff = defaultMinBackoff
	}
	if o.MaxBackoff < o.MinBackoff {
		o.MaxBackoff = max(defaultMaxBackoff, o.MinBackoff)
	}
	if o.CheckEvery <= 0 {
		o.CheckEvery = defaultCheckEvery
	}
	if o.PingTimeout <= 0 {
		o.PingTimeout = defaultPingTimeout
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &PGHub{o: o, subs: map[*subscriber]struct{}{}}
}

// Publish phát s bằng pg_notify trong tx: PostgreSQL chỉ giao khi tx commit.
func (h *PGHub) Publish(ctx context.Context, tx pgx.Tx, s Signal) error {
	payload, err := encodeSignal(s)
	if err != nil {
		return err
	}
	if err := store.New(tx).Notify(ctx, payload); err != nil {
		return fmt.Errorf("phát báo hiệu: %w", err)
	}
	return nil
}

// Subscribe đăng ký nhận khung cho u; send được gọi từ goroutine của hub nên không được chặn.
func (h *PGHub) Subscribe(u identity.AuthUser, send func(Frame)) (cancel func()) {
	s := &subscriber{u: u, send: send}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	h.clientsGauge()
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, s)
			h.mu.Unlock()
			h.clientsGauge()
		})
	}
}

// Status trả trạng thái kết nối LISTEN và số client.
func (h *PGHub) Status() (listening bool, clients int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.listening.Load(), len(h.subs)
}

// Run nghe tới khi ctx bị hủy: nối, LISTEN rt, phát thông báo; rớt thì nối lại lùi dần MinBackoff → MaxBackoff (về
// MinBackoff sau mỗi lần đã nghe được). Lỗi kết nối ghi log một lần mỗi lần rớt (đây là mép của tiến trình nền).
func (h *PGHub) Run(ctx context.Context) {
	backoff := h.o.MinBackoff
	for {
		listened, err := h.listen(ctx)
		h.setListening(false)
		if ctx.Err() != nil {
			return
		}
		if listened {
			backoff = h.o.MinBackoff
		}
		h.inc(metricReconnect)
		h.o.Logger.LogAttrs(ctx, slog.LevelWarn, "realtime: kết nối LISTEN rớt, nối lại",
			slog.String("error", err.Error()), slog.Duration("backoff", backoff))
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		backoff = min(backoff*2, h.o.MaxBackoff)
	}
}

// listen chạy một vòng đời kết nối: listened = đã LISTEN được. Trả khi kết nối hỏng (err) hoặc ctx bị hủy.
func (h *PGHub) listen(ctx context.Context) (listened bool, err error) {
	cfg, err := pgx.ParseConfig(h.o.URL)
	if err != nil {
		return false, fmt.Errorf("đọc chuỗi kết nối LISTEN: %w", err)
	}
	// Hết hạn chờ thông báo chỉ đặt hạn đọc trên socket (không gửi yêu cầu hủy, không đóng kết nối).
	cfg.BuildContextWatcherHandler = func(pc *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.DeadlineContextWatcherHandler{Conn: pc.Conn()}
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return false, fmt.Errorf("nối kết nối LISTEN: %w", err)
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()
		_ = conn.Close(cctx)
	}()
	if err := store.New(conn).Listen(ctx); err != nil {
		return false, fmt.Errorf("LISTEN rt: %w", err)
	}
	h.setListening(true)
	h.broadcast(resyncFrame())
	for {
		wctx, cancel := context.WithTimeout(ctx, h.o.CheckEvery)
		n, err := conn.WaitForNotification(wctx)
		cancel()
		switch {
		case ctx.Err() != nil:
			return true, nil
		case err == nil:
			h.dispatch(n.Payload)
		case errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err):
			// Ping có hạn: kết nối chết im lặng (không RST) không được giữ listening = true tới khi TCP tự bỏ.
			pctx, pcancel := context.WithTimeout(ctx, h.o.PingTimeout)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return true, fmt.Errorf("kiểm kết nối LISTEN: %w", err)
			}
			h.setListening(true) // ghi lại gauge trong mỗi phút của bảng chỉ số
		default:
			return true, fmt.Errorf("chờ thông báo: %w", err)
		}
	}
}

// dispatch đọc payload của NOTIFY và gửi khung changed cho client đúng tenant, có quyền Requires trên Scope.
func (h *PGHub) dispatch(payload string) {
	var w wireSignal
	if err := json.Unmarshal([]byte(payload), &w); err != nil {
		h.inc(metricBadPayload)
		return
	}
	var scope *identity.Scope
	if w.Scope != nil {
		scope = &identity.Scope{Type: w.Scope.Type, ID: w.Scope.ID}
	}
	data, err := json.Marshal(changedData{Topic: w.Topic, Entity: w.Entity, Scope: w.Scope})
	if err != nil {
		h.inc(metricBadPayload)
		return
	}
	f := Frame{Event: EventChanged, Data: data}
	req := identity.Permission(w.Requires)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs {
		if s.u.TenantID == w.TenantID && identity.Can(s.u, req, scope) {
			s.send(f)
		}
	}
}

// broadcast gửi f cho mọi client.
func (h *PGHub) broadcast(f Frame) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs {
		s.send(f)
	}
}

func (h *PGHub) setListening(v bool) {
	h.listening.Store(v)
	if h.o.Metrics == nil {
		return
	}
	g := 0.0
	if v {
		g = 1
	}
	// Chỉ ghi rt_listening ở đây (cùng lúc đổi trạng thái), để lời gọi đồng thời khác không ghi đè giá trị cũ.
	h.o.Metrics.Gauge(metricListening, g, "")
	h.clientsGauge()
}

// clientsGauge ghi sse_connections.
func (h *PGHub) clientsGauge() {
	if h.o.Metrics == nil {
		return
	}
	_, clients := h.Status()
	h.o.Metrics.Gauge(metricClients, float64(clients), "")
}

func (h *PGHub) inc(name string) {
	if h.o.Metrics != nil {
		h.o.Metrics.Inc(name, "", 1)
	}
}
