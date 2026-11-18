package arch

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mappedModules: module mà mọi sentinel xuất ra (var Err… của gói gốc) phải có dòng trong bảng ánh xạ lỗi của lớp
// HTTP (kế hoạch M2, Task M2-02: customer, order, promotion).
func mappedModules() []string { return []string{"customer", "order", "promotion"} }

// declaredSentinels trả "<module>.ErrX" của mọi biến cấp gói tên Err… xuất ra, trong tệp không phải test của gói gốc
// internal/modules/<module> (không quét gói con: lỗi của gói con được bọc thành sentinel của gói gốc).
func declaredSentinels(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, m := range mappedModules() {
		dir := "internal/modules/" + m + "/"
		files := parseGoFiles(t, root, func(rel string) bool {
			rest, ok := strings.CutPrefix(rel, dir)
			return ok && !strings.Contains(rest, "/") && !strings.HasSuffix(rel, "_test.go")
		})
		for _, g := range files {
			for _, name := range exportedErrVars(g.file) {
				out = append(out, m+"."+name)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// exportedErrVars trả tên mọi biến cấp gói Err… xuất ra của f.
func exportedErrVars(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			for _, n := range spec.(*ast.ValueSpec).Names {
				if strings.HasPrefix(n.Name, "Err") && n.IsExported() {
					out = append(out, n.Name)
				}
			}
		}
	}
	return out
}

// mappedSentinels trả mọi biểu thức "<module>.ErrX" nhắc trong internal/httpapi/errors.go (bảng ánh xạ, một chỗ).
func mappedSentinels(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, g := range parseGoFiles(t, root, func(rel string) bool { return rel == "internal/httpapi/errors.go" }) {
		ast.Inspect(g.file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && strings.HasPrefix(sel.Sel.Name, "Err") {
					out[id.Name+"."+sel.Sel.Name] = true
				}
			}
			return true
		})
	}
	return out
}

// internalSentinels: sentinel cố ý KHÔNG ánh xạ — chỉ xảy ra khi dữ liệu đã lệch (lỗi nội bộ), phải ra 500 qua
// writeHandlerError để có log và dấu lỗi, không bị giấu thành mã 4xx.
func internalSentinels() map[string]string {
	return map[string]string{
		// fulfillment.CreateForOrder đã khóa đơn và kiểm trạng thái confirmed trước order.AttachShipment; lỗi này chỉ
		// còn nghĩa "đơn hay giữ hàng lệch trạng thái" (reviewer Task 59).
		"order.ErrStatusConflict": "dữ liệu đơn lệch trạng thái",
	}
}

func unmappedSentinels(t *testing.T, root string) []string {
	t.Helper()
	mapped := mappedSentinels(t, root)
	internal := internalSentinels()
	var out []string
	for _, s := range declaredSentinels(t, root) {
		if _, ok := internal[s]; ok {
			if mapped[s] {
				out = append(out, s+" (lỗi nội bộ, không được ánh xạ)")
			}
			continue
		}
		if !mapped[s] {
			out = append(out, s)
		}
	}
	return out
}

func TestErrorsMappedSample(t *testing.T) {
	got := unmappedSentinels(t, filepath.Join("testdata", "errorsmapped"))
	if want := []string{"order.ErrFoo", "promotion.ErrBaz"}; !slices.Equal(got, want) {
		t.Fatalf("mong %v chưa ánh xạ, có %v", want, got)
	}
}

func TestErrorsMappedRealCode(t *testing.T) {
	root := repoRoot(t)
	if n := len(declaredSentinels(t, root)); n < 30 {
		t.Fatalf("chỉ thấy %d sentinel trong ba module: quét sai chỗ?", n)
	}
	if got := unmappedSentinels(t, root); len(got) != 0 {
		t.Fatalf("sentinel chưa có dòng trong internal/httpapi/errors.go:\n%s", strings.Join(got, "\n"))
	}
}
