package arch

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"
)

// findTxOpeners trả mọi lời gọi .Begin(, .BeginTx(, pgx.BeginFunc, pgx.BeginTxFunc trong mã sản phẩm
// (ADR-46: transaction chỉ mở qua db.RetryTx). Bỏ qua internal/platform/db và tệp _test.go.
func findTxOpeners(t *testing.T, root string, includeTestdata bool) []string {
	t.Helper()
	files := parseGoFiles(t, root, func(rel string) bool {
		return !strings.HasSuffix(rel, "_test.go") && !strings.HasPrefix(rel, "internal/platform/db/") &&
			(includeTestdata || !isTestdata(rel))
	})
	var out []string
	for _, g := range files {
		ast.Inspect(g.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Begin", "BeginTx", "BeginFunc", "BeginTxFunc":
				out = append(out, g.pos(call.Pos())+": "+sel.Sel.Name)
			}
			return true
		})
	}
	return out
}

func TestTxUsageReportsSample(t *testing.T) {
	got := findTxOpeners(t, filepath.Join(repoRoot(t), "tests/arch/testdata/txusage"), true)
	assertMentions(t, got, "begin.go")
}

func TestTxUsageProductCode(t *testing.T) {
	if got := findTxOpeners(t, repoRoot(t), false); len(got) != 0 {
		t.Fatalf("mở transaction ngoài db.RetryTx (ADR-46):\n%s", strings.Join(got, "\n"))
	}
}
