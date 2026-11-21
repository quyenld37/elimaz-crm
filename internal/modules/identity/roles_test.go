package identity_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/testkit"
)

// Năm vai trò mẫu đúng kế hoạch Task M1-26: owner đủ mọi quyền; manager mọi quyền trừ identity.users.manage,
// identity.roles.manage, platform.*; ba vai trò còn lại đúng danh sách.
func TestDefaultRoles_Content(t *testing.T) {
	all := make([]identity.Permission, 0, len(identity.Permissions))
	for _, d := range identity.Permissions {
		all = append(all, d.Key)
	}
	var manager []identity.Permission
	// M3 (Task M3-04): Quản lý không nhập, chốt bảng kê, không điều chỉnh sổ COD, không ghi hay miễn khoản hoàn tiền.
	cashOnlyOwner := []identity.Permission{
		"cash.statements.import", "cash.statements.confirm", "cash.adjust", "cash.refunds.record", "cash.refunds.waive",
	}
	for _, p := range all {
		if p != "identity.users.manage" && p != "identity.roles.manage" && p != "identity.service_keys.manage" && (len(p) < 9 || p[:9] != "platform.") &&
			!slices.Contains(cashOnlyOwner, p) {
			manager = append(manager, p)
		}
	}
	want := map[string][]identity.Permission{
		"owner":   all,
		"manager": manager,
		"sale": {
			"catalog.read", "order.create", "order.read", "inventory.read", "fulfillment.read", "fulfillment.ship", "realtime.subscribe",
			// M2 (Task M2-01).
			"customer.read", "customer.write", "order.update", "order.confirm", "order.cancel", "order.return", "promotion.read",
			// M3 (Task M3-04).
			"fulfillment.quote", "fulfillment.cancel", "fulfillment.callbacks.work", "cash.payments.record",
		},
		"warehouse": {
			"catalog.read", "inventory.read", "inventory.documents.create", "inventory.stocktake", "fulfillment.read",
			"fulfillment.ship", "inventory.suppliers.manage", "realtime.subscribe",
			// M2 (Task M2-01).
			"order.read", "order.return.receive", "customer.read",
			// M3 (Task M3-04).
			"fulfillment.carrier_accounts.read", "fulfillment.quote", "fulfillment.cancel", "fulfillment.print",
			"fulfillment.pack", "fulfillment.manual_status", "inventory.aged.read",
		},
		"accountant": {
			"catalog.read", "inventory.read", "order.read", "fulfillment.read", "realtime.subscribe",
			// M2 (Task M2-01).
			"customer.read", "promotion.read",
			// M3 (Task M3-04).
			"cash.cod.read", "cash.statements.import", "cash.statements.confirm", "cash.adjust", "cash.payments.record",
			"cash.refunds.record", "files.upload",
		},
	}
	names := map[string]string{"owner": "Chủ", "manager": "Quản lý", "sale": "Bán hàng", "warehouse": "Kho", "accountant": "Kế toán"}
	if len(identity.DefaultRoles) != len(want) {
		t.Fatalf("mong %d vai trò mẫu, có %d", len(want), len(identity.DefaultRoles))
	}
	for _, r := range identity.DefaultRoles {
		w, ok := want[r.Key]
		if !ok || r.Name != names[r.Key] {
			t.Errorf("vai trò lạ hoặc sai tên: %s %q", r.Key, r.Name)
			continue
		}
		got := slices.Sorted(slices.Values(r.Permissions))
		if !slices.Equal(got, slices.Sorted(slices.Values(w))) {
			t.Errorf("quyền của %s = %v, mong %v", r.Key, got, w)
		}
		for _, p := range r.Permissions {
			if !identity.IsKnown(p) {
				t.Errorf("%s có quyền lạ %s", r.Key, p)
			}
		}
	}
}

// rolesTenant: database thử có một tenant đã chạy EnsureDefaultRoles một lần.
func rolesTenant(t *testing.T) (*testkit.Staff, uuid.UUID) {
	t.Helper()
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	ensureRoles(t, s, a)
	return s, a
}

func ensureRoles(t *testing.T, s *testkit.Staff, a uuid.UUID) {
	t.Helper()
	s.Exec(t, a, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, a) })
}

func listRoles(t *testing.T, s *testkit.Staff, a uuid.UUID) []identity.RoleRow {
	t.Helper()
	var rows []identity.RoleRow
	s.Exec(t, a, func(tx pgx.Tx) error {
		var err error
		rows, err = identity.ListRoles(t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	return rows
}

func countRoleRows(t *testing.T, s *testkit.Staff, a uuid.UUID) (roles, perms int) {
	t.Helper()
	for _, r := range listRoles(t, s, a) {
		roles++
		perms += len(r.Permissions)
	}
	return roles, perms
}

// EnsureDefaultRoles chạy hai lần không đổi gì (số dòng roles, role_permissions giữ nguyên).
func TestEnsureDefaultRoles_Idempotent(t *testing.T) {
	s, a := rolesTenant(t)
	r1, p1 := countRoleRows(t, s, a)
	wantPerms := 0
	for _, d := range identity.DefaultRoles {
		wantPerms += len(d.Permissions)
	}
	if r1 != len(identity.DefaultRoles) || p1 != wantPerms {
		t.Fatalf("lần 1: %d vai trò, %d quyền; mong %d, %d", r1, p1, len(identity.DefaultRoles), wantPerms)
	}
	ensureRoles(t, s, a)
	if r2, p2 := countRoleRows(t, s, a); r2 != r1 || p2 != p1 {
		t.Fatalf("lần 2 đổi số dòng: %d/%d → %d/%d", r1, p1, r2, p2)
	}
	for _, r := range listRoles(t, s, a) {
		if r.Status != "active" || r.Customized {
			t.Errorf("vai trò %s: trạng thái %s, customized %v", r.Key, r.Status, r.Customized)
		}
	}
}

// Chủ shop đã sửa vai trò sale (bỏ order.create) và owner (bỏ audit.read): sale giữ nguyên, owner được thêm lại.
func TestEnsureDefaultRoles_CustomizedKept(t *testing.T) {
	s, a := rolesTenant(t)
	s.Exec(t, a, func(tx pgx.Tx) error {
		for _, q := range []string{
			"UPDATE identity.roles SET customized = true WHERE key IN ('sale', 'owner')",
			"DELETE FROM identity.role_permissions WHERE permission = 'order.create' AND role_id = (SELECT id FROM identity.roles WHERE key = 'sale')",
			"DELETE FROM identity.role_permissions WHERE permission = 'audit.read' AND role_id = (SELECT id FROM identity.roles WHERE key = 'owner')",
		} {
			if _, err := tx.Exec(t.Context(), q); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	ensureRoles(t, s, a)
	rows := listRoles(t, s, a)
	idx := slices.IndexFunc(rows, func(r identity.RoleRow) bool { return r.Key == "sale" })
	if idx < 0 || slices.Contains(rows[idx].Permissions, "order.create") || !rows[idx].Customized {
		t.Errorf("sale đã sửa bị thêm lại quyền: %+v", rows)
	}
	idx = slices.IndexFunc(rows, func(r identity.RoleRow) bool { return r.Key == "owner" })
	if idx < 0 || len(rows[idx].Permissions) != len(identity.Permissions) {
		t.Errorf("owner phải đủ %d quyền: %+v", len(identity.Permissions), rows)
	}
}

// Bước đồng bộ sau migrate (Task 315): shop chưa có vai trò nhận đủ vai trò mẫu; quyền mới của bảng quyền (giả lập
// bằng cách xóa dòng role_permissions như database có trước khi quyền đó được thêm vào mã) vào vai trò mẫu chưa sửa;
// vai trò mẫu đã sửa (customized) và vai trò tự định nghĩa không đổi; chạy lần hai không đổi thêm.
func TestSyncDefaultRoles(t *testing.T) {
	s, a := rolesTenant(t)
	b := s.Tenant(t, "B") // shop mới, chưa có vai trò nào
	s.Exec(t, a, func(tx pgx.Tx) error {
		for _, q := range []string{
			"UPDATE identity.roles SET customized = true WHERE key = 'accountant'",
			"DELETE FROM identity.role_permissions WHERE permission = 'order.read' AND role_id IN (SELECT id FROM identity.roles WHERE key IN ('sale', 'accountant'))",
			"INSERT INTO identity.roles (tenant_id, key, name) VALUES (current_setting('app.tenant_id')::uuid, 'tu-dat', 'Tự đặt')",
		} {
			if _, err := tx.Exec(t.Context(), q); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	n, err := identity.SyncDefaultRoles(t.Context(), s.Owner)
	if err != nil || n != 2 {
		t.Fatalf("SyncDefaultRoles = %d, %v; mong 2 tenant", n, err)
	}
	byKey := func(tenant uuid.UUID) map[string]identity.RoleRow {
		m := map[string]identity.RoleRow{}
		for _, r := range listRoles(t, s, tenant) {
			m[r.Key] = r
		}
		return m
	}
	ra := byKey(a)
	if !slices.Contains(ra["sale"].Permissions, "order.read") {
		t.Errorf("sale chưa sửa phải nhận lại order.read: %v", ra["sale"].Permissions)
	}
	if slices.Contains(ra["accountant"].Permissions, "order.read") || !ra["accountant"].Customized {
		t.Errorf("accountant đã sửa không được đổi: %+v", ra["accountant"])
	}
	if len(ra["tu-dat"].Permissions) != 0 {
		t.Errorf("vai trò tự định nghĩa không được thêm quyền: %v", ra["tu-dat"].Permissions)
	}
	rb := byKey(b)
	for _, d := range identity.DefaultRoles {
		if r, ok := rb[d.Key]; !ok || len(r.Permissions) != len(d.Permissions) {
			t.Errorf("shop mới thiếu vai trò mẫu %s hoặc thiếu quyền: %+v", d.Key, r)
		}
	}
	ra1, pa1 := countRoleRows(t, s, a)
	rb1, pb1 := countRoleRows(t, s, b)
	if _, err := identity.SyncDefaultRoles(t.Context(), s.Owner); err != nil {
		t.Fatal(err)
	}
	ra2, pa2 := countRoleRows(t, s, a)
	rb2, pb2 := countRoleRows(t, s, b)
	if ra1 != ra2 || pa1 != pa2 || rb1 != rb2 || pb1 != pb2 {
		t.Fatalf("lần hai đổi số dòng: A %d/%d → %d/%d, B %d/%d → %d/%d", ra1, pa1, ra2, pa2, rb1, pb1, rb2, pb2)
	}
}

// m2Permissions là 15 quyền Task M2-01 thêm (order.read, order.create có từ M1).
func m2Permissions() []identity.Permission {
	return []identity.Permission{
		"customer.read", "customer.write", "customer.merge", "customer.import", "customer.tags.manage", "order.update",
		"order.confirm", "order.cancel", "order.discount.approve", "order.return", "order.return.receive",
		"order.duplicates.resolve", "order.settings.manage", "promotion.read", "promotion.manage",
	}
}

func rolesByKey(t *testing.T, s *testkit.Staff, a uuid.UUID) map[string]identity.RoleRow {
	t.Helper()
	m := map[string]identity.RoleRow{}
	for _, r := range listRoles(t, s, a) {
		m[r.Key] = r
	}
	return m
}

func execAll(t *testing.T, s *testkit.Staff, a uuid.UUID, qs ...string) {
	t.Helper()
	s.Exec(t, a, func(tx pgx.Tx) error {
		for _, q := range qs {
			if _, err := tx.Exec(t.Context(), q); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
}

// Tenant mới (Task M2-01): 5 vai trò mẫu của M1; sale không có quyền gộp khách, duyệt giảm giá, nhận hàng trả;
// warehouse nhận hàng trả; manager duyệt giảm giá và đủ 15 quyền M2; owner đủ 15 quyền M2.
func TestEnsureDefaultRoles_M2(t *testing.T) {
	s, a := rolesTenant(t)
	r := rolesByKey(t, s, a)
	if len(r) != 5 {
		t.Fatalf("mong 5 vai trò mẫu, có %d", len(r))
	}
	for _, p := range []identity.Permission{"customer.merge", "order.discount.approve", "order.return.receive"} {
		if slices.Contains(r["sale"].Permissions, p) {
			t.Errorf("sale không được có %s", p)
		}
	}
	if !slices.Contains(r["warehouse"].Permissions, "order.return.receive") {
		t.Errorf("warehouse phải có order.return.receive: %v", r["warehouse"].Permissions)
	}
	for _, p := range m2Permissions() {
		for _, k := range []string{"owner", "manager"} {
			if !slices.Contains(r[k].Permissions, p) {
				t.Errorf("%s thiếu %s", k, p)
			}
		}
	}
}

// Chỉ thêm, không bớt: quyền mẫu bị xóa tay khỏi sale chưa sửa được thêm lại; quyền ngoài mẫu thêm tay vẫn còn.
func TestEnsureDefaultRoles_M2AddOnly(t *testing.T) {
	s, a := rolesTenant(t)
	_, p1 := countRoleRows(t, s, a)
	ensureRoles(t, s, a)
	if _, p2 := countRoleRows(t, s, a); p2 != p1 {
		t.Fatalf("gọi lần hai đổi số quyền: %d → %d", p1, p2)
	}
	sale := "(SELECT id FROM identity.roles WHERE key = 'sale')"
	execAll(t, s, a,
		"DELETE FROM identity.role_permissions WHERE permission = 'order.cancel' AND role_id = "+sale,
		"INSERT INTO identity.role_permissions (tenant_id, role_id, permission) VALUES (current_setting('app.tenant_id')::uuid, "+sale+", 'customer.merge')",
	)
	ensureRoles(t, s, a)
	got := rolesByKey(t, s, a)["sale"]
	if got.Customized || !slices.Contains(got.Permissions, "order.cancel") {
		t.Errorf("sale chưa sửa phải được thêm lại order.cancel: %+v", got)
	}
	if !slices.Contains(got.Permissions, "customer.merge") {
		t.Errorf("quyền thêm tay customer.merge bị xóa: %v", got.Permissions)
	}
}

// Thay migration 0200 (Q-T57-6): tenant có trước M2 (giả lập bằng cách xóa mọi quyền M2 khỏi role_permissions) chạy
// SyncDefaultRoles: owner (kể cả đã customized) nhận đủ 15 quyền và chủ shop dùng được ngay (LoadUser → Can); sale
// chưa sửa nhận đúng phần M2 của mình; warehouse đã customized và vai trò tự định nghĩa không nhận quyền M2 nào.
func TestSyncDefaultRoles_M2(t *testing.T) {
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	const email = "chu-m2@elimaz.test"
	s.User(t, a, email, "active")
	if err := identity.SeedOwner(t.Context(), s.Owner, a, email); err != nil {
		t.Fatal(err)
	}
	m2 := make([]string, 0, len(m2Permissions()))
	for _, p := range m2Permissions() {
		m2 = append(m2, "'"+string(p)+"'")
	}
	execAll(t, s, a,
		"DELETE FROM identity.role_permissions WHERE permission IN ("+strings.Join(m2, ", ")+")",
		"UPDATE identity.roles SET customized = true WHERE key IN ('warehouse', 'owner')",
		"INSERT INTO identity.roles (tenant_id, key, name, customized) VALUES (current_setting('app.tenant_id')::uuid, 'custom_ab12cd34', 'Tự đặt', true)",
		"INSERT INTO identity.role_permissions (tenant_id, role_id, permission) SELECT tenant_id, id, 'catalog.read' FROM identity.roles WHERE key = 'custom_ab12cd34'",
	)
	r := syncOldTenant(t, s, a, email, "customer.merge")
	for _, p := range m2Permissions() {
		if !slices.Contains(r["owner"].Permissions, p) {
			t.Errorf("owner (customized) thiếu %s", p)
		}
		if slices.Contains(r["warehouse"].Permissions, p) {
			t.Errorf("warehouse đã sửa không được nhận %s", p)
		}
		if slices.Contains(r["custom_ab12cd34"].Permissions, p) {
			t.Errorf("vai trò tự định nghĩa không được nhận %s", p)
		}
	}
	if got := r["custom_ab12cd34"].Permissions; !slices.Equal(got, []identity.Permission{"catalog.read"}) {
		t.Errorf("vai trò tự định nghĩa bị đổi: %v", got)
	}
	for _, p := range []identity.Permission{"customer.read", "customer.write", "order.update", "order.confirm", "order.cancel", "order.return", "promotion.read"} {
		if !slices.Contains(r["sale"].Permissions, p) {
			t.Errorf("sale chưa sửa thiếu %s sau đồng bộ", p)
		}
	}
	u, err := identity.LoadUser(t.Context(), s.App, email)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range m2Permissions() {
		if !identity.Can(u, p, nil) {
			t.Errorf("chủ shop không có %s sau đồng bộ", p)
		}
	}
}

// m3Permissions là 18 quyền Task M3-04 thêm (quyền M1, M2 dùng lại không khai lại).
func m3Permissions() []identity.Permission {
	return []identity.Permission{
		"fulfillment.carrier_accounts.read", "fulfillment.carrier_accounts.manage", "fulfillment.settings.manage",
		"fulfillment.quote", "fulfillment.cancel", "fulfillment.print", "fulfillment.pack", "fulfillment.manual_status",
		"fulfillment.correct", "fulfillment.callbacks.work", "inventory.aged.read", "cash.cod.read",
		"cash.statements.import", "cash.statements.confirm", "cash.adjust", "cash.payments.record",
		"cash.refunds.record", "cash.refunds.waive",
	}
}

// Tenant mới (Task M3-04): Kho có fulfillment.print, không có cash.adjust; sale vẫn có fulfillment.ship của M1; owner
// đủ 18 quyền M3; manager không có quyền sổ COD chỉ dành cho Chủ, Kế toán.
func TestEnsureDefaultRoles_M3(t *testing.T) {
	s, a := rolesTenant(t)
	r := rolesByKey(t, s, a)
	if !slices.Contains(r["warehouse"].Permissions, "fulfillment.print") || slices.Contains(r["warehouse"].Permissions, "cash.adjust") {
		t.Errorf("Kho phải có fulfillment.print, không có cash.adjust: %v", r["warehouse"].Permissions)
	}
	if !slices.Contains(r["sale"].Permissions, "fulfillment.ship") {
		t.Errorf("sale phải giữ fulfillment.ship của M1: %v", r["sale"].Permissions)
	}
	for _, p := range m3Permissions() {
		if !slices.Contains(r["owner"].Permissions, p) {
			t.Errorf("owner thiếu %s", p)
		}
	}
	for _, p := range []identity.Permission{"cash.adjust", "cash.refunds.waive", "cash.statements.confirm"} {
		if slices.Contains(r["manager"].Permissions, p) {
			t.Errorf("manager không được có %s", p)
		}
	}
	if slices.Contains(r["accountant"].Permissions, "cash.refunds.waive") || !slices.Contains(r["accountant"].Permissions, "cash.adjust") {
		t.Errorf("Kế toán: có cash.adjust, không có cash.refunds.waive: %v", r["accountant"].Permissions)
	}
}

// Thay migration 0305 (Q-T91-5): tenant có trước M3 (xóa mọi quyền M3) chạy SyncDefaultRoles: owner (đã customized)
// đủ 18 quyền và dùng được ngay; warehouse chưa sửa nhận phần M3 của mình; accountant đã customized không nhận gì.
func TestSyncDefaultRoles_M3(t *testing.T) {
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	const email = "chu-m3@elimaz.test"
	s.User(t, a, email, "active")
	if err := identity.SeedOwner(t.Context(), s.Owner, a, email); err != nil {
		t.Fatal(err)
	}
	m3 := make([]string, 0, len(m3Permissions()))
	for _, p := range m3Permissions() {
		m3 = append(m3, "'"+string(p)+"'")
	}
	execAll(t, s, a,
		"DELETE FROM identity.role_permissions WHERE permission IN ("+strings.Join(m3, ", ")+")",
		"UPDATE identity.roles SET customized = true WHERE key IN ('accountant', 'owner')",
	)
	r := syncOldTenant(t, s, a, email, "cash.adjust")
	for _, p := range m3Permissions() {
		if !slices.Contains(r["owner"].Permissions, p) {
			t.Errorf("owner (customized) thiếu %s", p)
		}
		if slices.Contains(r["accountant"].Permissions, p) {
			t.Errorf("accountant đã sửa không được nhận %s", p)
		}
	}
	for _, p := range []identity.Permission{"fulfillment.print", "fulfillment.pack", "inventory.aged.read"} {
		if !slices.Contains(r["warehouse"].Permissions, p) {
			t.Errorf("warehouse chưa sửa thiếu %s sau đồng bộ", p)
		}
	}
	u, err := identity.LoadUser(t.Context(), s.App, email)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range m3Permissions() {
		if !identity.Can(u, p, nil) {
			t.Errorf("chủ shop không có %s sau đồng bộ", p)
		}
	}
}

// syncOldTenant kiểm chủ shop email chưa có quyền missing (giả lập tenant cũ đúng), chạy SyncDefaultRoles rồi trả vai
// trò của tenant a theo khóa.
func syncOldTenant(t *testing.T, s *testkit.Staff, a uuid.UUID, email string, missing identity.Permission) map[string]identity.RoleRow {
	t.Helper()
	before, err := identity.LoadUser(t.Context(), s.App, email)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Can(before, missing, nil) {
		t.Fatalf("giả lập tenant cũ hỏng: chủ đã có %s trước khi đồng bộ", missing)
	}
	if _, err := identity.SyncDefaultRoles(t.Context(), s.Owner); err != nil {
		t.Fatal(err)
	}
	return rolesByKey(t, s, a)
}
