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
	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
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
	// ServiceKeys kiểm khóa dịch vụ của website cho route x-auth: service_key (M5-05; bản thật KeyVerifier, Task 189). nil →
	// mọi route service_key 401.
	ServiceKeys ServiceKeyVerifier
	// RateLimiter giới hạn tần suất theo khóa dịch vụ (Valkey, M5-03/M5-06). nil → bộ đếm trong tiến trình
	// defaultServiceRateMax yêu cầu / phút mỗi khóa.
	RateLimiter WebRateLimiter
	// Admission: cổng nhận vào theo tên của x-admission (flash sale, Task 187). Route khai tên không có ở đây → 503
	// retry_later (đóng, không cho qua).
	Admission map[string]func(http.Handler) http.Handler
	// FlashGate là cổng điều tiết flash sale (Task 203; cmd/crm nối cùng cổng của Admission["flash"]): lượt mua trả
	// sold_out thì dựng cờ hết suất. nil → không dựng cờ (lượt sau xuống database).
	FlashGate cache.AdmissionGate
	// ReadCache là bộ đệm đọc của các route website (Task 190; cmd/crm nối cache.NewReadCache trên Valkey bộ đệm). nil
	// → bộ đệm không có Valkey: mọi lần đọc tải thẳng PostgreSQL có giới hạn (cache.NewReadCache(nil, …)).
	ReadCache cache.ReadCache
	// WebOrderLimiter giới hạn số đơn web của mỗi SĐT (Task 194; cmd/crm nối NewWebOrderLimiter trên Valkey gate). nil →
	// bộ đếm trong tiến trình với FallbackMax của cache.DefaultLimits().WebOrderCustomer, secret HMAC ngẫu nhiên.
	WebOrderLimiter WebOrderLimiter
	// CouponGuard giới hạn số lần nhập mã giảm giá sai ở báo giá, tạo đơn web (Task 482; cmd/crm nối NewCouponGuard
	// trên Valkey gate). nil → bộ đếm trong tiến trình với FallbackMax của cache.DefaultLimits(), secret ngẫu nhiên.
	CouponGuard *CouponGuard
	// Channels là bộ adapter kênh nhắn tin (cmd/crm nối wireM4().Channels); ChannelKeys giải mã, mã hóa token Page và
	// token người dùng trong phiên đăng nhập (FIELD_ENC_KEYS). Một trong hai nil, hay chưa có adapter Facebook có
	// Connector → route kết nối Page trả 503 retry_later (Task 149).
	Channels    *channel.Registry
	ChannelKeys *fieldcrypt.Keyring
	// Reports chạy báo cáo M6 tại chỗ có chỗ và hạn giờ (cmd/crm nối REPORT_API_SLOTS, REPORT_INTERACTIVE_TIMEOUT; Task
	// 228). nil → bộ chạy mặc định (2 chỗ, 30 s) trên DB.
	Reports *reporting.Interactive

	limiter        *userLimiter  // một bộ đếm chung cho mọi route của nhân viên (prepare)
	serviceLimiter *userLimiter  // bộ đếm dự phòng của khóa dịch vụ khi RateLimiter nil (prepare)
	webCatalog     *web.Catalog  // danh mục cho website (prepare)
	webOrders      *web.Orders   // báo giá, đơn của website (prepare)
	webShipping    *web.Shipping // hãng, báo phí, ước thời gian giao của website (prepare; nil khi Fulfillment nil)
	webFlash       *web.Flash    // lượt mua flash sale của website (prepare)
}

// prepare dựng phần dùng chung giữa các route (bộ đếm tần suất) một lần cho mỗi server.
func prepare(d Deps) Deps {
	if d.limiter == nil {
		d.limiter = newUserLimiter(d.RateLimit)
	}
	if d.serviceLimiter == nil {
		d.serviceLimiter = newUserLimiter(RateLimit{Max: defaultServiceRateMax, Window: time.Minute, Now: d.RateLimit.Now})
	}
	if d.Events == nil {
		d.Events = events.NewRegistry()
	}
	if d.ReadCache == nil {
		d.ReadCache = cache.NewReadCache(nil, cache.ReadCacheOptions{Metrics: d.Metrics})
	}
	if d.webCatalog == nil {
		d.webCatalog = web.NewCatalog(d.DB, d.ReadCache, catalog.ImageView{
			BaseURL: d.Config.FilesPublicBaseURL, Preset: d.Config.ImagePresetVersion,
		}, d.Logger).WithClock(d.Now).WithFlashGate(d.FlashGate)
	}
	if d.webOrders == nil {
		d.webOrders = web.NewOrders(d.DB, order.Writer{Events: d.Events, Queue: queue.NewQueue(), Realtime: d.Realtime}, d.Now)
	}
	if d.webFlash == nil {
		d.webFlash = web.NewFlash(d.DB, order.Writer{Events: d.Events, Queue: queue.NewQueue(), Realtime: d.Realtime},
			customer.Writer{Events: d.Events, Queue: queue.NewQueue(), Realtime: d.Realtime}, d.Now)
	}
	if d.webShipping == nil && d.Fulfillment != nil {
		d.webShipping = web.NewShipping(d.DB, d.ReadCache, d.Fulfillment, d.Now)
	}
	if d.WebOrderLimiter == nil {
		d.WebOrderLimiter = NewWebOrderLimiter(cache.NewRateLimiter(nil, d.Now, d.Metrics), cache.DefaultLimits().WebOrderCustomer, nil)
	}
	if d.Reports == nil {
		d.Reports = reporting.NewInteractive(d.DB, reporting.InteractiveOptions{Metrics: d.Metrics})
	}
	if d.CouponGuard == nil {
		ls := cache.DefaultLimits()
		d.CouponGuard = NewCouponGuard(cache.NewRateLimiter(nil, d.Now, d.Metrics), ls.CouponGuessCustomer, ls.CouponGuessIP, nil)
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

// chain bọc h theo thứ tự của kế hoạch (M0, mở rộng ở M5-05): requestID → recover → otelhttp →
// bodyLimit(route.BodyLimit, mặc định 1 MiB) → origin (route Write trừ service_key) → auth(route.Auth) →
// guard(route) → rateLimit (nhân viên: userRateLimit, bỏ qua Stream; service_key: serviceRateLimit) → admission
// (route có Admission) → idempotent (route Write có IdempotencyMode required hoặc no-replay; route exempt bỏ qua,
// ADR-47) → handler. Route Public bỏ qua auth, guard, rateLimit. Riêng healthDeep (công khai) qua healthToken (thẻ
// HEALTH_TOKEN) ở chỗ của auth. Cổng nhận vào đứng trước chống trùng: yêu cầu bị cổng từ chối không giữ khóa.
func chain(d Deps, rt Route, h http.Handler) http.Handler {
	d = prepare(d)
	service := rt.Auth == authServiceKey
	if rt.Write && (rt.IdempotencyMode == modeRequired || rt.IdempotencyMode == modeNoReplay) {
		h = idempotent(d, rt, h)
	}
	if rt.Admission != "" {
		h = admission(d, rt, h)
	}
	if !rt.Public {
		switch {
		case service:
			h = serviceRateLimit(d, h)
		case !rt.Stream:
			h = userRateLimit(d.limiter, h)
		}
		h = guard(rt, h)
		if service {
			h = serviceKeyAuth(d, h)
		} else {
			h = auth(d, h)
		}
	}
	if rt.OperationID == opHealthDeep {
		h = healthToken(d.Config.HealthToken, h)
	}
	if rt.Write && !service {
		h = origin(d.Config.ConsoleOrigins, h)
	}
	h = bodyLimit(rt.BodyLimit, h)
	h = otelhttp.NewHandler(observability.ScrubPanics(h), rt.Pattern)
	h = recoverPanic(d, rt, h)
	return requestID(h)
}

// admission gắn cổng nhận vào d.Admission[rt.Admission]; tên chưa nối → mọi yêu cầu 503 retry_later có Retry-After
// (cổng là chỗ bảo vệ khi quá tải: thiếu cổng thì đóng, không cho qua).
func admission(d Deps, rt Route, next http.Handler) http.Handler {
	if gate, ok := d.Admission[rt.Admission]; ok && gate != nil {
		return gate(next)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		writeError(w, r, http.StatusServiceUnavailable, codeRetryLater)
	})
}
