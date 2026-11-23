package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// guard: route đọc có khai quyền vẫn đòi quyền đó; chỉ route Authenticated không quyền mới cho mọi nhân viên đã xác
// thực (mutation testing Task 23: guard.go, điều kiện authOnly còn sống).
func TestGuardReadRouteNeedsPermission(t *testing.T) {
	t.Parallel()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	none := identity.AuthUser{}
	has := identity.AuthUser{Grants: []identity.Grant{{Permission: "platform.health.read", Scope: identity.Scope{Type: identity.ScopeAll}}}}
	cases := []struct {
		name string
		rt   Route
		u    identity.AuthUser
		want int
	}{
		{"đọc có quyền, người thiếu quyền", Route{Method: "GET", Permission: "platform.health.read"}, none, http.StatusForbidden},
		{"đọc có quyền, người có quyền", Route{Method: "GET", Permission: "platform.health.read"}, has, http.StatusNoContent},
		{"đọc không khai quyền", Route{Method: "GET"}, has, http.StatusForbidden},
		{"Authenticated, ai đã xác thực cũng qua", Route{Method: "GET", Authenticated: true}, none, http.StatusNoContent},
		{"Authenticated nhưng là route ghi", Route{Method: "POST", Authenticated: true, Write: true}, has, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.WithValue(t.Context(), authUserKey{}, c.u), c.rt.Method, "/x", nil)
			w := httptest.NewRecorder()
			guard(c.rt, ok).ServeHTTP(w, r)
			if w.Code != c.want {
				t.Fatalf("mã %d, muốn %d", w.Code, c.want)
			}
		})
	}
}
