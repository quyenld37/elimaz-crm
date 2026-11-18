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
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/observability"
)

// Deps là mọi thứ lớp HTTP cần, nối trong cmd/crm. Task 7 thêm Access (identity.Verifier) và ngưỡng giới hạn
// tần suất, Task 15 thêm Realtime.
type Deps struct {
	Config config.Config
	DB     *db.DB
	Logger *slog.Logger
}

// NewServer trả ServeMux đã đăng ký mọi Route; đường dẫn không có trong Routes → 404 {"error":"not_found"}.
func NewServer(d Deps) http.Handler {
	mux, _ := newMux(d)
	return mux
}

// newMux trả mux và số route đã đăng ký (test so với len(Routes)).
func newMux(d Deps) (*http.ServeMux, int) {
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
// auth (bỏ qua Public) → handler. Task 7 thêm guard(route), userRateLimit (bỏ qua Stream); Task 9 thêm idempotency
// (chỉ Write).
func chain(d Deps, rt Route, h http.Handler) http.Handler {
	if !rt.Public {
		h = denyUntilAuth(h)
	}
	if rt.Write {
		h = origin(d.Config.ConsoleOrigins, h)
	}
	h = bodyLimit(h)
	h = otelhttp.NewHandler(h, rt.Pattern)
	h = recoverPanic(d, rt, h)
	return requestID(h)
}
