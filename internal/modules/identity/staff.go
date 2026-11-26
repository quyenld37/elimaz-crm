package identity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/identity/internal/store"
)

// Lỗi của quản lý nhân viên (PQ07). Lớp HTTP đổi sang mã ở internal/httpapi/errors.go. Lỗi không mang email.
var (
	// ErrSelfModify: người thao tác sửa grant, khóa hay mở chính mình.
	ErrSelfModify = errors.New("identity: không được sửa quyền hay trạng thái của chính mình")
	// ErrGrantExceedsActor: người thao tác thiếu một quyền của vai trò được gán hay gỡ (trên đúng phạm vi đó), thiếu
	// quyền của vai trò mà người bị khóa/mở đang giữ, hoặc không có identity.users.manage.
	ErrGrantExceedsActor = errors.New("identity: thao tác vượt quá quyền của người thao tác")
	// ErrLastOwner: thao tác làm số Chủ còn hoạt động về 0.
	ErrLastOwner = errors.New("identity: không được khóa hay gỡ quyền của Chủ còn hoạt động cuối cùng")
	// ErrEmailTaken: email đã là nhân viên (ở tenant này hay tenant khác).
	ErrEmailTaken = errors.New("identity: email đã được dùng")
	// ErrVersionConflict: version gửi lên khác version hiện tại (người khác vừa sửa).
	ErrVersionConflict = errors.New("identity: nhân viên vừa được người khác sửa")
	// ErrStaffNotFound: không có nhân viên với mã này trong tenant.
	ErrStaffNotFound = errors.New("identity: không có nhân viên này")
	// ErrInvalidInput: email, tên, vai trò hay phạm vi không hợp lệ.
	ErrInvalidInput = errors.New("identity: dữ liệu nhân viên không hợp lệ")
)

const permUsersManage Permission = "identity.users.manage"

const (
	maxEmailLen         = 254
	maxDisplayName      = 200
	maxScopeID          = 128
	maxGrants           = 100
	statusLocked        = "locked"
	codeUniqueViolation = "23505"
	roleStatusActive    = "active"
)

// User là một nhân viên (không kèm grant).
type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Status      string // active, locked
	Version     int64
	CreatedAt   time.Time
}

// GrantInput là một grant theo vai trò: khóa vai trò và phạm vi (all; page hay warehouse kèm mã đích).
type GrantInput struct {
	RoleKey string
	Scope   Scope
}

// StaffRow là nhân viên kèm grant theo vai trò (sắp theo khóa vai trò, loại phạm vi, mã đích) và cảnh báo cho các
// gán có quyền quản trị không hiệu lực (ShopOnlyWarnings, cùng thứ tự với Grants).
type StaffRow struct {
	User
	Grants   []GrantInput
	Warnings []ScopeWarning
}

// Change là trạng thái trước và sau của một thao tác lên nhân viên (để ghi audit).
type Change struct {
	Before, After StaffRow
}

// ListStaff trả mọi nhân viên của tenant trong ngữ cảnh của tx, kèm grant, theo thứ tự tạo.
func ListStaff(ctx context.Context, tx pgx.Tx) ([]StaffRow, error) {
	q := store.New(tx)
	users, err := q.ListStaffUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("liệt kê nhân viên: %w", err)
	}
	grants, err := q.ListStaffGrants(ctx, pgtype.UUID{})
	if err != nil {
		return nil, fmt.Errorf("liệt kê grant của nhân viên: %w", err)
	}
	roles, err := ListRoles(ctx, tx)
	if err != nil {
		return nil, err
	}
	byUser := map[uuid.UUID][]GrantInput{}
	for _, g := range grants {
		byUser[g.UserID] = append(byUser[g.UserID], GrantInput{RoleKey: g.RoleKey, Scope: Scope{Type: g.ScopeType, ID: g.ScopeID}})
	}
	out := make([]StaffRow, 0, len(users))
	for _, u := range users {
		gs := nonNil(byUser[u.ID])
		out = append(out, StaffRow{
			User:     User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Status: u.Status, Version: u.Version, CreatedAt: u.CreatedAt},
			Grants:   gs,
			Warnings: ShopOnlyWarnings(roles, gs),
		})
	}
	return out, nil
}

// GetStaff trả một nhân viên kèm grant; không có (hay của tenant khác) → ErrStaffNotFound.
func GetStaff(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (StaffRow, error) {
	q := store.New(tx)
	u, err := q.GetStaffUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffRow{}, ErrStaffNotFound
	}
	if err != nil {
		return StaffRow{}, fmt.Errorf("đọc nhân viên: %w", err)
	}
	rows, err := q.ListStaffGrants(ctx, pgtype.UUID{Bytes: userID, Valid: true})
	if err != nil {
		return StaffRow{}, fmt.Errorf("đọc grant của nhân viên: %w", err)
	}
	grants := make([]GrantInput, 0, len(rows))
	for _, g := range rows {
		grants = append(grants, GrantInput{RoleKey: g.RoleKey, Scope: Scope{Type: g.ScopeType, ID: g.ScopeID}})
	}
	roles, err := ListRoles(ctx, tx)
	if err != nil {
		return StaffRow{}, err
	}
	return StaffRow{
		User:     User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Status: u.Status, Version: u.Version, CreatedAt: u.CreatedAt},
		Grants:   grants,
		Warnings: ShopOnlyWarnings(roles, grants),
	}, nil
}

// CreateStaff tạo nhân viên active chưa có grant (PQ03: chưa thấy gì) và tuyến email → tenant của actor, trong tx
// của tenant đó. Email chuẩn hóa chữ thường. Lỗi: ErrGrantExceedsActor (actor thiếu identity.users.manage),
// ErrInvalidInput, ErrEmailTaken (email đã có ở tenant này hay tenant khác).
func CreateStaff(ctx context.Context, tx pgx.Tx, actor AuthUser, email, displayName string) (User, error) {
	if !Can(actor, permUsersManage, nil) {
		return User{}, ErrGrantExceedsActor
	}
	key := NormalizeEmail(email)
	name := strings.TrimSpace(displayName)
	if !validEmail(key) || name == "" || utf8.RuneCountInString(name) > maxDisplayName {
		return User{}, ErrInvalidInput
	}
	q := store.New(tx)
	u, err := q.InsertStaffUser(ctx, store.InsertStaffUserParams{TenantID: actor.TenantID, Email: key, DisplayName: name})
	if isUniqueViolation(err) {
		return User{}, ErrEmailTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("ghi nhân viên: %w", err)
	}
	err = q.AddStaffRoute(ctx, key)
	if isUniqueViolation(err) {
		return User{}, ErrEmailTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("ghi tuyến email nhân viên: %w", err)
	}
	return User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Status: u.Status, Version: u.Version, CreatedAt: u.CreatedAt}, nil
}

// ReplaceGrants thay toàn bộ grant của userID bằng grants (trùng lặp được gộp) khi version còn khớp; version tăng 1.
// Luật (PQ07, vùng rủi ro cao), theo thứ tự:
//  1. actor không có identity.users.manage → ErrGrantExceedsActor; userID là chính actor → ErrSelfModify;
//  2. vai trò lạ, phạm vi sai → ErrInvalidInput; không có nhân viên → ErrStaffNotFound; version khác →
//     ErrVersionConflict;
//  3. khóa FOR SHARE (id tăng dần) vai trò của mọi grant được thêm hay gỡ rồi đọc lại quyền của chúng (lần sửa vai trò
//     chạy cùng lúc chờ hoặc đã xong, Task M1-56); grant được thêm của vai trò đã lưu trữ → ErrRoleArchived (giữ hay
//     gỡ grant cũ của vai trò đã lưu trữ vẫn được);
//  4. mỗi grant được thêm hay gỡ: mọi quyền của vai trò phải thỏa Can(actor, p, &phạm vi) → ngược lại
//     ErrGrantExceedsActor;
//  5. sau khi đổi, số Chủ còn hoạt động bằng 0 → ErrLastOwner;
//  6. kiểm lại bước 4 với grant của actor đọc lại trong transaction (actor vừa bị hạ quyền → ErrGrantExceedsActor,
//     vừa bị khóa → ErrLocked).
//
// Trước khi đọc, khóa dòng users của userID, actor và mọi Chủ còn hoạt động theo id tăng dần (ADR-46 nhóm 2: users rồi
// roles), nên hai
// Chủ thao tác lên nhau cùng lúc thì bên sau thấy kết quả của bên trước. Lỗi trả về thì tx phải lùi (bên gọi trả lỗi
// từ hàm của WithTenant/WriteTx).
func ReplaceGrants(ctx context.Context, tx pgx.Tx, actor AuthUser, userID uuid.UUID, version int64, grants []GrantInput) (Change, error) {
	if len(grants) > maxGrants {
		return Change{}, ErrInvalidInput
	}
	want := normalizeGrants(grants)
	st, err := begin(ctx, tx, actor, userID, func(index map[string]RoleRow) error {
		for _, g := range want {
			if _, ok := index[g.RoleKey]; !ok || !validScope(g.Scope) {
				return ErrInvalidInput
			}
		}
		return nil
	})
	if err != nil {
		return Change{}, err
	}
	q, locked := st.q, st.locked
	if locked[userID].Version != version {
		return Change{}, ErrVersionConflict
	}
	before, err := GetStaff(ctx, tx, userID)
	if err != nil {
		return Change{}, err
	}
	changed := symmetricDiff(before.Grants, want)
	index, err := st.lockRoles(ctx, tx, changed, want)
	if err != nil {
		return Change{}, err
	}
	st.index = index
	if !canTouch(actor, index, changed) {
		return Change{}, ErrGrantExceedsActor
	}
	for _, g := range changed {
		if slices.Contains(want, g) {
			err = q.AddUserRoleGrant(ctx, store.AddUserRoleGrantParams{
				TenantID: actor.TenantID, UserID: userID, RoleID: index[g.RoleKey].ID, ScopeType: g.Scope.Type, ScopeID: g.Scope.ID,
			})
		} else {
			err = q.DeleteUserRoleGrant(ctx, store.DeleteUserRoleGrantParams{
				UserID: userID, RoleKey: g.RoleKey, ScopeType: g.Scope.Type, ScopeID: g.Scope.ID,
			})
		}
		if err != nil {
			return Change{}, fmt.Errorf("đổi grant %s của nhân viên: %w", g.RoleKey, err)
		}
	}
	if _, err := q.BumpStaffVersion(ctx, store.BumpStaffVersionParams{ID: userID, Version: version}); err != nil {
		return Change{}, fmt.Errorf("tăng version của nhân viên: %w", err)
	}
	return st.finish(ctx, tx, actor, before, changed)
}

// SetLocked khóa (locked = true) hay mở userID; version tăng 1. Luật như ReplaceGrants: identity.users.manage,
// không sửa chính mình (ErrSelfModify), actor phải có mọi quyền của mọi vai trò userID đang giữ trên đúng phạm vi
// (ErrGrantExceedsActor), không làm số Chủ còn hoạt động về 0 (ErrLastOwner), kiểm lại quyền của actor đọc trong
// transaction (ErrGrantExceedsActor, ErrLocked). Không có nhân viên → ErrStaffNotFound.
func SetLocked(ctx context.Context, tx pgx.Tx, actor AuthUser, userID uuid.UUID, locked bool) (Change, error) {
	st, err := begin(ctx, tx, actor, userID, nil)
	if err != nil {
		return Change{}, err
	}
	q, index := st.q, st.index
	before, err := GetStaff(ctx, tx, userID)
	if err != nil {
		return Change{}, err
	}
	if !canTouch(actor, index, before.Grants) {
		return Change{}, ErrGrantExceedsActor
	}
	status := statusActive
	if locked {
		status = statusLocked
	}
	if _, err := q.SetStaffStatus(ctx, store.SetStaffStatusParams{ID: userID, Status: status}); err != nil {
		return Change{}, fmt.Errorf("đổi trạng thái nhân viên: %w", err)
	}
	return st.finish(ctx, tx, actor, before, before.Grants)
}

// lockRoles khóa FOR SHARE (theo id tăng dần, sau khi đã khóa users) vai trò của các grant changed, từ chối grant mới
// (có trong want) của vai trò đã lưu trữ (ErrRoleArchived), rồi đọc lại vai trò (quyền sau mọi lần sửa đã commit).
func (st staffTx) lockRoles(ctx context.Context, tx pgx.Tx, changed, want []GrantInput) (map[string]RoleRow, error) {
	ids := make([]uuid.UUID, 0, len(changed))
	for _, g := range changed {
		if r, ok := st.index[g.RoleKey]; ok && !slices.Contains(ids, r.ID) {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return st.index, nil
	}
	rows, err := st.q.ShareLockRoles(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("khóa vai trò được gán: %w", err)
	}
	archived := map[uuid.UUID]bool{}
	for _, r := range rows {
		archived[r.ID] = r.Status == roleStatusArchived
	}
	for _, g := range changed {
		if slices.Contains(want, g) && archived[st.index[g.RoleKey].ID] {
			return nil, ErrRoleArchived
		}
	}
	roles, err := ListRoles(ctx, tx)
	if err != nil {
		return nil, err
	}
	return roleIndex(roles), nil
}

// staffTx là phần chung đầu mỗi thao tác lên nhân viên: câu truy vấn, vai trò theo khóa, dòng users đã khóa.
type staffTx struct {
	q      *store.Queries
	index  map[string]RoleRow
	locked map[uuid.UUID]store.LockStaffRowsRow
}

// begin kiểm actor (checkActor), đọc vai trò, chạy validate (nếu có) rồi khóa dòng users (lockStaff).
func begin(ctx context.Context, tx pgx.Tx, actor AuthUser, userID uuid.UUID, validate func(map[string]RoleRow) error) (staffTx, error) {
	if err := checkActor(actor, userID); err != nil {
		return staffTx{}, err
	}
	roles, err := ListRoles(ctx, tx)
	if err != nil {
		return staffTx{}, err
	}
	index := roleIndex(roles)
	if validate != nil {
		if err := validate(index); err != nil {
			return staffTx{}, err
		}
	}
	q := store.New(tx)
	locked, err := lockStaff(ctx, q, userID, actor.UserID)
	if err != nil {
		return staffTx{}, err
	}
	return staffTx{q: q, index: index, locked: locked}, nil
}

func checkActor(actor AuthUser, userID uuid.UUID) error {
	switch {
	case !Can(actor, permUsersManage, nil):
		return ErrGrantExceedsActor
	case userID == actor.UserID:
		return ErrSelfModify
	}
	return nil
}

// lockStaff khóa dòng users của target, actor và mọi Chủ còn hoạt động (LockStaffRows); target không có →
// ErrStaffNotFound.
func lockStaff(ctx context.Context, q *store.Queries, target, actor uuid.UUID) (map[uuid.UUID]store.LockStaffRowsRow, error) {
	rows, err := q.LockStaffRows(ctx, []uuid.UUID{target, actor})
	if err != nil {
		return nil, fmt.Errorf("khóa dòng nhân viên: %w", err)
	}
	m := make(map[uuid.UUID]store.LockStaffRowsRow, len(rows))
	for _, r := range rows {
		m[r.ID] = r
	}
	if _, ok := m[target]; !ok {
		return nil, ErrStaffNotFound
	}
	return m, nil
}

// finish chạy afterChecks cho các grant touched rồi đọc trạng thái sau của nhân viên before.ID.
func (st staffTx) finish(ctx context.Context, tx pgx.Tx, actor AuthUser, before StaffRow, touched []GrantInput) (Change, error) {
	if err := afterChecks(ctx, st.q, actor, st.locked, st.index, touched); err != nil {
		return Change{}, err
	}
	after, err := GetStaff(ctx, tx, before.ID)
	if err != nil {
		return Change{}, err
	}
	return Change{Before: before, After: after}, nil
}

// afterChecks chạy sau khi đã đổi trong tx: luật Chủ cuối cùng, rồi quyền của actor đọc lại trong transaction (dòng
// của actor đã khóa ở lockStaff nên không ai đổi được nó tới khi tx xong).
func afterChecks(ctx context.Context, q *store.Queries, actor AuthUser, locked map[uuid.UUID]store.LockStaffRowsRow,
	index map[string]RoleRow, touched []GrantInput,
) error {
	owners, err := q.CountActiveOwners(ctx)
	if err != nil {
		return fmt.Errorf("đếm Chủ còn hoạt động: %w", err)
	}
	if owners == 0 {
		return ErrLastOwner
	}
	if row, ok := locked[actor.UserID]; !ok || row.Status != statusActive {
		return ErrLocked
	}
	fresh, err := freshGrants(ctx, q, actor)
	if err != nil {
		return err
	}
	if !Can(fresh, permUsersManage, nil) || !canTouch(fresh, index, touched) {
		return ErrGrantExceedsActor
	}
	return nil
}

// freshGrants đọc lại grant của actor trong transaction (vai trò đã lưu trữ không tính, như LoadUser).
func freshGrants(ctx context.Context, q *store.Queries, actor AuthUser) (AuthUser, error) {
	rows, err := q.ListUserGrants(ctx, actor.UserID)
	if err != nil {
		return AuthUser{}, fmt.Errorf("đọc lại quyền của người thao tác: %w", err)
	}
	fresh := AuthUser{UserID: actor.UserID, TenantID: actor.TenantID, Email: actor.Email, Grants: make([]Grant, 0, len(rows))}
	for _, r := range rows {
		fresh.Grants = append(fresh.Grants, Grant{Permission: Permission(r.Permission), Scope: Scope{Type: r.ScopeType, ID: r.ScopeID}})
	}
	return fresh, nil
}

// canTouch: actor có mọi quyền của vai trò của từng grant trên đúng phạm vi của grant đó. Vai trò không còn trong
// index thì không ai chạm được (an toàn: từ chối).
func canTouch(actor AuthUser, index map[string]RoleRow, grants []GrantInput) bool {
	for _, g := range grants {
		r, ok := index[g.RoleKey]
		if !ok || !hasAllPermissions(actor, r, g.Scope) {
			return false
		}
	}
	return true
}

// normalizeGrants bỏ trùng và sắp như GetStaff (khóa vai trò, loại phạm vi, mã đích).
func normalizeGrants(in []GrantInput) []GrantInput {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b GrantInput) int {
		return cmp.Or(cmp.Compare(a.RoleKey, b.RoleKey), cmp.Compare(a.Scope.Type, b.Scope.Type), cmp.Compare(a.Scope.ID, b.Scope.ID))
	})
	return nonNil(slices.Compact(out))
}

// symmetricDiff: grant có ở đúng một trong hai danh sách (được thêm hoặc bị gỡ).
func symmetricDiff(a, b []GrantInput) []GrantInput {
	var out []GrantInput
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	for _, x := range b {
		if !slices.Contains(a, x) {
			out = append(out, x)
		}
	}
	return out
}

func validScope(s Scope) bool {
	switch s.Type {
	case ScopeAll:
		return s.ID == ""
	case ScopePage, ScopeWarehouse:
		return s.ID != "" && len(s.ID) <= maxScopeID && strings.TrimSpace(s.ID) == s.ID
	}
	return false
}

// validEmail: kiểm hình thức tối thiểu (một @, hai phần khác rỗng, không khoảng trắng); Cloudflare Access mới là
// nơi xác minh email thật.
func validEmail(e string) bool {
	local, domain, ok := strings.Cut(e, "@")
	return ok && local != "" && domain != "" && !strings.Contains(domain, "@") && len(e) <= maxEmailLen &&
		!strings.ContainsAny(e, " \t\r\n")
}

func isUniqueViolation(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == codeUniqueViolation
}

// nonNil: lát rỗng khác nil (để JSON và so sánh không phân biệt nil với rỗng).
func nonNil[T any](g []T) []T {
	if g == nil {
		return []T{}
	}
	return g
}

// StaffHas cho biết userID là nhân viên đang hoạt động của tenant trong tx và có quyền p trên phạm vi bất kỳ (đọc lại
// grant trong tx, vai trò đã lưu trữ không tính; kiểm như Can không đích). Không có nhân viên, bị khóa → false, nil.
func StaffHas(ctx context.Context, tx pgx.Tx, userID uuid.UUID, p Permission) (bool, error) {
	q := store.New(tx)
	u, err := q.GetStaffUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("đọc nhân viên: %w", err)
	}
	if u.Status != statusActive {
		return false, nil
	}
	fresh, err := freshGrants(ctx, q, AuthUser{UserID: userID})
	if err != nil {
		return false, err
	}
	return Can(fresh, p, nil), nil
}

// StaffWithPermissionAt trả nhân viên đang hoạt động của tenant trong tx có quyền p (qua vai trò còn active) ở phạm vi
// all hoặc đúng target, sắp theo tên hiển thị (Task 160: người được giao hội thoại của một Page). Không đọc grant từ
// bộ nhớ: đọc trong tx nên khóa tài khoản, gỡ quyền có hiệu lực ngay.
func StaffWithPermissionAt(ctx context.Context, tx pgx.Tx, p Permission, target Scope) ([]User, error) {
	rows, err := store.New(tx).ListStaffWithPermissionAt(ctx, store.ListStaffWithPermissionAtParams{
		Permission: string(p), ScopeType: target.Type, ScopeID: target.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("liệt kê nhân viên có quyền %s: %w", p, err)
	}
	out := make([]User, 0, len(rows))
	for _, u := range rows {
		out = append(out, User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Status: u.Status, Version: u.Version, CreatedAt: u.CreatedAt})
	}
	return out, nil
}
