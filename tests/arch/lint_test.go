// Package arch chứa test kiến trúc: luật của dự án mà trình biên dịch không tự giữ.
package arch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// repoRoot trả thư mục chứa go.mod của module gốc.
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	return filepath.Dir(strings.TrimSpace(string(out)))
}

type lintIssue struct {
	FromLinter string `json:"FromLinter"`
	Text       string `json:"Text"`
	Pos        struct {
		Filename string `json:"Filename"`
		Line     int    `json:"Line"`
	} `json:"Pos"`
}

// lintTempDir là $TMPDIR riêng của worktree cho golangci-lint chạy trong test: golangci-lint khóa tệp
// $TMPDIR/golangci-lint.lock và bỏ sau 5 giây nếu tiến trình khác giữ khóa, nên dùng $TMPDIR chung thì lint của hook,
// ci-task hay phiên khác làm test đỏ. Thư mục nằm trong cache người dùng, cố định theo worktree (thư mục go.mod).
func lintTempDir(t *testing.T) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("thư mục cache người dùng: %v", err)
	}
	sum := sha256.Sum256([]byte(repoRoot(t)))
	dir := filepath.Join(base, "crm-arch-lint", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

func golangciLint(t *testing.T, root string, args ...string) ([]lintIssue, string) {
	t.Helper()
	bin, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Fatalf("thiếu golangci-lint (cài theo tools/versions.mk): %v", err)
	}
	full := append([]string{
		"run", "--output.json.path=stdout", "--output.text.path=stderr",
		"--max-issues-per-linter=0", "--max-same-issues=0", "--show-stats=false",
		// Cùng worktree có lần lint khác của test đang chạy: chờ nó xong (giới hạn bởi timeout của go test), không bỏ.
		"--allow-serial-runners",
	}, args...)
	cmd := exec.CommandContext(t.Context(), bin, full...)
	cmd.Dir = root
	// Cache golangci-lint giữ thư mục mặc định dùng chung (an toàn khi nhiều tiến trình, như cache của Go).
	cmd.Env = append(os.Environ(), "TMPDIR="+lintTempDir(t))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if exitErr := (*exec.ExitError)(nil); err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		t.Fatalf("golangci-lint %v: %v\n%s", args, err, stderr.String())
	}
	var report struct {
		Issues []lintIssue `json:"Issues"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("đọc JSON của golangci-lint: %v\n%s\n%s", err, out, stderr.String())
	}
	return report.Issues, stderr.String()
}

// Mỗi mẫu sai cài sẵn trong testdata/lint phải bị đúng linter báo.
func TestLintCatchesPlantedMistakes(t *testing.T) {
	root := repoRoot(t)
	issues, stderr := golangciLint(t, root, "./tests/arch/testdata/lint/")
	want := []struct {
		linter, fn, textPart string
	}{
		{"forbidigo", "PrintIt", "fmt.Println"},
		{"forbidigo", "ReadEnv", "os.Getenv"},
		{"nolintlint", "Silence", ""},
		{"errorlint", "IsNotFound", ""},
	}
	lines := funcLines(t, filepath.Join(root, "tests/arch/testdata/lint/bad.go"))
	for _, w := range want {
		found := false
		for _, is := range issues {
			r := lines[w.fn]
			if is.FromLinter == w.linter && is.Pos.Line >= r[0] && is.Pos.Line <= r[1] && strings.Contains(is.Text, w.textPart) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s không báo mẫu trong %s; các phát hiện: %+v\n%s", w.linter, w.fn, issues, stderr)
		}
	}
}

// Mã thật phải sạch lint.
func TestLintCleanOnRealCode(t *testing.T) {
	issues, stderr := golangciLint(t, repoRoot(t), "./...")
	for _, is := range issues {
		t.Errorf("%s:%d %s: %s", is.Pos.Filename, is.Pos.Line, is.FromLinter, is.Text)
	}
	if t.Failed() {
		t.Log(stderr)
	}
}

func funcLines(t *testing.T, path string) map[string][2]int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][2]int{}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			out[fd.Name.Name] = [2]int{fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line}
		}
	}
	return out
}

// isErrorCall báo e có phải lời gọi x.Error() không đối số.
func isErrorCall(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok || len(c.Args) != 0 {
		return false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	return ok && s.Sel.Name == "Error"
}

// isStringsMatch báo c có phải strings.Contains/HasPrefix/HasSuffix/EqualFold nhận một đối số x.Error() không.
func isStringsMatch(c *ast.CallExpr) bool {
	s, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := s.X.(*ast.Ident)
	if !ok || pkg.Name != "strings" {
		return false
	}
	switch s.Sel.Name {
	case "Contains", "HasPrefix", "HasSuffix", "EqualFold":
		return slices.ContainsFunc(c.Args, isErrorCall)
	}
	return false
}

// errStringCompares trả vị trí những chỗ so lỗi bằng chuỗi: err.Error() trong == hoặc !=, hay làm đối số của
// strings.Contains/HasPrefix/HasSuffix/EqualFold. Không linter nào trong .golangci.yml bắt mẫu này.
func errStringCompares(fset *token.FileSet, f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if (x.Op == token.EQL || x.Op == token.NEQ) && (isErrorCall(x.X) || isErrorCall(x.Y)) {
				out = append(out, fset.Position(x.Pos()).String())
			}
		case *ast.CallExpr:
			if isStringsMatch(x) {
				out = append(out, fset.Position(x.Pos()).String())
			}
		}
		return true
	})
	return out
}

func TestErrorStringCompareDetector(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(repoRoot(t), "tests/arch/testdata/lint/bad.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := errStringCompares(fset, f); len(got) != 1 {
		t.Fatalf("phải bắt đúng 1 chỗ so lỗi bằng chuỗi trong mẫu, nhận %v", got)
	}
}

// Mã chạy thật (không tính tệp test, testdata, spikes) không so lỗi bằng chuỗi.
func TestNoErrorStringCompareInRealCode(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "spikes" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("đọc %s: %w", path, err)
		}
		for _, pos := range errStringCompares(fset, f) {
			t.Errorf("so lỗi bằng chuỗi (dùng errors.Is/errors.As): %s", pos)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
