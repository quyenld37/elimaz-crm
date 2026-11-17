package metrics

import (
	"net/http"
	"strings"
	"time"
)

// Middleware đo thời gian xử lý mỗi yêu cầu và ghi Observe("http_ms", ms, "<METHOD> <pattern>"), với pattern là
// mẫu route của http.ServeMux đã khớp (không phải đường dẫn thật, không có tham số, nên không chứa dữ liệu cá
// nhân). Yêu cầu không khớp route nào ghi nhãn "<METHOD> unmatched"; phương thức lạ ghi "OTHER".
func Middleware(reg *Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		reg.Observe("http_ms", float64(time.Since(start).Microseconds())/1000, routeLabel(r))
	})
}

// routeLabel đọc r.Pattern mà http.ServeMux gán vào chính yêu cầu khi chọn handler.
func routeLabel(r *http.Request) string {
	method := knownMethod(r.Method)
	p := r.Pattern
	if p == "" {
		return method + " unmatched"
	}
	if _, path, ok := strings.Cut(p, " "); ok {
		p = path // mẫu có sẵn phương thức ("GET /health/live"): dùng phương thức thật của yêu cầu
	}
	return method + " " + p
}

func knownMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodOptions:
		return m
	}
	return "OTHER"
}
