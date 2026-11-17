package arch

import (
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

// checkSQL trả vi phạm trong một đoạn SQL của module: bảng thiếu schema, hoặc nhắc schema của module khác.
func checkSQL(where, module, sql string) []string {
	sql = strLitRe.ReplaceAllString(lineCmtRe.ReplaceAllString(sql, ""), "''")
	ctes := map[string]bool{}
	for _, m := range cteRe.FindAllStringSubmatch(sql, -1) {
		ctes[strings.ToLower(m[1])] = true
	}
	own := schemaOfModule(module)
	var out []string
	for _, ident := range tableRefs(sql) {
		if ctes[strings.ToLower(ident)] {
			continue
		}
		schema, _, qualified := strings.Cut(ident, ".")
		switch {
		case !qualified:
			out = append(out, where+": bảng thiếu tên schema: "+ident)
		case schema == "pg_catalog" || schema == "information_schema":
		case schema != own:
			out = append(out, where+": module "+module+" nhắc schema "+schema+" ("+ident+")")
		}
	}
	return out
}

// findSQLUsage quét root/queries/<module>/**.sql và mọi *_dynsql.go (module lấy từ đường dẫn
// internal/modules/<module>/… hoặc internal/platform/… → platform).
func findSQLUsage(t *testing.T, root string, includeTestdata bool) []string {
	t.Helper()
	var out []string
	qdir := filepath.Join(root, "queries")
	if _, err := os.Stat(qdir); err == nil {
		qfs := os.DirFS(qdir)
		err := fs.WalkDir(qfs, ".", func(rel string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(rel, ".sql") {
				return err
			}
			module, _, _ := strings.Cut(rel, "/")
			b, err := fs.ReadFile(qfs, rel)
			if err != nil {
				return fmt.Errorf("đọc queries/%s: %w", rel, err)
			}
			out = append(out, checkSQL("queries/"+rel, module, string(b))...)
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
				out = append(out, checkSQL(g.pos(lit.Pos()), module, s)...)
			}
			return true
		})
	}
	return out
}

func moduleOfPath(rel string) string {
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
