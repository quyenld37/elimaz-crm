package httpapi

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// guard là chốt quyền toàn cục, mặc định từ chối (ADR-18 điểm 2; vùng rủi ro cao): mọi route không công khai đi qua
// đây sau auth. Không có nhân viên trong ctx, route không khai quyền, hay identity.Can(user, route.Permission, nil)
// sai → 403 forbidden. Quyền theo đích (conversation.read…) qua guard khi có ít nhất một grant; handler kiểm đích
// bằng identity.Can với Scope cụ thể.
func guard(rt Route, next http.Handler) http.Handler {
	perm := identity.Permission(rt.Permission)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := authUser(r.Context())
		if !ok || perm == "" || !identity.Can(u, perm, nil) {
			writeError(w, r, http.StatusForbidden, codeForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
