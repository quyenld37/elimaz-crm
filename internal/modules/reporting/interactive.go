package reporting

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lý do chuyển chạy nền (enum RunInBackground.reason của hợp đồng).
const (
	ReasonBusy    = "busy"
	ReasonTimeout = "timeout"
)

// Giá trị mặc định của InteractiveOptions (Global Constraints M6: REPORT_API_SLOTS 2, REPORT_INTERACTIVE_TIMEOUT 30 s,
// lock_timeout xem tại chỗ 1 s).
const (
	defaultSlots       = 2
	defaultTimeout     = 30 * time.Second
	defaultLockTimeout = time.Second
)

// sqlstateQueryCanceled là SQLSTATE 57014 (statement_timeout hay câu bị hủy).
const sqlstateQueryCanceled = "57014"

// ctxMargin: hạn của ctx riêng của lần chạy là Timeout + ctxMargin, để statement_timeout của máy chủ dừng câu trước
// (kết nối trả về pool sạch); ctx chỉ là chốt chặn cho phần không phải câu SQL (chờ kết nối của pool, nhiều câu nối
// tiếp, mã Go giữa các câu). Câu bị dừng bởi ctx làm pgx đóng kết nối.
const ctxMargin = time.Second

// BackgroundError là lỗi báo cáo không chạy tại chỗ được; Reason là ReasonBusy (hết chỗ) hay ReasonTimeout (quá hạn giờ).
// errors.Is(err, ErrRunInBackground) nhận ra.
type BackgroundError struct {
	Reason string
}

func (e *BackgroundError) Error() string { return "reporting: chạy nền (" + e.Reason + ")" }

// Is cho errors.Is(err, ErrRunInBackground) nhận mọi BackgroundError.
func (e *BackgroundError) Is(target error) bool { return target == ErrRunInBackground }

// TxReport là TxOptions của mọi transaction báo cáo (Global Constraints M6): chỉ đọc, không chạy song song trong
// database, work_mem 32MB, application_name crm-report (đếm phiên báo cáo trong pg_stat_activity); hai hạn giờ theo
// đường (xem tại chỗ: 1 s, REPORT_INTERACTIVE_TIMEOUT; chạy nền: 5 s, REPORT_BACKGROUND_TIMEOUT). Thiết lập đặt bằng
// set_config(…, true) nên hết hiệu lực khi transaction kết thúc.
func TxReport(statement, lock time.Duration) db.TxOptions {
	return db.TxOptions{LockTimeout: lock, StatementTimeout: statement, Settings: []db.Setting{
		{Name: "transaction_read_only", Value: "on"},
		{Name: "max_parallel_workers_per_gather", Value: "0"},
		{Name: "work_mem", Value: "32MB"},
		{Name: "application_name", Value: "crm-report"},
	}}
}

// InteractiveOptions cấu hình bộ chạy tại chỗ. Slots: số báo cáo chạy cùng lúc của tiến trình (REPORT_API_SLOTS);
// Timeout: statement_timeout và hạn của cả lần chạy (cộng ctxMargin; REPORT_INTERACTIVE_TIMEOUT); LockTimeout: lock_timeout. Giá trị
// 0 lấy mặc định (2, 30 s, 1 s). Metrics nil: không đếm. Defs nil: sổ Defs(); test cắm Def thử.
type InteractiveOptions struct {
	Slots                int
	Timeout, LockTimeout time.Duration
	Metrics              *metrics.Registry
	Defs                 map[Key]Def
}

// Interactive chạy báo cáo tại chỗ (GET báo cáo của api) với số chỗ và hạn giờ cố định (BC05): hết chỗ hay quá hạn thì
// trả BackgroundError để màn hình tạo lần chạy nền, không xếp hàng chờ kết nối của api.
type Interactive struct {
	d     *db.DB
	o     InteractiveOptions
	slots chan struct{}
	inUse atomic.Int64
}

// NewInteractive dựng bộ chạy tại chỗ trên d.
func NewInteractive(d *db.DB, o InteractiveOptions) *Interactive {
	if o.Slots < 1 {
		o.Slots = defaultSlots
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if o.LockTimeout <= 0 {
		o.LockTimeout = defaultLockTimeout
	}
	if o.Defs == nil {
		o.Defs = Defs()
	}
	return &Interactive{d: d, o: o, slots: make(chan struct{}, o.Slots)}
}

// Run chạy JSON của báo cáo k với tham số p cho người gọi u: kiểm p (lỗi trả ngay, không lấy chỗ); lấy chỗ không chờ
// (hết chỗ → BackgroundError{busy}); trong tenancy.WithTenant(TxReport) đọc người bán own, dựng phạm vi (ScopeFor) rồi
// gọi Def.JSON. Quá Timeout (SQLSTATE 57014 hay hạn của ctx riêng) → BackgroundError{timeout}; ctx của bên gọi bị hủy
// → lỗi của ctx đó. Khóa lạ → ErrUnknownReport; báo cáo không có JSON → ErrNotImplemented. Chỗ nhả ở mọi đường.
func (r *Interactive) Run(ctx context.Context, u identity.AuthUser, k Key, p Params) (any, error) {
	def, ok := r.o.Defs[k]
	if !ok {
		return nil, fmt.Errorf("báo cáo %s: %w", k, ErrUnknownReport)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("tham số báo cáo %s: %w", k, err)
	}
	if def.JSON == nil {
		return nil, fmt.Errorf("báo cáo %s không có bản xem tại chỗ: %w", k, ErrNotImplemented)
	}
	select {
	case r.slots <- struct{}{}:
	default:
		r.inc("report_busy", k)
		return nil, &BackgroundError{Reason: ReasonBusy}
	}
	r.gauge(r.inUse.Add(1))
	defer func() {
		r.gauge(r.inUse.Add(-1))
		<-r.slots
	}()
	ctx, span := otel.Tracer("elimaz.com.vn/crm/internal/modules/reporting").Start(ctx, "reporting."+string(k),
		trace.WithAttributes(attribute.String("report.key", string(k))))
	defer span.End()
	if dp, ok := p.(interface{ ReportDays() int }); ok {
		span.SetAttributes(attribute.Int("report.days", dp.ReportDays()))
	}
	res, err := r.run(ctx, u, def, p)
	if err != nil {
		span.SetStatus(codes.Error, "báo cáo lỗi")
		return nil, r.classify(ctx, k, err)
	}
	if rr, ok := res.(interface{ ReportRows() int }); ok {
		span.SetAttributes(attribute.Int("report.rows", rr.ReportRows()))
	}
	return res, nil
}

// run chạy một transaction báo cáo với hạn Timeout + ctxMargin và ghi report_ms. Kết quả chỉ lấy khi transaction xong không lỗi.
func (r *Interactive) run(ctx context.Context, u identity.AuthUser, def Def, p Params) (any, error) {
	rctx, cancel := context.WithTimeout(ctx, r.o.Timeout+ctxMargin)
	defer cancel()
	start := time.Now()
	var res any
	err := tenancy.WithTenant(rctx, r.d, u.TenantID, func(tx pgx.Tx) error {
		res = nil // RetryTx có thể chạy lại hàm: gán lại kết quả mỗi lần
		seller, err := OwnSeller(rctx, tx)
		if err != nil {
			return err
		}
		v, err := def.JSON(rctx, tx, ScopeFor(u, seller), p)
		if err != nil {
			return fmt.Errorf("báo cáo %s: %w", def.Key, err)
		}
		res = v
		return nil
	}, TxReport(r.o.Timeout, r.o.LockTimeout))
	if r.o.Metrics != nil {
		r.o.Metrics.Observe("report_ms", float64(time.Since(start).Milliseconds()), string(def.Key))
	}
	if err != nil {
		return nil, err //nolint:wrapcheck // lỗi đã có ngữ cảnh (báo cáo, người bán) hay là lỗi của ctx; classify bọc
	}
	return res, nil
}

// classify đổi lỗi của lần chạy: ctx của bên gọi đã hủy → lỗi của ctx; quá hạn → BackgroundError{timeout}; còn lại bọc.
func (r *Interactive) classify(ctx context.Context, k Key, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("chạy báo cáo %s: %w", k, cerr)
	}
	if code, _ := db.TransientCode(err); code == sqlstateQueryCanceled || errors.Is(err, context.DeadlineExceeded) {
		r.inc("report_timeout", k)
		return &BackgroundError{Reason: ReasonTimeout}
	}
	return fmt.Errorf("chạy báo cáo %s: %w", k, err)
}

func (r *Interactive) inc(name string, k Key) {
	if r.o.Metrics != nil {
		r.o.Metrics.Inc(name, string(k), 1)
	}
}

func (r *Interactive) gauge(n int64) {
	if r.o.Metrics != nil {
		r.o.Metrics.Gauge("report_slots_in_use", float64(n), "")
	}
}
