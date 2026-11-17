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
	var out []goFile
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
		if !strings.HasSuffix(rel, ".go") || !keep(rel) {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("phân tích %s: %w", rel, err)
		}
		out = append(out, goFile{rel: rel, fset: fset, file: f})
		return nil
	})
	if err != nil {
		t.Fatalf("quét %s: %v", root, err)
	}
	return out
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
