package identity

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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
	// M3 (Task M3-04): nhập, chốt bảng kê, điều chỉnh sổ COD, ghi và miễn khoản hoàn tiền chỉ Chủ và Kế toán.
	notManager := []Permission{
		"identity.users.manage", "identity.roles.manage", "identity.service_keys.manage",
		"cash.statements.import", "cash.statements.confirm", "cash.adjust", "cash.refunds.record", "cash.refunds.waive",
		// M7 (Task 260): quyết đêm mở (tạo lượt, đi/dừng, mở bán, đóng theo dõi) chỉ Chủ (Q25).
		"reporting.launch.decide",
	}
	for _, d := range Permissions {
		all = append(all, d.Key)
		if !slices.Contains(notManager, d.Key) && !strings.HasPrefix(string(d.Key), "platform.") {
			manager = append(manager, d.Key)
		}
	}
	return []RoleDef{
		{Key: RoleKeyOwner, Name: "Chủ", Permissions: all},
		{Key: "manager", Name: "Quản lý", Permissions: manager},
		{Key: "sale", Name: "Bán hàng", Permissions: []Permission{
			"catalog.read", "order.create", "order.read", "inventory.read", "fulfillment.read", "fulfillment.ship",
			"realtime.subscribe",
			// M2 (Task M2-01).
			"customer.read", "customer.write", "order.update", "order.confirm", "order.cancel", "order.return",
			"promotion.read",
			// M3 (Task M3-04).
			"fulfillment.quote", "fulfillment.cancel", "fulfillment.callbacks.work", "cash.payments.record",
			// M5 (Task 195).
			"order.requests.read", "order.requests.manage",
			// M4 (Task 143): đọc, trả lời hội thoại, chốt Live trên Page được gán; gửi ảnh cho khách.
			"conversation.read", "conversation.reply", "live.operate", "files.upload",
			// M6 (Task 225): báo cáo bán hàng của Page được gán.
			"report.sales.read",
			// M7 (Task 260): báo cáo T2 của Page được gán.
			"conversation.t2.read",
		}},
		{Key: "warehouse", Name: "Kho", Permissions: []Permission{
			"catalog.read", "inventory.read", "inventory.documents.create", "inventory.stocktake", "fulfillment.read",
			"fulfillment.ship", "inventory.suppliers.manage", "realtime.subscribe",
			// M2 (Task M2-01).
			"order.read", "order.return.receive", "customer.read",
			// M3 (Task M3-04).
			"fulfillment.carrier_accounts.read", "fulfillment.quote", "fulfillment.cancel", "fulfillment.print",
			"fulfillment.pack", "fulfillment.manual_status", "inventory.aged.read",
			// M6 (Task 225): báo cáo tồn của kho được gán.
			"report.inventory.read",
			// M7 (Task 260): kiểm kho mở sổ, nhập hàng hoàn của đơn cũ ở kho được gán.
			"inventory.stocktake.opening", "inventory.legacy_return.create",
		}},
		{Key: "accountant", Name: "Kế toán", Permissions: []Permission{
			"catalog.read", "inventory.read", "order.read", "fulfillment.read", "realtime.subscribe",
			// M2 (Task M2-01).
			"customer.read", "promotion.read",
			// M3 (Task M3-04).
			"cash.cod.read", "cash.statements.import", "cash.statements.confirm", "cash.adjust", "cash.payments.record",
			"cash.refunds.record", "files.upload",
			// M6 (Task 225): báo cáo bán hàng và xuất Excel (gán toàn shop mới xuất được).
			"report.sales.read", "report.export",
			// M7 (Task 260): sự kiện hãng của đơn ngoài hệ thống, báo cáo sẵn sàng và đêm mở (chỉ xem).
			"fulfillment.unmatched.read", "reporting.launch.read",
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

// RoleRow là một vai trò của tenant kèm quyền (đã sắp). HolderCount: số nhân viên active có ít nhất một grant của
// vai trò.
type RoleRow struct {
	ID          uuid.UUID
	Key, Name   string
	Status      string
	Customized  bool
	Version     int64
	HolderCount int64
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
		out = append(out, RoleRow{
			ID: r.ID, Key: r.Key, Name: r.Name, Status: r.Status, Customized: r.Customized, Version: r.Version,
			HolderCount: r.HolderCount, Permissions: perms,
		})
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

// Lỗi của vai trò tự định nghĩa (Task M1-56; Q35). Dùng cùng ErrGrantExceedsActor, ErrVersionConflict, ErrInvalidInput,
// ErrLocked của quản lý nhân viên. Lớp HTTP đổi sang mã ở internal/httpapi/errors.go.
var (
	// ErrOwnerRoleLocked: vai trò Chủ luôn đủ mọi quyền, không sửa, không lưu trữ được.
	ErrOwnerRoleLocked = errors.New("identity: vai trò Chủ không sửa, không lưu trữ được")
	// ErrRoleInUse: vai trò còn nhân viên active giữ, không lưu trữ được.
	ErrRoleInUse = errors.New("identity: vai trò còn nhân viên đang giữ")
	// ErrUnknownPermission: quyền không có trong Permissions.
	ErrUnknownPermission = errors.New("identity: quyền không có trong hệ thống")
	// ErrRoleNameTaken: đã có vai trò cùng tên (không phân biệt hoa thường, kể cả vai trò đã lưu trữ).
	ErrRoleNameTaken = errors.New("identity: đã có vai trò cùng tên")
	// ErrRoleArchived: vai trò đã lưu trữ, không sửa và không gán mới được.
	ErrRoleArchived = errors.New("identity: vai trò đã lưu trữ")
	// ErrRoleNotFound: không có vai trò với mã này trong tenant.
	ErrRoleNotFound = errors.New("identity: không có vai trò này")
)

const (
	permRolesManage    Permission = "identity.roles.manage"
	roleStatusArchived string     = "archived"
	customRolePrefix              = "custom_"
	customRoleKeyLen              = 8
	maxRoleName                   = 100
	maxRolePermissions            = 200
)

// RoleInput là tên và bộ quyền của một vai trò khi tạo hay sửa.
type RoleInput struct {
	Name        string
	Permissions []Permission
}

// RoleChange là vai trò trước và sau một lần ghi (để ghi audit). Before rỗng khi tạo.
type RoleChange struct {
	Before, After RoleRow
}

// CreateRole tạo vai trò tự định nghĩa (khóa "custom_" + 8 ký tự ngẫu nhiên, customized = true) trong tx của tenant
// actor. Luật (vùng rủi ro cao), theo thứ tự: actor thiếu identity.roles.manage → ErrGrantExceedsActor; tên rỗng,
// dài quá 100 ký tự hay có ký tự điều khiển → ErrInvalidInput; quyền lạ → ErrUnknownPermission; actor (đọc lại trong
// transaction) thiếu một quyền ở phạm vi all → ErrGrantExceedsActor (bị khóa → ErrLocked); trùng tên →
// ErrRoleNameTaken. Kiểm tên dưới khóa tư vấn của tenant nên hai lần tạo cùng tên cùng lúc chỉ một lần qua.
func CreateRole(ctx context.Context, tx pgx.Tx, actor AuthUser, in RoleInput) (RoleRow, error) {
	q := store.New(tx)
	name, want, fresh, err := beginRoleWrite(ctx, q, actor, in)
	if err != nil {
		return RoleRow{}, err
	}
	if !canChangePermissions(fresh, nil, want) {
		return RoleRow{}, ErrGrantExceedsActor
	}
	if err := q.LockRoleNames(ctx); err != nil {
		return RoleRow{}, fmt.Errorf("khóa tên vai trò: %w", err)
	}
	if err := checkRoleName(ctx, tx, name, uuid.Nil); err != nil {
		return RoleRow{}, err
	}
	id, err := q.InsertCustomRole(ctx, store.InsertCustomRoleParams{TenantID: actor.TenantID, Key: newRoleKey(), Name: name})
	if err != nil {
		return RoleRow{}, fmt.Errorf("ghi vai trò: %w", err)
	}
	for _, p := range want {
		if err := q.AddRolePermission(ctx, store.AddRolePermissionParams{TenantID: actor.TenantID, RoleID: id, Permission: string(p)}); err != nil {
			return RoleRow{}, fmt.Errorf("ghi quyền %s của vai trò: %w", p, err)
		}
	}
	return getRole(ctx, tx, id)
}

// UpdateRole đặt tên và bộ quyền của roleID khi version còn khớp; version tăng 1, customized = true (EnsureDefaultRoles
// không thêm lại quyền đã gỡ khỏi vai trò mẫu). Luật như CreateRole, cộng: không có vai trò → ErrRoleNotFound; vai trò
// owner → ErrOwnerRoleLocked; đã lưu trữ → ErrRoleArchived; version khác → ErrVersionConflict; mọi quyền thêm vào hay gỡ
// khỏi vai trò phải có ở actor phạm vi all → ErrGrantExceedsActor (nên không ai tự nâng quyền bằng cách sửa vai trò
// mình giữ; gỡ quyền lạ còn sót trong database thì không cần). Khóa: tư vấn tên → dòng roles FOR UPDATE →
// role_permissions (ADR-46 nhóm 2), không khóa users.
func UpdateRole(ctx context.Context, tx pgx.Tx, actor AuthUser, roleID uuid.UUID, version int64, in RoleInput) (RoleChange, error) {
	q := store.New(tx)
	name, want, fresh, err := beginRoleWrite(ctx, q, actor, in)
	if err != nil {
		return RoleChange{}, err
	}
	if err := q.LockRoleNames(ctx); err != nil {
		return RoleChange{}, fmt.Errorf("khóa tên vai trò: %w", err)
	}
	before, err := lockRole(ctx, tx, q, roleID, version)
	if err != nil {
		return RoleChange{}, err
	}
	if !canChangePermissions(fresh, before.Permissions, want) {
		return RoleChange{}, ErrGrantExceedsActor
	}
	if err := checkRoleName(ctx, tx, name, roleID); err != nil {
		return RoleChange{}, err
	}
	for _, p := range before.Permissions {
		if !slices.Contains(want, p) {
			if err := q.DeleteRolePermission(ctx, store.DeleteRolePermissionParams{RoleID: roleID, Permission: string(p)}); err != nil {
				return RoleChange{}, fmt.Errorf("gỡ quyền %s khỏi vai trò: %w", p, err)
			}
		}
	}
	for _, p := range want {
		if !slices.Contains(before.Permissions, p) {
			if err := q.AddRolePermission(ctx, store.AddRolePermissionParams{TenantID: actor.TenantID, RoleID: roleID, Permission: string(p)}); err != nil {
				return RoleChange{}, fmt.Errorf("thêm quyền %s vào vai trò: %w", p, err)
			}
		}
	}
	if _, err := q.UpdateRoleRow(ctx, store.UpdateRoleRowParams{ID: roleID, Name: name}); err != nil {
		return RoleChange{}, fmt.Errorf("sửa vai trò: %w", err)
	}
	after, err := getRole(ctx, tx, roleID)
	if err != nil {
		return RoleChange{}, err
	}
	return RoleChange{Before: before, After: after}, nil
}

// ArchiveRole lưu trữ roleID khi version còn khớp và không còn nhân viên active nào giữ (ErrRoleInUse); version tăng 1.
// Vai trò đã lưu trữ không gán mới được (ReplaceGrants → ErrRoleArchived) và không cho quyền (LoadUser bỏ qua).
// Lưu trữ gỡ mọi quyền của vai trò khỏi người còn giữ (bị khóa), nên actor phải có mọi quyền đó ở phạm vi all như khi
// gỡ quyền (ErrGrantExceedsActor). Lỗi khác như UpdateRole. Khóa dòng roles FOR UPDATE rồi đếm người giữ (không khóa
// users): lần gán chạy cùng lúc khóa dòng vai trò FOR SHARE, nên hoặc chờ lưu trữ xong rồi thấy archived, hoặc đã
// commit trước và được đếm.
func ArchiveRole(ctx context.Context, tx pgx.Tx, actor AuthUser, roleID uuid.UUID, version int64) (RoleChange, error) {
	q := store.New(tx)
	fresh, err := checkRolesManager(ctx, q, actor)
	if err != nil {
		return RoleChange{}, err
	}
	before, err := lockRole(ctx, tx, q, roleID, version)
	if err != nil {
		return RoleChange{}, err
	}
	if !canChangePermissions(fresh, before.Permissions, nil) {
		return RoleChange{}, ErrGrantExceedsActor
	}
	holders, err := q.CountActiveHolders(ctx, roleID)
	if err != nil {
		return RoleChange{}, fmt.Errorf("đếm người giữ vai trò: %w", err)
	}
	if holders > 0 {
		return RoleChange{}, ErrRoleInUse
	}
	if _, err := q.ArchiveRoleRow(ctx, roleID); err != nil {
		return RoleChange{}, fmt.Errorf("lưu trữ vai trò: %w", err)
	}
	after, err := getRole(ctx, tx, roleID)
	if err != nil {
		return RoleChange{}, err
	}
	return RoleChange{Before: before, After: after}, nil
}

// beginRoleWrite: phần đầu chung của tạo và sửa vai trò — quyền identity.roles.manage, chuẩn hóa đầu vào, quyền của
// actor đọc lại trong transaction.
func beginRoleWrite(ctx context.Context, q *store.Queries, actor AuthUser, in RoleInput) (string, []Permission, AuthUser, error) {
	if !Can(actor, permRolesManage, nil) {
		return "", nil, AuthUser{}, ErrGrantExceedsActor
	}
	name, want, err := normalizeRoleInput(in)
	if err != nil {
		return "", nil, AuthUser{}, err
	}
	fresh, err := checkRolesManager(ctx, q, actor)
	if err != nil {
		return "", nil, AuthUser{}, err
	}
	return name, want, fresh, nil
}

// checkRolesManager: actor có identity.roles.manage theo AuthUser của yêu cầu và theo grant đọc lại trong transaction
// (actor vừa bị khóa → ErrLocked, vừa bị hạ quyền → ErrGrantExceedsActor). Trả AuthUser đọc lại.
func checkRolesManager(ctx context.Context, q *store.Queries, actor AuthUser) (AuthUser, error) {
	if !Can(actor, permRolesManage, nil) {
		return AuthUser{}, ErrGrantExceedsActor
	}
	u, err := q.GetStaffUser(ctx, actor.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthUser{}, ErrLocked
	}
	if err != nil {
		return AuthUser{}, fmt.Errorf("đọc lại người thao tác: %w", err)
	}
	if u.Status != statusActive {
		return AuthUser{}, ErrLocked
	}
	fresh, err := freshGrants(ctx, q, actor)
	if err != nil {
		return AuthUser{}, err
	}
	if !Can(fresh, permRolesManage, nil) {
		return AuthUser{}, ErrGrantExceedsActor
	}
	return fresh, nil
}

// lockRole khóa dòng vai trò FOR UPDATE và kiểm: có (ErrRoleNotFound), không phải owner (ErrOwnerRoleLocked), chưa lưu
// trữ (ErrRoleArchived), version khớp (ErrVersionConflict). Trả vai trò (kèm quyền) đọc sau khi khóa.
func lockRole(ctx context.Context, tx pgx.Tx, q *store.Queries, roleID uuid.UUID, version int64) (RoleRow, error) {
	row, err := q.LockRole(ctx, roleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RoleRow{}, ErrRoleNotFound
	}
	if err != nil {
		return RoleRow{}, fmt.Errorf("khóa vai trò: %w", err)
	}
	switch {
	case row.Key == RoleKeyOwner:
		return RoleRow{}, ErrOwnerRoleLocked
	case row.Status == roleStatusArchived:
		return RoleRow{}, ErrRoleArchived
	case row.Version != version:
		return RoleRow{}, ErrVersionConflict
	}
	return getRole(ctx, tx, roleID)
}

// getRole đọc một vai trò (kèm quyền, số người giữ) theo mã; không có → ErrRoleNotFound.
func getRole(ctx context.Context, tx pgx.Tx, roleID uuid.UUID) (RoleRow, error) {
	roles, err := ListRoles(ctx, tx)
	if err != nil {
		return RoleRow{}, err
	}
	i := slices.IndexFunc(roles, func(r RoleRow) bool { return r.ID == roleID })
	if i < 0 {
		return RoleRow{}, ErrRoleNotFound
	}
	return roles[i], nil
}

// checkRoleName: không vai trò nào khác except có cùng tên (không phân biệt hoa thường; tính cả vai trò đã lưu trữ).
func checkRoleName(ctx context.Context, tx pgx.Tx, name string, except uuid.UUID) error {
	roles, err := ListRoles(ctx, tx)
	if err != nil {
		return err
	}
	for _, r := range roles {
		if r.ID != except && strings.EqualFold(strings.TrimSpace(r.Name), name) {
			return ErrRoleNameTaken
		}
	}
	return nil
}

// normalizeRoleInput: tên bỏ khoảng trắng hai đầu (1–100 ký tự, không ký tự điều khiển); quyền bỏ trùng, sắp, đều có
// trong Permissions.
func normalizeRoleInput(in RoleInput) (string, []Permission, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > maxRoleName || !utf8.ValidString(name) ||
		strings.ContainsFunc(name, unicode.IsControl) || len(in.Permissions) > maxRolePermissions {
		return "", nil, ErrInvalidInput
	}
	want := slices.Clone(in.Permissions)
	for _, p := range want {
		// Quyền web.* chỉ khóa dịch vụ giữ (ServiceKeyPermissions), vai trò nhân viên không nhận.
		if !IsKnown(p) || IsServiceKeyPermission(p) {
			return "", nil, ErrUnknownPermission
		}
	}
	slices.Sort(want)
	return name, nonNil(slices.Compact(want)), nil
}

// canChangePermissions: mọi quyền có ở đúng một trong before, after (thêm vào hay gỡ khỏi vai trò) thỏa
// Can(actor, p, all). Quyền lạ còn sót trong database (không có trong Permissions) không cho ai gì nên gỡ tự do.
func canChangePermissions(actor AuthUser, before, after []Permission) bool {
	all := Scope{Type: ScopeAll}
	for _, p := range before {
		if !slices.Contains(after, p) && IsKnown(p) && !Can(actor, p, &all) {
			return false
		}
	}
	for _, p := range after {
		if !slices.Contains(before, p) && !Can(actor, p, &all) {
			return false
		}
	}
	return true
}

// newRoleKey: "custom_" + 8 ký tự [a-z0-9] từ crypto/rand.
func newRoleKey() string {
	return customRolePrefix + strings.ToLower(rand.Text()[:customRoleKeyLen])
}

// PermissionInfo mô tả một quyền cho màn soạn vai trò: nhóm, nhãn tiếng Việt và loại phạm vi ("none": toàn shop từ
// gán bất kỳ; "shop": quyền quản trị, chỉ hiệu lực khi gán toàn shop; "warehouse", "page": theo kho, theo Page).
type PermissionInfo struct {
	Key       Permission
	ScopeType string
	Group     string
	Label     string
}

// Nhóm quyền theo thứ tự hiện trên màn Vai trò (mockup docs/mockups/m1/vai-tro.html).
const (
	groupCatalog   = "Sản phẩm"
	groupInventory = "Kho"
	groupOrder     = "Đơn hàng"
	groupShipping  = "Vận chuyển và tiền"
	groupCustomer  = "Khách hàng"
	groupChat      = "Hội thoại"
	groupPromo     = "Khuyến mại, báo cáo, cộng tác viên"
	groupFiles     = "Tệp"
	groupSystem    = "Hệ thống"
	groupAdmin     = "Quản trị"
)

func permissionGroups() []string {
	return []string{
		groupCatalog, groupInventory, groupOrder, groupShipping, groupCustomer, groupChat, groupPromo, groupFiles,
		groupSystem, groupAdmin,
	}
}

// permissionLabels là bảng nhãn của mọi quyền: nhóm, nhãn, đích của quyền theo đích (warehouse, page). Quyền mới của
// mốc sau thêm một dòng ở đây (test TestPermissionCatalog_Complete đỏ khi thiếu).
func permissionLabels() map[Permission][3]string {
	labels := map[Permission][3]string{
		"catalog.read":               {groupCatalog, "Xem sản phẩm", ""},
		"catalog.manage":             {groupCatalog, "Sửa sản phẩm, ảnh, phân loại", ""},
		"catalog.import":             {groupCatalog, "Nhập sản phẩm từ Excel", ""},
		"inventory.read":             {groupInventory, "Xem tồn kho và phiếu", ScopeWarehouse},
		"inventory.documents.create": {groupInventory, "Soạn phiếu kho", ScopeWarehouse},
		"inventory.documents.post":   {groupInventory, "Ghi sổ phiếu kho", ScopeWarehouse},
		"inventory.stocktake":        {groupInventory, "Đếm kiểm kho", ScopeWarehouse},
		"inventory.stocktake.apply":  {groupInventory, "Áp dụng kiểm kho", ScopeWarehouse},
		"inventory.suppliers.manage": {groupInventory, "Quản lý nhà cung cấp", ""},
		"order.create":               {groupOrder, "Lên đơn", ""},
		"order.read":                 {groupOrder, "Xem đơn", ""},
		"order.update":               {groupOrder, "Sửa đơn, ghi chú đơn", ""},
		"order.confirm":              {groupOrder, "Xác nhận đơn, chuyển chờ khách", ""},
		"order.cancel":               {groupOrder, "Hủy đơn", ""},
		"order.discount.approve":     {groupOrder, "Duyệt giảm giá vượt ngưỡng", ""},
		"order.return":               {groupOrder, "Tạo phiếu trả, đổi hàng", ""},
		"order.return.receive":       {groupOrder, "Nhận hàng trả về kho", ""},
		"order.duplicates.resolve":   {groupOrder, "Xử lý đơn trùng", ""},
		"order.settings.manage":      {groupOrder, "Cài đặt đơn, nguồn đơn", ""},
		"order.web_intake.manage":    {groupOrder, "Tạm dừng, mở lại nhận đơn web", ""},
		"order.requests.read":        {groupOrder, "Xem yêu cầu về đơn của khách", ""},
		"order.requests.manage":      {groupOrder, "Trả lời, xử lý yêu cầu về đơn", ""},
		"customer.read":              {groupCustomer, "Xem khách hàng", ""},
		"customer.write":             {groupCustomer, "Tạo, sửa hồ sơ khách", ""},
		"customer.merge":             {groupCustomer, "Gộp khách, hoàn tác gộp", ""},
		"customer.import":            {groupCustomer, "Nhập khách từ Excel", ""},
		"customer.tags.manage":       {groupCustomer, "Quản lý nhãn khách", ""},
		"customer.feedback.read":     {groupCustomer, "Xem góp ý của khách", ""},
		"customer.feedback.manage":   {groupCustomer, "Đổi trạng thái, ghi chú góp ý của khách", ""},
		"promotion.read":             {groupPromo, "Xem mã giảm giá", ""},
		"promotion.manage":           {groupPromo, "Tạo, sửa mã giảm giá và đợt mã", ""},
		"promotion.birthday.manage":  {groupPromo, "Cài đặt quà sinh nhật, tạo mã ngay, đánh dấu đã nhắn, xuất danh sách", ""},
		"promotion.flash.read":       {groupPromo, "Xem đợt flash sale", ""},
		"promotion.flash.manage":     {groupPromo, "Soạn đợt flash sale (tên, giờ, món, giá, số suất)", ""},
		"content.read":               {groupPromo, "Xem nội dung trang chủ website (banner)", ""},
		"content.manage":             {groupPromo, "Sửa nội dung trang chủ website (banner)", ""},
		// M6 (Task 225).
		"report.sales.read":     {groupPromo, "Xem báo cáo bán hàng", ScopePage},
		"report.inventory.read": {groupPromo, "Xem báo cáo tồn kho", ScopeWarehouse},
		"report.export":         {groupPromo, "Xuất báo cáo ra Excel", ""},
		"report.exports.manage": {groupPromo, "Xem, tải tệp xuất của mọi người", ""},
		"customer.export":       {groupCustomer, "Xuất danh sách khách ra Excel", ""},
		"fulfillment.read":      {groupShipping, "Xem vận đơn", ScopeWarehouse},
		"fulfillment.ship":      {groupShipping, "Tạo vận đơn, giao hàng", ScopeWarehouse},
		// M3 (Task M3-04).
		"fulfillment.carrier_accounts.read":   {groupShipping, "Xem kết nối hãng vận chuyển", ""},
		"fulfillment.carrier_accounts.manage": {groupShipping, "Quản lý kết nối hãng, khóa API, địa chỉ lấy hàng", ""},
		"fulfillment.settings.manage":         {groupShipping, "Cài đặt vận chuyển", ""},
		"fulfillment.quote":                   {groupShipping, "So phí các hãng", ScopeWarehouse},
		"fulfillment.cancel":                  {groupShipping, "Hủy vận đơn, đổi hãng", ScopeWarehouse},
		"fulfillment.print":                   {groupShipping, "In tem, phiếu nhặt hàng", ScopeWarehouse},
		"fulfillment.pack":                    {groupShipping, "Đánh dấu đóng gói", ScopeWarehouse},
		"fulfillment.manual_status":           {groupShipping, "Cập nhật tay trạng thái vận đơn", ScopeWarehouse},
		"fulfillment.correct":                 {groupShipping, "Sửa sai vận đơn, giao một phần", ""},
		"fulfillment.callbacks.work":          {groupShipping, "Gọi lại khách giao thất bại", ""},
		"cash.cod.read":                       {groupShipping, "Xem tiền COD, bảng kê, khoản hoàn", ""},
		"cash.statements.import":              {groupShipping, "Nhập bảng kê COD", ""},
		"cash.statements.confirm":             {groupShipping, "Chốt bảng kê COD", ""},
		"cash.adjust":                         {groupShipping, "Điều chỉnh sổ COD", ""},
		"cash.payments.record":                {groupShipping, "Ghi phiếu thu, cọc", ""},
		"cash.refunds.record":                 {groupShipping, "Ghi phiếu chi hoàn tiền", ""},
		"cash.refunds.waive":                  {groupShipping, "Đóng khoản hoàn không hoàn tiền", ""},
		"inventory.aged.read":                 {groupInventory, "Xem hàng giữ quá hạn", ScopeWarehouse},
		"conversation.read":                   {groupChat, "Xem hội thoại", ScopePage},
		// M4 (Task 143).
		"conversation.reply":           {groupChat, "Trả lời khách, comment, ghi chú, nhắc việc", ScopePage},
		"conversation.assign":          {groupChat, "Giao hội thoại cho nhân viên", ScopePage},
		"conversation.settings":        {groupChat, "Cài đặt hộp chat: nhãn, mẫu trả lời, tự động", ScopePage},
		"live.operate":                 {groupChat, "Chốt đơn từ comment Live", ScopePage},
		"channel.manage":               {groupChat, "Kết nối Page, bật gửi thật", ""},
		"files.upload":                 {groupFiles, "Tải tệp lên", ""},
		"platform.files.manage":        {groupFiles, "Kiểm tệp, tạo lại cỡ ảnh", ""},
		"realtime.subscribe":           {groupSystem, "Nhận cập nhật tức thì", ""},
		"platform.health.read":         {groupSystem, "Xem sức khỏe hệ thống", ""},
		"platform.jobs.manage":         {groupSystem, "Xử lý việc nền hỏng", ""},
		"audit.read":                   {groupSystem, "Xem nhật ký thao tác", ""},
		"identity.users.manage":        {groupAdmin, "Quản lý nhân viên", ""},
		"identity.roles.manage":        {groupAdmin, "Quản lý vai trò", ""},
		"identity.service_keys.manage": {groupAdmin, "Quản lý khóa cho website", ""},
	}
	maps.Copy(labels, launchPermissionLabels())
	return labels
}

// launchPermissionLabels là nhãn của quyền phần khởi động M7 (Task 260).
func launchPermissionLabels() map[Permission][3]string {
	return map[Permission][3]string{
		"catalog.launch.publish":         {groupCatalog, "Hiện, ẩn sản phẩm hàng loạt (mở bán)", ""},
		"inventory.stocktake.opening":    {groupInventory, "Kiểm kho mở sổ", ScopeWarehouse},
		"inventory.legacy_return.create": {groupInventory, "Nhập hàng hoàn của đơn cũ", ScopeWarehouse},
		"fulfillment.unmatched.read":     {groupShipping, "Xem sự kiện hãng của đơn ngoài hệ thống", ""},
		"conversation.t2.read":           {groupChat, "Xem báo cáo đối chiếu tin (chạy chỉ xem)", ScopePage},
		"channel.golive":                 {groupChat, "Mở gửi Page, trả Page về chỉ xem", ScopePage},
		"order.fallback.import":          {groupOrder, "Tải mẫu, nhập lại bảng tính đơn dự phòng", ""},
		"reporting.launch.read":          {groupPromo, "Xem báo cáo sẵn sàng, đêm mở, theo dõi sau mở", ""},
		"reporting.launch.run":           {groupPromo, "Chạy các bước của đêm mở", ""},
		"reporting.launch.decide":        {groupPromo, "Quyết đi/dừng, mở bán đêm mở", ""},
	}
}

// PermissionCatalog trả mọi quyền của Permissions kèm nhóm, nhãn, loại phạm vi; sắp theo nhóm (thứ tự của màn) rồi theo
// thứ tự trong Permissions. Quyền thiếu nhãn có Group, Label rỗng (test bắt).
func PermissionCatalog() []PermissionInfo {
	labels := permissionLabels()
	groups := permissionGroups()
	out := make([]PermissionInfo, 0, len(Permissions))
	for _, d := range Permissions {
		l := labels[d.Key]
		scope := "none"
		switch d.Scope {
		case ScopeShop:
			scope = "shop"
		case ScopeByTarget:
			scope = l[2]
		case ScopeNone:
		}
		out = append(out, PermissionInfo{Key: d.Key, ScopeType: scope, Group: l[0], Label: l[1]})
	}
	slices.SortStableFunc(out, func(a, b PermissionInfo) int {
		return cmp.Compare(groupRank(groups, a.Group), groupRank(groups, b.Group))
	})
	return out
}

func groupRank(groups []string, g string) int {
	if i := slices.Index(groups, g); i >= 0 {
		return i
	}
	return len(groups)
}
