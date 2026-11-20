package arch

import (
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var (
	sqlTokenRe = regexp.MustCompile(`[A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)*|\S`)
	identRe    = regexp.MustCompile(`^[A-Za-z_][\w$]*(?:\.[A-Za-z_][\w$]*)?$`)
	cteRe      = regexp.MustCompile(`(?i)(?:\bWITH\s+(?:RECURSIVE\s+)?|,\s*)([A-Za-z_]\w*)\s+AS\s+(?:NOT\s+)?(?:MATERIALIZED\s+)?\(`)
	lineCmtRe  = regexp.MustCompile(`--[^\n]*`)
	strLitRe   = regexp.MustCompile(`'(?:[^']|'')*'`)
)

// endsFromList: từ khóa kết thúc danh sách bảng sau FROM hay USING (và không thể là tên bảng).
func endsFromList(up string) bool {
	switch up {
	case "WHERE", "GROUP", "ORDER", "LIMIT", "OFFSET", "HAVING", "WINDOW", "UNION", "INTERSECT", "EXCEPT",
		"RETURNING", "ON", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "NATURAL", "SET", "FOR", "USING",
		"FETCH", "WHEN", "SELECT", "VALUES":
		return true
	}
	return false
}

// joinWord: từ của mệnh đề JOIN, không kết thúc danh sách bảng của FROM.
func joinWord(up string) bool {
	switch up {
	case "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "OUTER", "CROSS", "NATURAL", "ON", "USING":
		return true
	}
	return false
}

// sqlTables dò tên bảng trên dãy token của một đoạn SQL.
type sqlTables struct {
	toks   []string
	inCall []bool // token nằm trong ngoặc của một lời gọi hàm (không phải truy vấn con)
	out    []string
}

func newSQLTables(sql string) *sqlTables {
	st := &sqlTables{toks: sqlTokenRe.FindAllString(sql, -1)}
	st.inCall = make([]bool, len(st.toks))
	var stack []bool // mỗi "(" đang mở: true khi là ngoặc của lời gọi hàm
	for i, tok := range st.toks {
		switch tok {
		case "(":
			next := st.up(i + 1)
			stack = append(stack, next != "SELECT" && next != "WITH" && next != "VALUES")
		case ")":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
		st.inCall[i] = len(stack) > 0 && stack[len(stack)-1]
	}
	return st
}

func (st *sqlTables) up(i int) string {
	if i < 0 || i >= len(st.toks) {
		return ""
	}
	return strings.ToUpper(st.toks[i])
}

// item đọc một phần tử bảng bắt đầu ở i (bỏ ONLY, LATERAL, IF [NOT] EXISTS). Lời gọi hàm (FROM unnest(…)) và truy
// vấn con không tính. Trả chỉ số ngay sau phần đã đọc.
func (st *sqlTables) item(i int, callable bool) int {
	for st.up(i) == "ONLY" || st.up(i) == "LATERAL" || st.up(i) == "IF" || st.up(i) == "NOT" || st.up(i) == "EXISTS" {
		i++
	}
	if i >= len(st.toks) || !identRe.MatchString(st.toks[i]) || endsFromList(st.up(i)) {
		return i
	}
	if callable && st.up(i+1) == "(" {
		return i + 1
	}
	st.out = append(st.out, st.toks[i])
	return i + 1
}

// list đọc tiếp danh sách sau phần tử đầu: ở độ sâu 0, dấu phẩy mở phần tử mới; ")", ";" hay từ khóa thì dừng.
func (st *sqlTables) list(j int) {
	depth := 0
	for ; j < len(st.toks); j++ {
		switch tok := st.toks[j]; {
		case tok == "(":
			depth++
		case tok == ")":
			if depth == 0 {
				return
			}
			depth--
		case depth > 0:
		case joinWord(st.up(j)):
			// JOIN … ON … thuộc cùng mệnh đề FROM; bảng sau JOIN do tableRefs đọc, dấu phẩy sau đó vẫn mở phần tử mới.
		case tok == ";" || endsFromList(st.up(j)):
			return
		case tok == ",":
			j = st.item(j+1, true) - 1
		}
	}
}

// tableRefs trả mọi tên bảng (có thể kèm schema) mà sql nhắc: sau FROM và USING (cả danh sách cách nhau dấu phẩy),
// JOIN, INTO, UPDATE, TABLE. Bỏ FROM nằm trong lời gọi hàm (EXTRACT(… FROM x), substring, trim), IS DISTINCT FROM,
// và USING (cột) của JOIN.
func tableRefs(sql string) []string {
	st := newSQLTables(sql)
	for i := range st.toks {
		switch kw := st.up(i); kw {
		case "UPDATE":
			if prev := st.up(i - 1); prev != "FOR" && prev != "KEY" { // không phải FOR [NO KEY] UPDATE
				st.item(i+1, false)
			}
		case "INTO", "TABLE":
			st.item(i+1, false)
		case "JOIN":
			st.item(i+1, true)
		case "FROM", "USING":
			if st.inCall[i] || st.up(i-1) == "DISTINCT" || (kw == "USING" && st.up(i+1) == "(") {
				continue
			}
			st.list(st.item(i+1, true))
		}
	}
	return st.out
}

// schemaOfModule: module "order" dùng schema "ord" (05 quy ước chung); module khác trùng tên.
func schemaOfModule(module string) string {
	if module == "order" {
		return "ord"
	}
	return module
}

// moduleOfSchema: ngược của schemaOfModule.
func moduleOfSchema(schema string) string {
	if schema == "ord" {
		return "order"
	}
	return schema
}

// queriesPkg: tên "module" của gói internal/queries (ghép view cho màn danh sách): không có schema riêng, chỉ được nhắc
// view _v đã công bố.
const queriesPkg = "queries"

// viewReadExceptions: ngoại lệ "chỉ đọc view, không nhập Go" (kế hoạch M2, Q19, Task M2-02): module ở khóa được nhắc
// đúng các view trong danh sách dù không được gọi module chủ. Lý do ghi ở cột "Mốc đọc" của dòng docs/views.md.
func viewReadExceptions() map[string][]string {
	return map[string][]string{
		// customer tính chỉ số, gợi ý gộp, dòng thời gian từ đơn (Task M2-11, 12, 25) mà không nhập gói order.
		"customer": {"ord.orders_v", "ord.order_items_v", "ord.order_status_history_v", "ord.returns_v"},
		// fulfillment đọc địa chỉ lấy hàng của kho để gửi hãng mà không nhập gói inventory (kế hoạch M3, Global
		// Constraints "Ranh giới module"; Task M3-09 ResolveAccount, Task M3-12 lệnh tạo vận đơn).
		"fulfillment": {"inventory.warehouses_v"},
	}
}

// checkRef trả vi phạm (rỗng nếu không) của một lần nhắc ident (đã có schema) từ module. views: view công bố của
// docs/views.md.
func checkRef(module, ident string, views map[string][]string) string {
	schema, name, _ := strings.Cut(ident, ".")
	if schema == "pg_catalog" || schema == "information_schema" || (module != queriesPkg && schema == schemaOfModule(module)) {
		return ""
	}
	if !strings.HasSuffix(name, "_v") {
		if module == queriesPkg {
			return "internal/queries chỉ được nhắc view _v đã công bố (" + ident + ")"
		}
		return "module " + module + " nhắc schema " + schema + " (" + ident + ")"
	}
	if _, ok := views[ident]; !ok {
		return "view " + ident + " chưa công bố trong docs/views.md"
	}
	if module == queriesPkg || schema == "platform" {
		return ""
	}
	if slices.Contains(viewReadExceptions()[module], ident) {
		return ""
	}
	if dst := moduleOfSchema(schema); !slices.Contains(moduleCalls()[module], dst) {
		return "module " + module + " không được gọi " + dst + " (cột \"Được gọi\" của 04 mục 3; " + ident + ")"
	}
	return ""
}

// checkSQL trả vi phạm trong một đoạn SQL của module: bảng thiếu schema; nhắc bảng của schema khác; nhắc view _v của
// module khác mà module không được gọi, hoặc view chưa có trong docs/views.md. View platform.*_v đã công bố thì mọi
// module được nhắc; gói internal/queries chỉ được nhắc view đã công bố.
func checkSQL(where, module, sql string, views map[string][]string) []string {
	sql = strLitRe.ReplaceAllString(lineCmtRe.ReplaceAllString(sql, ""), "''")
	ctes := map[string]bool{}
	for _, m := range cteRe.FindAllStringSubmatch(sql, -1) {
		ctes[strings.ToLower(m[1])] = true
	}
	var out []string
	for _, ident := range tableRefs(sql) {
		if ctes[strings.ToLower(ident)] {
			continue
		}
		if !strings.Contains(ident, ".") {
			out = append(out, where+": bảng thiếu tên schema: "+ident)
			continue
		}
		if v := checkRef(module, strings.ToLower(ident), views); v != "" {
			out = append(out, where+": "+v)
		}
	}
	return out
}

// findSQLUsage quét root/queries/<module>/**.sql và mọi *_dynsql.go (module lấy từ đường dẫn
// internal/modules/<module>/…, internal/queries/… → queries, còn lại → platform); view công bố đọc ở root/docs/views.md.
func findSQLUsage(t *testing.T, root string, includeTestdata bool) []string {
	t.Helper()
	views := readViewsDoc(t, root).views
	var out []string
	qdir := filepath.Join(root, "queries")
	if _, err := os.Stat(qdir); err == nil {
		qfs := os.DirFS(qdir)
		err := fs.WalkDir(qfs, ".", func(rel string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(rel, ".sql") {
				return err
			}
			dir, _, _ := strings.Cut(rel, "/")
			module := moduleOfSchema(dir) // queries/ord là của module order (bảng "Được gọi" theo tên module)
			b, err := fs.ReadFile(qfs, rel)
			if err != nil {
				return fmt.Errorf("đọc queries/%s: %w", rel, err)
			}
			out = append(out, checkSQL("queries/"+rel, module, string(b), views)...)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	files := parseGoFiles(t, root, func(rel string) bool {
		return strings.HasSuffix(rel, "_dynsql.go") && (includeTestdata || !isTestdata(rel))
	})
	for _, g := range files {
		module := moduleOfPath(g.rel)
		ast.Inspect(g.file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, checkSQL(g.pos(lit.Pos()), module, s, views)...)
			}
			return true
		})
	}
	return out
}

func moduleOfPath(rel string) string {
	if hasPathPrefix(rel, "internal/queries") {
		return queriesPkg
	}
	if rest, ok := strings.CutPrefix(rel, "internal/modules/"); ok {
		m, _, _ := strings.Cut(rest, "/")
		return m
	}
	if strings.Contains(rel, "modules/") {
		_, rest, _ := strings.Cut(rel, "modules/")
		m, _, _ := strings.Cut(rest, "/")
		return m
	}
	return "platform"
}

func TestSQLUsageReportsSample(t *testing.T) {
	got := findSQLUsage(t, filepath.Join(repoRoot(t), "tests/arch/testdata/sqlusage"), true)
	assertMentions(t, got, "queries/identity/cross.sql", "queries/platform/noschema.sql", "modules/identity/x_dynsql.go",
		"commalist.sql: module platform nhắc schema identity", "commalist.sql: bảng thiếu tên schema: sessions",
		"using.sql: module platform nhắc schema identity", "joincomma.sql: module platform nhắc schema identity")
	for _, f := range got {
		if strings.Contains(f, "queries/platform/ok.sql") {
			t.Errorf("câu đúng luật không được báo: %s", f)
		}
	}
}

func TestSQLUsageProductCode(t *testing.T) {
	if got := findSQLUsage(t, repoRoot(t), false); len(got) != 0 {
		t.Fatalf("SQL sai luật schema:\n%s", strings.Join(got, "\n"))
	}
}

// Luật view giữa module (kế hoạch M1, Task M1-25): mỗi tệp mẫu báo đúng số dòng ghi ở đây, tệp đúng luật không báo.
func TestSQLUsageViewRules(t *testing.T) {
	got := findSQLUsage(t, filepath.Join(repoRoot(t), "tests/arch/testdata/sqlusage"), true)
	want := map[string]int{
		"queries/inventory/view_ok.sql":         0,
		"queries/inventory/table.sql":           1, // catalog.variants
		"queries/inventory/unlisted.sql":        1, // catalog.secret_v chưa công bố
		"queries/catalog/not_called.sql":        1, // catalog không được gọi inventory
		"internal/queries/list_dynsql.go":       2, // platform.file_variants, catalog.products
		"queries/identity/cross.sql":            1,
		"internal/modules/identity/x_dynsql.go": 1,
		// Kế hoạch M2 (Task M2-02): ngoại lệ chỉ đọc view của customer với bốn view ord.
		"queries/customer/m2_view_ok.sql":    0,
		"queries/customer/m2_table.sql":      1, // ord.orders
		"queries/customer/m2_other_view.sql": 1, // ord.traffic_sources_v không thuộc ngoại lệ
		"queries/promotion/m2_view.sql":      1, // promotion không có ngoại lệ
	}
	for file, n := range want {
		var lines []string
		for _, f := range got {
			if strings.HasPrefix(f, file+":") {
				lines = append(lines, f)
			}
		}
		if len(lines) != n {
			t.Errorf("%s: muốn %d dòng, nhận %d: %q", file, n, len(lines), lines)
		}
	}
	for _, must := range []string{
		"table.sql:", "catalog.variants)", "unlisted.sql:", "docs/views.md", "not_called.sql:", "không được gọi inventory",
		"list_dynsql.go:", "platform.file_variants", "catalog.products",
	} {
		if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, must) }) {
			t.Errorf("không dòng nào chứa %q: %q", must, got)
		}
	}
}
