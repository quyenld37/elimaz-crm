package identity

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// RoleKeyOwner là vai trò Chủ: luôn đủ mọi quyền; "Chủ còn hoạt động" là nhân viên active có vai trò này phạm vi all.
const RoleKeyOwner = "owner"

// RoleDef là một vai trò mẫu (01 mục 8): khóa, tên hiện cho nhân viên, quyền.
type RoleDef struct {
	Key, Name   string
	Permissions []Permission
}

// DefaultRoles là bộ vai trò mẫu duy nhất của CRM (kế hoạch M1, A9). Mốc sau thêm quyền của mình vào đây (và vào
// docs/quyen.md), không tạo bộ mẫu thứ hai.
var DefaultRoles = defaultRoles() //nolint:gochecknoglobals // danh sách hằng dựng một lần, không sửa lúc chạy

func defaultRoles() []RoleDef {
	all := make([]Permission, 0, len(Permissions))
	var manager []Permission
	for _, d := range Permissions {
		all = append(all, d.Key)
		if d.Key != "identity.users.manage" && d.Key != "identity.roles.manage" && !strings.HasPrefix(string(d.Key), "platform.") {
			manager = append(manager, d.Key)
		}
	}
	return []RoleDef{
		{Key: RoleKeyOwner, Name: "Chủ", Permissions: all},
		{Key: "manager", Name: "Quản lý", Permissions: manager},
		{Key: "sale", Name: "Bán hàng", Permissions: []Permission{
			"catalog.read", "order.create", "order.read", "inventory.read", "fulfillment.read", "fulfillment.ship",
			"realtime.subscribe",
		}},
		{Key: "warehouse", Name: "Kho", Permissions: []Permission{
			"catalog.read", "inventory.read", "inventory.documents.create", "inventory.stocktake", "fulfillment.read",
			"fulfillment.ship", "inventory.suppliers.manage", "realtime.subscribe",
		}},
		{Key: "accountant", Name: "Kế toán", Permissions: []Permission{
			"catalog.read", "inventory.read", "order.read", "fulfillment.read", "realtime.subscribe",
		}},
	}
}

// EnsureDefaultRoles thêm vai trò mẫu và quyền mẫu còn thiếu của tenant tenantID trong tx (mở bằng
// tenancy.WithTenant của đúng tenant đó). Không bớt quyền, không đổi tên vai trò đã có; chạy nhiều lần không đổi gì.
// Vai trò customized (chủ shop đã sửa) không được thêm quyền, trừ owner luôn đủ mọi quyền. Dòng vai trò được khóa
// FOR SHARE trước khi đọc customized, nên lần sửa vai trò chạy cùng lúc hoặc đã commit (đồng bộ thấy customized), hoặc
// chờ đồng bộ xong. Thứ tự khóa nhóm 2: roles → role_permissions.
func EnsureDefaultRoles(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) error {
	q := store.New(tx)
	for _, r := range DefaultRoles {
		if err := q.InsertRoleIfMissing(ctx, store.InsertRoleIfMissingParams{TenantID: tenantID, Key: r.Key, Name: r.Name}); err != nil {
			return fmt.Errorf("ghi vai trò mẫu %s: %w", r.Key, err)
		}
		row, err := q.GetRoleByKey(ctx, r.Key)
		if err != nil {
			return fmt.Errorf("đọc vai trò mẫu %s: %w", r.Key, err)
		}
		if row.Customized && r.Key != RoleKeyOwner {
			continue
		}
		for _, p := range r.Permissions {
			if err := q.AddRolePermission(ctx, store.AddRolePermissionParams{TenantID: tenantID, RoleID: row.ID, Permission: string(p)}); err != nil {
				return fmt.Errorf("ghi quyền %s của vai trò %s: %w", p, r.Key, err)
			}
		}
	}
	return nil
}

// SyncDefaultRoles (vai trò migrate, sau khi áp dụng migration; chạy bằng chủ bảng) chạy EnsureDefaultRoles cho mọi
// tenant, mỗi tenant một transaction: shop chưa có vai trò mẫu nhận đủ; quyền mới của DefaultRoles vào vai trò mẫu
// chưa bị chủ shop sửa (customized), owner luôn đủ; vai trò đã sửa và vai trò tự định nghĩa không đổi. Chạy lại không
// đổi gì. Trả số tenant đã đồng bộ.
func SyncDefaultRoles(ctx context.Context, owner *db.DB) (int, error) {
	ids, err := tenancy.ListTenantIDs(ctx, owner)
	if err != nil {
		return 0, fmt.Errorf("đồng bộ vai trò mẫu: %w", err)
	}
	for i, id := range ids {
		if err := tenancy.WithTenant(ctx, owner, id, func(tx pgx.Tx) error { return EnsureDefaultRoles(ctx, tx, id) }); err != nil {
			return i, fmt.Errorf("đồng bộ vai trò mẫu của tenant %s: %w", id, err)
		}
	}
	return len(ids), nil
}

// RoleRow là một vai trò của tenant kèm quyền (đã sắp).
type RoleRow struct {
	ID          uuid.UUID
	Key, Name   string
	Status      string
	Customized  bool
	Permissions []Permission
}

// ListRoles trả mọi vai trò của tenant trong ngữ cảnh của tx, theo thứ tự tạo.
func ListRoles(ctx context.Context, tx pgx.Tx) ([]RoleRow, error) {
	rows, err := store.New(tx).ListRolesWithPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("liệt kê vai trò: %w", err)
	}
	out := make([]RoleRow, 0, len(rows))
	for _, r := range rows {
		perms := make([]Permission, 0, len(r.Permissions))
		for _, p := range r.Permissions {
			perms = append(perms, Permission(p))
		}
		out = append(out, RoleRow{ID: r.ID, Key: r.Key, Name: r.Name, Status: r.Status, Customized: r.Customized, Permissions: perms})
	}
	return out, nil
}

// roleIndex: vai trò theo khóa (để kiểm grant).
func roleIndex(roles []RoleRow) map[string]RoleRow {
	m := make(map[string]RoleRow, len(roles))
	for _, r := range roles {
		m[r.Key] = r
	}
	return m
}

// hasAllPermissions: mỗi quyền của role thỏa Can(actor, p, &scope). Quyền ScopeNone có hiệu lực toàn shop từ grant ở
// bất kỳ phạm vi nào (Task 315), nên actor chỉ cần có nó ở một grant; quyền ScopeShop đòi actor có nó ở phạm vi all
// dù gán theo kho (Task 315 lượt 2): người được gán không nhận được quyền hiệu lực nào mà actor không có.
func hasAllPermissions(actor AuthUser, role RoleRow, scope Scope) bool {
	return !slices.ContainsFunc(role.Permissions, func(p Permission) bool { return !Can(actor, p, &scope) })
}
