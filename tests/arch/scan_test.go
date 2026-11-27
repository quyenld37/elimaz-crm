package arch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// goFile là một tệp Go đã phân tích, kèm đường dẫn tương đối (dấu "/") so với gốc quét.
type goFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
}

// skipDir: thư mục ẩn (.git, .worktrees của repo gốc), node_modules, và module Go riêng spikes/.
func skipDir(rel string, d fs.DirEntry) bool {
	name := d.Name()
	return rel != "." && (strings.HasPrefix(name, ".") || name == "node_modules" || rel == "spikes")
}

// parseGoFiles phân tích mọi tệp .go dưới root mà keep(rel) chấp nhận.
func parseGoFiles(t *testing.T, root string, keep func(rel string) bool) []goFile {
	t.Helper()
	files, err := parseTree(root, keep)
	if err != nil {
		t.Fatalf("quét %s: %v", root, err)
	}
	return files
}

// parsedFile là một tệp .go của cây đã phân tích; err là lỗi phân tích của riêng tệp đó.
type parsedFile struct {
	goFile
	err error
}

// parsedTree là mọi tệp .go dưới một gốc (bỏ thư mục theo skipDir), phân tích đúng một lần.
type parsedTree struct {
	once  sync.Once
	files []parsedFile
	err   error // lỗi duyệt thư mục
}

// treeCache giữ cây đã phân tích theo đường dẫn tuyệt đối của gốc (Task 480: trước đây mỗi test kiến trúc tự phân tích
// lại ~1.300 tệp của repo dưới -race, mất 5–9 giây mỗi lần).
type treeCache struct {
	mu    sync.Mutex
	trees map[string]*parsedTree
}

// parsedTrees là bộ đệm của tiến trình test: test kiến trúc chạy song song dùng chung; cây AST chỉ đọc sau khi dựng.
//
//nolint:gochecknoglobals // bộ đệm chỉ của test, cây chỉ đọc; cây của một gốc không đổi trong một lần chạy test
var parsedTrees = &treeCache{trees: map[string]*parsedTree{}}

// parseTree trả các tệp .go dưới root mà keep(rel) chấp nhận (lát mới mỗi lần gọi, thứ tự duyệt thư mục). Lỗi phân
// tích của một tệp chỉ trả khi keep chọn tệp đó, như khi mỗi test tự phân tích.
func parseTree(root string, keep func(rel string) bool) ([]goFile, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("đường dẫn tuyệt đối của %s: %w", root, err)
	}
	parsedTrees.mu.Lock()
	tree, ok := parsedTrees.trees[abs]
	if !ok {
		tree = &parsedTree{}
		parsedTrees.trees[abs] = tree
	}
	parsedTrees.mu.Unlock()
	tree.once.Do(func() { tree.files, tree.err = walkGoFiles(abs) })
	if tree.err != nil {
		return nil, tree.err
	}
	var out []goFile
	for _, f := range tree.files {
		if !keep(f.rel) {
			continue
		}
		if f.err != nil {
			return nil, f.err
		}
		out = append(out, f.goFile)
	}
	return out, nil
}

// walkGoFiles phân tích mọi tệp .go dưới root (bỏ thư mục theo skipDir); lỗi phân tích ghi vào từng tệp.
func walkGoFiles(root string) ([]parsedFile, error) {
	var out []parsedFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("đường dẫn tương đối của %s: %w", path, err)
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir(rel, d) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		pf := parsedFile{goFile: goFile{rel: rel, fset: fset, file: f}}
		if err != nil {
			pf.err = fmt.Errorf("phân tích %s: %w", rel, err)
		}
		out = append(out, pf)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("duyệt %s: %w", root, err)
	}
	return out, nil
}

// isTestdata: tệp mẫu của test kiến trúc (go build bỏ qua testdata); chỉ quét khi test chỉ thẳng vào đó.
func isTestdata(rel string) bool {
	return strings.HasPrefix(rel, "testdata/") || strings.Contains(rel, "/testdata/")
}

func (g goFile) pos(p token.Pos) string {
	return g.rel + ":" + strconv.Itoa(g.fset.Position(p).Line)
}

// assertMentions: trên testdata phải báo đúng các tệp mẫu; trên mã thật phải 0.
func assertMentions(t *testing.T, findings []string, wantFiles ...string) {
	t.Helper()
	for _, w := range wantFiles {
		found := false
		for _, f := range findings {
			if strings.Contains(f, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("không báo tệp mẫu %s; báo: %q", w, findings)
		}
	}
}

// findSelectorCalls quét mã dưới root (bỏ tệp _test.go, tệp có đường dẫn bắt đầu bằng skipPrefix, và testdata khi
// includeTestdata là false) và trả mọi phát hiện mà report trả cho từng lời gọi dạng x.Tên(...).
func findSelectorCalls(t *testing.T, root string, includeTestdata bool, skipPrefix string,
	report func(g goFile, call *ast.CallExpr, sel *ast.SelectorExpr) []string,
) []string {
	t.Helper()
	files := parseGoFiles(t, root, func(rel string) bool {
		return !strings.HasSuffix(rel, "_test.go") && !strings.HasPrefix(rel, skipPrefix) &&
			(includeTestdata || !isTestdata(rel))
	})
	var out []string
	for _, g := range files {
		out = append(out, selectorCalls(g, report)...)
	}
	return out
}

// selectorCalls trả mọi phát hiện mà report trả cho từng lời gọi dạng x.Tên(...) trong tệp g.
func selectorCalls(g goFile, report func(g goFile, call *ast.CallExpr, sel *ast.SelectorExpr) []string) []string {
	var out []string
	ast.Inspect(g.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			out = append(out, report(g, call, sel)...)
		}
		return true
	})
	return out
}

// allGoFiles phân tích mọi tệp .go dưới root, cả _test.go; testdata chỉ khi includeTestdata (luật áp ở mọi nơi).
func allGoFiles(t *testing.T, root string, includeTestdata bool) []goFile {
	t.Helper()
	return parseGoFiles(t, root, func(rel string) bool { return includeTestdata || !isTestdata(rel) })
}
