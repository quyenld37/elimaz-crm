package live

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Nguồn comment live của kênh có channel.LiveSource (Facebook, 02 mục 2): luồng SSE làm chính, hỏi định kỳ làm dự
// phòng tự động. Chế độ source_mode = auto: mở luồng; rớt hay lỗi → nối lại sau 1 s, 2 s, 5 s, mỗi lần mở (được hay
// không) đều hỏi định kỳ một lượt từ mốc − Overlap để lấp chỗ hở; 3 lần lỗi trong 60 s, hoặc luồng im SilenceAfter mà
// hỏi định kỳ thấy comment mới → chuyển sang hỏi định kỳ mỗi PollEvery (active_source = poll, một cảnh báo thường
// live-source:<phiên>), cứ RetrySSEEvery thử lại luồng. source_mode = sse | poll ép một đường.
const (
	PollEvery     = 2 * time.Second
	SilenceAfter  = 30 * time.Second
	RetrySSEEvery = 5 * time.Minute
	// FailWindow, failLimit: số lần luồng lỗi trong cửa sổ thì chuyển sang hỏi định kỳ.
	FailWindow = 60 * time.Second
	failLimit  = 3
	// maxPollFails: số lần hỏi định kỳ lỗi liền thì nguồn dừng (bộ quản lý nhả khóa, quét lại và đọc lại token).
	maxPollFails = 30

	// Nguồn đang dùng (cột active_source) và chế độ ép (cột source_mode).
	SourceSSE  = "sse"
	SourcePoll = "poll"
	modeAuto   = "auto"

	// metricSourceFailed đếm lỗi của nguồn (nhãn: sse_open, sse_read, poll, active_source).
	metricSourceFailed = "live_source_failed"
)

// ReconnectDelays là các khoảng chờ trước lần nối lại luồng thứ 1, 2, 3 trở đi.
func ReconnectDelays() []time.Duration {
	return []time.Duration{time.Second, 2 * time.Second, 5 * time.Second}
}

// errNoLiveSource là lỗi: kênh của Page không đọc được comment live.
var errNoLiveSource = errors.New("live: kênh của Page không đọc được comment live")

// SourceOptions là cấu hình nguồn; giá trị 0 dùng hằng của gói. Test thu nhỏ các khoảng thời gian.
type SourceOptions struct {
	PollEvery, SilenceAfter, RetrySSEEvery, FailWindow, Overlap time.Duration
	ReconnectDelays                                             []time.Duration
	Logger                                                      *slog.Logger
}

// FBSource là CommentSource của kênh có channel.LiveSource.
type FBSource struct {
	d deps.Deps
	o SourceOptions
}

// NewSource dựng nguồn comment live đọc qua adapter của d.Registry.
func NewSource(d deps.Deps, o SourceOptions) *FBSource {
	o.PollEvery = orDefault(o.PollEvery, PollEvery)
	o.SilenceAfter = orDefault(o.SilenceAfter, SilenceAfter)
	o.RetrySSEEvery = orDefault(o.RetrySSEEvery, RetrySSEEvery)
	o.FailWindow = orDefault(o.FailWindow, FailWindow)
	o.Overlap = orDefault(o.Overlap, Overlap)
	if len(o.ReconnectDelays) == 0 {
		o.ReconnectDelays = ReconnectDelays()
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &FBSource{d: d, o: o}
}

// sourceTarget là Page, token và adapter của một phiên (đọc một lần mỗi Run).
type sourceTarget struct {
	acc  channel.ChannelAccount
	cred channel.Credentials
	src  channel.LiveSource
}

// Run đọc comment của s từ since tới khi ctx bị hủy (trả nil) hay emit lỗi (trả lỗi đó). Page, token đọc trong một
// transaction ngắn; mọi lời gọi kênh ở ngoài transaction.
func (f *FBSource) Run(ctx context.Context, s Session, since time.Time, emit func([]webhooks.InboundEvent, string) error) error {
	t, err := readTarget(ctx, f.d, s)
	if err != nil {
		return err
	}
	r := &reader{f: f, s: s, t: t, emit: emit, since: since, mode: s.SourceMode, active: s.ActiveSource}
	if r.mode != SourceSSE && r.mode != SourcePoll {
		r.mode = modeAuto
	}
	for ctx.Err() == nil {
		if r.mode == SourcePoll || (r.mode == modeAuto && r.degraded) {
			err = r.pollPhase(ctx)
		} else {
			err = r.ssePhase(ctx)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// readTarget đọc Page, token và adapter đọc comment live của phiên s trong một transaction ngắn; kênh của Page không
// đọc được live → errNoLiveSource.
func readTarget(ctx context.Context, d deps.Deps, s Session) (sourceTarget, error) {
	var t sourceTarget
	err := tenancy.WithTenant(ctx, d.DB, s.TenantID, func(tx pgx.Tx) error {
		t = sourceTarget{}
		acc, err := channel.Account(ctx, tx, s.AccountID)
		if err != nil {
			return err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
		src, ok := deps.AdapterAs[channel.LiveSource](d.Registry, acc.Channel)
		if !ok {
			return errNoLiveSource
		}
		cred, err := channel.CredentialsFor(ctx, tx, d.Keys, acc.ID)
		if err != nil {
			return err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
		t = sourceTarget{acc: acc, cred: cred, src: src}
		return nil
	})
	if err != nil {
		return sourceTarget{}, fmt.Errorf("đọc Page của phiên live: %w", err)
	}
	return t, nil
}

func (f *FBSource) failed(label string) {
	if f.d.Metrics != nil {
		f.d.Metrics.Inc(metricSourceFailed, label, 1)
	}
}

// reader là trạng thái đọc của một lần Run (một goroutine, không cần khóa).
type reader struct {
	f    *FBSource
	s    Session
	t    sourceTarget
	emit func([]webhooks.InboundEvent, string) error
	// since là mốc hỏi định kỳ: comment mới nhất đã thấy − Overlap (ban đầu là since của bộ quản lý).
	since time.Time
	// newest là giờ comment mới nhất đã thấy qua mọi đường.
	newest   time.Time
	lastPoll time.Time
	// mode: auto | sse | poll; degraded: auto đang hỏi định kỳ (từ degradedAt); retrying: đang thử lại luồng sau
	// RetrySSEEvery (mở lỗi thì quay lại hỏi định kỳ ngay).
	mode                string
	degraded, retrying  bool
	degradedAt          time.Time
	fails               []time.Time
	pollFails           int
	active              string
	alerted, activeWarn bool
}

// ssePhase mở luồng, lấp chỗ hở bằng một lượt hỏi định kỳ, rồi đọc tới khi luồng hỏng, im quá lâu hay ctx bị hủy.
// Chỉ trả lỗi của emit.
func (r *reader) ssePhase(ctx context.Context) error {
	st, openErr := r.t.src.StreamComments(ctx, r.t.acc, r.t.cred, r.s.LiveVideoID, r.since)
	if openErr == nil {
		defer func() { _ = st.Close() }()
	}
	if _, err := r.pollOnce(ctx); err != nil || ctx.Err() != nil {
		return err
	}
	if openErr != nil {
		r.f.failed("sse_open")
		r.streamFailed(ctx)
		return nil
	}
	r.retrying = false
	r.setActive(ctx, SourceSSE, false)
	for {
		nctx, cancel := context.WithTimeout(ctx, r.f.o.SilenceAfter)
		evs, err := st.Next(nctx)
		silent := nctx.Err() != nil
		cancel()
		switch {
		case ctx.Err() != nil:
			return nil
		case err == nil:
			r.fails = nil
			if err := r.send(evs, webhooks.ViaLiveStream); err != nil {
				return err
			}
		case silent:
			found, err := r.pollOnce(ctx)
			if err != nil {
				return err
			}
			if found && r.mode == modeAuto {
				r.degrade(ctx) // luồng im mà Meta có comment mới: luồng đã hỏng
				return nil
			}
		default:
			r.f.failed("sse_read")
			r.streamFailed(ctx)
			return nil
		}
	}
}

// streamFailed ghi một lần luồng lỗi: auto mà đủ failLimit lần trong FailWindow (hay đang thử lại sau khi đã chuyển)
// → chuyển sang hỏi định kỳ; không thì chờ khoảng nối lại.
func (r *reader) streamFailed(ctx context.Context) {
	now := time.Now()
	r.fails = slices.DeleteFunc(r.fails, func(t time.Time) bool { return now.Sub(t) > r.f.o.FailWindow })
	r.fails = append(r.fails, now)
	if r.mode == modeAuto && (r.retrying || len(r.fails) >= failLimit) {
		r.degrade(ctx)
		return
	}
	delays := r.f.o.ReconnectDelays
	sleep(ctx, delays[min(len(r.fails), len(delays))-1])
}

// degrade chuyển phiên sang hỏi định kỳ: active_source = poll, cảnh báo live-source một lần mỗi phiên.
func (r *reader) degrade(ctx context.Context) {
	r.degraded, r.retrying, r.degradedAt, r.fails = true, false, time.Now(), nil
	r.setActive(ctx, SourcePoll, true)
}

// pollPhase hỏi định kỳ mỗi PollEvery tới khi ctx bị hủy; auto thì sau RetrySSEEvery quay lại thử luồng.
func (r *reader) pollPhase(ctx context.Context) error {
	r.setActive(ctx, SourcePoll, false)
	for ctx.Err() == nil {
		if r.mode == modeAuto && time.Since(r.degradedAt) >= r.f.o.RetrySSEEvery {
			r.degraded, r.retrying = false, true
			return nil
		}
		if _, err := r.pollOnce(ctx); err != nil {
			return err
		}
	}
	return nil
}

// pollOnce chờ tới lượt (cách lượt trước ≥ PollEvery) rồi hỏi kênh comment từ since và ghi; found = có comment mới hơn
// mọi comment đã thấy. Kênh lỗi → không ghi gì, thử lượt sau (maxPollFails lần liền → trả lỗi để bộ quản lý đọc lại
// token). Chỉ trả lỗi của emit hay lỗi kênh kéo dài.
func (r *reader) pollOnce(ctx context.Context) (found bool, err error) {
	if wait := time.Until(r.lastPoll.Add(r.f.o.PollEvery)); wait > 0 && !sleep(ctx, wait) {
		return false, nil
	}
	r.lastPoll = time.Now()
	evs, err := r.t.src.PollComments(ctx, r.t.acc, r.t.cred, r.s.LiveVideoID, r.since)
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		r.f.failed("poll")
		if r.pollFails++; r.pollFails >= maxPollFails {
			return false, fmt.Errorf("hỏi định kỳ comment live lỗi %d lần liền: %w", r.pollFails, err)
		}
		return false, nil
	}
	r.pollFails = 0
	before := r.newest
	if err := r.send(evs, webhooks.ViaLivePoll); err != nil {
		return false, err
	}
	return r.newest.After(before), nil
}

// send ghi lô evs qua emit rồi đẩy mốc comment mới nhất và mốc hỏi định kỳ.
func (r *reader) send(evs []webhooks.InboundEvent, via string) error {
	if len(evs) == 0 {
		return nil
	}
	if err := r.emit(evs, via); err != nil {
		return err
	}
	for _, e := range evs {
		if e.OccurredAt.After(r.newest) {
			r.newest = e.OccurredAt
		}
	}
	if s := r.newest.Add(-r.f.o.Overlap); s.After(r.since) {
		r.since = s
	}
	return nil
}

// setActive ghi nguồn đang dùng src của phiên (khi đổi) và, khi alert, lệnh cảnh báo live-source (một lần mỗi phiên)
// trong một transaction ngắn. Lỗi chỉ đếm (lần đổi sau ghi lại); không ghi được không dừng việc đọc.
func (r *reader) setActive(ctx context.Context, src string, alert bool) {
	alert = alert && !r.alerted
	if r.active == src && !alert {
		return
	}
	err := tenancy.WithTenant(ctx, r.f.d.DB, r.s.TenantID, func(tx pgx.Tx) error {
		if _, err := store.New(tx).SetLiveActiveSource(ctx, store.SetLiveActiveSourceParams{ActiveSource: src, ID: r.s.ID}); err != nil {
			return fmt.Errorf("ghi nguồn comment live: %w", err)
		}
		if alert {
			return enqueueAlert(ctx, tx, r.f.d.Queue, r.s.TenantID, alertPayload{Kind: alertSource, SessionID: r.s.ID},
				"live-source:"+r.s.ID.String())
		}
		return nil
	})
	if err != nil {
		r.f.failed("active_source")
		if !r.activeWarn && ctx.Err() == nil {
			r.activeWarn = true
			r.f.o.Logger.Warn("ghi nguồn comment live", slog.String("session", r.s.ID.String()), logging.Err(err))
		}
		return
	}
	r.active = src
	if alert {
		r.alerted = true
	}
}

// sleep chờ d hay tới khi ctx bị hủy; trả false khi ctx bị hủy.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// anyAdapter: có adapter nào trong r hiện thực T không.
func anyAdapter[T any](r *channel.Registry) bool {
	return r != nil && slices.ContainsFunc(r.Keys(), func(k string) bool {
		_, ok := deps.AdapterAs[T](r, k)
		return ok
	})
}
