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

// Luật người bán (kế hoạch M1, Task M1-37; ADR-29): mọi câu trong queries/catalog, queries/ord và mọi *_dynsql.go có
// nhắc catalog.products, catalog.variants, ord.orders, ord.order_items (bảng hoặc view _v) phải có điều kiện
// seller_id cho từng lần nhắc: có bí danh → "<bí danh>.seller_id =" hoặc "= <bí danh>.seller_id" (trong WHERE hay
// JOIN … ON); không bí danh → "seller_id =" (hoặc "<tên bảng>.seller_id ="); INSERT vào các bảng đó phải ghi cột
// seller_id. Câu sqlc tách theo "-- name:"; *_dynsql.go gộp mọi chuỗi trong một hàm.
var (
	sellerScopedRe = regexp.MustCompile(`(?i)\b((?:catalog\.(?:products|variants)|ord\.(?:orders|order_items))(?:_v)?)\b` +
		`(?:\s+(?:AS\s+)?([A-Za-z_]\w*))?`)
	sqlcNameRe   = regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)`)
	insertTailRe = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s*$`)
	sellerColRe  = regexp.MustCompile(`(?i)\bseller_id\b`)
	bareSellerRe = regexp.MustCompile(`(?i)(?:^|[^\w.@])seller_id\s*(?:=|\bIN\b)`)
)

// sellerExempt: câu được miễn, khóa "<tệp>#<tên câu hoặc hàm>", giá trị là lý do. Mỗi dòng phải còn được dùng.
func sellerExempt() map[string]string {
	return map[string]string{
		"queries/catalog/categories.sql#CategoryInUse": "danh mục dùng chung cả shop (spec 05: không có seller_id): " +
			"hỏi có mẫu nào của bất kỳ người bán nào còn dùng danh mục trước khi lưu trữ",
		"queries/ord/create.sql#VariantExists": "lên đơn (Task 73, KH14): biến thể không bán được cho người bán của đơn " +
			"thì hỏi có/không ở cả shop để phân biệt mixed_sellers với variant_not_found; chỉ trả boolean",
		"queries/ord/drafts.sql#LockExpiredDrafts": "việc hệ thống order.expire_drafts (Task 78, Q8): hủy đơn nháp quá " +
			"hạn của mọi người bán trong tenant; chỉ trả (id, seller_id), từng đơn được khóa và hủy lại theo seller_id của dòng",
	}
}

// notAlias: từ khóa có thể đứng ngay sau tên bảng mà không phải bí danh.
func notAlias(w string) bool {
	switch strings.ToUpper(w) {
	case "WHERE", "JOIN", "ON", "SET", "LEFT", "RIGHT", "INNER", "CROSS", "FULL", "GROUP", "ORDER", "LIMIT", "FOR",
		"USING", "VALUES", "RETURNING", "WHEN", "UNION", "AND", "OR", "OFFSET", "HAVING", "WINDOW", "SELECT", "NATURAL":
		return true
	}
	return false
}

// sellerProblems trả các lần nhắc bảng/view có chiều người bán mà câu sql không có điều kiện seller_id tương ứng.
func sellerProblems(sql string) []string {
	sql = strLitRe.ReplaceAllString(lineCmtRe.ReplaceAllString(sql, ""), "''")
	var out []string
	for _, m := range sellerScopedRe.FindAllStringSubmatchIndex(sql, -1) {
		rel := sql[m[2]:m[3]]
		if insertTailRe.MatchString(sql[:m[0]]) {
			if !sellerColRe.MatchString(sql) {
				out = append(out, "INSERT "+rel+" thiếu cột seller_id")
			}
			continue
		}
		alias := ""
		if m[4] >= 0 && !notAlias(sql[m[4]:m[5]]) {
			alias = sql[m[4]:m[5]]
		}
		quals := []string{alias}
		if alias == "" {
			_, table, _ := strings.Cut(rel, ".")
			quals = []string{table, ""}
		}
		if !slices.ContainsFunc(quals, func(q string) bool { return hasSellerPredicate(sql, q) }) {
			name := rel
			if alias != "" {
				name += " AS " + alias
			}
			out = append(out, name+" thiếu điều kiện seller_id")
		}
	}
	return out
}

// hasSellerPredicate: sql có "q.seller_id =" / "q.seller_id IN" / "= q.seller_id"; q rỗng → "seller_id =" không tiền
// tố (không tính tham số @seller_id).
func hasSellerPredicate(sql, q string) bool {
	if q == "" {
		return bareSellerRe.MatchString(sql)
	}
	qq := regexp.QuoteMeta(q)
	return regexp.MustCompile(`(?i)\b` + qq + `\.seller_id\s*(?:=|\bIN\b)|=\s*` + qq + `\.seller_id\b`).MatchString(sql)
}

// sqlcStatements tách tệp sqlc thành từng câu theo dòng "-- name:"; trả tên → nội dung.
func sqlcStatements(src string) map[string]string {
	out := map[string]string{}
	idx := sqlcNameRe.FindAllStringSubmatchIndex(src, -1)
	for i, m := range idx {
		end := len(src)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out[src[m[2]:m[3]]] = src[m[1]:end]
	}
	return out
}

// sellerFinding là các vi phạm của một câu sqlc hay một hàm *_dynsql.go: key "<tệp>#<tên>" (khóa miễn), where là chỗ
// in ra.
type sellerFinding struct {
	key, where string
	problems   []string
}

// sqlcSellerFindings quét root/queries/{catalog,ord}/**.sql, từng câu theo "-- name:".
func sqlcSellerFindings(t *testing.T, root string) []sellerFinding {
	t.Helper()
	var out []sellerFinding
	for _, dir := range []string{"catalog", "ord"} {
		qdir := filepath.Join(root, "queries", dir)
		if _, err := os.Stat(qdir); err != nil {
			continue
		}
		err := fs.WalkDir(os.DirFS(qdir), ".", func(rel string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(rel, ".sql") {
				return err
			}
			b, err := os.ReadFile(filepath.Join(qdir, rel))
			if err != nil {
				return fmt.Errorf("đọc %s: %w", rel, err)
			}
			file := "queries/" + dir + "/" + rel
			for name, sql := range sqlcStatements(string(b)) {
				out = append(out, sellerFinding{file + "#" + name, file + " " + name, sellerProblems(sql)})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// dynsqlSellerFindings quét mọi *_dynsql.go dưới root, gộp các chuỗi trong từng hàm thành một đoạn SQL.
func dynsqlSellerFindings(t *testing.T, root string, includeTestdata bool) []sellerFinding {
	t.Helper()
	var out []sellerFinding
	files := parseGoFiles(t, root, func(rel string) bool {
		return strings.HasSuffix(rel, "_dynsql.go") && (includeTestdata || !isTestdata(rel))
	})
	for _, g := range files {
		for _, decl := range g.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				name := fn.Name.Name
				out = append(out, sellerFinding{g.rel + "#" + name, g.pos(fn.Pos()) + " " + name, sellerProblems(funcStrings(fn))})
			}
		}
	}
	return out
}

// funcStrings nối mọi chuỗi trong thân hàm fn (mỗi chuỗi một dòng).
func funcStrings(fn *ast.FuncDecl) string {
	var b strings.Builder
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				b.WriteString(s + "\n")
			}
		}
		return true
	})
	return b.String()
}

// findSellerFilter trả vi phạm (trừ câu miễn, đã sắp) và các khóa miễn đã dùng.
func findSellerFilter(t *testing.T, root string, includeTestdata bool) (problems []string, used map[string]bool) {
	t.Helper()
	exempt := sellerExempt()
	used = map[string]bool{}
	for _, f := range append(sqlcSellerFindings(t, root), dynsqlSellerFindings(t, root, includeTestdata)...) {
		if len(f.problems) == 0 {
			continue
		}
		if _, ok := exempt[f.key]; ok {
			used[f.key] = true
			continue
		}
		for _, p := range f.problems {
			problems = append(problems, f.where+": "+p)
		}
	}
	slices.Sort(problems)
	return problems, used
}

func TestSellerFilterReportsSample(t *testing.T) {
	got, _ := findSellerFilter(t, filepath.Join(repoRoot(t), "tests/arch/testdata/sellerfilter"), true)
	want := []string{
		"queries/catalog/bad.sql BySKU: catalog.variants thiếu điều kiện seller_id",
		"queries/catalog/bad.sql JoinMissing: catalog.products_v AS p thiếu điều kiện seller_id",
		"queries/catalog/bad.sql InsertNoSeller: INSERT catalog.products thiếu cột seller_id",
		"queries/ord/bad.sql LockOrder: ord.orders thiếu điều kiện seller_id",
		"internal/queries/bad_dynsql.go:10 listBad: ord.order_items_v AS i thiếu điều kiện seller_id",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("vi phạm trên testdata:\n%s\nmuốn:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSellerFilterProductCode(t *testing.T) {
	got, used := findSellerFilter(t, repoRoot(t), false)
	if len(got) != 0 {
		t.Fatalf("câu đọc sản phẩm, đơn thiếu điều kiện người bán (ADR-29):\n%s", strings.Join(got, "\n"))
	}
	for key := range sellerExempt() {
		if !used[key] {
			t.Errorf("dòng miễn %q không còn câu nào dùng: xóa khỏi sellerExempt", key)
		}
	}
}
