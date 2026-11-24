package arch

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"
)

// findSimpleProtocol trả mọi chỗ nhắc QueryExecModeSimpleProtocol (Global Constraints: cấm ở mọi nơi, kể cả test).
func findSimpleProtocol(t *testing.T, root string, includeTestdata bool) []string {
	t.Helper()
	files := parseGoFiles(t, root, func(rel string) bool { return includeTestdata || !isTestdata(rel) })
	var out []string
	for _, g := range files {
		ast.Inspect(g.file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "QueryExecModeSimpleProtocol" {
				out = append(out, g.pos(id.Pos()))
			}
			return true
		})
	}
	return out
}

func TestSQLModeReportsSample(t *testing.T) {
	got := findSimpleProtocol(t, filepath.Join(repoRoot(t), "tests/arch/testdata/sqlmode"), true)
	assertMentions(t, got, "simple.go")
}

func TestSQLModeProductCode(t *testing.T) {
	t.Parallel()
	if got := findSimpleProtocol(t, repoRoot(t), false); len(got) != 0 {
		t.Fatalf("cấm simple protocol của pgx:\n%s", strings.Join(got, "\n"))
	}
}
