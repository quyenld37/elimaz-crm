package db_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/migrations"
)

const measureMmMigration = "0541_catalog_product_measure_mm.sql"

// migrationsBefore trả FS gồm mọi migration thật có tên nhỏ hơn last.
func migrationsBefore(t *testing.T, last string) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for _, n := range migrationNames(t) {
		if n >= last {
			continue
		}
		b, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			t.Fatal(err)
		}
		m[n] = &fstest.MapFile{Data: b}
	}
	return m
}

type measureRow struct{ l, w, h *int32 }

// Migration 0541 (Task 517): số đo cm số nguyên của 0535 thành mm (nhân 10) ở mọi tenant dù RLS ép cả chủ bảng; số
// rỗng giữ rỗng; view products_v đọc length_mm…; CHECK mới nhận 1 và 10000 (0,1 và 1000,0 cm), chặn 0 và 10001
// (SQLSTATE 23514); bảng ép RLS lại sau migration.
func TestMigration0541_MeasureCmToMm(t *testing.T) {
	t.Parallel()
	_, owner, _ := testkit.CreateDatabase(t)
	if _, err := db.MigrateFS(t.Context(), owner, migrationsBefore(t, measureMmMigration)); err != nil {
		t.Fatalf("migrate tới trước 0541: %v", err)
	}
	conn := ownerConn(t, owner)
	seed := map[string]measureRow{"A": {ptr32(72), ptr32(20), nil}, "B": {}, "C": {ptr32(1), ptr32(1000), ptr32(5)}}
	for code, r := range seed {
		seedMeasureRow(t, conn, code, r) // mỗi mẫu một tenant riêng
	}
	if _, err := db.MigrateFS(t.Context(), owner, realPlus(t, nil)); err != nil {
		t.Fatalf("migrate 0541: %v", err)
	}
	var forced bool
	if err := conn.QueryRow(t.Context(), "SELECT relforcerowsecurity FROM pg_class WHERE oid = 'catalog.products'::regclass").
		Scan(&forced); err != nil || !forced {
		t.Fatalf("RLS phải được ép lại: %v %v", forced, err)
	}
	if _, err := conn.Exec(t.Context(), "ALTER TABLE catalog.products NO FORCE ROW LEVEL SECURITY"); err != nil {
		t.Fatal(err) // database test riêng: chủ bảng đọc mọi tenant
	}
	got := readMeasureMm(t, conn)
	want := map[string]measureRow{"A": {ptr32(720), ptr32(200), nil}, "B": {}, "C": {ptr32(10), ptr32(10000), ptr32(50)}}
	eq := func(a, b *int32) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	if len(got) != len(want) {
		t.Fatalf("view có %d mẫu, mong %d", len(got), len(want))
	}
	for c, w := range want {
		if g := got[c]; !eq(g.l, w.l) || !eq(g.w, w.w) || !eq(g.h, w.h) {
			t.Errorf("mẫu %s: %v %v %v", c, g.l, g.w, g.h)
		}
	}
	for _, tc := range []struct {
		v  int
		ok bool
	}{{1, true}, {10000, true}, {0, false}, {10001, false}} {
		err := pgx.BeginFunc(t.Context(), conn, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), "UPDATE catalog.products SET width_mm = $1 WHERE code = 'B'", tc.v)
			return err //nolint:wrapcheck // test
		})
		var pgErr *pgconn.PgError
		if tc.ok != (err == nil) || !tc.ok && (!errors.As(err, &pgErr) || pgErr.Code != "23514") {
			t.Errorf("width_mm = %d: %v", tc.v, err)
		}
	}
}

// seedMeasureRow chèn (chủ bảng, schema trước 0541) một tenant, người bán và mẫu code có số đo cm r.
func seedMeasureRow(t *testing.T, conn *pgx.Conn, code string, r measureRow) {
	t.Helper()
	ctx := t.Context()
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var tenant, seller string
		if err := tx.QueryRow(ctx, "INSERT INTO platform.tenants (name) VALUES ('t') RETURNING id").Scan(&tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(ctx, `INSERT INTO catalog.sellers (tenant_id, code, name, kind) VALUES ($1, 'S', 'S', 'own')
			RETURNING id`, tenant).Scan(&seller); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(ctx, `INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, length_cm,
			width_cm, height_cm) VALUES ($1, $2, $3, 'n', 'n', $4, $5, $6)`, tenant, seller, code, r.l, r.w, r.h)
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		t.Fatal(err)
	}
}

// readMeasureMm đọc số đo mm của mọi mẫu qua catalog.products_v (theo mã mẫu).
func readMeasureMm(t *testing.T, conn *pgx.Conn) map[string]measureRow {
	t.Helper()
	rows, err := conn.Query(t.Context(), "SELECT code, length_mm, width_mm, height_mm FROM catalog.products_v")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]measureRow{}
	for rows.Next() {
		var c string
		var r measureRow
		if err := rows.Scan(&c, &r.l, &r.w, &r.h); err != nil {
			t.Fatal(err)
		}
		got[c] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func ptr32(v int32) *int32 { return &v }
