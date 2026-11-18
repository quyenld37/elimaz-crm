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

// ListRoles (GET /api/v1/identity/roles, identity.users.manage): vai trò của tenant và quyền của từng vai trò.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListRoles(ctx context.Context) (gen.ListRolesRes, error) {
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
	out := make(gen.ListRolesOKApplicationJSON, 0, len(rows))
	for _, r := range rows {
		perms := make([]string, 0, len(r.Permissions))
		for _, p := range r.Permissions {
			perms = append(perms, string(p))
		}
		out = append(out, gen.Role{Key: r.Key, Name: r.Name, Status: gen.RoleStatus(r.Status), Customized: r.Customized, Permissions: perms})
	}
	return &out, nil
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
