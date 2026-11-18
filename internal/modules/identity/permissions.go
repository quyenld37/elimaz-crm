// Package identity sở hữu người dùng, vai trò và quyền theo phạm vi của CRM (spec 04 mục 3, mục 11; ADR-18, ADR-35).
//
// Phần công khai: danh sách quyền (Permissions), người dùng đã xác thực (AuthUser) và câu hỏi duy nhất về quyền
// (Can); nạp người dùng theo email (LoadUser); luật thẻ Cloudflare Access (Verifier, CheckMFA). Gói không biết HTTP:
// phần tải khóa và kiểm chữ ký thẻ nằm ở internal/adapters/cfaccess, lớp HTTP ở internal/httpapi.
//
// Đây là module mẫu cho module và đường đọc mới: phần công khai ở gói này, câu SQL trong queries/identity sinh ra
// internal/store (sqlc), mọi truy vấn trong tenancy.WithTenant, test trên PostgreSQL thật qua internal/testkit.
package identity

import (
	"slices"

	"github.com/google/uuid"
)

// Permission là khóa quyền, ví dụ "audit.read". Route khai bằng x-permission trong openapi.yaml.
type Permission string

// ScopeKind cho biết quyền được kiểm theo đích (một Page, một kho) hay không.
type ScopeKind int

const (
	// ScopeNone là quyền toàn shop, chỉ grant phạm vi all mới cho quyền.
	ScopeNone ScopeKind = iota
	// ScopeByTarget là quyền trên từng đích; grant all hoặc grant đúng đích cho quyền.
	ScopeByTarget
)

// PermissionDef là một quyền của hệ thống và cách kiểm phạm vi của nó.
type PermissionDef struct {
	Key   Permission
	Scope ScopeKind
}

// Permissions là mọi quyền của M0 (kế hoạch Task 7). Route khai quyền ngoài danh sách này thì tests/arch đỏ.
var Permissions = []PermissionDef{ //nolint:gochecknoglobals // danh sách hằng, không sửa lúc chạy
	{"realtime.subscribe", ScopeNone},
	{"audit.read", ScopeNone},
	{"platform.jobs.manage", ScopeNone},
	{"platform.health.read", ScopeNone},
	{"identity.users.manage", ScopeNone},
	{"conversation.read", ScopeByTarget},
}

// Loại phạm vi của một grant (cột user_role_grants.scope_type).
const (
	ScopeAll       = "all"
	ScopePage      = "page"
	ScopeWarehouse = "warehouse"
)

// Scope là phạm vi của một grant hoặc đích của một lần kiểm quyền. Type all thì ID rỗng.
type Scope struct {
	Type string
	ID   string
}

// Grant là một cặp (quyền, phạm vi) của người dùng.
type Grant struct {
	Permission Permission
	Scope      Scope
}

// AuthUser là nhân viên đã xác thực của một yêu cầu, kèm mọi grant tại thời điểm yêu cầu.
type AuthUser struct {
	UserID   uuid.UUID
	TenantID uuid.UUID
	Email    string
	Grants   []Grant
}

// IsKnown cho biết p có trong Permissions không.
func IsKnown(p Permission) bool {
	return slices.ContainsFunc(Permissions, func(d PermissionDef) bool { return d.Key == p })
}

// KindOf trả cách kiểm phạm vi của p; quyền lạ coi là ScopeNone (chỉ all mới cho, và Can từ chối quyền lạ).
func KindOf(p Permission) ScopeKind {
	for _, d := range Permissions {
		if d.Key == p {
			return d.Scope
		}
	}
	return ScopeNone
}

// Can trả lời "u có quyền p trên target không" (mặc định từ chối, ADR-18):
//   - quyền lạ: luôn sai;
//   - quyền ScopeNone: cần grant của p phạm vi all (target bỏ qua);
//   - quyền ScopeByTarget có target: cần grant của p phạm vi all hoặc đúng target;
//   - quyền ScopeByTarget không target: cần ít nhất một grant của p (handler kiểm đích sau).
//
// Không có grant nào thì không có quyền.
func Can(u AuthUser, p Permission, target *Scope) bool {
	if !IsKnown(p) {
		return false
	}
	byTarget := KindOf(p) == ScopeByTarget
	for _, g := range u.Grants {
		if g.Permission != p {
			continue
		}
		if g.Scope.Type == ScopeAll && g.Scope.ID == "" {
			return true
		}
		if byTarget && (target == nil || g.Scope == *target) {
			return true
		}
	}
	return false
}

// ScopesFor trả phạm vi của u cho quyền p: all = true khi có grant phạm vi all; ngược lại ids là mã đích (đã sắp,
// không trùng) để lọc danh sách. Không grant nào: false, rỗng.
func ScopesFor(u AuthUser, p Permission) (all bool, ids []string) {
	for _, g := range u.Grants {
		if g.Permission != p {
			continue
		}
		if g.Scope.Type == ScopeAll {
			return true, nil
		}
		ids = append(ids, g.Scope.ID)
	}
	slices.Sort(ids)
	return false, slices.Compact(ids)
}
