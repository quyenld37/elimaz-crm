package identity_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// staffEnv: tenant có vai trò mẫu (EnsureDefaultRoles), Chủ A, Chủ B, Quản lý M (phạm vi all) và Kho K
// (warehouse:w1) — bộ dữ liệu của kế hoạch Task M1-26 Step 1.
type staffEnv struct {
	t      *testing.T
	s      *testkit.Staff
	tenant uuid.UUID
	ids    map[string]uuid.UUID
}

const (
	emailA = "a@elimaz.test"
	emailB = "b@elimaz.test"
	emailM = "m@elimaz.test"
	emailK = "k@elimaz.test"
)

func newStaffEnv(t *testing.T) *staffEnv {
	t.Helper()
	s := testkit.OpenStaff(t)
	e := &staffEnv{t: t, s: s, ids: map[string]uuid.UUID{}}
	e.tenant = s.Tenant(t, "A")
	e.seedTenant(e.tenant)
	e.ids[emailA] = e.user(e.tenant, emailA, "owner", identity.ScopeAll, "")
	e.ids[emailB] = e.user(e.tenant, emailB, "owner", identity.ScopeAll, "")
	e.ids[emailM] = e.user(e.tenant, emailM, "manager", identity.ScopeAll, "")
	e.ids[emailK] = e.user(e.tenant, emailK, "warehouse", identity.ScopeWarehouse, "w1")
	return e
}

func (e *staffEnv) seedTenant(tenant uuid.UUID) {
	e.t.Helper()
	e.s.Exec(e.t, tenant, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(e.t.Context(), tx, tenant) })
}

// user chèn nhân viên đang hoạt động và gán vai trò mẫu roleKey (rỗng: không gán).
func (e *staffEnv) user(tenant uuid.UUID, email, roleKey, scopeType, scopeID string) uuid.UUID {
	e.t.Helper()
	id := e.s.User(e.t, tenant, email, "active")
	if roleKey != "" {
		e.grantRole(tenant, id, roleKey, scopeType, scopeID)
	}
	return id
}

// grantRole gán vai trò có sẵn theo khóa bằng chủ bảng (dựng dữ liệu, không qua luật).
func (e *staffEnv) grantRole(tenant, userID uuid.UUID, roleKey, scopeType, scopeID string) {
	e.t.Helper()
	e.s.Exec(e.t, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `INSERT INTO identity.user_role_grants (tenant_id, user_id, role_id, scope_type, scope_id)
			SELECT $1, $2, id, $4, $5 FROM identity.roles WHERE key = $3`, tenant, userID, roleKey, scopeType, scopeID)
		return err //nolint:wrapcheck // test
	})
}

// actor nạp nhân viên như lớp HTTP (LoadUser bằng crm_app).
func (e *staffEnv) actor(email string) identity.AuthUser {
	e.t.Helper()
	u, err := identity.LoadUser(e.t.Context(), e.s.App, email)
	if err != nil {
		e.t.Fatalf("nạp %s: %v", email, err)
	}
	return u
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant của e.
func (e *staffEnv) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(context.Background(), e.s.App, e.tenant, fn) //nolint:wrapcheck // test
}

func (e *staffEnv) staff(id uuid.UUID) identity.StaffRow {
	e.t.Helper()
	var row identity.StaffRow
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		row, err = identity.GetStaff(context.Background(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("đọc nhân viên: %v", err)
	}
	return row
}

func (e *staffEnv) replace(actor identity.AuthUser, target uuid.UUID, grants ...identity.GrantInput) error {
	e.t.Helper()
	v := e.staff(target).Version
	return e.app(func(tx pgx.Tx) error {
		_, err := identity.ReplaceGrants(context.Background(), tx, actor, target, v, grants)
		return err //nolint:wrapcheck // test
	})
}

func (e *staffEnv) lock(actor identity.AuthUser, target uuid.UUID, locked bool) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := identity.SetLocked(context.Background(), tx, actor, target, locked)
		return err //nolint:wrapcheck // test
	})
}

// activeOwners đếm nhân viên active có vai trò owner phạm vi all.
func (e *staffEnv) activeOwners(tenant uuid.UUID) int {
	e.t.Helper()
	var n int
	e.s.Exec(e.t, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(DISTINCT u.id) FROM identity.users u
			JOIN identity.user_role_grants g ON g.user_id = u.id AND g.scope_type = 'all'
			JOIN identity.roles r ON r.id = g.role_id AND r.key = 'owner'
			WHERE u.status = 'active'`).Scan(&n)
	})
	return n
}

func g(roleKey, scopeType, scopeID string) identity.GrantInput {
	return identity.GrantInput{RoleKey: roleKey, Scope: identity.Scope{Type: scopeType, ID: scopeID}}
}

func wantErr(t *testing.T, name string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: lỗi %v, mong %v", name, err, want)
	}
}

func TestReplaceGrants_ExceedsActor(t *testing.T) {
	e := newStaffEnv(t)
	// M (Quản lý) được cấp tạm identity.users.manage phạm vi all cho các ca dưới.
	e.s.Grant(t, e.tenant, e.ids[emailM], []string{"identity.users.manage"}, identity.ScopeAll, "")
	m := e.actor(emailM)
	k := e.ids[emailK]

	wantErr(t, "M gán owner cho K", e.replace(m, k, g("owner", identity.ScopeAll, "")), identity.ErrGrantExceedsActor)
	if err := e.replace(m, k, g("warehouse", identity.ScopeWarehouse, "w1"), g("sale", identity.ScopeWarehouse, "w2")); err != nil {
		t.Fatalf("M gán warehouse, sale theo kho cho K: %v", err)
	}
	got := e.staff(k).Grants
	want := []identity.GrantInput{g("sale", identity.ScopeWarehouse, "w2"), g("warehouse", identity.ScopeWarehouse, "w1")}
	if !slices.Equal(got, want) {
		t.Fatalf("grant của K = %v, mong %v", got, want)
	}
	// Gỡ cũng phải trong quyền của người gỡ: M không gỡ được owner của A (M thiếu identity.roles.manage, platform.*).
	wantErr(t, "M gỡ owner của A", e.replace(m, e.ids[emailA]), identity.ErrGrantExceedsActor)
	wantErr(t, "M khóa A", e.lock(m, e.ids[emailA], true), identity.ErrGrantExceedsActor)
	// Không đổi gì ở A.
	if a := e.staff(e.ids[emailA]); a.Status != "active" || len(a.Grants) != 1 {
		t.Fatalf("A bị đổi: %+v", a)
	}
	// K không có identity.users.manage: module cũng từ chối (không chỉ guard).
	wantErr(t, "K gán cho M", e.replace(e.actor(emailK), e.ids[emailM]), identity.ErrGrantExceedsActor)
}

func TestReplaceGrants_SelfModify(t *testing.T) {
	e := newStaffEnv(t)
	a := e.actor(emailA)
	wantErr(t, "A sửa grant của A", e.replace(a, e.ids[emailA], g("owner", identity.ScopeAll, ""), g("sale", identity.ScopeAll, "")), identity.ErrSelfModify)
	wantErr(t, "A khóa A", e.lock(a, e.ids[emailA], true), identity.ErrSelfModify)
	wantErr(t, "A mở A", e.lock(a, e.ids[emailA], false), identity.ErrSelfModify)
}

func TestReplaceGrants_VersionAndInput(t *testing.T) {
	e := newStaffEnv(t)
	a := e.actor(emailA)
	k := e.ids[emailK]
	v := e.staff(k).Version
	if err := e.app(func(tx pgx.Tx) error {
		_, err := identity.ReplaceGrants(context.Background(), tx, a, k, v, []identity.GrantInput{g("sale", identity.ScopeAll, "")})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if nv := e.staff(k).Version; nv != v+1 {
		t.Fatalf("version sau khi thay = %d, mong %d", nv, v+1)
	}
	err := e.app(func(tx pgx.Tx) error {
		_, err := identity.ReplaceGrants(context.Background(), tx, a, k, v, nil)
		return err //nolint:wrapcheck // test
	})
	wantErr(t, "version cũ", err, identity.ErrVersionConflict)
	for name, in := range map[string]identity.GrantInput{
		"vai trò lạ":        g("khong-co", identity.ScopeAll, ""),
		"phạm vi lạ":        g("sale", "shop", "x"),
		"all có mã đích":    g("sale", identity.ScopeAll, "x"),
		"kho thiếu mã đích": g("warehouse", identity.ScopeWarehouse, ""),
	} {
		wantErr(t, name, e.replace(a, k, in), identity.ErrInvalidInput)
	}
	wantErr(t, "nhân viên không có", e.app(func(tx pgx.Tx) error {
		_, err := identity.ReplaceGrants(context.Background(), tx, a, uuid.New(), 1, nil)
		return err //nolint:wrapcheck // test
	}), identity.ErrStaffNotFound)
}

func TestLock_LastOwner(t *testing.T) {
	e := newStaffEnv(t)
	a := e.actor(emailA)
	b := e.actor(emailB)
	if err := e.lock(a, e.ids[emailB], true); err != nil {
		t.Fatalf("A khóa B: %v", err)
	}
	if st := e.staff(e.ids[emailB]); st.Status != "locked" {
		t.Fatalf("B chưa bị khóa: %+v", st)
	}
	// B (đã bị khóa, AuthUser nạp từ trước) khóa A, Chủ hoạt động cuối cùng → ErrLastOwner.
	wantErr(t, "khóa Chủ cuối cùng", e.lock(b, e.ids[emailA], true), identity.ErrLastOwner)
	wantErr(t, "gỡ owner của Chủ cuối cùng", e.replace(b, e.ids[emailA], g("sale", identity.ScopeAll, "")), identity.ErrLastOwner)
	if n := e.activeOwners(e.tenant); n != 1 {
		t.Fatalf("số Chủ hoạt động = %d, mong 1", n)
	}
	// Người thao tác đã bị khóa không làm được việc khác (grant đọc lại trong transaction).
	wantErr(t, "B đã khóa gán cho K", e.replace(b, e.ids[emailK], g("sale", identity.ScopeAll, "")), identity.ErrLocked)
	// A mở khóa B.
	if err := e.lock(a, e.ids[emailB], false); err != nil {
		t.Fatalf("A mở B: %v", err)
	}
	if n := e.activeOwners(e.tenant); n != 2 {
		t.Fatalf("số Chủ hoạt động = %d, mong 2", n)
	}
}

// Hai Chủ cùng lúc khóa nhau, hoặc cùng lúc gỡ owner của nhau (50 lượt mỗi loại): không lượt nào còn 0 Chủ hoạt
// động; đúng một bên thành công, bên kia ErrLastOwner.
func TestLock_LastOwnerConcurrent(t *testing.T) {
	e := newStaffEnv(t)
	ida, idb := e.ids[emailA], e.ids[emailB]
	reset := func() {
		for _, id := range []uuid.UUID{ida, idb} {
			e.s.SetStatus(t, e.tenant, id, "active")
			e.grantRoleIfMissing(id)
		}
	}
	ops := map[string]func(actor identity.AuthUser, target uuid.UUID, v int64) error{
		"khóa": func(actor identity.AuthUser, target uuid.UUID, _ int64) error { return e.lock(actor, target, true) },
		"gỡ owner": func(actor identity.AuthUser, target uuid.UUID, v int64) error {
			return e.app(func(tx pgx.Tx) error {
				_, err := identity.ReplaceGrants(context.Background(), tx, actor, target, v, []identity.GrantInput{g("sale", identity.ScopeAll, "")})
				return err //nolint:wrapcheck // test
			})
		},
	}
	for name, op := range ops {
		for round := range 50 {
			reset()
			a, b := e.actor(emailA), e.actor(emailB)
			va, vb := e.staff(ida).Version, e.staff(idb).Version
			var wg sync.WaitGroup
			start := make(chan struct{})
			errs := make([]error, 2)
			wg.Go(func() { <-start; errs[0] = op(a, idb, vb) })
			wg.Go(func() { <-start; errs[1] = op(b, ida, va) })
			close(start)
			wg.Wait()
			ok := 0
			for _, err := range errs {
				switch {
				case err == nil:
					ok++
				case !errors.Is(err, identity.ErrLastOwner):
					t.Fatalf("%s lượt %d: lỗi lạ %v", name, round, err)
				}
			}
			if ok != 1 {
				t.Fatalf("%s lượt %d: %d bên thành công (%v), mong 1", name, round, ok, errs)
			}
			if n := e.activeOwners(e.tenant); n != 1 {
				t.Fatalf("%s lượt %d: %d Chủ hoạt động, mong 1", name, round, n)
			}
		}
	}
}

func (e *staffEnv) grantRoleIfMissing(id uuid.UUID) {
	e.t.Helper()
	e.s.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `INSERT INTO identity.user_role_grants (tenant_id, user_id, role_id, scope_type, scope_id)
			SELECT $1, $2, id, 'all', '' FROM identity.roles WHERE key = 'owner' ON CONFLICT DO NOTHING`, e.tenant, id)
		return err //nolint:wrapcheck // test
	})
}

func TestCreateStaff(t *testing.T) {
	e := newStaffEnv(t)
	a := e.actor(emailA)
	other := e.s.Tenant(t, "B")
	e.s.User(t, other, "taken@elimaz.test", "active")

	var u identity.User
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		u, err = identity.CreateStaff(context.Background(), tx, a, "  Moi@Elimaz.test ", "Nhân viên mới")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if u.Email != "moi@elimaz.test" || u.DisplayName != "Nhân viên mới" || u.Status != "active" || u.Version != 1 {
		t.Fatalf("nhân viên mới: %+v", u)
	}
	// Có tuyến email → tenant A; chưa có grant.
	if id, ok, err := tenancy.Resolve(t.Context(), e.s.App, identity.RouteKindStaffEmail, "moi@elimaz.test"); err != nil || !ok || id != e.tenant {
		t.Fatalf("tuyến email: %v %v %v", id, ok, err)
	}
	if st := e.staff(u.ID); len(st.Grants) != 0 {
		t.Fatalf("nhân viên mới không được có grant: %+v", st)
	}
	if mu := e.actor("moi@elimaz.test"); len(mu.Grants) != 0 {
		t.Fatalf("AuthUser của nhân viên mới có grant: %+v", mu)
	}

	create := func(email string) error {
		return e.app(func(tx pgx.Tx) error {
			_, err := identity.CreateStaff(context.Background(), tx, a, email, "x")
			return err //nolint:wrapcheck // test
		})
	}
	wantErr(t, "email đã có tuyến ở tenant khác", create("Taken@elimaz.test"), identity.ErrEmailTaken)
	wantErr(t, "email đã có trong tenant", create("moi@elimaz.test"), identity.ErrEmailTaken)
	wantErr(t, "email rỗng", create("  "), identity.ErrInvalidInput)
	wantErr(t, "email sai", create("khong-co-a-cong"), identity.ErrInvalidInput)
	wantErr(t, "K không có identity.users.manage", e.app(func(tx pgx.Tx) error {
		_, err := identity.CreateStaff(context.Background(), tx, e.actor(emailK), "k2@elimaz.test", "x")
		return err //nolint:wrapcheck // test
	}), identity.ErrGrantExceedsActor)
}

func TestAddStaffRoute(t *testing.T) {
	e := newStaffEnv(t)
	other := e.s.Tenant(t, "B")
	ctx := t.Context()
	// Trong ngữ cảnh tenant B, hàm chỉ ghi được tuyến cho B (không có tham số tenant).
	if err := tenancy.WithTenant(ctx, e.s.App, other, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT platform.add_staff_route('Route@Elimaz.test')")
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if id, ok, _ := tenancy.Resolve(ctx, e.s.App, identity.RouteKindStaffEmail, "route@elimaz.test"); !ok || id != other {
		t.Fatalf("tuyến phải trỏ tenant B, có %v %v", id, ok)
	}
	// Thiếu ngữ cảnh tenant → lỗi.
	if _, err := e.s.App.Pool.Exec(ctx, "SELECT platform.add_staff_route('x@elimaz.test')"); err == nil {
		t.Fatal("gọi ngoài ngữ cảnh tenant phải lỗi")
	}
	// crm_app không chèn thẳng được vào platform.tenant_routes.
	err := tenancy.WithTenant(ctx, e.s.App, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('staff_email', 'y@elimaz.test', $1)", e.tenant)
		return err //nolint:wrapcheck // test
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "42501" {
		t.Fatalf("crm_app INSERT tenant_routes: mong 42501, có %v", err)
	}
}

func TestUsersView(t *testing.T) {
	e := newStaffEnv(t)
	ctx := t.Context()
	var cols []string
	if err := e.s.Owner.Pool.QueryRow(ctx, `SELECT array_agg(column_name::text ORDER BY ordinal_position)
		FROM information_schema.columns WHERE table_schema = 'identity' AND table_name = 'users_v'`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cols, []string{"id", "display_name", "status"}) {
		t.Fatalf("cột của identity.users_v = %v", cols)
	}
	var opts []string
	if err := e.s.Owner.Pool.QueryRow(ctx, "SELECT coalesce(reloptions, '{}') FROM pg_class WHERE oid = 'identity.users_v'::regclass").Scan(&opts); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(opts, "security_invoker=true") {
		t.Fatalf("identity.users_v thiếu security_invoker: %v", opts)
	}
	other := e.s.Tenant(t, "B")
	e.s.User(t, other, "khac@elimaz.test", "active")
	count := func(tenant uuid.UUID) int {
		var n int
		if err := tenancy.WithTenant(ctx, e.s.App, tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM identity.users_v").Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if a, b := count(e.tenant), count(other); a != 4 || b != 1 {
		t.Fatalf("users_v theo tenant: A=%d (mong 4), B=%d (mong 1)", a, b)
	}
}

func TestListStaff(t *testing.T) {
	e := newStaffEnv(t)
	var rows []identity.StaffRow
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		rows, err = identity.ListStaff(context.Background(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("mong 4 nhân viên, có %d", len(rows))
	}
	for _, r := range rows {
		if r.ID == e.ids[emailK] {
			if !slices.Equal(r.Grants, []identity.GrantInput{g("warehouse", identity.ScopeWarehouse, "w1")}) ||
				!strings.EqualFold(r.Email, emailK) || r.Version != 1 {
				t.Fatalf("dòng của K: %+v", r)
			}
		}
	}
}

// Người thao tác bị gỡ identity.users.manage sau khi đã đọc quyền (thẻ cũ trong yêu cầu đang chạy): lần kiểm lại sau khi
// khóa dòng đọc quyền mới và từ chối, dù vai trò được gán vẫn nằm trong quyền còn lại của người đó (mutation testing
// Task 23: staff.go afterChecks `!Can(...) || !canTouch(...)` còn sống).
func TestReplaceGrants_ActorLostManageMeanwhile(t *testing.T) {
	e := newStaffEnv(t)
	e.s.Grant(t, e.tenant, e.ids[emailM], []string{"identity.users.manage"}, identity.ScopeAll, "")
	m := e.actor(emailM) // còn identity.users.manage
	if err := e.replace(e.actor(emailB), e.ids[emailM], g("sale", identity.ScopeAll, "")); err != nil {
		t.Fatalf("B đổi M thành sale: %v", err)
	}
	// K giữ nguyên grant warehouse w1 đang có; chỉ thêm sale w2, vai trò mà quyền còn lại của M (sale all) vẫn chạm được.
	before := e.staff(e.ids[emailK]).Grants
	add := append(slices.Clone(before), g("sale", identity.ScopeWarehouse, "w2"))
	wantErr(t, "M (đã mất quyền) thêm sale cho K", e.replace(m, e.ids[emailK], add...), identity.ErrGrantExceedsActor)
	if got := e.staff(e.ids[emailK]).Grants; !slices.Equal(got, before) {
		t.Fatalf("grant của K bị đổi: %v, mong %v", got, before)
	}
}
