package httpapi

import (
	"mime"
	"net/http"
	"slices"
	"strings"
)

// origin chặn yêu cầu ghi giả từ trang khác (CSRF): method ghi phải có content-type application/json và header
// Origin đúng nguyên văn một mục của CONSOLE_ORIGINS; sai → 403 {"error":"bad_origin"}. Chỉ gắn cho route Write.
func origin(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o := r.Header.Get("Origin")
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if o == "" || !slices.Contains(allowed, o) || err != nil || !strings.EqualFold(mt, "application/json") {
			writeError(w, r, http.StatusForbidden, codeBadOrigin)
			return
		}
		next.ServeHTTP(w, r)
	})
}
