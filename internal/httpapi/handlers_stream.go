package httpapi

import (
	"net/http"

	"elimaz.com.vn/crm/internal/platform/realtime"
)

// stream (GET /api/v1/stream, realtime.subscribe) là handler SSE của route Stream: đọc nhân viên mà auth đã nạp
// (guard đã kiểm realtime.subscribe) rồi giao luồng cho realtime.ServeSSE, nơi lọc báo hiệu theo tenant và quyền.
func stream(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := authUser(r.Context())
		if !ok {
			writeError(w, r, http.StatusInternalServerError, codeInternal)
			return
		}
		if d.Realtime == nil {
			writeError(w, r, http.StatusServiceUnavailable, codeStreamUnavailable)
			return
		}
		realtime.ServeSSE(w, r, d.Realtime, u, d.SSE)
	})
}
