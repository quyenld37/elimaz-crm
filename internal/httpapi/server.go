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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// Deps là mọi thứ lớp HTTP cần, nối trong cmd/crm. Task 15 thêm Realtime.
type Deps struct {
	Config config.Config
	DB     *db.DB
	Logger *slog.Logger
	// Access kiểm thẻ Cloudflare Access (cmd/crm nối internal/adapters/cfaccess). nil → mọi route không công khai 401.
	Access identity.Verifier
	// RateLimit: ngưỡng giới hạn tần suất theo nhân viên; để trống lấy mặc định 300 yêu cầu / 60 s.
	RateLimit RateLimit

	limiter *userLimiter // một bộ đếm chung cho mọi route của server (prepare)
}

// prepare dựng phần dùng chung giữa các route (bộ đếm tần suất) một lần cho mỗi server.
func prepare(d Deps) Deps {
	if d.limiter == nil {
		d.limiter = newUserLimiter(d.RateLimit)
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
		&handler{health: observability.NewHealth(pinger)},
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
		mux.Handle(rt.Pattern, chain(d, rt, api))
		registered++
	}
	mux.Handle("/", requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, codeNotFound)
	})))
	return mux, registered
}

// chain bọc h theo thứ tự của kế hoạch: requestID → recover → otelhttp → bodyLimit(1 MiB) → origin (chỉ Write) →
// auth → guard(route) → userRateLimit (bỏ qua Stream) → idempotent (route Write có IdempotencyMode required hoặc
// no-replay; route exempt bỏ qua, ADR-47) → handler; route Public bỏ qua auth, guard, userRateLimit.
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
	if rt.Write {
		h = origin(d.Config.ConsoleOrigins, h)
	}
	h = bodyLimit(h)
	h = otelhttp.NewHandler(h, rt.Pattern)
	h = recoverPanic(d, rt, h)
	return requestID(h)
}
