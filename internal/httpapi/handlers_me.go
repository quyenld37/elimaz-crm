package httpapi

import (
	"context"
	"errors"

	"elimaz.com.vn/crm/internal/httpapi/gen"
)

// errNoAuthUser: handler của route không công khai chạy mà ctx không có nhân viên (lỗi nối chuỗi, không phải lỗi
// của người gọi) → 500.
var errNoAuthUser = errors.New("httpapi: thiếu nhân viên đã xác thực trong ctx")

// GetMe (GET /api/v1/me, x-authenticated: chỉ cần đăng nhập, Task 315) trả nhân viên đang gọi và grant của họ. Mẫu đường đọc: hợp đồng →
// handler mỏng (đọc AuthUser mà auth đã nạp từ identity) → kết quả; không truy vấn thêm.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetMe(ctx context.Context) (gen.GetMeRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	grants := make([]gen.Grant, 0, len(u.Grants))
	for _, g := range u.Grants {
		grants = append(grants, gen.Grant{
			Permission: string(g.Permission),
			Scope:      gen.Scope{Type: gen.ScopeType(g.Scope.Type), ID: g.Scope.ID},
		})
	}
	return &gen.Me{UserId: u.UserID, Email: u.Email, Grants: grants}, nil
}
