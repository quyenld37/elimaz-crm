package customer_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// storeSQL trả câu SQL sqlc đã sinh cho truy vấn name (đúng câu ứng dụng chạy) từ mã sinh của store (mọi tệp
// internal/store/*.sql.go; Task 486 thêm feedback.sql.go).
func storeSQL(t *testing.T, name string) string {
	t.Helper()
	paths, err := filepath.Glob("internal/store/*.sql.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile("(?s)const " + regexp.QuoteMeta(name) + " = `(.*?)`")
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if m := re.FindSubmatch(src); m != nil {
			return string(m[1])
		}
	}
	t.Fatalf("không thấy câu %s trong store", name)
	return ""
}

// explainSeed: 100.000 khách và 130.000 danh tính SĐT trong tenant A (sinh bằng generate_series), 2.000 khách trong
// tenant B, ANALYZE. Chạy bằng chủ bảng trong ngữ cảnh từng tenant (SET ROLE, set_config: RLS lọc như
// testkit.Staff.Exec trước Task 423); mã tenant cố định để dựng một lần làm database mẫu.
func explainSeed(a, b uuid.UUID) testkit.Seed {
	q := []string{
		"SET ROLE crm_owner",
		fmt.Sprintf("INSERT INTO platform.tenants (id, name) VALUES ('%s', 'A'), ('%s', 'B')", a, b),
	}
	for _, c := range []struct {
		tenant uuid.UUID
		n      int
		prefix string
	}{{a, 100000, "9"}, {b, 2000, "8"}} {
		q = append(q, fmt.Sprintf("SELECT set_config('app.tenant_id', '%s', false)", c.tenant),
			`INSERT INTO customer.customers (tenant_id, full_name, name_search, last_order_at)
				SELECT current_setting('app.tenant_id')::uuid, n, n,
				       CASE WHEN g % 3 = 0 THEN now() - g * interval '1 minute' END
				FROM generate_series(1, `+strconv.Itoa(c.n)+`::int) AS g,
				     LATERAL (SELECT (ARRAY['nguyen','tran','le','pham','hoang','huynh','phan','vu','vo','dang'])[1 + g % 10] || ' ' ||
				        (ARRAY['thi','van','ngoc','minh','thu','hoang','thanh','duc'])[1 + (g / 10) % 8] || ' ' ||
				        (ARRAY['anh','hoa','lan','tuyet','mai','huong','linh','trang','phuong','nga','hung','nam'])[1 + (g / 80) % 12]
				        || ' ' || g AS n) AS x`,
			// Mỗi khách một SĐT; 30% khách có thêm SĐT thứ hai.
			`INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, value, value_reversed, source)
				SELECT tenant_id, id, 'phone', v, reverse(substr(v, 2)), 'import'
				FROM (SELECT tenant_id, id, '+84`+c.prefix+`' || lpad((row_number() OVER (ORDER BY id))::text, 8, '0') AS v
				      FROM customer.customers) AS s`,
			`INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, value, value_reversed, source)
				SELECT tenant_id, id, 'phone', v, reverse(substr(v, 2)), 'import'
				FROM (SELECT tenant_id, id, '+843' || lpad((row_number() OVER (ORDER BY id))::text, 8, '0') AS v, random() AS r
				      FROM customer.customers) AS s
				WHERE r < 0.3`,
			// Hai nhãn; 20% khách có nhãn thứ nhất, 5% có nhãn thứ hai.
			`INSERT INTO customer.customer_tags (tenant_id, name, color)
				VALUES (current_setting('app.tenant_id')::uuid, 'VIP', 'red'), (current_setting('app.tenant_id')::uuid, 'Sỉ', 'blue')`,
			`INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source)
				SELECT c.tenant_id, c.id, t.id, 'staff'
				FROM customer.customers AS c
				JOIN customer.customer_tags AS t ON t.tenant_id = c.tenant_id
				WHERE (t.name = 'VIP' AND abs(hashtext(c.id::text)) % 5 = 0) OR (t.name = 'Sỉ' AND abs(hashtext(c.id::text)) % 20 = 1)`,
		)
	}
	return testkit.Seed{Name: "customer-search-explain", SQL: append(q,
		"ANALYZE customer.customers, customer.customer_identities, customer.customer_tag_links")}
}

// explainFixture lấy database thử có dữ liệu của explainSeed (dựng một lần làm database mẫu), kiểm số danh tính của A;
// auto_explain bật cho crm_app để đọc kế hoạch bên trong hàm.
func explainFixture(t *testing.T) (*db.DB, uuid.UUID) {
	t.Helper()
	a, b := testkit.FixedUUID("customer-search-explain/A"), testkit.FixedUUID("customer-search-explain/B")
	adminURL, ownerURL, appURL := testkit.StartPostgresSeeded(t, explainSeed(a, b))
	var owner, app *db.DB
	for dst, url := range map[**db.DB]string{&owner: ownerURL, &app: appURL} {
		d, err := db.Open(t.Context(), url, db.PoolOptions{MaxConns: 2})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(d.Pool.Close)
		*dst = d
	}
	s := &testkit.Staff{Owner: owner}
	var ids int
	s.Exec(t, a, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT count(*) FROM customer.customer_identities").Scan(&ids)
	})
	if ids < 125000 || ids > 135000 {
		t.Fatalf("danh tính của A: %d, muốn ~130.000", ids)
	}
	// auto_explain áp cho kết nối mở sau lời gọi: ExplainAsApp mở kết nối riêng mỗi lần.
	testkit.EnableAutoExplain(t, adminURL)
	return app, a
}

// Kế hoạch các câu tìm và danh sách bằng crm_app trong ngữ cảnh tenant: không Seq Scan (đuôi số bằng câu khoảng trên
// index value_reversed; tên qua hàm dùng GIN trigram, đọc bằng auto_explain); LIKE thẳng trên bảng có RLS là Seq Scan
// (lý do phải đi qua hàm).
func TestSearchPlans(t *testing.T) {
	t.Parallel()
	app, a := explainFixture(t)
	tag := uuid.New()
	roots := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	after := []any{nil, nil, nil, int32(51)}
	cases := []struct {
		name string
		sql  string
		args []any
	}{
		{"đuôi số 4 chữ số", storeSQL(t, "searchPhoneTail"), []any{"8765", "8766", int32(200)}},
		{"đuôi số 6 chữ số", storeSQL(t, "searchPhoneTail"), []any{"876543", "876544", int32(200)}},
		{"SĐT đủ", storeSQL(t, "searchPhoneExact"), []any{"+84900012345"}},
		{"mã khách", storeSQL(t, "searchCode"), []any{"KH000042"}},
		{"tên qua hàm", storeSQL(t, "searchByName"), []any{[]string{"tuyet", "123"}, int32(200)}},
		{"tên một từ qua hàm", storeSQL(t, "searchByName"), []any{[]string{"huong"}, int32(200)}},
		{"dòng gốc của kết quả", storeSQL(t, "listSearchRoots"), []any{roots, nil, int32(20)}},
		{"dòng gốc của kết quả lọc nhãn", storeSQL(t, "listSearchRoots"), []any{roots, tag, int32(20)}},
		{"SĐT của nhóm", storeSQL(t, "listGroupPhones"), []any{roots}},
		{"danh sách trang đầu", storeSQL(t, "listRoots"), after},
		{"danh sách lọc nhãn", storeSQL(t, "listRoots"), []any{nil, nil, tag, int32(51)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes, err := testkit.ExplainAsApp(t.Context(), app, a, c.sql, c.args...)
			if err != nil {
				t.Fatal(err)
			}
			testkit.RequireNoSeqScan(t, nodes)
			if strings.HasPrefix(c.name, "tên") && !slices.Contains(nodes, "Bitmap Index Scan") {
				t.Errorf("hàm tên phải dùng index trigram: %v", nodes)
			}
		})
	}
	plain, err := testkit.ExplainAsApp(t.Context(), app, a,
		`SELECT id FROM customer.customers WHERE name_search LIKE $1
		ORDER BY last_order_at DESC NULLS LAST, id LIMIT 20`, "%tuyet%123%")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plain, "Seq Scan") {
		t.Fatalf("LIKE thẳng bằng crm_app trên bảng có RLS phải là Seq Scan, nhận %v", plain)
	}
	// Kết quả hàm trên dữ liệu lớn khớp câu thẳng (cùng tenant).
	var viaFn, direct int
	err = tenancy.WithTenant(t.Context(), app, a, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), "SELECT count(*) FROM customer.search_by_name($1, 500)",
			[]string{"tuyet", "1234"}).Scan(&viaFn); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(t.Context(),
			"SELECT count(*) FROM customer.customers WHERE name_search LIKE '%tuyet%' AND name_search LIKE '%1234%'").Scan(&direct)
	})
	if err != nil || viaFn == 0 || viaFn != direct {
		t.Fatalf("hàm %d dòng, câu thẳng %d (err %v)", viaFn, direct, err)
	}
}

// indexCondsInFn chạy sql bằng crm_app trong ngữ cảnh tenant (auto_explain của explainFixture ghi kế hoạch của câu lồng
// trong hàm) và trả "Index Cond" của mọi nút Bitmap Index Scan trên index indexName.
func indexCondsInFn(t *testing.T, app *db.DB, tenant uuid.UUID, indexName, sql string, args ...any) []string {
	t.Helper()
	plans, err := testkit.AutoExplainPlansAsApp(t.Context(), app, tenant, sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	var conds []string
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if n["Node Type"] == "Bitmap Index Scan" && n["Index Name"] == indexName {
			c, _ := n["Index Cond"].(string)
			conds = append(conds, c)
		}
		children, _ := n["Plans"].([]any)
		for _, ch := range children {
			if m, ok := ch.(map[string]any); ok {
				walk(m)
			}
		}
	}
	for _, p := range plans {
		walk(p)
	}
	return conds
}

// Task 88 (thử tải M2: tìm tên p95 580 ms trên 1 triệu khách, DH02 đòi < 0,3 s): hàm tên đưa mọi từ (tối đa ba từ dài
// nhất) vào điều kiện của index trigram để GIN lấy giao các từ, không chỉ từ dài nhất (họ "nguyen" khớp ~38% khách, các
// từ khác lọc sau khi đọc cả trăm nghìn dòng). Một từ, hai từ: điều kiện lặp lại từ dài nhất (không đổi kết quả).
func TestSearchByName_AllWordsInIndexCond(t *testing.T) {
	t.Parallel()
	app, a := explainFixture(t)
	for _, c := range []struct {
		words []string
		want  int // số điều kiện LIKE (~~) trên name_search trong Index Cond
	}{
		{[]string{"nguyen", "thi", "anh"}, 3},
		{[]string{"nguyen", "thi", "anh", "12"}, 3},
		{[]string{"nguyen", "anh"}, 3},
	} {
		conds := indexCondsInFn(t, app, a, "customers_name_search_trgm", storeSQL(t, "searchByName"), c.words, int32(200))
		if len(conds) != 1 {
			t.Fatalf("%v: %d lần quét index trigram (%v), muốn 1", c.words, len(conds), conds)
		}
		if got := strings.Count(conds[0], "name_search ~~"); got != c.want {
			t.Errorf("%v: Index Cond có %d điều kiện LIKE, muốn %d: %s", c.words, got, c.want, conds[0])
		}
	}
}
