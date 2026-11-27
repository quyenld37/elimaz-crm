package arch

import (
	"go/ast"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Luật của Task M6-13 (kế hoạch M6): không đường nào ghi công thức, siêu liên kết hay macro vào tệp Excel — cấm gọi
// SetCellFormula, SetCellHyperLink, AddVBAProject ở mọi nơi (cả tệp _test.go, cả trong internal/platform/xlsx), và
// mã chạy thật chỉ nhập excelize trong internal/platform/xlsx (luật h có sẵn). Tệp _test.go được nhập excelize để dựng
// tệp thù địch cho test bộ đọc (vd ô có <f> trong internal/modules/customer/importer_test.go), QĐ-236-5.

// xlsxForbiddenCalls: phương thức của excelize ghi công thức, siêu liên kết, macro.
func xlsxForbiddenCalls() []string {
	return []string{"SetCellFormula", "SetCellHyperLink", "AddVBAProject"}
}

// findXlsxViolations quét mọi tệp .go dưới root (gồm _test.go; testdata chỉ khi includeTestdata) và trả các lời gọi
// bị cấm cùng các lần nhập excelize ngoài internal/platform/xlsx ở tệp không phải _test.go (đường dẫn gói tính tương
// đối so với root).
func findXlsxViolations(t *testing.T, root string, includeTestdata bool) []string {
	t.Helper()
	var out []string
	for _, g := range allGoFiles(t, root, includeTestdata) {
		if !strings.HasSuffix(g.rel, "_test.go") {
			out = append(out, excelizeImports(g)...)
		}
		out = append(out, forbiddenXlsxCalls(g)...)
	}
	return out
}

// excelizeImports: lần nhập excelize ngoài internal/platform/xlsx (luật h).
func excelizeImports(g goFile) []string {
	var out []string
	for _, imp := range g.file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if v := readerViolation(path.Dir(g.rel), p); strings.HasPrefix(v, "(h) chỉ internal/platform/xlsx") {
			out = append(out, g.pos(imp.Pos())+": "+v)
		}
	}
	return out
}

// forbiddenXlsxCalls: lời gọi x.SetCellFormula(...), x.SetCellHyperLink(...), x.AddVBAProject(...).
func forbiddenXlsxCalls(g goFile) []string {
	return selectorCalls(g, func(g goFile, call *ast.CallExpr, sel *ast.SelectorExpr) []string {
		if !slices.Contains(xlsxForbiddenCalls(), sel.Sel.Name) {
			return nil
		}
		return []string{g.pos(call.Pos()) + ": gọi " + sel.Sel.Name + " (tệp Excel không được có công thức, liên kết, macro)"}
	})
}

func TestXlsxRulesReportSample(t *testing.T) {
	t.Parallel()
	root := filepath.Join(repoRoot(t), "tests/arch/testdata/xlsx")
	got := findXlsxViolations(t, root, true)
	assertMentions(t, got, "formula.go:3", "formula.go:6", "link.go:9", "link.go:10", "hostile_test.go:5")
	for _, f := range got {
		if strings.Contains(f, "hostile_test.go:3") {
			t.Errorf("tệp _test.go được nhập excelize: %s", f)
		}
	}
	for _, f := range got {
		if strings.Contains(f, "ok.go") {
			t.Errorf("báo nhầm tệp trong internal/platform/xlsx: %s", f)
		}
	}
	if len(got) != 5 {
		t.Errorf("mong đúng 5 phát hiện, có %d: %q", len(got), got)
	}
}

func TestXlsxRulesProductCode(t *testing.T) {
	t.Parallel()
	if got := findXlsxViolations(t, repoRoot(t), false); len(got) != 0 {
		t.Fatalf("vi phạm luật tệp Excel (Task M6-13):\n%s", strings.Join(got, "\n"))
	}
}
