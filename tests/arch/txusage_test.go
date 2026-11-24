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
	return findSelectorCalls(t, root, includeTestdata, "internal/platform/db/",
		func(g goFile, call *ast.CallExpr, sel *ast.SelectorExpr) []string {
			switch sel.Sel.Name {
			case "Begin", "BeginTx", "BeginFunc", "BeginTxFunc":
				return []string{g.pos(call.Pos()) + ": " + sel.Sel.Name}
			}
			return nil
		})
}

func TestTxUsageReportsSample(t *testing.T) {
	got := findTxOpeners(t, filepath.Join(repoRoot(t), "tests/arch/testdata/txusage"), true)
	assertMentions(t, got, "begin.go")
}

func TestTxUsageProductCode(t *testing.T) {
	t.Parallel()
	if got := findTxOpeners(t, repoRoot(t), false); len(got) != 0 {
		t.Fatalf("mở transaction ngoài db.RetryTx (ADR-46):\n%s", strings.Join(got, "\n"))
	}
}
