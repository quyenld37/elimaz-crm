// Package httpapi là lớp HTTP mỏng của vai trò api: đăng ký mọi route của hợp đồng openapi/openapi.yaml (bảng
// Routes sinh bằng tools/routegen) vào một http.ServeMux, mỗi route qua cùng một chuỗi middleware rồi tới handler
// do ogen sinh (internal/httpapi/gen). Handler chỉ đọc tham số đã kiểm, gọi public API của module, trả kết quả;
// nghiệp vụ không nằm ở đây.
package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Deps là mọi thứ lớp HTTP cần, nối trong cmd/crm.
type Deps struct {
	Config config.Config
	DB     *db.DB
	Logger *slog.Logger
	// Files là kho tệp (cmd/crm nối files.S3Store; test nối filestest.MemFileStore). nil → route tệp trả 500.
	Files files.FileStore
	// Access kiểm thẻ Cloudflare Access (cmd/crm nối internal/adapters/cfaccess). nil → mọi route không công khai 401.
	Access identity.Verifier
	// RateLimit: ngưỡng giới hạn tần suất theo nhân viên; để trống lấy mặc định 300 yêu cầu / 60 s.
	RateLimit RateLimit
	// WriteLockTimeout > 0 thay lock_timeout của db.TxAPI cho WriteTx (test ca retry_later); 0: db.TxAPI.
	WriteLockTimeout time.Duration
	// Realtime phát báo hiệu cho GET /api/v1/stream (cmd/crm nối realtime.PGHub). nil → route Stream 503.
	Realtime realtime.Bus
	// SSE: nhịp gộp lô, nhịp tim và kênh dừng của luồng SSE; để trống lấy mặc định (300 ms, 20 s).
	SSE realtime.SSEOptions
	// Events là sổ consumer của sự kiện nội bộ (cmd/crm dùng chung một sổ cho api và worker); nil → sổ rỗng (sự kiện
	// vẫn được ghi vào platform.domain_events, chỉ không có việc consumer nào).
	Events *events.Registry
	// Metrics đếm counter errors khi ghi lỗi 500 (cmd/crm nối registry của tiến trình); nil: không đếm.
	Metrics *metrics.Registry
	// Cash là public API của module tiền M3 (cmd/crm nối từ wireM3, cùng sổ định dạng bảng kê, hàng đợi, kho tệp); nil
	// → route bảng kê COD trả 500.
	Cash *cash.Service
	// Fulfillment là public API của vận chuyển M3 cho các route M3 (cmd/crm nối wireM3().Fulfillment.Service); nil →
	// route của vùng đã làm trả 500.
	Fulfillment *fulfillment.Service
	// Now là đồng hồ của nghiệp vụ cần "bây giờ" (kiểm mã giảm giá); nil → time.Now (test cắm ngày cố định).
	Now func() time.Time

	limiter *userLimiter // một bộ đếm chung cho mọi route của server (prepare)
}

// prepare dựng phần dùng chung giữa các route (bộ đếm tần suất) một lần cho mỗi server.
func prepare(d Deps) Deps {
	if d.limiter == nil {
		d.limiter = newUserLimiter(d.RateLimit)
	}
	if d.Events == nil {
		d.Events = events.NewRegistry()
	}
	return d
}

// NewServer trả ServeMux đã đăng ký mọi Route; đường dẫn không có trong Routes → 404 {"error":"not_found"}.
func NewServer(d Deps) http.Handler {
	mux, _ := newMux(d)
	return mux
}

// newMux trả mux và số route đã đăng ký (test so với len(Routes)).
func newMux(d Deps) (*http.ServeMux, int) {
	d = prepare(d)
	var pinger observability.Pinger
	if d.DB != nil {
		pinger = d.DB.Pool
	}
	api, err := gen.NewServer(
		&handler{d: d, health: observability.NewHealth(pinger)},
		gen.WithErrorHandler(func(_ context.Context, w http.ResponseWriter, r *http.Request, err error) {
			writeHandlerError(d, w, r, err)
		}),
		gen.WithNotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, r, http.StatusNotFound, codeNotFound)
		}),
		gen.WithMethodNotAllowed(func(w http.ResponseWriter, r *http.Request, _ string) {
			writeError(w, r, http.StatusNotFound, codeNotFound)
		}),
	)
	if err != nil {
		// Mã ogen sinh với ogen/otel tắt không bao giờ trả lỗi ở đây; lỗi là lỗi dựng, không phải lỗi lúc chạy.
		panic(fmt.Sprintf("httpapi: dựng máy chủ ogen: %v", err))
	}
	mux := http.NewServeMux()
	registered := 0
	for _, rt := range Routes {
		var h http.Handler = api
		if rt.Stream {
			h = stream(d) // câu trả lời text/event-stream: handler SSE riêng, không qua ogen
		}
		mux.Handle(rt.Pattern, chain(d, rt, h))
		registered++
	}
	mux.Handle("/", requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, codeNotFound)
	})))
	return mux, registered
}

// chain bọc h theo thứ tự của kế hoạch: requestID → recover → otelhttp → bodyLimit(1 MiB) → origin (chỉ Write) →
// auth → guard(route) → userRateLimit (bỏ qua Stream) → idempotent (route Write có IdempotencyMode required hoặc
// no-replay; route exempt bỏ qua, ADR-47) → handler; route Public bỏ qua auth, guard, userRateLimit. Riêng healthDeep
// (công khai) qua healthToken (thẻ HEALTH_TOKEN) ở chỗ của auth.
func chain(d Deps, rt Route, h http.Handler) http.Handler {
	d = prepare(d)
	if rt.Write && (rt.IdempotencyMode == modeRequired || rt.IdempotencyMode == modeNoReplay) {
		h = idempotent(d, rt, h)
	}
	if !rt.Public {
		if !rt.Stream {
			h = userRateLimit(d.limiter, h)
		}
		h = guard(rt, h)
		h = auth(d, h)
	}
	if rt.OperationID == opHealthDeep {
		h = healthToken(d.Config.HealthToken, h)
	}
	if rt.Write {
		h = origin(d.Config.ConsoleOrigins, h)
	}
	h = bodyLimit(h)
	h = otelhttp.NewHandler(observability.ScrubPanics(h), rt.Pattern)
	h = recoverPanic(d, rt, h)
	return requestID(h)
}
