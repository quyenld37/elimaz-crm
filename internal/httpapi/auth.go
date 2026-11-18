package httpapi

import (
	"context"
	"net/http"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// accessHeader là header duy nhất mang thẻ Cloudflare Access (ADR-18 điểm 3): không đọc cookie CF_Authorization,
// không đọc Authorization, để trang khác không gửi lệnh thay nhân viên bằng cookie của trình duyệt.
const accessHeader = "Cf-Access-Jwt-Assertion"

type authUserKey struct{}

// authUser trả nhân viên mà auth đã đặt vào ctx.
func authUser(ctx context.Context) (identity.AuthUser, bool) {
	u, ok := ctx.Value(authUserKey{}).(identity.AuthUser)
	return u, ok
}

// auth kiểm thẻ trong header cf-access-jwt-assertion (d.Access), rồi nạp nhân viên và grant từ database ở mỗi yêu
// cầu (identity.LoadUser, không đệm: khóa tài khoản có hiệu lực ngay). Thiếu d.Access hay thiếu thẻ → 401. Lỗi đổi
// sang mã HTTP ở writeAuthError.
func auth(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get(accessHeader)
		if d.Access == nil || tok == "" {
			writeError(w, r, http.StatusUnauthorized, codeUnauthenticated)
			return
		}
		email, err := d.Access.Verify(r.Context(), tok)
		if err != nil {
			writeAuthError(d, w, r, err)
			return
		}
		u, err := identity.LoadUser(r.Context(), d.DB, email)
		if err != nil {
			writeAuthError(d, w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authUserKey{}, u)))
	})
}
