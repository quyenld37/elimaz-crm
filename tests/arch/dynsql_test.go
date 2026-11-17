package arch

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isSQLCall: các phương thức pgx nhận chuỗi SQL (hoặc lô câu) làm đối số.
func isSQLCall(name string) bool {
	switch name {
	case "Exec", "Query", "QueryRow", "SendBatch":
		return true
	}
	return false
}

// findDynSQL (ADR-44 chốt 3): lời gọi Exec, Query, QueryRow, SendBatch ngoài mã sqlc sinh ra chỉ được nằm trong
// tệp *_dynsql.go, và mỗi tệp đó có *_dynsql_test.go cùng thư mục. Không quét tệp _test.go và internal/testkit
// (hạ tầng test, không phải mã sản phẩm).
func findDynSQL(t *testing.T, root string, includeTestdata bool) []string {
	t.Helper()
	files := parseGoFiles(t, root, func(rel string) bool {
		return !strings.HasSuffix(rel, "_test.go") && !strings.Contains(rel, "/internal/store/") &&
			!strings.HasPrefix(rel, "internal/testkit/") && (includeTestdata || !isTestdata(rel))
	})
	var out []string
	for _, g := range files {
		if strings.HasSuffix(g.rel, "_dynsql.go") {
			test := filepath.Join(root, strings.TrimSuffix(g.rel, ".go")+"_test.go")
			if _, err := os.Stat(test); err != nil {
				out = append(out, g.rel+": thiếu "+filepath.Base(test))
			}
			continue
		}
		ast.Inspect(g.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isSQLCall(sel.Sel.Name) {
				out = append(out, g.pos(call.Pos())+": "+sel.Sel.Name+" ngoài *_dynsql.go")
			}
			return true
		})
	}
	return out
}

func TestDynSQLReportsSample(t *testing.T) {
	got := findDynSQL(t, filepath.Join(repoRoot(t), "tests/arch/testdata/dynsql"), true)
	assertMentions(t, got, "query.go", "lonely_dynsql.go")
	for _, f := range got {
		if strings.Contains(f, "ok_dynsql.go") {
			t.Errorf("tệp *_dynsql.go có test đi kèm không được báo: %s", f)
		}
	}
}

func TestDynSQLProductCode(t *testing.T) {
	if got := findDynSQL(t, repoRoot(t), false); len(got) != 0 {
		t.Fatalf("SQL ngoài sqlc và *_dynsql.go:\n%s", strings.Join(got, "\n"))
	}
}
