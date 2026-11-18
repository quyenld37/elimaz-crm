package httpapi

import (
	"context"
	"errors"
	"slices"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// errNoAuthUser: handler của route không công khai chạy mà ctx không có nhân viên (lỗi nối chuỗi, không phải lỗi
// của người gọi) → 500.
var errNoAuthUser = errors.New("httpapi: thiếu nhân viên đã xác thực trong ctx")

// GetMe (GET /api/v1/me, x-authenticated: chỉ cần đăng nhập, Task 315) trả nhân viên đang gọi, grant có hiệu lực của họ và cảnh báo cho quyền quản trị gán ở phạm vi hẹp (không hiệu lực, Task 315 lượt 2). Mẫu đường đọc: hợp đồng →
// handler mỏng (đọc AuthUser mà auth đã nạp từ identity) → kết quả; không truy vấn thêm.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetMe(ctx context.Context) (gen.GetMeRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	grants := make([]gen.Grant, 0, len(u.Grants))
	var ignored []identity.ScopeWarning // grant không hiệu lực, gộp theo phạm vi theo thứ tự gặp
	for _, g := range u.Grants {
		if identity.Effective(g) {
			grants = append(grants, gen.Grant{
				Permission: string(g.Permission),
				Scope:      gen.Scope{Type: gen.ScopeType(g.Scope.Type), ID: g.Scope.ID},
			})
			continue
		}
		i := slices.IndexFunc(ignored, func(w identity.ScopeWarning) bool { return w.Scope == g.Scope })
		if i < 0 {
			ignored = append(ignored, identity.ScopeWarning{Scope: g.Scope})
			i = len(ignored) - 1
		}
		if !slices.Contains(ignored[i].Permissions, g.Permission) {
			ignored[i].Permissions = append(ignored[i].Permissions, g.Permission)
		}
	}
	for i := range ignored {
		slices.Sort(ignored[i].Permissions)
	}
	return &gen.Me{UserId: u.UserID, Email: u.Email, Grants: grants, Warnings: scopeWarningsOut(ignored)}, nil
}

// msgAdminNeedsShopScope là câu báo kèm cảnh báo admin_needs_shop_scope (màn hình hiện nguyên câu).
const msgAdminNeedsShopScope = "Quyền quản trị chỉ hiệu lực khi gán toàn shop"

// scopeWarningsOut đổi cảnh báo của identity sang JSON; khóa vai trò rỗng (ở /me) thì bỏ trường roleKey.
func scopeWarningsOut(ws []identity.ScopeWarning) []gen.ScopeWarning {
	out := make([]gen.ScopeWarning, 0, len(ws))
	for _, w := range ws {
		perms := make([]string, 0, len(w.Permissions))
		for _, p := range w.Permissions {
			perms = append(perms, string(p))
		}
		sw := gen.ScopeWarning{
			Code: gen.ScopeWarningCodeAdminNeedsShopScope, Message: msgAdminNeedsShopScope,
			ScopeType: gen.ScopeWarningScopeType(w.Scope.Type), ScopeId: w.Scope.ID, Permissions: perms,
		}
		if w.RoleKey != "" {
			sw.RoleKey = gen.NewOptString(w.RoleKey)
		}
		out = append(out, sw)
	}
	return out
}
