package arch

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/testkit"
)

var (
	backtickRe = regexp.MustCompile("`([^`]+)`")
	viewNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*_v$`)
	indexRe    = regexp.MustCompile(`^[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*$`)
)

// viewsDoc là nội dung đã đọc của docs/views.md: view công bố (tên đầy đủ → cột theo thứ tự) và index được miễn luật
// (g) của CheckSchemaRules.
type viewsDoc struct {
	views  map[string][]string
	exempt []string
}

// readViewsDoc đọc root/docs/views.md: bảng dưới "## View công bố" (ô 1: `schema.tên_v`, ô 2: `cột, cột`) và bảng dưới
// "## Ngoại lệ luật (g)" (ô 1: `schema.index`). Dòng sai dạng trong hai bảng đó là lỗi.
func readViewsDoc(t *testing.T, root string) viewsDoc {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "docs", "views.md"))
	if err != nil {
		t.Fatalf("đọc docs/views.md: %v", err)
	}
	doc, err := parseViewsDoc(b)
	if err != nil {
		t.Fatalf("docs/views.md: %v", err)
	}
	return doc
}

func parseViewsDoc(b []byte) (viewsDoc, error) {
	doc := viewsDoc{views: map[string][]string{}}
	section := ""
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if title, ok := strings.CutPrefix(line, "## "); ok {
			section = title
			continue
		}
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|---") {
			continue
		}
		var err error
		switch section {
		case "View công bố":
			err = doc.addView(strings.Split(strings.Trim(line, "|"), "|"))
		case "Ngoại lệ luật (g)":
			err = doc.addExempt(strings.Split(strings.Trim(line, "|"), "|"))
		}
		if err != nil {
			return doc, fmt.Errorf("dòng %d: %w: %q", n, err, line)
		}
	}
	if err := sc.Err(); err != nil {
		return doc, fmt.Errorf("đọc: %w", err)
	}
	slices.Sort(doc.exempt)
	return doc, nil
}

var errViewsDocRow = errors.New("dòng bảng sai dạng")

// addView đọc một dòng của bảng "View công bố" (bỏ dòng tiêu đề).
func (d *viewsDoc) addView(cells []string) error {
	if strings.TrimSpace(cells[0]) == "View" {
		return nil
	}
	first := backtickRe.FindStringSubmatch(cells[0])
	if first == nil || !viewNameRe.MatchString(first[1]) || len(cells) < 2 {
		return fmt.Errorf("%w: ô đầu phải là `schema.tên_v`", errViewsDocRow)
	}
	cols := backtickRe.FindStringSubmatch(cells[1])
	if cols == nil {
		return fmt.Errorf("%w: ô thứ hai phải là `cột, cột`", errViewsDocRow)
	}
	if _, dup := d.views[first[1]]; dup {
		return fmt.Errorf("%w: view %s ghi hai lần", errViewsDocRow, first[1])
	}
	d.views[first[1]] = strings.Split(strings.ReplaceAll(cols[1], " ", ""), ",")
	return nil
}

// addExempt đọc một dòng của bảng "Ngoại lệ luật (g)" (bỏ dòng tiêu đề).
func (d *viewsDoc) addExempt(cells []string) error {
	if strings.TrimSpace(cells[0]) == "Index" {
		return nil
	}
	first := backtickRe.FindStringSubmatch(cells[0])
	if first == nil || !indexRe.MatchString(first[1]) {
		return fmt.Errorf("%w: ô đầu phải là `schema.index`", errViewsDocRow)
	}
	d.exempt = append(d.exempt, first[1])
	return nil
}

// dbViews trả mọi view thường có tên _v trong database (ngoài schema hệ thống) với cột theo thứ tự.
func dbViews(t *testing.T, conn *pgx.Conn) map[string][]string {
	t.Helper()
	rows, err := conn.Query(t.Context(), `
		SELECT format('%s.%s', n.nspname, c.relname), a.attname
		FROM pg_catalog.pg_class AS c
		JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_attribute AS a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		WHERE c.relkind = 'v' AND c.relname LIKE '%\_v'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\_%'
		ORDER BY 1, a.attnum`)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for rows.Next() {
		var view, col string
		if err := rows.Scan(&view, &col); err != nil {
			t.Fatal(err)
		}
		out[view] = append(out[view], col)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// dbExemptIndexes trả mọi index có ghi chú bắt đầu bằng "seller-exempt:".
func dbExemptIndexes(t *testing.T, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(t.Context(), `
		SELECT format('%s.%s', n.nspname, c.relname)
		FROM pg_catalog.pg_class AS c
		JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
		WHERE c.relkind = 'i' AND pg_catalog.obj_description(c.oid, 'pg_class') LIKE 'seller-exempt:%'
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// compareViews trả mỗi chỗ lệch giữa database và tài liệu một dòng. View trong tài liệu mà database chưa có thì bỏ qua
// (task tạo view chưa làm).
func compareViews(db map[string][]string, doc viewsDoc, dbExempt []string) []string {
	var out []string
	for view, cols := range db {
		want, ok := doc.views[view]
		switch {
		case !ok:
			out = append(out, view+": có trong database nhưng thiếu dòng trong docs/views.md")
		case !slices.Equal(cols, want):
			out = append(out, fmt.Sprintf("%s: cột trong database %v khác docs/views.md %v", view, cols, want))
		}
	}
	if !slices.Equal(dbExempt, doc.exempt) && (len(dbExempt) > 0 || len(doc.exempt) > 0) {
		out = append(out, fmt.Sprintf("index seller-exempt trong database %v khác mục \"Ngoại lệ luật (g)\" %v", dbExempt, doc.exempt))
	}
	slices.Sort(out)
	return out
}

func ownerConn(t *testing.T, ownerURL string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(t.Context()) })
	return conn
}

func TestViewsDocParses(t *testing.T) {
	doc := readViewsDoc(t, repoRoot(t))
	if got := doc.views["inventory.stock_available_v"]; !slices.Equal(got, []string{
		"variant_id", "warehouse_id", "seller_id", "on_hand", "held", "in_transit", "damaged", "available", "updated_at",
	}) {
		t.Fatalf("inventory.stock_available_v đọc sai: %v", got)
	}
	// 20 view của M1, 10 view mới của M2 viết trước ở Task M2-02 (kế hoạch M2; customers_v, orders_v là của M1) và 3 view
	// mới của M3 Task 92 (carrier_accounts_v, shipment_events_v, locations_v), 5 view mới của M3 Task 93 (shipment_cod_v,
	// cod_transfers_v, payments_v, refund_obligations_v, reconcile_runs_v), 1 view mới của M3 Task 109 (admin_units_v),
	// 2 view mới của M5 Task 452 (web_campaigns_v, web_campaign_targets_v), 1 view mới của M5 Task 468
	// (birthday_profiles_v), 1 view mới của M4 Task 145 (channel_accounts_v).
	if len(doc.views) != 43 {
		t.Fatalf("docs/views.md phải có 43 view (20 của M1, 10 mới của M2, 9 mới của M3, 3 mới của M5, 1 mới của M4), có %d", len(doc.views))
	}
	for _, bad := range []string{"| `catalog.variants` | `a` | 27 | M2 |", "| `catalog.x_v` | a, b | 27 | M2 |"} {
		if _, err := parseViewsDoc([]byte("## View công bố\n\n| View | Cột | Task | Mốc |\n|---|---|---|---|\n" + bad + "\n")); err == nil {
			t.Errorf("dòng sai dạng phải lỗi: %s", bad)
		}
	}
}

// Mọi view _v trong database đã migrate có dòng đúng cột trong docs/views.md; ca thử: view lạ, view sai cột, index
// miễn luật (g) không có trong tài liệu đều bị báo.
func TestViewsDocMatchesDatabase(t *testing.T) {
	t.Parallel()
	_, owner, _ := testkit.StartPostgres(t)
	conn := ownerConn(t, owner)
	doc := readViewsDoc(t, repoRoot(t))
	if got := compareViews(dbViews(t, conn), doc, dbExemptIndexes(t, conn)); len(got) != 0 {
		t.Fatalf("database lệch docs/views.md:\n%s", strings.Join(got, "\n"))
	}
	for _, s := range []string{
		"CREATE SCHEMA probe",
		"CREATE TABLE probe.t (tenant_id uuid, seller_id uuid, sku text)",
		"CREATE VIEW probe.stray_v WITH (security_invoker = true) AS SELECT sku FROM probe.t",
		"CREATE VIEW probe.wrongcols_v WITH (security_invoker = true) AS SELECT seller_id, sku AS code FROM probe.t",
		"CREATE UNIQUE INDEX t_sku ON probe.t (tenant_id, sku)",
		"COMMENT ON INDEX probe.t_sku IS 'seller-exempt: thử'",
	} {
		if _, err := conn.Exec(t.Context(), s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	// View có dòng trong tài liệu nhưng sai cột: thêm dòng giả vào bản đọc (không dùng view thật của M1, task sau tạo).
	doc.views["probe.wrongcols_v"] = []string{"seller_id", "code", "name"}
	got := compareViews(dbViews(t, conn), doc, dbExemptIndexes(t, conn))
	want := []string{"index seller-exempt", "probe.stray_v: có trong database", "probe.wrongcols_v: cột"}
	if len(got) != len(want) {
		t.Fatalf("muốn %d dòng, nhận %q", len(want), got)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("dòng %d: muốn bắt đầu %q, nhận %q", i, want[i], got[i])
		}
	}
}
