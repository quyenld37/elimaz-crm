package tenancy_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Lấy lại kết quả phép thử ngắn số 1 của Task 2 (scripts/backup/search-plan-probe.sql) bằng mã Go: tìm tên dưới RLS.
const (
	searchTenantA = "00000000-0000-0000-0000-00000000000a"
	searchTenantB = "00000000-0000-0000-0000-00000000000b"
)

// probeSeed dựng probe.names (200.000 tên chia đều hai tenant, index trigram) và hàm probe.search do crm_search làm
// chủ, chép theo search-plan-probe.sql. Chạy bằng người quản trị (đổi chủ hàm sang crm_search cần quyền đó); dựng một
// lần làm database mẫu (testkit.StartPostgresSeeded).
func probeSeed() testkit.Seed {
	return testkit.Seed{Name: "tenancy-search-probe", SQL: []string{
		"CREATE SCHEMA probe",
		`CREATE TABLE probe.names (
			id uuid NOT NULL DEFAULT uuidv7() PRIMARY KEY,
			tenant_id uuid NOT NULL,
			name_search text NOT NULL,
			UNIQUE (tenant_id, id)
		)`,
		`INSERT INTO probe.names (tenant_id, name_search)
		SELECT CASE WHEN g % 2 = 0 THEN '` + searchTenantA + `'::uuid ELSE '` + searchTenantB + `'::uuid END,
			(ARRAY['nguyen','tran','le','pham','hoang','huynh','phan','vu','vo','dang'])[1 + g % 10] || ' ' ||
			(ARRAY['thi','van','ngoc','minh','thu','hoang','thanh','duc'])[1 + (g / 10) % 8] || ' ' ||
			(ARRAY['anh','hoa','lan','tuyet','mai','huong','linh','trang','phuong','nga','hung','nam'])[1 + (g / 80) % 12] || ' ' || g
		FROM generate_series(1, 200000) AS g`,
		"CREATE INDEX names_name_search_trgm ON probe.names USING gin (name_search gin_trgm_ops)",
		"SELECT platform.enable_tenant_rls('probe.names')",
		`CREATE FUNCTION probe.search(q text, lim int)
		RETURNS TABLE (id uuid, name_search text)
		LANGUAGE sql STABLE SECURITY DEFINER
		SET search_path = pg_catalog, pg_temp
		AS $fn$
			SELECT n.id, n.name_search
			FROM probe.names AS n
			WHERE n.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
				AND n.name_search LIKE '%' || replace(replace(replace(q, '\', '\\'), '%', '\%'), '_', '\_') || '%'
			ORDER BY n.id
			LIMIT least(lim, 100)
		$fn$`,
		"GRANT USAGE ON SCHEMA probe TO crm_search, crm_app",
		"GRANT SELECT ON probe.names TO crm_search, crm_app",
		"ALTER FUNCTION probe.search(text, int) OWNER TO crm_search",
		"REVOKE EXECUTE ON FUNCTION probe.search(text, int) FROM PUBLIC",
		"GRANT EXECUTE ON FUNCTION probe.search(text, int) TO crm_app",
		"ANALYZE probe.names",
	}}
}

func searchIn(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, term string,
) ([]string, error) {
	rows, err := q.Query(ctx, "SELECT name_search FROM probe.search($1, 100)", term)
	if err != nil {
		return nil, fmt.Errorf("gọi probe.search: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("gọi probe.search: %w", err)
	}
	return names, nil
}

// searchApp lấy database thử có dữ liệu tìm (probeSeed), bật auto_explain, rồi mở pool crm_app.
func searchApp(t *testing.T) *db.DB {
	t.Helper()
	return probeApp(t, true)
}

// probeApp lấy database thử có dữ liệu tìm (probeSeed), bật auto_explain nếu explain, rồi mở pool crm_app (tham số
// của role chỉ áp cho kết nối mở sau khi đặt).
func probeApp(t *testing.T, explain bool) *db.DB {
	t.Helper()
	adminURL, _, appURL := testkit.StartPostgresSeeded(t, probeSeed())
	if explain {
		testkit.EnableAutoExplain(t, adminURL)
	}
	app, err := db.Open(t.Context(), appURL, db.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Pool.Close)
	return app
}

// searchAs gọi probe.search(term) trong WithTenant(tenant).
func searchAs(t *testing.T, app *db.DB, tenant, term string) []string {
	t.Helper()
	var out []string
	err := tenancy.WithTenant(t.Context(), app, uuid.MustParse(tenant), func(tx pgx.Tx) error {
		var err error
		out, err = searchIn(t.Context(), tx, term)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Hàm SECURITY DEFINER trong WithTenant(A) chỉ ra dòng của A, bằng đúng câu thẳng dưới RLS; ngoài ngữ cảnh 0 dòng;
// ký tự đại diện trong q không thành "khớp mọi thứ".
func TestSearchFunctionSeesOnlyOwnTenant(t *testing.T) {
	app := searchApp(t)
	// "tuyet 123" khớp tên của cả hai tenant (số chẵn thuộc A, số lẻ thuộc B); "tuyet 1234" chỉ khớp một dòng của A.
	inA, inB := searchAs(t, app, searchTenantA, "tuyet 123"), searchAs(t, app, searchTenantB, "tuyet 123")
	if len(inA) == 0 || len(inB) == 0 {
		t.Fatalf("mỗi tenant phải tìm thấy dòng: A %d, B %d", len(inA), len(inB))
	}
	for _, n := range inA {
		if slices.Contains(inB, n) {
			t.Fatalf("dòng %q hiện ở cả A và B", n)
		}
	}
	if got := searchAs(t, app, searchTenantB, "tuyet 1234"); len(got) != 0 {
		t.Fatalf("tenant B thấy dòng của A: %v", got)
	}
	if got := searchAs(t, app, searchTenantA, "%"); len(got) != 0 {
		t.Fatalf("q = %%%% trả %d dòng, muốn 0", len(got))
	}
	direct := 0
	err := tenancy.WithTenant(t.Context(), app, uuid.MustParse(searchTenantA), func(tx pgx.Tx) error {
		err := tx.QueryRow(t.Context(),
			"SELECT count(*) FROM (SELECT 1 FROM probe.names WHERE name_search LIKE '%tuyet 123%' LIMIT 100) s").Scan(&direct)
		if err != nil {
			return fmt.Errorf("đếm câu thẳng: %w", err)
		}
		return nil
	})
	if err != nil || direct != len(inA) {
		t.Fatalf("hàm trả %d dòng, câu thẳng trả %d (err %v): phải bằng nhau", len(inA), direct, err)
	}
	if got, err := searchIn(t.Context(), app.Pool, "tuyet 123"); err != nil || len(got) != 0 {
		t.Fatalf("ngoài ngữ cảnh hàm trả %v (err %v), muốn 0 dòng", got, err)
	}
}

// Kế hoạch: qua hàm dùng index trigram (RequireNoSeqScan đạt); LIKE thẳng trên bảng có RLS là Seq Scan.
func TestSearchPlans(t *testing.T) {
	app := searchApp(t)
	a := uuid.MustParse(searchTenantA)
	viaFn, err := testkit.ExplainAsApp(t.Context(), app, a, "SELECT * FROM probe.search($1, 100)", "tuyet 1234")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(viaFn, "Bitmap Index Scan") {
		t.Fatalf("kế hoạch bên trong hàm phải dùng index trigram, nhận %v", viaFn)
	}
	testkit.RequireNoSeqScan(t, viaFn)

	plain, err := testkit.ExplainAsApp(t.Context(), app, a,
		"SELECT id, name_search FROM probe.names WHERE name_search LIKE $1 ORDER BY id LIMIT 100", "%tuyet 1234%")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plain, "Seq Scan") {
		t.Fatalf("LIKE thẳng trên bảng có RLS phải là Seq Scan, nhận %v", plain)
	}
}

// Thiếu auto_explain thì không đọc được kế hoạch trong hàm: ExplainAsApp phải báo lỗi chứ không trả mỗi
// "Function Scan" (khiến RequireNoSeqScan đạt sai).
func TestExplainAsAppNeedsAutoExplainForFunctions(t *testing.T) {
	app := probeApp(t, false)
	nodes, err := testkit.ExplainAsApp(t.Context(), app, uuid.MustParse(searchTenantA),
		"SELECT * FROM probe.search($1, 100)", "tuyet 1234")
	if err == nil {
		t.Fatalf("muốn lỗi khi auto_explain chưa bật, nhận kế hoạch %v", nodes)
	}
}

// RequireNoSeqScan phải báo lỗi khi có Seq Scan.
func TestRequireNoSeqScanFails(t *testing.T) {
	ft := &fakeTB{TB: t}
	testkit.RequireNoSeqScan(ft, []string{"Limit", "Seq Scan"})
	if !ft.failed {
		t.Fatal("RequireNoSeqScan không báo Seq Scan")
	}
	ft = &fakeTB{TB: t}
	testkit.RequireNoSeqScan(ft, []string{"Limit", "Bitmap Heap Scan", "Bitmap Index Scan"})
	if ft.failed {
		t.Fatal("RequireNoSeqScan báo sai khi không có Seq Scan")
	}
}

type fakeTB struct {
	testing.TB
	failed bool
}

func (f *fakeTB) Helper()               {}
func (f *fakeTB) Errorf(string, ...any) { f.failed = true }
func (f *fakeTB) Fatalf(string, ...any) { f.failed = true }
