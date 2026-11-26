package live

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Chỉ số của bộ quản lý luồng.
const (
	metricReaders   = "live_readers"    // số luồng đang đọc trong tiến trình (nhãn: chủ khóa)
	metricLeaseLost = "live_lease_lost" // luồng dừng vì mất khóa hay quá hạn chưa gia hạn được
	// releaseTimeout là thời gian tối đa nhả khóa lúc luồng dừng (kể cả khi tắt êm, ctx của worker đã hủy).
	releaseTimeout = 5 * time.Second
)

// CommentSource đọc comment của phiên s từ since tới khi ctx bị hủy hay phiên hết (Task 171 hiện thực bằng Facebook;
// test dùng nguồn giả). Mỗi lô gọi emit(sự kiện, via) với via là webhooks.ViaLiveStream hay ViaLivePoll; emit trả lỗi
// (mất khóa, database) thì nguồn dừng và trả lỗi đó.
type CommentSource interface {
	Run(ctx context.Context, s Session, since time.Time, emit func([]webhooks.InboundEvent, string) error) error
}

// Options là cấu hình bộ quản lý; giá trị 0 dùng hằng của gói (Q3). Test thu nhỏ các khoảng thời gian.
type Options struct {
	// Owner là mã chủ khóa ($me); rỗng → "<máy>/<số ngẫu nhiên>" lúc dựng.
	Owner                                    string
	LeaseTTL, RenewEvery, ScanEvery, Overlap time.Duration
	Logger                                   *slog.Logger
}

// Manager là bộ quản lý luồng đọc comment live chạy trong worker (04 mục 6): mỗi ScanEvery nhận các phiên live hết
// khóa của mọi tenant, mỗi phiên một goroutine chạy CommentSource.Run, gia hạn khóa mỗi RenewEvery; mất khóa, phiên
// kết thúc hay quá hạn chưa gia hạn được thì dừng luồng đó.
type Manager struct {
	d       deps.Deps
	src     CommentSource
	o       Options
	mu      sync.Mutex
	active  map[uuid.UUID]context.CancelFunc
	wg      sync.WaitGroup
	crashed atomic.Bool
}

// NewManager dựng bộ quản lý đọc comment bằng src.
func NewManager(d deps.Deps, src CommentSource, o Options) *Manager {
	if o.Owner == "" {
		host, _ := os.Hostname()
		o.Owner = host + "/" + uuid.NewString()
	}
	o.LeaseTTL = orDefault(o.LeaseTTL, LeaseTTL)
	o.RenewEvery = orDefault(o.RenewEvery, RenewEvery)
	o.ScanEvery = orDefault(o.ScanEvery, ScanEvery)
	o.Overlap = orDefault(o.Overlap, Overlap)
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Manager{d: d, src: src, o: o, active: map[uuid.UUID]context.CancelFunc{}}
}

func orDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// Owner là mã chủ khóa của bộ quản lý.
func (m *Manager) Owner() string { return m.o.Owner }

// Run quét và mở luồng tới khi ctx bị hủy; lúc đó dừng mọi luồng, chờ chúng thoát và nhả khóa (tắt êm) rồi trả nil.
// Luồng đọc chạy trên ngữ cảnh tách khỏi ctx (để kịp nhả khóa sau khi ctx hủy) và bị hủy tường minh.
func (m *Manager) Run(ctx context.Context) error {
	base, cancelAll := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelAll()
	t := time.NewTicker(m.o.ScanEvery)
	defer t.Stop()
	for {
		m.scan(ctx, base)
		select {
		case <-ctx.Done():
			cancelAll()
			m.wg.Wait()
			return nil
		case <-t.C:
		}
	}
}

// scan nhận phiên hết khóa của từng tenant và mở luồng cho phiên chưa có luồng trong tiến trình này.
func (m *Manager) scan(ctx, base context.Context) {
	tenants, err := tenancy.ListTenantIDs(ctx, m.d.DB)
	if err != nil {
		if ctx.Err() == nil {
			m.o.Logger.Warn("quét phiên live: liệt kê tenant", logging.Err(err))
		}
		return
	}
	for _, tenant := range tenants {
		// Mốc trước transaction nhận: hạn cục bộ không muộn hơn hạn database ghi (now() của transaction + LeaseTTL).
		claimed := time.Now()
		got, err := m.claimTenant(ctx, tenant)
		if err != nil {
			if ctx.Err() == nil {
				m.o.Logger.Warn("quét phiên live: nhận phiên", logging.Err(err))
			}
			continue
		}
		for _, s := range got {
			m.start(base, s, claimed)
		}
	}
}

// claimTenant nhận phiên hết khóa của tenant trong một transaction ngắn, kèm khóa kênh của Page (nguồn sự kiện vào).
func (m *Manager) claimTenant(ctx context.Context, tenant uuid.UUID) ([]Session, error) {
	var got []Session
	err := tenancy.WithTenant(ctx, m.d.DB, tenant, func(tx pgx.Tx) error {
		got = nil
		ss, err := claim(ctx, tx, m.o.Owner, m.o.LeaseTTL)
		if err != nil {
			return err
		}
		for _, s := range ss {
			acc, err := channel.Account(ctx, tx, s.AccountID)
			if err != nil {
				return err //nolint:wrapcheck // channel đã bọc việc đang làm
			}
			s.Channel = acc.Channel
			got = append(got, s)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("nhận phiên live của tenant: %w", err)
	}
	return got, nil
}

// start mở luồng cho s (khóa nhận lúc claimed); phiên đã có luồng trong tiến trình thì giữ luồng cũ.
func (m *Manager) start(base context.Context, s Session, claimed time.Time) {
	m.mu.Lock()
	if _, ok := m.active[s.ID]; ok {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(base)
	m.active[s.ID] = cancel
	m.gaugeLocked()
	m.mu.Unlock()
	m.wg.Go(func() { m.read(ctx, cancel, s, claimed) })
}

func (m *Manager) gaugeLocked() {
	if m.d.Metrics != nil {
		m.d.Metrics.Gauge(metricReaders, float64(len(m.active)), m.o.Owner)
	}
}

func (m *Manager) leaseLost() {
	if m.d.Metrics != nil {
		m.d.Metrics.Inc(metricLeaseLost, m.o.Owner, 1)
	}
}

// read chạy nguồn cho s cùng vòng gia hạn; nguồn dừng (lỗi, mất khóa, phiên hết, tắt) thì nhả khóa, trừ khi tiến
// trình giả lập chết (test LV10).
func (m *Manager) read(ctx context.Context, cancel context.CancelFunc, s Session, claimed time.Time) {
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.active, s.ID)
		m.gaugeLocked()
		m.mu.Unlock()
	}()
	var keeper sync.WaitGroup
	keeper.Go(func() { m.keep(ctx, cancel, s, claimed) })
	since := s.StartedAt
	if s.LastCommentAt != nil {
		since = s.LastCommentAt.Add(-m.o.Overlap)
	}
	err := m.src.Run(ctx, s, since, m.emitter(ctx, cancel, s))
	cancel()
	keeper.Wait()
	if err != nil && ctx.Err() == nil && !errors.Is(err, ErrLeaseLost) {
		m.o.Logger.Warn("luồng comment live dừng", slog.String("session", s.ID.String()), logging.Err(err))
	}
	if m.crashed.Load() {
		return
	}
	rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer rcancel()
	if err := tenancy.WithTenant(rctx, m.d.DB, s.TenantID, func(tx pgx.Tx) error {
		return release(rctx, tx, s.ID, m.o.Owner)
	}); err != nil {
		m.o.Logger.Warn("nhả khóa phiên live", slog.String("session", s.ID.String()), logging.Err(err))
	}
}

// keep gia hạn khóa của s mỗi RenewEvery tới khi ctx bị hủy. Mất khóa hay phiên kết thúc → hủy luồng. Hạn cục bộ
// (LeaseTTL − RenewEvery/2 tính từ lúc bắt đầu lần gia hạn thành công gần nhất, sớm hơn hạn database ghi, chừa nửa
// RenewEvery cho luồng kịp thoát) do một bộ hẹn giờ độc lập giữ: tới hạn là hủy luồng kể cả khi lần gia hạn đang kẹt
// chờ database, để không lúc nào hai worker cùng đọc một phiên.
func (m *Manager) keep(ctx context.Context, cancel context.CancelFunc, s Session, claimed time.Time) {
	margin := m.o.LeaseTTL - m.o.RenewEvery/2
	expire := time.AfterFunc(time.Until(claimed.Add(margin)), func() {
		m.leaseLost()
		cancel()
	})
	defer expire.Stop()
	t := time.NewTicker(m.o.RenewEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		start := time.Now()
		var status string
		var held bool
		err := tenancy.WithTenant(ctx, m.d.DB, s.TenantID, func(tx pgx.Tx) error {
			var err error
			status, held, err = renew(ctx, tx, s.ID, m.o.Owner, m.o.LeaseTTL)
			return err
		})
		switch {
		case err != nil:
			continue // lỗi tạm: thử lại kỳ sau; quá hạn thì bộ hẹn giờ dừng luồng
		case !held:
			m.leaseLost()
			cancel()
			return
		case status != StatusLive:
			cancel()
			return
		}
		if !expire.Stop() {
			return // bộ hẹn giờ đã bắn (đã hủy luồng) trước khi lần gia hạn này về
		}
		expire.Reset(time.Until(start.Add(margin)))
	}
}

// emitter trả hàm ghi một lô comment của s trong một transaction ngắn: mỗi sự kiện một việc vào (trùng thì bỏ), rồi
// mốc comment mới nhất và số việc mới, chỉ khi còn giữ khóa (mất khóa → lùi cả lô, dừng luồng).
func (m *Manager) emitter(ctx context.Context, cancel context.CancelFunc, s Session) func([]webhooks.InboundEvent, string) error {
	return func(evs []webhooks.InboundEvent, via string) error {
		if len(evs) == 0 {
			return nil
		}
		received := clock(m.d)
		err := tenancy.WithTenant(ctx, m.d.DB, s.TenantID, func(tx pgx.Tx) error {
			return m.writeBatch(ctx, tx, s, evs, via, received)
		})
		if errors.Is(err, ErrLeaseLost) {
			m.leaseLost()
			cancel()
		}
		return err //nolint:wrapcheck // lỗi của nền tảng, live đã bọc việc đang làm
	}
}

// writeBatch ghi lô evs của s trong tx: mỗi sự kiện một việc vào (trùng thì bỏ, không đếm), rồi mốc comment mới nhất
// và số việc mới; không còn giữ khóa → ErrLeaseLost.
func (m *Manager) writeBatch(ctx context.Context, tx pgx.Tx, s Session, evs []webhooks.InboundEvent, via string,
	received time.Time,
) error {
	n := 0
	var last *time.Time
	for _, e := range evs {
		created, err := webhooks.EnqueueInbound(ctx, tx, m.d.Queue, s.TenantID, s.Channel, e, received, via)
		if err != nil {
			return err //nolint:wrapcheck // webhooks đã bọc việc đang làm
		}
		if created {
			n++
		}
		if at := e.OccurredAt; !at.IsZero() && (last == nil || at.After(*last)) {
			last = &at
		}
	}
	return record(ctx, tx, s.ID, m.o.Owner, last, n)
}

// clock là đồng hồ của d (nil → giờ thật), UTC.
func clock(d deps.Deps) time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}
