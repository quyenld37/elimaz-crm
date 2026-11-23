package identity_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Khóa dịch vụ của website (kế hoạch M5 Task 189, M5-06; ADR-18).

var fullKeyPattern = regexp.MustCompile(`^ek_[a-z2-7]{12}_[a-z2-7]{52}$`)

// seq trả n byte cố định bắt đầu từ b (để hai lần sinh cùng đầu vào cho cùng khóa).
func seq(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b + byte(i)
	}
	return out
}

func TestGenerateServiceKey_Format(t *testing.T) {
	t.Parallel()
	full, prefix, hash, err := identity.GenerateServiceKey(bytes.NewReader(seq(1, 40)))
	if err != nil {
		t.Fatal(err)
	}
	if !fullKeyPattern.MatchString(full) || !strings.HasPrefix(full, "ek_"+prefix+"_") || len(prefix) != 12 {
		t.Fatalf("khóa sai dạng: %q (tiền tố %q)", full, prefix)
	}
	if hash != sha256.Sum256([]byte(full)) {
		t.Fatal("băm phải là sha256 của khóa đầy đủ")
	}
	again, _, _, err := identity.GenerateServiceKey(bytes.NewReader(seq(1, 40)))
	if err != nil || again != full {
		t.Fatalf("cùng byte ngẫu nhiên phải ra cùng khóa: %q %v", again, err)
	}
	if _, _, _, err := identity.GenerateServiceKey(bytes.NewReader(seq(1, 10))); err == nil {
		t.Fatal("thiếu byte ngẫu nhiên phải lỗi")
	}
}

// ServiceKeyPermissions chứa đúng tám quyền của mốc này (mốc sau thêm được, không đếm cứng tổng, D4-3); mọi quyền là
// quyền đã biết, loại none, không nằm trong bộ quyền nhân viên, không vai trò mẫu nào nhận.
func TestServiceKeyPermissions(t *testing.T) {
	t.Parallel()
	for _, p := range []identity.Permission{
		"web.catalog.read", "web.orders.create", "web.orders.read", "web.orders.cancel", "web.requests.write",
		"web.coupons.issue", "web.shipping.quote", "web.flash.buy",
	} {
		if !slices.Contains(identity.ServiceKeyPermissions, p) || !identity.IsServiceKeyPermission(p) {
			t.Errorf("thiếu quyền khóa %s", p)
		}
		if !identity.IsKnown(p) || identity.KindOf(p) != identity.ScopeNone {
			t.Errorf("%s phải là quyền đã biết loại none", p)
		}
		if slices.ContainsFunc(identity.Permissions, func(d identity.PermissionDef) bool { return d.Key == p }) {
			t.Errorf("%s không được nằm trong bộ quyền nhân viên", p)
		}
		if rs := rolesWith(p); len(rs) != 0 {
			t.Errorf("%s không được vào vai trò mẫu: %v", p, rs)
		}
	}
	if identity.IsServiceKeyPermission("identity.users.manage") {
		t.Fatal("quyền nhân viên không phải quyền khóa")
	}
	if rs := rolesWith("identity.service_keys.manage"); identity.KindOf("identity.service_keys.manage") != identity.ScopeShop ||
		!slices.Equal(rs, []string{"owner"}) {
		t.Fatalf("identity.service_keys.manage: toàn shop, chỉ owner; có %v", rs)
	}
}

// Vai trò tự định nghĩa không nhận quyền web.* (chỉ khóa dịch vụ giữ được).
func TestCustomRole_RejectsServiceKeyPermission(t *testing.T) {
	t.Parallel()
	e := newStaffEnv(t)
	if _, err := e.createRole(e.actor(emailA), "Web", "web.catalog.read"); !errors.Is(err, identity.ErrUnknownPermission) {
		t.Fatalf("mong ErrUnknownPermission, có %v", err)
	}
}

// Owner đã customized vẫn nhận identity.service_keys.manage khi đồng bộ; vai trò mẫu khác đã sửa thì không.
func TestSyncDefaultRoles_ServiceKeysManage(t *testing.T) {
	t.Parallel()
	e := newStaffEnv(t)
	e.s.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `DELETE FROM identity.role_permissions WHERE permission = 'identity.service_keys.manage'`); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `UPDATE identity.roles SET customized = true WHERE key IN ('owner', 'manager')`)
		return err //nolint:wrapcheck // test
	})
	if _, err := identity.SyncDefaultRoles(t.Context(), e.s.Owner); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(e.roleByKey("owner").Permissions, "identity.service_keys.manage") {
		t.Fatal("owner đã sửa vẫn phải nhận identity.service_keys.manage")
	}
	if slices.Contains(e.roleByKey("manager").Permissions, "identity.service_keys.manage") {
		t.Fatal("manager không được nhận identity.service_keys.manage")
	}
}

// keyEnv: tenant A (Chủ A) và tenant B (Chủ B2) có vai trò mẫu.
type keyEnv struct {
	*staffEnv

	tenantB uuid.UUID
	ownerB  identity.AuthUser
}

func newKeyEnv(t *testing.T) *keyEnv {
	t.Helper()
	e := &keyEnv{staffEnv: newStaffEnv(t)}
	e.tenantB = e.s.Tenant(t, "B")
	e.seedTenant(e.tenantB)
	e.user(e.tenantB, "b2@elimaz.test", "owner", identity.ScopeAll, "")
	e.ownerB = e.actor("b2@elimaz.test")
	return e
}

func (e *keyEnv) create(tenant uuid.UUID, in identity.CreateServiceKeyInput, r io.Reader) (identity.ServiceKey, string, error) {
	var (
		k    identity.ServiceKey
		full string
	)
	err := tenancy.WithTenant(e.t.Context(), e.s.App, tenant, func(tx pgx.Tx) error {
		var err error
		k, full, err = identity.CreateServiceKeyWith(e.t.Context(), tx, in, r)
		return err //nolint:wrapcheck // test
	})
	return k, full, err //nolint:wrapcheck // test
}

func (e *keyEnv) routes(tenant uuid.UUID) int {
	return e.s.Count(e.t, `SELECT count(*) FROM platform.tenant_routes WHERE kind = 'service_key' AND tenant_id = $1`, tenant)
}

func webInput(actor identity.AuthUser, perms ...identity.Permission) identity.CreateServiceKeyInput {
	return identity.CreateServiceKeyInput{Name: "Website", Permissions: perms, Actor: actor}
}

// crm_app không chèn thẳng được bảng tuyến; tạo khóa ghi đúng một tuyến service_key qua tenancy.PutRoute; tenant B ghi
// cùng tiền tố → ErrRouteTaken; tiền tố trùng ở lần sinh đầu → khóa nhận tiền tố thứ hai; trùng cả hai lần → lỗi.
func TestCreateServiceKey_Routes(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	_, err := e.s.App.Pool.Exec(t.Context(),
		`INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('service_key', 'x', $1)`, e.tenant)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("crm_app chèn bảng tuyến phải bị từ chối quyền, có %v", err)
	}

	first := seq(1, 40)
	kA, fullA, err := e.create(e.tenant, webInput(e.actor(emailA), "web.catalog.read"), bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if !fullKeyPattern.MatchString(fullA) || e.routes(e.tenant) != 1 {
		t.Fatalf("khóa %q, số tuyến %d", fullA, e.routes(e.tenant))
	}
	var owner uuid.UUID
	if err := e.s.Owner.Pool.QueryRow(t.Context(), `SELECT tenant_id FROM platform.tenant_routes WHERE kind = 'service_key' AND external_key = $1`,
		kA.KeyPrefix).Scan(&owner); err != nil || owner != e.tenant {
		t.Fatalf("tuyến của tiền tố phải trỏ tenant A: %v %v", owner, err)
	}

	err = tenancy.WithTenant(t.Context(), e.s.App, e.tenantB, func(tx pgx.Tx) error {
		return tenancy.PutRoute(t.Context(), tx, identity.RouteKindServiceKey, kA.KeyPrefix)
	})
	if !errors.Is(err, tenancy.ErrRouteTaken) {
		t.Fatalf("tenant B ghi cùng tiền tố phải ErrRouteTaken, có %v", err)
	}

	// Lần sinh đầu trùng tiền tố của A → khóa của B mang tiền tố thứ hai, B có đúng một tuyến.
	kB, _, err := e.create(e.tenantB, webInput(e.ownerB, "web.catalog.read"), io.MultiReader(bytes.NewReader(first), bytes.NewReader(seq(90, 40))))
	if err != nil {
		t.Fatal(err)
	}
	if kB.KeyPrefix == kA.KeyPrefix || e.routes(e.tenantB) != 1 {
		t.Fatalf("B phải nhận tiền tố khác (%s / %s), một tuyến (%d)", kB.KeyPrefix, kA.KeyPrefix, e.routes(e.tenantB))
	}

	// Trùng cả hai lần → lỗi, không thêm khóa, không thêm tuyến.
	_, _, err = e.create(e.tenantB, webInput(e.ownerB, "web.catalog.read"), io.MultiReader(bytes.NewReader(first), bytes.NewReader(first)))
	if !errors.Is(err, tenancy.ErrRouteTaken) {
		t.Fatalf("trùng hai lần phải ErrRouteTaken, có %v", err)
	}
	if e.routes(e.tenantB) != 1 || e.s.CountIn(t, e.tenantB, `SELECT count(*) FROM identity.service_keys`) != 1 {
		t.Fatal("trùng hai lần không được ghi gì")
	}
	// Cùng tenant sinh lại tiền tố đã có của chính mình → cũng thử lại (unique (tenant_id, key_prefix)).
	kA2, _, err := e.create(e.tenant, webInput(e.actor(emailA), "web.catalog.read"), io.MultiReader(bytes.NewReader(first), bytes.NewReader(seq(150, 40))))
	if err != nil || kA2.KeyPrefix == kA.KeyPrefix {
		t.Fatalf("tiền tố trùng của chính tenant phải sinh lại: %v %v", kA2.KeyPrefix, err)
	}
}

// Khóa sai dạng không tách được tiền tố (không chạm database).
func TestSplitServiceKey(t *testing.T) {
	t.Parallel()
	full, prefix, _, err := identity.GenerateServiceKey(bytes.NewReader(seq(1, 40)))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"ek_x_y", "ek_" + prefix, "ek_" + prefix + "_", "xx_" + full[3:], full + "a", strings.ToUpper(full)} {
		if _, ok := identity.SplitServiceKey(bad); ok {
			t.Errorf("%q phải sai dạng", bad)
		}
	}
}

// Kiểm đầu vào: quyền ngoài tám quyền khóa → ErrPermissionNotForKey; tên, quyền rỗng → ErrInvalidInput. Quyền và
// CIDR được chuẩn hóa (sắp, bỏ trùng, che bit host).
func TestCreateServiceKey_Validation(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	a := e.actor(emailA)
	for name, c := range map[string]struct {
		in   identity.CreateServiceKeyInput
		want error
	}{
		"quyền nhân viên": {webInput(a, "web.catalog.read", "identity.users.manage"), identity.ErrPermissionNotForKey},
		"quyền lạ":        {webInput(a, "web.khong.co"), identity.ErrPermissionNotForKey},
		"không quyền":     {webInput(a), identity.ErrInvalidInput},
		"tên rỗng":        {identity.CreateServiceKeyInput{Name: "  ", Permissions: perms("web.catalog.read"), Actor: a}, identity.ErrInvalidInput},
		"tên quá dài":     {identity.CreateServiceKeyInput{Name: strings.Repeat("a", 81), Permissions: perms("web.catalog.read"), Actor: a}, identity.ErrInvalidInput},
		"CIDR sai":        {identity.CreateServiceKeyInput{Name: "W", Permissions: perms("web.catalog.read"), AllowedCIDRs: []netip.Prefix{{}}, Actor: a}, identity.ErrInvalidInput},
	} {
		if _, _, err := e.create(e.tenant, c.in, bytes.NewReader(seq(1, 80))); !errors.Is(err, c.want) {
			t.Errorf("%s: mong %v, có %v", name, c.want, err)
		}
	}
	k, _, err := e.create(e.tenant, identity.CreateServiceKeyInput{
		Name: " Web ", Permissions: perms("web.orders.read", "web.catalog.read", "web.orders.read"),
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.1.2.3/8")}, Actor: a,
	}, bytes.NewReader(seq(1, 80)))
	if err != nil {
		t.Fatal(err)
	}
	if k.Name != "Web" || !slices.Equal(k.Permissions, perms("web.catalog.read", "web.orders.read")) ||
		len(k.AllowedCIDRs) != 1 || k.AllowedCIDRs[0].String() != "10.0.0.0/8" || k.Version != 1 || k.RevokedAt != nil {
		t.Fatalf("khóa đã chuẩn hóa sai: %+v", k)
	}
}

// Khóa đúng → AuthUser{Kind: service} có đúng quyền đã cấp, phạm vi all; sai một ký tự bí mật, tiền tố lạ, sai dạng →
// ErrInvalidKey; đã thu hồi → ErrRevoked; CIDR: IP ngoài, IP thiếu → ErrIPNotAllowed, IP trong → được.
func TestCheckServiceKey(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	a := e.actor(emailA)
	k, full, err := e.create(e.tenant, identity.CreateServiceKeyInput{
		Name: "Web", Permissions: perms("web.catalog.read", "web.orders.create"),
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Actor: a,
	}, bytes.NewReader(seq(1, 40)))
	if err != nil {
		t.Fatal(err)
	}
	load := func(prefix string) (identity.ServiceKeyRecord, error) {
		var rec identity.ServiceKeyRecord
		err := tenancy.WithTenant(t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error {
			var err error
			rec, err = identity.LoadServiceKey(t.Context(), tx, prefix)
			return err //nolint:wrapcheck // test
		})
		return rec, err //nolint:wrapcheck // test
	}
	prefix, ok := identity.SplitServiceKey(full)
	if !ok || prefix != k.KeyPrefix {
		t.Fatalf("tách tiền tố: %q %v", prefix, ok)
	}
	rec, err := load(prefix)
	if err != nil {
		t.Fatal(err)
	}
	ip := netip.MustParseAddr("10.1.2.3")
	u, err := identity.CheckServiceKey(rec, full, ip)
	if err != nil {
		t.Fatal(err)
	}
	if u.Kind != identity.KindService || u.ServiceKeyID != k.ID || u.TenantID != e.tenant || u.UserID != uuid.Nil {
		t.Fatalf("người gọi sai: %+v", u)
	}
	if !identity.Can(u, "web.catalog.read", nil) || !identity.Can(u, "web.orders.create", nil) || identity.Can(u, "web.orders.read", nil) ||
		len(u.Grants) != 2 || u.Grants[0].Scope.Type != identity.ScopeAll {
		t.Fatalf("quyền của khóa sai: %+v", u.Grants)
	}

	wrong := []byte(full)
	wrong[len(wrong)-1] ^= 1 // đổi một ký tự của phần bí mật
	if _, err := identity.CheckServiceKey(rec, string(wrong), ip); !errors.Is(err, identity.ErrInvalidKey) {
		t.Errorf("sai một ký tự: %v", err)
	}
	if _, err := load("aaaaaaaaaaaa"); !errors.Is(err, identity.ErrInvalidKey) {
		t.Errorf("tiền tố lạ: %v", err)
	}
	if _, err := identity.CheckServiceKey(rec, full, netip.MustParseAddr("1.2.3.4")); !errors.Is(err, identity.ErrIPNotAllowed) {
		t.Errorf("IP ngoài CIDR: %v", err)
	}
	if _, err := identity.CheckServiceKey(rec, full, netip.Addr{}); !errors.Is(err, identity.ErrIPNotAllowed) {
		t.Errorf("thiếu IP: %v", err)
	}
	if _, err := identity.CheckServiceKey(rec, full, netip.MustParseAddr("::ffff:10.9.9.9")); err != nil {
		t.Errorf("IPv4 dạng IPv6 trong CIDR: %v", err)
	}

	if _, err := e.revoke(e.tenant, k.ID, k.Version, a); err != nil {
		t.Fatal(err)
	}
	rec, err = load(prefix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.CheckServiceKey(rec, full, ip); !errors.Is(err, identity.ErrRevoked) {
		t.Errorf("đã thu hồi: %v", err)
	}
	if _, err := identity.CheckServiceKey(rec, string(wrong), ip); !errors.Is(err, identity.ErrInvalidKey) {
		t.Errorf("đã thu hồi mà sai bí mật vẫn là khóa sai (không lộ trạng thái): %v", err)
	}
}

func (e *keyEnv) revoke(tenant, id uuid.UUID, version int, actor identity.AuthUser) (string, error) {
	var k identity.ServiceKey
	err := tenancy.WithTenant(e.t.Context(), e.s.App, tenant, func(tx pgx.Tx) error {
		var err error
		k, err = identity.RevokeServiceKey(e.t.Context(), tx, id, version, actor)
		return err //nolint:wrapcheck // test
	})
	if err == nil && (k.ID != id || k.RevokedAt == nil || k.Version != version+1) {
		e.t.Errorf("khóa sau thu hồi sai: %+v", k)
	}
	return k.KeyPrefix, err //nolint:wrapcheck // test
}

func (e *keyEnv) list(tenant uuid.UUID) []identity.ServiceKey {
	e.t.Helper()
	var out []identity.ServiceKey
	if err := tenancy.WithTenant(e.t.Context(), e.s.App, tenant, func(tx pgx.Tx) error {
		var err error
		out, err = identity.ListServiceKeys(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return out
}

// Thu hồi: version cũ → ErrVersionConflict; đã thu hồi → ErrVersionConflict; mã lạ hay của tenant khác →
// ErrServiceKeyNotFound; thu hồi ghi audit staff, revoked_by; danh sách chỉ tenant mình, không băm.
func TestRevokeAndListServiceKeys(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	a := e.actor(emailA)
	k, _, err := e.create(e.tenant, webInput(a, "web.catalog.read"), bytes.NewReader(seq(1, 40)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.revoke(e.tenantB, k.ID, k.Version, e.ownerB); !errors.Is(err, identity.ErrServiceKeyNotFound) {
		t.Fatalf("tenant khác: %v", err)
	}
	if _, err := e.revoke(e.tenant, uuid.New(), 1, a); !errors.Is(err, identity.ErrServiceKeyNotFound) {
		t.Fatalf("mã lạ: %v", err)
	}
	if _, err := e.revoke(e.tenant, k.ID, k.Version+1, a); !errors.Is(err, identity.ErrVersionConflict) {
		t.Fatalf("version lệch: %v", err)
	}
	prefix, err := e.revoke(e.tenant, k.ID, k.Version, a)
	if err != nil || prefix != k.KeyPrefix {
		t.Fatalf("thu hồi: %q %v", prefix, err)
	}
	if _, err := e.revoke(e.tenant, k.ID, k.Version+1, a); !errors.Is(err, identity.ErrVersionConflict) {
		t.Fatalf("thu hồi lần hai: %v", err)
	}
	got := e.list(e.tenant)
	if len(got) != 1 || got[0].RevokedAt == nil || got[0].Version != k.Version+1 || got[0].KeyPrefix != k.KeyPrefix {
		t.Fatalf("danh sách sau thu hồi: %+v", got)
	}
	if len(e.list(e.tenantB)) != 0 {
		t.Fatal("tenant B không được thấy khóa của A")
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM identity.service_keys WHERE revoked_by = $1`, a.UserID); n != 1 {
		t.Fatalf("revoked_by phải là người thu hồi, có %d dòng", n)
	}
	for _, action := range []string{"identity.service_key.create", "identity.service_key.revoke"} {
		if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = $1 AND actor_type = 'staff'
			AND actor_id = $2 AND entity_id = $3`, action, a.UserID.String(), k.ID.String()); n != 1 {
			t.Errorf("audit %s: %d dòng", action, n)
		}
	}
	// Người thu hồi là hệ thống (lệnh crm service-key revoke): revoked_by NULL, audit system.
	k2, _, err := e.create(e.tenant, webInput(a, "web.catalog.read"), bytes.NewReader(seq(60, 40)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.revoke(e.tenant, k2.ID, k2.Version, identity.AuthUser{TenantID: e.tenant}); err != nil {
		t.Fatal(err)
	}
	if n := e.s.CountIn(t, e.tenant, `SELECT count(*) FROM identity.audit_logs WHERE action = 'identity.service_key.revoke'
		AND actor_type = 'system' AND entity_id = $1`, k2.ID.String()); n != 1 {
		t.Fatalf("audit thu hồi của hệ thống: %d", n)
	}
}

// TouchServiceKeys chỉ đẩy last_used_at tiến lên (hai tiến trình ghi lệch thứ tự không lùi mốc).
func TestTouchServiceKeys(t *testing.T) {
	t.Parallel()
	e := newKeyEnv(t)
	k, _, err := e.create(e.tenant, webInput(e.actor(emailA), "web.catalog.read"), bytes.NewReader(seq(1, 40)))
	if err != nil {
		t.Fatal(err)
	}
	t1 := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{t1, t1.Add(-time.Hour)} {
		if err := tenancy.WithTenant(t.Context(), e.s.App, e.tenant, func(tx pgx.Tx) error {
			return identity.TouchServiceKeys(t.Context(), tx, []uuid.UUID{k.ID}, at)
		}); err != nil {
			t.Fatal(err)
		}
	}
	got := e.list(e.tenant)
	if len(got) != 1 || got[0].LastUsedAt == nil || !got[0].LastUsedAt.Equal(t1) || got[0].Version != 1 {
		t.Fatalf("last_used_at phải là %v (không đổi version): %+v", t1, got)
	}
}
