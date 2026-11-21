package httpapi

import (
	"context"
	"net/http"
	"net/netip"
	"strings"

	"elimaz.com.vn/crm/internal/modules/identity"
)

// accessHeader là header duy nhất mang thẻ Cloudflare Access (ADR-18 điểm 3): không đọc cookie CF_Authorization,
// không đọc Authorization, để trang khác không gửi lệnh thay nhân viên bằng cookie của trình duyệt.
const accessHeader = "Cf-Access-Jwt-Assertion"

// Khóa dịch vụ của website (M5-05, ADR-18): chỉ header Authorization dạng "Bearer ek_…", chỉ trên route x-auth:
// service_key. IP của máy gọi lấy từ cf-connecting-ip (CRM chỉ nhận yêu cầu qua Cloudflare).
const (
	serviceKeyScheme = "Bearer "
	serviceKeyPrefix = "ek_"
	clientIPHeader   = "Cf-Connecting-Ip"
	authAccess       = "access"
	authServiceKey   = "service_key"
)

// ServiceKeyVerifier kiểm khóa dịch vụ đầy đủ (bearer, dạng "ek_<tiền tố>_<bí mật>") và IP của máy gọi (địa chỉ rỗng
// khi thiếu hoặc sai dạng), trả "người gọi" mang quyền web.* của khóa. Lỗi: identity.ErrInvalidToken (và sentinel của
// M5-06) → writeAuthError. Bản thật: M5-06 (identity.NewServiceKeyVerifier).
type ServiceKeyVerifier interface {
	Verify(ctx context.Context, bearer string, clientIP netip.Addr) (identity.AuthUser, error)
}

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
		withCaller(d, w, r, next, u, err)
	})
}

// withCaller: bước xác thực lỗi → writeAuthError; không thì đặt người gọi u vào ctx rồi chạy next.
func withCaller(d Deps, w http.ResponseWriter, r *http.Request, next http.Handler, u identity.AuthUser, err error) {
	if err != nil {
		writeAuthError(d, w, r, err)
		return
	}
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authUserKey{}, u)))
}

// serviceKeyAuth là bước xác thực của route x-auth: service_key: chỉ đọc Authorization: Bearer ek_…, bỏ qua thẻ
// Access (khóa dịch vụ và phiên nhân viên không thay nhau được). Thiếu d.ServiceKeys, thiếu header hay sai dạng →
// 401 mà không gọi bộ kiểm. Không log header (bí mật).
func serviceKeyAuth(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), serviceKeyScheme)
		if d.ServiceKeys == nil || !ok || !strings.HasPrefix(bearer, serviceKeyPrefix) {
			writeError(w, r, http.StatusUnauthorized, codeUnauthenticated)
			return
		}
		ip, err := netip.ParseAddr(r.Header.Get(clientIPHeader))
		if err != nil {
			ip = netip.Addr{}
		}
		u, err := d.ServiceKeys.Verify(r.Context(), bearer, ip)
		withCaller(d, w, r, next, u, err)
	})
}
