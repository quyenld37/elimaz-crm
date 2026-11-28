package customer_test

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/textnorm"
	"elimaz.com.vn/crm/internal/testkit"
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

var khCode = regexp.MustCompile(`^KH\d{6,}$`)

// Mở rộng bảng của M1: khách có sẵn trước 0201 được điền code, name_search; root_id giữ; customer.Create của M1 vẫn
// chạy sau migrate; customers_v giữ 4 cột đầu cùng kiểu.
func TestSchemaExtendsM1Customers(t *testing.T) {
	t.Parallel()
	_, ownerURL, appURL := testkit.CreateDatabase(t)
	testkit.MigrateBefore(t, ownerURL, "0201")
	owner, app := testkit.OpenDB(t, ownerURL), testkit.OpenDB(t, appURL)

	var tenant uuid.UUID
	if err := owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ('A') RETURNING id").
		Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	seeded := seedBefore0201(t, owner, app, tenant)
	before := testkit.Columns(t, owner, "customer", "customers_v")

	if _, err := db.Migrate(t.Context(), ownerURL); err != nil {
		t.Fatalf("migrate 0201: %v", err)
	}

	codes := checkBackfilled(t, owner, tenant)
	if len(codes) != seeded {
		t.Fatalf("có %d mã khách khác nhau, mong %d", len(codes), seeded)
	}

	// customers_v: 4 cột của M1 ở vị trí 1–4 cùng kiểu, rồi 11 cột M2.
	after := testkit.Columns(t, owner, "customer", "customers_v")
	// Mốc sau nối cột sau version (R3; 0403 của Task 152 có test riêng): chỉ xét 15 cột đầu.
	if len(before) != 4 || len(after) < 15 {
		t.Fatalf("customers_v trước 0201 %v, sau %v", before, after)
	}
	expectColumnsAppended(t, before, after[:15], 11)
	checkM1CreateAfter(t, app, tenant, codes)
}

// seedBefore0201 tạo 3 khách bằng đúng câu chèn của M1 (InsertCustomer của Task 30, chạy bằng crm_app; customer.Create
// nay ghi cột M2 nên không chạy được trên schema trước 0201) và chèn thẳng các mẫu bỏ dấu bằng chủ bảng (giữ nguyên
// khoảng trắng hai đầu, tab, xuống dòng); trả số khách.
func seedBefore0201(t *testing.T, owner, app *db.DB, tenant uuid.UUID) int {
	t.Helper()
	ctx := t.Context()
	m1Names := []string{"Chị Lan", "Nguyễn Thị Hồng Nhung", "Anh Bình"}
	testkit.InTenant(t, app, tenant, func(tx pgx.Tx) error {
		for _, n := range m1Names {
			if _, err := tx.Exec(ctx, `INSERT INTO customer.customers (id, tenant_id, root_id, full_name)
				SELECT g, NULLIF(current_setting('app.tenant_id', true), '')::uuid, g, $1 FROM uuidv7() AS g`, n); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	samples := foldSamples()
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
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
	testkit.InTenant(t, owner, tenant, func(tx pgx.Tx) error {
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
	testkit.InTenant(t, app, tenant, func(tx pgx.Tx) error {
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
		testkit.InTenant(t, e.staff.App, p.tenant, func(tx pgx.Tx) error {
			id, err := customer.Create(t.Context(), tx, "Khách")
			*p.id = id
			return err //nolint:wrapcheck // test
		})
	}
	return e
}

// inA chạy fn trong transaction của tenant A (database lỗi thì test dừng).
func (e *schemaEnv) inA(t *testing.T, fn func(tx pgx.Tx) error) {
	t.Helper()
	testkit.InTenant(t, e.staff.App, e.a, fn)
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func TestSchemaRootDefaultAndIdentityUnique(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	// Chèn không có root_id → root_id = id.
	e.inA(t, func(tx pgx.Tx) error {
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
	e.inA(t, func(tx pgx.Tx) error {
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
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	e.inA(t, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO customer.customer_notes (tenant_id, customer_id, body, author_id)
			VALUES ($1, $2, 'ghi chú', $3)`, e.a, e.custA, uuid.New())
		return err //nolint:wrapcheck // test
	})
	qs := []string{"UPDATE customer.customer_notes SET body = 'sửa'", "DELETE FROM customer.customer_notes"}
	e.staff.CheckOwnerAppendOnly(t, e.a, qs...)
	for _, q := range qs {
		err := tenancy.WithTenant(ctx, e.staff.App, e.a, func(tx pgx.Tx) error {
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
	t.Parallel()
	e := newSchemaEnv(t)
	ctx := t.Context()
	e.inA(t, func(tx pgx.Tx) error {
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
			testkit.InTenant(t, e.staff.App, c.tenant, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, q, e.a).Scan(&got)
			})
			if got != c.want {
				t.Errorf("%s nhìn từ tenant %s: %d dòng của A, mong %d", rel, c.tenant, got, c.want)
			}
		}
	}
	// customers_v: B không thấy khách của A.
	var got int
	testkit.InTenant(t, e.staff.App, e.b, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM customer.customers_v WHERE id = $1", e.custA).Scan(&got)
	})
	if got != 0 {
		t.Errorf("tenant B thấy %d khách của A qua customers_v", got)
	}
}

func TestSchemaViewsAndRules(t *testing.T) {
	t.Parallel()
	e := newSchemaEnv(t)
	owner := e.staff.Owner
	want := map[string]string{
		"customers_v": "id, root_id, full_name, created_at, tenant_id, code, lifecycle, birthday, order_count, " +
			"delivered_count, returned_count, total_spent, last_order_at, updated_at, version, do_not_contact, name_source, " +
			"return_rate_bp", // M6 Task 238 (0605) nối cuối
		"customer_identities_v": "tenant_id, id, customer_id, kind, scope, value, verified_at, source, created_at",
		"customer_addresses_v": "tenant_id, id, customer_id, recipient_name, phone, province_code, ward_code, street, " +
			"is_default",
		"customer_tags_v":      "tenant_id, id, name, color",
		"customer_tag_links_v": "tenant_id, customer_id, tag_id, source",
	}
	testkit.CheckViews(t, owner, "customer", want)

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

	testkit.CheckSchemaRulesClean(t, owner)
}

// Ràng buộc mới của customers: lifecycle, chỉ số không âm; khóa màu nhãn.
func TestSchemaChecks(t *testing.T) {
	t.Parallel()
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
		e.inA(t, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE customer.customers SET lifecycle = $2 WHERE id = $1", e.custA, string(l))
			return err //nolint:wrapcheck // test
		})
	}
	for _, k := range []customer.IdentityKind{
		customer.IdentityPhone, customer.IdentityFBPSID, customer.IdentityZaloUID, customer.IdentityEmail,
		customer.IdentityWebAccount,
	} {
		e.inA(t, func(tx pgx.Tx) error {
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

// expectColumnsAppended: cột của customers_v trước migration là tiền tố đúng thứ tự, cùng kiểu của cột sau; sau có
// đúng added cột mới ở cuối (R3).
func expectColumnsAppended(t *testing.T, before, after []testkit.Column, added int) {
	t.Helper()
	if len(after) != len(before)+added {
		t.Fatalf("customers_v trước %v, sau %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("cột %d của customers_v đổi: %v → %v", i+1, before[i], after[i])
		}
	}
}
