package customer_test

import (
	"errors"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/textnorm"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/migrations"
)

// foldSamples: 11 mẫu khác rỗng của TestFold (M1 Task 27; mẫu rỗng không chèn được vì full_name 1–120 ký tự, đã kiểm ở
// TestNormalizeName) cộng hai mẫu của Task M2-03. Mẫu NFD viết bằng dấu rời tường minh.
func foldSamples() []string {
	return []string{
		"Đầm Lụa  Xòe", "Áo Thun Ơ", "ĐỒNG HỒ", "đường  hai   khoảng", "  Quần Jean Ống Rộng  ", "Ưu đãi Ỹ Ỷ Ỵ ỳ ý",
		"Bộ Sưu Tập Thu Đông", "Chân váy xếp ly", "Áo khoác\tdạ\nnữ", "DAM01-ĐỎ-M", "\u0110a\u0302\u0300m lu\u0323a",
		"Nguyễn Thị Hồng Nhung", "ĐẶNG  Văn   Ơn",
	}
}

// migrateBefore chạy mọi migration có số nhỏ hơn 0201 (trạng thái của M1) bằng goose, như db.Migrate.
func migrateBefore(t *testing.T, ownerURL string) {
	t.Helper()
	ctx := t.Context()
	cc, err := pgx.ParseConfig(ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	cc.DefaultQueryExecMode = pgx.QueryExecModeExec
	conn, err := pgx.ConnectConfig(ctx, cc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS platform"); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)

	old := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < "0201" {
			b, err := fs.ReadFile(migrations.FS, e.Name())
			if err != nil {
				t.Fatal(err)
			}
			old[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	sqlDB := stdlib.OpenDB(*cc)
	defer func() { _ = sqlDB.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, old,
		goose.WithTableName("platform.goose_db_version"), goose.WithDisableGlobalRegistry(true),
		goose.WithAllowOutofOrder(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("migrate tới trước 0201: %v", err)
	}
}

type viewCol struct{ name, typ string }

func viewColumns(t *testing.T, owner *db.DB, view string) []viewCol {
	t.Helper()
	rows, err := owner.Pool.Query(t.Context(), `
		SELECT column_name::text, data_type::text FROM information_schema.columns
		WHERE table_schema = 'customer' AND table_name = $1 ORDER BY ordinal_position`, view)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (viewCol, error) {
		var c viewCol
		return c, r.Scan(&c.name, &c.typ)
	})
	if err != nil {
		t.Fatal(err)
	}
	return cols
}

func open(t *testing.T, url string) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), url, db.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Pool.Close)
	return d
}

func inTenant(t *testing.T, d *db.DB, tenant uuid.UUID, fn func(tx pgx.Tx) error) {
	t.Helper()
	if err := tenancy.WithTenant(t.Context(), d, tenant, fn); err != nil {
		t.Fatal(err)
	}
}

var khCode = regexp.MustCompile(`^KH\d{6,}$`)

// Mở rộng bảng của M1: khách có sẵn trước 0201 được điền code, name_search; root_id giữ; customer.Create của M1 vẫn
// chạy sau migrate; customers_v giữ 4 cột đầu cùng kiểu.
func TestSchemaExtendsM1Customers(t *testing.T) {
	_, ownerURL, appURL := testkit.CreateDatabase(t)
	migrateBefore(t, ownerURL)
	owner, app := open(t, ownerURL), open(t, appURL)

	var tenant uuid.UUID
	if err := owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('A') RETURNING id").
		Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	seeded := seedBefore0201(t, owner, app, tenant)
	before := viewColumns(t, owner, "customers_v")

	if _, err := db.Migrate(t.Context(), ownerURL); err != nil {
		t.Fatalf("migrate 0201: %v", err)
	}

	codes := checkBackfilled(t, owner, tenant)
	if len(codes) != seeded {
		t.Fatalf("có %d mã khách khác nhau, mong %d", len(codes), seeded)
	}

	// customers_v: 4 cột của M1 ở vị trí 1–4 cùng kiểu, rồi 11 cột M2.
	after := viewColumns(t, owner, "customers_v")
	if len(before) != 4 || len(after) != 15 {
		t.Fatalf("customers_v trước %v, sau %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("cột %d của customers_v đổi: %v → %v", i+1, before[i], after[i])
		}
	}
	checkM1CreateAfter(t, app, tenant, codes)
}

// seedBefore0201 tạo 3 khách bằng customer.Create của M1 và chèn thẳng các mẫu bỏ dấu bằng chủ bảng (giữ nguyên khoảng
// trắng hai đầu, tab, xuống dòng); trả số khách.
func seedBefore0201(t *testing.T, owner, app *db.DB, tenant uuid.UUID) int {
	t.Helper()
	ctx := t.Context()
	m1Names := []string{"Chị Lan", "Nguyễn Thị Hồng Nhung", "Anh Bình"}
	inTenant(t, app, tenant, func(tx pgx.Tx) error {
		for _, n := range m1Names {
			if _, err := customer.Create(ctx, tx, n); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	samples := foldSamples()
	inTenant(t, owner, tenant, func(tx pgx.Tx) error {
		for _, n := range samples {
			if _, err := tx.Exec(ctx, `INSERT INTO customer.customers (id, tenant_id, root_id, full_name)
				SELECT g, $1, g, $2 FROM uuidv7() AS g`, tenant, n); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return len(m1Names) + len(samples)
}

type backfilledRow struct {
	id, root               uuid.UUID
	code, full, nameSearch string
}

// checkBackfilled: mọi dòng có root_id = id, code KH + ≥ 6 chữ số khác nhau, name_search = textnorm.Fold(full_name);
// trả tập mã.
func checkBackfilled(t *testing.T, owner *db.DB, tenant uuid.UUID) map[string]bool {
	t.Helper()
	var got []backfilledRow
	inTenant(t, owner, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), "SELECT id, root_id, code, full_name, name_search FROM customer.customers")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		got, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (backfilledRow, error) {
			var b backfilledRow
			return b, r.Scan(&b.id, &b.root, &b.code, &b.full, &b.nameSearch)
		})
		return err //nolint:wrapcheck // test
	})
	codes := map[string]bool{}
	for _, r := range got {
		if r.root != r.id {
			t.Errorf("root_id %s khác id %s", r.root, r.id)
		}
		if !khCode.MatchString(r.code) || codes[r.code] {
			t.Errorf("code %q sai mẫu hoặc trùng", r.code)
		}
		codes[r.code] = true
		if want := textnorm.Fold(r.full); r.nameSearch != want {
			t.Errorf("name_search(%q) = %q, textnorm.Fold = %q", r.full, r.nameSearch, want)
		}
	}
	return codes
}

// checkM1CreateAfter: customer.Create của M1 sau migrate chạy (trigger đặt root_id, code lấy từ sequence); câu đọc view
// của M1 vẫn chạy.
func checkM1CreateAfter(t *testing.T, app *db.DB, tenant uuid.UUID, codes map[string]bool) {
	t.Helper()
	ctx := t.Context()
	inTenant(t, app, tenant, func(tx pgx.Tx) error {
		id, err := customer.Create(ctx, tx, "Khách Mới")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		var root uuid.UUID
		var code, full string
		var created time.Time
		if err := tx.QueryRow(ctx, "SELECT id, root_id, full_name, created_at FROM customer.customers_v WHERE id = $1", id).
			Scan(&id, &root, &full, &created); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, "SELECT code FROM customer.customers_v WHERE id = $1", id).Scan(&code); err != nil {
			return err //nolint:wrapcheck // test
		}
		if root != id || !khCode.MatchString(code) || codes[code] || full != "Khách Mới" {
			t.Errorf("khách mới sau migrate: root %s id %s code %q tên %q", root, id, code, full)
		}
		return nil
	})
}

// schemaEnv: database đã migrate đủ, hai tenant, một khách mỗi tenant.
type schemaEnv struct {
	staff        *testkit.Staff
	a, b         uuid.UUID
	custA, custB uuid.UUID
}

func newSchemaEnv(t *testing.T) *schemaEnv {
	t.Helper()
	e := &schemaEnv{staff: testkit.OpenStaff(t)}
	e.a, e.b = e.staff.Tenant(t, "A"), e.staff.Tenant(t, "B")
	for _, p := range []struct {
		tenant uuid.UUID
		id     *uuid.UUID
	}{{e.a, &e.custA}, {e.b, &e.custB}} {
		inTenant(t, e.staff.App, p.tenant, func(tx pgx.Tx) error {
			id, err := customer.Create(t.Context(), tx, "Khách")
			*p.id = id
			return err //nolint:wrapcheck // test
		})
	}
	return e
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func TestSchemaRootDefaultAndIdentityUnique(t *testing.T) {
	e := newSchemaEnv(t)
	ctx := t.Context()
	// Chèn không có root_id → root_id = id.
	inTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
		var id, root uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO customer.customers (tenant_id, full_name)
			VALUES ($1, 'Không gốc') RETURNING id, root_id`, e.a).Scan(&id, &root); err != nil {
			return err //nolint:wrapcheck // test
		}
		if id != root {
			t.Errorf("root_id %s, mong %s", root, id)
		}
		return nil
	})

	var other uuid.UUID
	inTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
		var err error
		other, err = customer.Create(ctx, tx, "Khách khác")
		return err //nolint:wrapcheck // test
	})
	insertPhone := func(cust uuid.UUID, scope any) error {
		return tenancy.WithTenant(ctx, e.staff.App, e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO customer.customer_identities
				(tenant_id, customer_id, kind, scope, value, value_reversed, source)
				VALUES ($1, $2, 'phone', $3, '+84912345678', '87654321948', 'staff')`, e.a, cust, scope)
			return err //nolint:wrapcheck // test
		})
	}
	if err := insertPhone(e.custA, ""); err != nil {
		t.Fatal(err)
	}
	if err := insertPhone(other, ""); pgCode(err) != "23505" {
		t.Errorf("SĐT trùng ở khách khác: %v, mong 23505", err)
	}
	if err := insertPhone(other, nil); pgCode(err) != "23502" {
		t.Errorf("scope NULL: %v, mong 23502", err)
	}
	// Cùng SĐT ở tenant B được phép (unique theo tenant).
	if err := tenancy.WithTenant(ctx, e.staff.App, e.b, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, value, source)
			VALUES ($1, $2, 'phone', '+84912345678', 'staff')`, e.b, e.custB)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Errorf("cùng SĐT ở tenant B: %v", err)
	}
}

func TestSchemaNotesAppendOnly(t *testing.T) {
	e := newSchemaEnv(t)
	ctx := t.Context()
	inTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO customer.customer_notes (tenant_id, customer_id, body, author_id)
			VALUES ($1, $2, 'ghi chú', $3)`, e.a, e.custA, uuid.New())
		return err //nolint:wrapcheck // test
	})
	for _, q := range []string{
		"UPDATE customer.customer_notes SET body = 'sửa'",
		"DELETE FROM customer.customer_notes",
	} {
		err := e.staff.TryExec(ctx, e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q)
			return err //nolint:wrapcheck // test
		})
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("chủ bảng %q: %v, mong lỗi append-only", q, err)
		}
		err = tenancy.WithTenant(ctx, e.staff.App, e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q)
			return err //nolint:wrapcheck // test
		})
		if pgCode(err) != "42501" || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("crm_app %q: %v, mong lỗi quyền", q, err)
		}
	}
}

// Mọi bảng mới và mọi view: tenant B không thấy dòng của A; crm_app của A thấy.
func TestSchemaTenantIsolation(t *testing.T) {
	e := newSchemaEnv(t)
	ctx := t.Context()
	inTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
		var tag uuid.UUID
		stmts := []struct {
			q    string
			args []any
		}{
			{`INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, value, source)
				VALUES ($1, $2, 'phone', '+84900000001', 'order')`, []any{e.a, e.custA}},
			{`INSERT INTO customer.customer_addresses
				(tenant_id, customer_id, recipient_name, phone, province_code, ward_code, street, is_default)
				VALUES ($1, $2, 'Lan', '+84900000001', '01', '00001', '1 Lê Lợi', true)`, []any{e.a, e.custA}},
			{`INSERT INTO customer.customer_merges (tenant_id, survivor_id, merged_id, moved_ids, matched_by, actor_id)
				VALUES ($1, $2, $2, ARRAY[$2]::uuid[], 'staff', $3)`, []any{e.a, e.custA, uuid.New()}},
			{
				`INSERT INTO customer.customer_notes (tenant_id, customer_id, body, author_id) VALUES ($1, $2, 'x', $3)`,
				[]any{e.a, e.custA, uuid.New()},
			},
			{`INSERT INTO customer.customer_imports (tenant_id, file_id, status, created_by)
				VALUES ($1, $2, 'pending', $3)`, []any{e.a, uuid.New(), uuid.New()}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.q, s.args...); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO customer.customer_tags (tenant_id, name, color)
			VALUES ($1, 'VIP', 'red') RETURNING id`, e.a).Scan(&tag); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, `INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source)
			VALUES ($1, $2, $3, 'staff')`, e.a, e.custA, tag); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, `INSERT INTO customer.customer_tag_rules (tenant_id, tag_id, conditions)
			VALUES ($1, $2, '{}')`, e.a, tag)
		return err //nolint:wrapcheck // test
	})
	for _, rel := range []string{
		"customer_identities", "customer_addresses", "customer_merges", "customer_tags", "customer_tag_links",
		"customer_tag_rules", "customer_notes", "customer_imports",
		"customer_identities_v", "customer_addresses_v", "customer_tags_v", "customer_tag_links_v",
	} {
		q := "SELECT count(*) FROM customer." + rel + " WHERE tenant_id = $1"
		for _, c := range []struct {
			tenant uuid.UUID
			want   int
		}{{e.a, 1}, {e.b, 0}} {
			var got int
			inTenant(t, e.staff.App, c.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, q, e.a).Scan(&got)
			})
			if got != c.want {
				t.Errorf("%s nhìn từ tenant %s: %d dòng của A, mong %d", rel, c.tenant, got, c.want)
			}
		}
	}
	// customers_v: B không thấy khách của A.
	var got int
	inTenant(t, e.staff.App, e.b, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM customer.customers_v WHERE id = $1", e.custA).Scan(&got)
	})
	if got != 0 {
		t.Errorf("tenant B thấy %d khách của A qua customers_v", got)
	}
}

func TestSchemaViewsAndRules(t *testing.T) {
	e := newSchemaEnv(t)
	owner := e.staff.Owner
	want := map[string]string{
		"customers_v": "id, root_id, full_name, created_at, tenant_id, code, lifecycle, birthday, order_count, " +
			"delivered_count, returned_count, total_spent, last_order_at, updated_at, version",
		"customer_identities_v": "tenant_id, id, customer_id, kind, scope, value, verified_at, source, created_at",
		"customer_addresses_v": "tenant_id, id, customer_id, recipient_name, phone, province_code, ward_code, street, " +
			"is_default",
		"customer_tags_v":      "tenant_id, id, name, color",
		"customer_tag_links_v": "tenant_id, customer_id, tag_id, source",
	}
	for view, cols := range want {
		var names []string
		for _, c := range viewColumns(t, owner, view) {
			names = append(names, c.name)
		}
		if got := strings.Join(names, ", "); got != cols {
			t.Errorf("%s: cột %q, mong %q", view, got, cols)
		}
		var invoker bool
		if err := owner.Pool.QueryRow(t.Context(), `
			SELECT coalesce('security_invoker=true' = ANY (c.reloptions), false) FROM pg_class c
			WHERE c.oid = ('customer.' || $1)::regclass`, view).Scan(&invoker); err != nil {
			t.Fatal(err)
		}
		if !invoker {
			t.Errorf("%s thiếu security_invoker", view)
		}
	}

	// Bảng customer.* không có tenant_id chỉ là danh mục dùng chung admin_units (Task M2-04, dữ liệu tham chiếu).
	rows, err := owner.Pool.Query(t.Context(), `
		SELECT c.relname::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'customer' AND c.relkind = 'r' AND NOT EXISTS (
			SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped)`)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(shared, []string{"admin_units"}) {
		t.Errorf("bảng customer.* không có tenant_id: %v, chỉ được là [admin_units]", shared)
	}

	// Mọi index của bảng customer.* có tenant_id bắt đầu bằng tenant_id.
	rows, err = owner.Pool.Query(t.Context(), `
		SELECT ic.relname::text FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = i.indkey[0]
		WHERE n.nspname = 'customer' AND NOT i.indisprimary AND a.attname <> 'tenant_id'
		  AND c.relname <> ALL ($1::text[])`, shared)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Errorf("index không bắt đầu bằng tenant_id: %v", bad)
	}

	got, err := tenancy.CheckSchemaRules(t.Context(), owner.Pool, []string{"platform.tenant_routes", "platform.jobs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("CheckSchemaRules phải rỗng, có:\n%s", strings.Join(got, "\n"))
	}
}

// Ràng buộc mới của customers: lifecycle, chỉ số không âm; khóa màu nhãn.
func TestSchemaChecks(t *testing.T) {
	e := newSchemaEnv(t)
	ctx := t.Context()
	for _, q := range []string{
		"UPDATE customer.customers SET lifecycle = 'vip' WHERE id = $1",
		"UPDATE customer.customers SET order_count = -1 WHERE id = $1",
		"UPDATE customer.customers SET total_spent = -1 WHERE id = $1",
	} {
		err := tenancy.WithTenant(ctx, e.staff.App, e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, q, e.custA)
			return err //nolint:wrapcheck // test
		})
		if pgCode(err) != "23514" {
			t.Errorf("%q: %v, mong 23514", q, err)
		}
	}
	for _, l := range []customer.Lifecycle{customer.LifecycleLead, customer.LifecycleCustomer} {
		inTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE customer.customers SET lifecycle = $2 WHERE id = $1", e.custA, string(l))
			return err //nolint:wrapcheck // test
		})
	}
	for _, k := range []customer.IdentityKind{
		customer.IdentityPhone, customer.IdentityFBPSID, customer.IdentityZaloUID, customer.IdentityEmail,
		customer.IdentityWebAccount,
	} {
		inTenant(t, e.staff.App, e.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, value, source)
				VALUES ($1, $2, $3, 'v', 'staff')`, e.a, e.custA, string(k))
			return err //nolint:wrapcheck // test
		})
	}
	err := tenancy.WithTenant(ctx, e.staff.App, e.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO customer.customer_tags (tenant_id, name, color) VALUES ($1, 'x', '#ff0000')", e.a)
		return err //nolint:wrapcheck // test
	})
	if pgCode(err) != "23514" {
		t.Errorf("màu ngoài danh sách: %v, mong 23514", err)
	}
}
