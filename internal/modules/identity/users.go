package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity/internal/store"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Lỗi của LoadUser. Lớp HTTP đổi cả hai sang 403 (không cho biết email có tồn tại hay không).
var (
	// ErrUnknownUser: email không có định tuyến staff_email, hoặc tenant đó không có user với email này.
	ErrUnknownUser = errors.New("identity: không có nhân viên với email này")
	// ErrLocked: user ở trạng thái locked.
	ErrLocked = errors.New("identity: tài khoản đã bị khóa")
)

// RouteKindStaffEmail là loại khóa của platform.tenant_routes dùng cho email nhân viên.
const RouteKindStaffEmail = "staff_email"

const statusActive = "active"

// NormalizeEmail đưa email về dạng khóa định tuyến: bỏ khoảng trắng hai đầu, chữ thường.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// LoadUser tìm tenant của email (tenancy.Resolve "staff_email"), rồi trong ngữ cảnh tenant đó nạp user và mọi grant.
// Trạng thái đọc ở mỗi lần gọi (không đệm), nên khóa tài khoản có hiệu lực ngay ở yêu cầu kế tiếp. Lỗi:
// ErrUnknownUser, ErrLocked, hoặc lỗi database (có thể bọc db.ErrRetryLater). Lỗi không mang email.
func LoadUser(ctx context.Context, d *db.DB, email string) (AuthUser, error) {
	key := NormalizeEmail(email)
	if key == "" {
		return AuthUser{}, ErrUnknownUser
	}
	tenantID, ok, err := tenancy.Resolve(ctx, d, RouteKindStaffEmail, key)
	if err != nil {
		return AuthUser{}, fmt.Errorf("nạp nhân viên: %w", err)
	}
	if !ok {
		return AuthUser{}, ErrUnknownUser
	}
	var u AuthUser
	err = tenancy.WithTenant(ctx, d, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.GetUserByEmail(ctx, key)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownUser
		}
		if err != nil {
			return fmt.Errorf("đọc nhân viên: %w", err)
		}
		if row.Status != statusActive {
			return ErrLocked
		}
		rows, err := q.ListUserGrants(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("đọc quyền của nhân viên: %w", err)
		}
		grants := make([]Grant, 0, len(rows))
		for _, r := range rows {
			grants = append(grants, Grant{Permission: Permission(r.Permission), Scope: Scope{Type: r.ScopeType, ID: r.ScopeID}})
		}
		// Gán trọn ở cuối: fn có thể chạy lại (RetryTx), không cộng dồn.
		u = AuthUser{UserID: row.ID, TenantID: tenantID, Email: row.Email, Grants: grants, Kind: KindStaff}
		return nil
	})
	if err != nil {
		return AuthUser{}, err //nolint:wrapcheck // lỗi sentinel của gói hoặc lỗi đã bọc ở trên
	}
	return u, nil
}

// SeedOwner (lệnh crm seed-dev, chạy bằng chủ bảng) dựng vai trò mẫu của tenant (EnsureDefaultRoles; owner có mọi
// quyền của Permissions), user theo email và grant owner phạm vi all. Chạy lại được: không thêm dòng trùng. Không tạo
// định tuyến email → tenant (bảng của platform: tenancy.AddRoute).
func SeedOwner(ctx context.Context, owner *db.DB, tenantID uuid.UUID, email string) error {
	key := NormalizeEmail(email)
	if key == "" {
		return ErrUnknownUser
	}
	err := tenancy.WithTenant(ctx, owner, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		// Thứ tự khóa ADR-46 nhóm 2: users → roles → role_permissions → user_role_grants.
		userID, err := q.UpsertUser(ctx, store.UpsertUserParams{TenantID: tenantID, Email: key, DisplayName: key})
		if err != nil {
			return fmt.Errorf("ghi user: %w", err)
		}
		if err := EnsureDefaultRoles(ctx, tx, tenantID); err != nil {
			return err
		}
		role, err := q.GetRoleByKey(ctx, RoleKeyOwner)
		if err != nil {
			return fmt.Errorf("đọc vai trò owner: %w", err)
		}
		if err := q.AddUserRoleGrant(ctx, store.AddUserRoleGrantParams{
			TenantID: tenantID, UserID: userID, RoleID: role.ID, ScopeType: ScopeAll, ScopeID: "",
		}); err != nil {
			return fmt.Errorf("gán vai trò owner: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("seed chủ shop: %w", err)
	}
	return nil
}
