package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Mặc định của WorkerOptions (spec 04 mục 5.1; Global Constraints).
const (
	defaultConcurrency = 8
	defaultLease       = 90 * time.Second
	defaultPoll        = 2 * time.Second
)

const (
	// recordTimeout giới hạn các lần ghi trạng thái sau khi handler xong (chạy trên ctx tách khỏi việc hủy).
	recordTimeout = 5 * time.Second
	// sweepLimit: số việc tối đa mỗi lần quét giữ chỗ hết hạn và việc quá hạn sống.
	sweepLimit = 100
	// maxErrorLen: last_error cắt tối đa chừng này ký tự.
	maxErrorLen = 1000
	// killWait: sau khi hủy ctx của handler (hết grace), Stop chờ thêm tối đa chừng này để việc dở ghi về hàng chờ.
	killWait = 5 * time.Second
)

// Lỗi của Worker.
var (
	// ErrAlreadyStarted: Start gọi lần hai.
	ErrAlreadyStarted = errors.New("queue: worker đã chạy")
	// ErrStopGrace: Stop phải hủy việc đang chạy vì quá grace.
	ErrStopGrace = errors.New("queue: hết thời gian tắt êm, đã hủy việc đang chạy")
	// errLostLease: lệnh đổi trạng thái có điều kiện chạm 0 dòng (worker khác đã nhận lại, hoặc việc đã đổi trạng
	// thái); transaction lùi, không ghi gì.
	errLostLease = errors.New("queue: mất giữ chỗ")
)

// WorkerOptions cấu hình Worker; trường bằng 0 lấy mặc định.
type WorkerOptions struct {
	// WorkerID ghi vào locked_by; mỗi tiến trình một mã (rỗng: "worker-<uuid>").
	WorkerID string
	// Concurrency: số việc chạy cùng lúc (8).
	Concurrency int
	// Lease: thời gian giữ chỗ của một lần nhận (90 s), lớn hơn Policy.Timeout.
	Lease time.Duration
	// Poll: chu kỳ hỏi định kỳ, lưới an toàn bên cạnh NOTIFY (2 s).
	Poll time.Duration
	// Metrics nil: không ghi chỉ số.
	Metrics *metrics.Registry
	// Logger cho lỗi của vòng chạy nền (Start); nil: slog.Default().
	Logger *slog.Logger
}

// Worker nhận và chạy việc của các Handler đã đăng ký.
type Worker struct {
	d        *db.DB
	handlers map[string]Handler
	policies map[string]Policy
	o        WorkerOptions
	tracer   trace.Tracer
	// recordTimeout: hạn của mỗi lần ghi trạng thái sau handler, tính từ lúc bắt đầu ghi (test đổi được).
	recordTimeout time.Duration

	inflight   sync.WaitGroup
	busy       atomic.Int64
	wake       chan struct{}
	hardStop   context.Context //nolint:containedctx // hủy ctx của mọi handler khi Stop quá grace
	hardStopFn context.CancelFunc

	mu       sync.Mutex
	started  bool
	stopLoop context.CancelFunc
	loopDone chan struct{}
}

// NewWorker tạo Worker cho hs (mỗi Kind().Name một handler; trùng tên thì cái sau thắng). Policy của handler lấy
// mặc định như Enqueue; Policy không hợp lệ thì dùng DefaultPolicy cho Timeout và MaxAttempts.
func NewWorker(d *db.DB, hs []Handler, o WorkerOptions) *Worker {
	if o.WorkerID == "" {
		o.WorkerID = "worker-" + uuid.NewString()
	}
	if o.Concurrency <= 0 {
		o.Concurrency = defaultConcurrency
	}
	if o.Lease <= 0 {
		o.Lease = defaultLease
	}
	if o.Poll <= 0 {
		o.Poll = defaultPoll
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	w := &Worker{
		d: d, o: o, tracer: newTracer(),
		handlers: make(map[string]Handler, len(hs)), policies: make(map[string]Policy, len(hs)),
		wake: make(chan struct{}, 1), recordTimeout: recordTimeout,
	}
	w.hardStop, w.hardStopFn = context.WithCancel(context.Background())
	for _, h := range hs {
		k := h.Kind()
		p, err := k.Policy.normalized()
		if err != nil {
			p = DefaultPolicy
		}
		w.handlers[k.Name], w.policies[k.Name] = h, p
	}
	return w
}

// RunOnce chạy một lượt, theo thứ tự: (1) giữ chỗ hết hạn: retry → về hàng chờ (hết lượt thì dead), in_doubt →
// in_doubt; (2) việc pending quá hạn sống → expired; (3) nhận tối đa limit việc tới hạn; (4) chạy chúng (tối đa
// Concurrency cùng lúc) và chờ xong. Trả số việc đã nhận. Lỗi của từng việc được gộp lại; việc khác vẫn chạy.
func (w *Worker) RunOnce(ctx context.Context, limit int) (int, error) {
	if err := w.sweep(ctx); err != nil {
		return 0, err
	}
	jobs, err := w.claim(ctx, limit)
	if err != nil {
		return 0, err
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	sem := make(chan struct{}, w.o.Concurrency)
	for _, row := range jobs {
		sem <- struct{}{}
		w.inflight.Add(1)
		wg.Go(func() {
			defer func() { <-sem; w.inflight.Done() }()
			if err := w.runJob(ctx, row); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	w.gauge(ctx)
	return len(jobs), errors.Join(errs...)
}

// Start nghe LISTEN jobs trên một kết nối riêng của pool rồi chạy vòng nhận việc nền: mỗi Poll, mỗi NOTIFY và mỗi
// khi một việc xong. Trả ngay; lỗi chỉ khi đã chạy hoặc không mở được kết nối LISTEN. Dừng bằng Stop.
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return ErrAlreadyStarted
	}
	conn, err := w.listenConn(ctx)
	if err != nil {
		return err
	}
	loopCtx, cancel := context.WithCancel(ctx)
	w.started, w.stopLoop, w.loopDone = true, cancel, make(chan struct{})
	base := context.WithoutCancel(ctx)
	var bg sync.WaitGroup
	bg.Go(func() { w.listen(loopCtx, conn) })
	bg.Go(func() { w.loop(loopCtx, base) })
	go func() { bg.Wait(); close(w.loopDone) }()
	return nil
}

// Stop ngừng nhận việc, chờ việc đang chạy tối đa grace; quá grace thì hủy ctx của chúng (việc transactional chưa
// xong về pending, trả lại lần thử) và trả ErrStopGrace. Sau Stop, Worker không dùng lại được.
func (w *Worker) Stop(grace time.Duration) error {
	w.mu.Lock()
	if w.started {
		w.stopLoop()
		<-w.loopDone
	}
	w.mu.Unlock()
	done := make(chan struct{})
	go func() { w.inflight.Wait(); close(done) }()
	t := time.NewTimer(grace)
	defer t.Stop()
	select {
	case <-done:
		w.hardStopFn()
		return nil
	case <-t.C:
	}
	w.hardStopFn()
	select {
	case <-done:
	case <-time.After(killWait):
	}
	return ErrStopGrace
}

// listenConn lấy một kết nối của pool và LISTEN jobs trên đó.
func (w *Worker) listenConn(ctx context.Context) (*pgxpool.Conn, error) {
	conn, err := w.d.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("lấy kết nối LISTEN jobs: %w", err)
	}
	if err := store.New(conn).ListenJobs(ctx); err != nil {
		conn.Release()
		return nil, fmt.Errorf("LISTEN jobs: %w", err)
	}
	return conn, nil
}

// listen đánh thức vòng nhận việc mỗi khi có NOTIFY. Kết nối hỏng thì mở lại sau Poll (trong lúc đó vẫn hỏi định
// kỳ). Khi dừng: đóng kết nối (bỏ LISTEN) rồi trả về pool, pool tự bỏ kết nối đã đóng.
func (w *Worker) listen(ctx context.Context, conn *pgxpool.Conn) {
	defer func() {
		if conn != nil {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
			_ = conn.Conn().Close(cctx)
			cancel()
			conn.Release()
		}
	}()
	for {
		if conn == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.o.Poll):
			}
			c, err := w.listenConn(ctx)
			if err != nil {
				if ctx.Err() == nil {
					w.o.Logger.Warn("mở lại kết nối LISTEN jobs", slog.String("error", err.Error()))
				}
				continue
			}
			conn = c
			w.signal() // có thể đã lỡ NOTIFY lúc mất kết nối
		}
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			w.o.Logger.Warn("kết nối LISTEN jobs rớt", slog.String("error", err.Error()))
			_ = conn.Conn().Close(ctx)
			conn.Release()
			conn = nil
			continue
		}
		w.signal()
	}
}

func (w *Worker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// loop nhận việc tới khi ctx hủy; base là ctx của các handler (không hủy theo loop, chỉ theo kill).
func (w *Worker) loop(ctx, base context.Context) {
	ticker := time.NewTicker(w.o.Poll)
	defer ticker.Stop()
	for {
		w.dispatch(ctx, base)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

// dispatch quét, nhận tối đa số chỗ còn trống và chạy mỗi việc trên một goroutine riêng (không chờ).
func (w *Worker) dispatch(ctx, base context.Context) {
	free := w.o.Concurrency - int(w.busy.Load())
	if free <= 0 || ctx.Err() != nil {
		return
	}
	if err := w.sweep(ctx); err != nil {
		w.logErr(ctx, "quét giữ chỗ hết hạn", err)
	}
	jobs, err := w.claim(ctx, free)
	if err != nil {
		w.logErr(ctx, "nhận việc", err)
		return
	}
	for _, row := range jobs {
		w.busy.Add(1)
		w.inflight.Add(1)
		go func() {
			defer func() { w.busy.Add(-1); w.inflight.Done(); w.signal() }()
			if err := w.runJob(base, row); err != nil {
				w.o.Logger.Error("chạy việc", slog.String("job_id", row.ID.String()), slog.String("kind", row.Kind),
					slog.String("error", err.Error()))
			}
		}()
	}
	w.gauge(ctx)
}

func (w *Worker) logErr(ctx context.Context, msg string, err error) {
	if ctx.Err() == nil {
		w.o.Logger.Error(msg, slog.String("error", err.Error()))
	}
}

// sweep: (1) giữ chỗ hết hạn, (2) việc pending quá hạn sống.
func (w *Worker) sweep(ctx context.Context) error {
	err := w.d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		_, err := store.New(tx).RequeueLeaseExpired(ctx)
		return err //nolint:wrapcheck // bọc một lần ở dưới
	})
	if err != nil {
		return fmt.Errorf("trả việc hết giữ chỗ về hàng chờ: %w", err)
	}
	q := store.New(w.d.Pool)
	stale, err := q.ListLeaseExpiredTerminal(ctx, sweepLimit)
	if err != nil {
		return fmt.Errorf("đọc việc hết giữ chỗ: %w", err)
	}
	var errs []error
	for _, r := range stale {
		j := Job{ID: r.ID, TenantID: fromPgUUID(r.TenantID), Kind: r.Kind, Payload: r.Payload, Attempts: int(r.Attempts), CreatedAt: r.CreatedAt}
		status := StatusDead
		if r.OnLeaseExpired == LeaseInDoubt {
			status = StatusInDoubt
		}
		errs = append(errs, w.finish(ctx, j, status, func(q *store.Queries) (int64, error) {
			return q.FinishLeaseExpired(ctx, store.FinishLeaseExpiredParams{
				Status: status, LastError: "lease expired", ID: r.ID, LockedBy: r.LockedBy.String,
			})
		}))
	}
	expired, err := q.ListExpiredPending(ctx, sweepLimit)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("đọc việc quá hạn sống: %w", err))...)
	}
	for _, r := range expired {
		j := Job{ID: r.ID, TenantID: fromPgUUID(r.TenantID), Kind: r.Kind, Payload: r.Payload, Attempts: int(r.Attempts), CreatedAt: r.CreatedAt}
		errs = append(errs, w.finish(ctx, j, StatusExpired, func(q *store.Queries) (int64, error) {
			return q.ExpirePending(ctx, r.ID)
		}))
	}
	return errors.Join(errs...)
}

// claim nhận tối đa limit việc (đặt running, attempts + 1, giữ chỗ) trong một transaction ngắn đã commit.
func (w *Worker) claim(ctx context.Context, limit int) ([]store.ClaimJobsRow, error) {
	if limit <= 0 {
		return nil, nil
	}
	var rows []store.ClaimJobsRow
	err := w.d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		var err error
		rows, err = store.New(tx).ClaimJobs(ctx, store.ClaimJobsParams{
			LeaseMs: w.o.Lease.Milliseconds(), LockedBy: w.o.WorkerID, Lim: int32(min(limit, maxInt32)), //nolint:gosec // đã chặn trên
		})
		return err //nolint:wrapcheck // bọc một lần ở dưới
	})
	if err != nil {
		return nil, fmt.Errorf("nhận việc: %w", err)
	}
	if w.o.Metrics != nil {
		for _, r := range rows {
			w.o.Metrics.Observe("job_pickup_ms", r.PickupMs, r.Kind)
		}
	}
	return rows, nil
}

// runJob chạy một việc đã nhận (bước 4, 5 của RunOnce) và ghi kết quả.
func (w *Worker) runJob(base context.Context, row store.ClaimJobsRow) error {
	j := Job{
		ID: row.ID, TenantID: fromPgUUID(row.TenantID), Kind: row.Kind, Payload: row.Payload,
		Attempts: int(row.Attempts), CreatedAt: row.CreatedAt,
	}
	ctx, span := startJobSpan(base, w.tracer, row.TraceContext, j)
	defer span.End()

	h, ok := w.handlers[j.Kind]
	if !ok {
		span.SetStatus(codes.Error, "no handler")
		rctx, rcancel := w.recordCtx(ctx)
		defer rcancel()
		return w.markDead(rctx, j, "no handler", false)
	}
	p := w.policies[j.Kind]
	hctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	stopKill := context.AfterFunc(w.hardStop, cancel) //nolint:contextcheck // hardStop chỉ để hủy hctx, không làm cha
	defer stopKill()

	start := time.Now()
	err := w.inTx(hctx, j.TenantID, func(tx pgx.Tx) error {
		res, err := h.Handle(hctx, j, tx)
		if err != nil {
			return fmt.Errorf("handle %s: %w", j.Kind, err)
		}
		n, err := store.New(tx).CompleteJob(hctx, store.CompleteJobParams{Result: res, ID: j.ID, LockedBy: w.o.WorkerID})
		if err != nil {
			return fmt.Errorf("ghi done: %w", err)
		}
		if n == 0 {
			return errLostLease
		}
		return nil
	})
	if w.o.Metrics != nil {
		w.o.Metrics.Observe("job_ms", float64(time.Since(start).Milliseconds()), j.Kind)
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errLostLease):
		span.AddEvent("mất giữ chỗ, đã lùi")
		return nil
	}
	span.SetStatus(codes.Error, "handler lỗi")
	// Hạn ghi tính từ đây (sau handler), trên ctx tách khỏi việc hủy của Stop và của Timeout.
	rctx, rcancel := w.recordCtx(ctx)
	defer rcancel()
	switch {
	case w.hardStop.Err() != nil: // Stop quá grace: trả về hàng chờ, không tính lần thử này
		return w.update(rctx, func(q *store.Queries) (int64, error) {
			return q.ReleaseJob(rctx, store.ReleaseJobParams{ID: j.ID, LockedBy: w.o.WorkerID})
		})
	case errors.Is(err, ErrPermanent) || j.Attempts >= int(row.MaxAttempts):
		return w.markDead(rctx, j, ErrorText(err), true)
	default:
		delay := NextDelay(j.Attempts, rand.Float64)
		return w.update(rctx, func(q *store.Queries) (int64, error) {
			return q.RescheduleJob(rctx, store.RescheduleJobParams{
				DelayMs: delay.Milliseconds(), LastError: ErrorText(err), ID: j.ID, LockedBy: w.o.WorkerID,
			})
		})
	}
}

// recordCtx là ctx của một lần ghi trạng thái: giữ giá trị (span) của ctx, không theo việc hủy của nó, hạn
// recordTimeout tính từ lúc gọi.
func (w *Worker) recordCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), w.recordTimeout)
}

// markDead đặt việc đang giữ chỗ thành dead (kèm OnTerminal khi hook).
func (w *Worker) markDead(ctx context.Context, j Job, msg string, hook bool) error {
	upd := func(q *store.Queries) (int64, error) {
		return q.KillJob(ctx, store.KillJobParams{LastError: msg, ID: j.ID, LockedBy: w.o.WorkerID})
	}
	if !hook {
		if err := w.update(ctx, upd); err != nil {
			return err
		}
		w.count("jobs_dead", j.Kind)
		return nil
	}
	return w.finish(ctx, j, StatusDead, upd)
}

// finish chạy OnTerminal (nếu handler có) rồi câu đổi trạng thái upd trong cùng một transaction (ngữ cảnh tenant của
// việc). Thứ tự khóa ADR-46: dòng nghiệp vụ của hook trước, platform.jobs cuối. upd chạm 0 dòng → lùi cả hai.
func (w *Worker) finish(ctx context.Context, j Job, status string, upd func(*store.Queries) (int64, error)) error {
	hook, _ := w.handlers[j.Kind].(TerminalHook)
	err := w.inTx(ctx, j.TenantID, func(tx pgx.Tx) error {
		if hook != nil {
			if err := hook.OnTerminal(ctx, j, status, tx); err != nil {
				return fmt.Errorf("OnTerminal: %w", err)
			}
		}
		n, err := upd(store.New(tx))
		if err != nil {
			return err
		}
		if n == 0 {
			return errLostLease
		}
		return nil
	})
	switch {
	case errors.Is(err, errLostLease):
		return nil
	case err != nil:
		return fmt.Errorf("đổi việc %s sang %s: %w", j.ID, status, err)
	}
	switch status {
	case StatusDead:
		w.count("jobs_dead", j.Kind)
	case StatusInDoubt:
		w.count("jobs_in_doubt", j.Kind)
	}
	return nil
}

// update chạy một câu đổi trạng thái có điều kiện trong transaction riêng; 0 dòng (mất giữ chỗ) không là lỗi.
func (w *Worker) update(ctx context.Context, upd func(*store.Queries) (int64, error)) error {
	err := w.d.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error {
		_, err := upd(store.New(tx))
		return err
	})
	if err != nil {
		return fmt.Errorf("ghi trạng thái việc: %w", err)
	}
	return nil
}

// inTx chạy fn trong ngữ cảnh tenant của việc, hoặc ngoài ngữ cảnh khi việc không có tenant.
func (w *Worker) inTx(ctx context.Context, tenant *uuid.UUID, fn func(pgx.Tx) error) error {
	if tenant != nil {
		return tenancy.WithTenant(ctx, w.d, *tenant, fn) //nolint:wrapcheck // bên gọi bọc
	}
	return w.d.RetryTx(ctx, db.TxOptions{}, fn) //nolint:wrapcheck // bên gọi bọc
}

func (w *Worker) count(name, kind string) {
	if w.o.Metrics != nil {
		w.o.Metrics.Inc(name, kind, 1)
	}
}

// gauge đặt jobs_oldest_pending_age_s (tuổi của việc tới hạn chờ lâu nhất).
func (w *Worker) gauge(ctx context.Context) {
	if w.o.Metrics == nil {
		return
	}
	age, err := store.New(w.d.Pool).OldestPendingAgeSeconds(ctx)
	if err != nil {
		w.logErr(ctx, "đo việc chờ lâu nhất", err)
		return
	}
	w.o.Metrics.Gauge("jobs_oldest_pending_age_s", age, "")
}

// ErrorText là chuỗi lưu vào last_error của một việc: err.Error(), tối đa 1000 ký tự (không cắt giữa ký tự UTF-8).
// Gói platform khác ghi last_error (commands) dùng hàm này để cùng một giới hạn.
func ErrorText(err error) string {
	s := err.Error()
	if utf8.RuneCountInString(s) <= maxErrorLen {
		return s
	}
	return string([]rune(s)[:maxErrorLen])
}
