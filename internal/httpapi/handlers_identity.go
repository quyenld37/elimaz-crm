package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// ListStaff (GET /api/v1/identity/users, identity.users.manage): mọi nhân viên của tenant kèm vai trò theo phạm vi.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListStaff(ctx context.Context) (gen.ListStaffRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var rows []identity.StaffRow
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		rows, err = identity.ListStaff(ctx, tx)
		return err //nolint:wrapcheck // identity đã bọc
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("liệt kê nhân viên: %w", err)
	}
	out := make(gen.ListStaffOKApplicationJSON, 0, len(rows))
	for _, r := range rows {
		out = append(out, *staffOut(r))
	}
	return &out, nil
}

// ListRoles (GET /api/v1/identity/roles, identity.users.manage): vai trò của tenant và quyền của từng vai trò, cho
// màn Nhân viên (gán vai trò).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListRoles(ctx context.Context) (gen.ListRolesRes, error) {
	out, err := h.roles(ctx)
	if err != nil {
		return nil, err
	}
	res := gen.ListRolesOKApplicationJSON(out)
	return &res, nil
}

// ListRolesToManage (GET /api/v1/identity/roles/manage, identity.roles.manage): cùng dữ liệu của ListRoles cho màn
// Vai trò, để người chỉ quản lý vai trò (không quản lý nhân viên) mở được danh sách. Guard chỉ nhận một quyền mỗi
// route nên hai route.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListRolesToManage(ctx context.Context) (gen.ListRolesToManageRes, error) {
	out, err := h.roles(ctx)
	if err != nil {
		return nil, err
	}
	res := gen.ListRolesToManageOKApplicationJSON(out)
	return &res, nil
}

// roles đọc vai trò của tenant người gọi kèm quyền, version, số người giữ (phần chung của hai route đọc vai trò).
func (h *handler) roles(ctx context.Context) ([]gen.Role, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var rows []identity.RoleRow
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		rows, err = identity.ListRoles(ctx, tx)
		return err //nolint:wrapcheck // identity đã bọc
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("liệt kê vai trò: %w", err)
	}
	out := make([]gen.Role, 0, len(rows))
	for _, r := range rows {
		out = append(out, *roleOut(r))
	}
	return out, nil
}

// ListPermissions (GET /api/v1/identity/permissions, identity.roles.manage): mọi quyền kèm nhóm, nhãn, loại phạm vi.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListPermissions(context.Context) (gen.ListPermissionsRes, error) {
	cat := identity.PermissionCatalog()
	out := make(gen.ListPermissionsOKApplicationJSON, 0, len(cat))
	for _, p := range cat {
		out = append(out, gen.PermissionInfo{Key: string(p.Key), ScopeType: gen.PermissionInfoScopeType(p.ScopeType), Group: p.Group, Label: p.Label})
	}
	return &out, nil
}

// CreateRole (POST /api/v1/identity/roles, identity.roles.manage) tạo vai trò tự định nghĩa → 201 (luật ở
// identity.CreateRole).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateRole(ctx context.Context, req *gen.CreateRoleRequest, _ gen.CreateRoleParams) (gen.CreateRoleRes, error) {
	in := roleInput(req.Name, req.Permissions)
	return identityWrite(ctx, h, http.StatusCreated, "identity.role.create", "identity.role", func(tx pgx.Tx, u identity.AuthUser) (written[gen.Role], error) {
		r, err := identity.CreateRole(ctx, tx, u, in)
		return roleWritten(identity.RoleChange{After: r}), err
	})
}

// UpdateRole (PUT /api/v1/identity/roles/{id}, identity.roles.manage) sửa tên và quyền của vai trò khi version còn khớp
// (luật ở identity.UpdateRole).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateRole(ctx context.Context, req *gen.UpdateRoleRequest, params gen.UpdateRoleParams) (gen.UpdateRoleRes, error) {
	in := roleInput(req.Name, req.Permissions)
	return identityWrite(ctx, h, http.StatusOK, "identity.role.update", "identity.role", func(tx pgx.Tx, u identity.AuthUser) (written[gen.Role], error) {
		c, err := identity.UpdateRole(ctx, tx, u, params.ID, req.Version, in)
		return roleWritten(c), err
	})
}

// ArchiveRole (POST /api/v1/identity/roles/{id}/archive, identity.roles.manage) lưu trữ vai trò không còn người active
// giữ (luật ở identity.ArchiveRole).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ArchiveRole(ctx context.Context, req *gen.ArchiveRoleRequest, params gen.ArchiveRoleParams) (gen.ArchiveRoleRes, error) {
	return identityWrite(ctx, h, http.StatusOK, "identity.role.archive", "identity.role", func(tx pgx.Tx, u identity.AuthUser) (written[gen.Role], error) {
		c, err := identity.ArchiveRole(ctx, tx, u, params.ID, req.Version)
		return roleWritten(c), err
	})
}

func roleInput(name string, perms []string) identity.RoleInput {
	in := identity.RoleInput{Name: name, Permissions: make([]identity.Permission, 0, len(perms))}
	for _, p := range perms {
		in.Permissions = append(in.Permissions, identity.Permission(p))
	}
	return in
}

// roleWritten: kết quả ghi vai trò cho identityWrite (audit: tên, trạng thái, version, danh sách quyền trước và sau).
func roleWritten(c identity.RoleChange) written[gen.Role] {
	return snapshot(c.Before.ID, c.Before, c.After.ID, c.After, roleAudit, roleOut)
}

// roleAudit: trạng thái của vai trò ghi vào audit (không có dữ liệu cá nhân).
func roleAudit(r identity.RoleRow) map[string]any {
	perms := make([]string, 0, len(r.Permissions))
	for _, p := range r.Permissions {
		perms = append(perms, string(p))
	}
	return map[string]any{"key": r.Key, "name": r.Name, "status": r.Status, "version": r.Version, "permissions": perms}
}

func roleOut(r identity.RoleRow) *gen.Role {
	perms := make([]string, 0, len(r.Permissions))
	for _, p := range r.Permissions {
		perms = append(perms, string(p))
	}
	return &gen.Role{
		ID: r.ID, Key: r.Key, Name: r.Name, Status: gen.RoleStatus(r.Status), Customized: r.Customized,
		Version: r.Version, HolderCount: r.HolderCount, Permissions: perms,
	}
}

// CreateStaff (POST /api/v1/identity/users, identity.users.manage) tạo nhân viên chưa có vai trò → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateStaff(ctx context.Context, req *gen.CreateStaffRequest, _ gen.CreateStaffParams) (gen.CreateStaffRes, error) {
	return h.staffWrite(ctx, http.StatusCreated, "identity.user.create", func(tx pgx.Tx, u identity.AuthUser) (identity.Change, error) {
		created, err := identity.CreateStaff(ctx, tx, u, req.Email, req.DisplayName)
		if err != nil {
			return identity.Change{}, err //nolint:wrapcheck // sentinel của identity
		}
		return identity.Change{After: identity.StaffRow{User: created, Grants: []identity.GrantInput{}}}, nil
	})
}

// ReplaceStaffGrants (PUT /api/v1/identity/users/{id}/grants, identity.users.manage) thay toàn bộ vai trò theo phạm vi
// của một nhân viên khi version còn khớp (luật ở identity.ReplaceGrants).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ReplaceStaffGrants(ctx context.Context, req *gen.ReplaceGrantsRequest, params gen.ReplaceStaffGrantsParams) (gen.ReplaceStaffGrantsRes, error) {
	grants := make([]identity.GrantInput, 0, len(req.Grants))
	for _, g := range req.Grants {
		grants = append(grants, identity.GrantInput{RoleKey: g.RoleKey, Scope: identity.Scope{Type: string(g.ScopeType), ID: g.ScopeId}})
	}
	return h.staffWrite(ctx, http.StatusOK, "identity.user.grants", func(tx pgx.Tx, u identity.AuthUser) (identity.Change, error) {
		return identity.ReplaceGrants(ctx, tx, u, params.ID, req.Version, grants)
	})
}

// LockStaff (POST /api/v1/identity/users/{id}/lock, identity.users.manage) khóa nhân viên; có hiệu lực từ yêu cầu kế
// tiếp của người đó.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) LockStaff(ctx context.Context, params gen.LockStaffParams) (gen.LockStaffRes, error) {
	return h.setLocked(ctx, params.ID, true)
}

// UnlockStaff (POST /api/v1/identity/users/{id}/unlock, identity.users.manage) mở khóa nhân viên.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UnlockStaff(ctx context.Context, params gen.UnlockStaffParams) (gen.UnlockStaffRes, error) {
	return h.setLocked(ctx, params.ID, false)
}

func (h *handler) setLocked(ctx context.Context, id uuid.UUID, locked bool) (*gen.StaffMember, error) {
	action := "identity.user.unlock"
	if locked {
		action = "identity.user.lock"
	}
	return h.staffWrite(ctx, http.StatusOK, action, func(tx pgx.Tx, u identity.AuthUser) (identity.Change, error) {
		return identity.SetLocked(ctx, tx, u, id, locked)
	})
}

// staffWrite chạy change trong WriteTx kèm một dòng audit (không có email: chỉ trạng thái, version, vai trò theo phạm
// vi), qua identityWrite.
func (h *handler) staffWrite(ctx context.Context, status int, action string,
	change func(tx pgx.Tx, u identity.AuthUser) (identity.Change, error),
) (*gen.StaffMember, error) {
	return identityWrite(ctx, h, status, action, "identity.user", func(tx pgx.Tx, u identity.AuthUser) (written[gen.StaffMember], error) {
		c, err := change(tx, u)
		return snapshot(c.Before.ID, c.Before, c.After.ID, c.After, staffAudit, staffOut), err
	})
}

// written là kết quả của một lần ghi identity: thực thể, trạng thái trước (nil khi tạo) và sau cho audit, thân trả về.
type written[O any] struct {
	entityID      uuid.UUID
	before, after map[string]any
	out           *O
}

// record ghi dòng audit của lần ghi w (người thao tác u) trong tx.
func (w written[O]) record(ctx context.Context, tx pgx.Tx, u identity.AuthUser, action, entityType string) error {
	err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: action, EntityType: entityType,
		EntityID: w.entityID.String(), Before: w.before, After: w.after, RequestID: RequestID(ctx),
	})
	if err != nil {
		return fmt.Errorf("ghi audit %s: %w", action, err)
	}
	return nil
}

// snapshot dựng kết quả ghi từ trạng thái trước (bỏ qua khi beforeID rỗng: vừa tạo) và sau.
func snapshot[T, O any](beforeID uuid.UUID, before T, afterID uuid.UUID, after T, audit func(T) map[string]any, out func(T) *O) written[O] {
	w := written[O]{entityID: afterID, after: audit(after), out: out(after)}
	if beforeID != uuid.Nil {
		w.before = audit(before)
	}
	return w
}

// identityWrite chạy change trong WriteTx kèm một dòng audit (action, entityType). Lỗi nghiệp vụ của identity là câu
// trả lời không đổi gì: tx lùi, không lưu vào khóa chống trùng (final = false), gửi lại cùng khóa thì xét lại; lỗi đổi
// sang HTTP ở errors.go.
func identityWrite[O any](ctx context.Context, h *handler, status int, action, entityType string,
	change func(tx pgx.Tx, u identity.AuthUser) (written[O], error),
) (*O, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var (
		out     *O
		refused error
	)
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		w, err := change(tx, u)
		if isStaffRefusal(err) {
			refused = err
			return 0, nil, false, nil
		}
		if err == nil {
			err = w.record(ctx, tx, u, action, entityType)
		}
		if err != nil {
			return 0, nil, false, err
		}
		out = w.out
		return status, out, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("%s: %w", action, refused)
	}
	return out, nil
}

func isStaffRefusal(err error) bool {
	for _, e := range []error{
		identity.ErrSelfModify, identity.ErrGrantExceedsActor, identity.ErrLastOwner, identity.ErrEmailTaken,
		identity.ErrVersionConflict, identity.ErrStaffNotFound, identity.ErrInvalidInput, identity.ErrLocked,
		identity.ErrOwnerRoleLocked, identity.ErrRoleInUse, identity.ErrUnknownPermission, identity.ErrRoleNameTaken,
		identity.ErrRoleArchived, identity.ErrRoleNotFound,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// staffAudit: trạng thái của nhân viên ghi vào audit — không email, không tên (dữ liệu cá nhân, 04 mục 17).
func staffAudit(r identity.StaffRow) map[string]any {
	grants := make([]string, 0, len(r.Grants))
	for _, g := range r.Grants {
		s := g.RoleKey + "@" + g.Scope.Type
		if g.Scope.ID != "" {
			s += ":" + g.Scope.ID
		}
		grants = append(grants, s)
	}
	return map[string]any{"status": r.Status, "version": r.Version, "grants": grants}
}

func staffOut(r identity.StaffRow) *gen.StaffMember {
	grants := make([]gen.StaffGrant, 0, len(r.Grants))
	for _, g := range r.Grants {
		grants = append(grants, gen.StaffGrant{RoleKey: g.RoleKey, ScopeType: gen.StaffGrantScopeType(g.Scope.Type), ScopeId: g.Scope.ID})
	}
	return &gen.StaffMember{
		ID: r.ID, Email: r.Email, DisplayName: r.DisplayName, Status: gen.StaffMemberStatus(r.Status),
		Version: r.Version, CreatedAt: r.CreatedAt, Grants: grants, Warnings: scopeWarningsOut(r.Warnings),
	}
}
