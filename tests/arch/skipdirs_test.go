package arch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Tệp Go lạc trong thư mục không phải mã của CRM: gói npm có tệp .go (console/node_modules, Task 18 gặp flatted),
// worktree lồng của repo gốc. Có lỗi lint cố ý (fmt.Println, os.Getenv bị forbidigo cấm).
const strayGo = `package bad

import (
	"fmt"
	"os"
)

func Bad() { fmt.Println(os.Getenv("X")) }
`

// strayTree dựng một bản sao tối thiểu của module (go.mod và .golangci.yml thật của repo) với một gói sạch và các tệp
// lạc ở console/node_modules, node_modules lồng, .worktrees.
func strayTree(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	files := map[string]string{
		"clean/clean.go":                      "// Package clean là gói sạch mẫu.\npackage clean\n\n// One trả 1.\nfunc One() int { return 1 }\n",
		"console/node_modules/x/bad.go":       strayGo,
		"console/node_modules/x/y/bad.go":     strayGo,
		"internal/foo/node_modules/z/bad.go":  strayGo,
		".worktrees/task-9/internal/w/bad.go": strayGo,
	}
	for _, src := range []string{"go.mod", ".golangci.yml"} {
		b, err := os.ReadFile(filepath.Join(root, src))
		if err != nil {
			t.Fatal(err)
		}
		files[src] = string(b)
	}
	return writeTree(t, files)
}

// `./...` của go list (test ranh giới) và golangci-lint (TestLintCleanOnRealCode) không nhận gói trong
// console/node_modules và .worktrees: go.mod có chỉ thị ignore.
func TestPackagePatternSkipsStrayDirs(t *testing.T) {
	t.Parallel()
	dir := strayTree(t)
	var paths []string
	for _, p := range goListPackages(t, dir) {
		paths = append(paths, p.ImportPath)
	}
	if want := []string{modPrefix + "clean"}; !slices.Equal(paths, want) {
		t.Errorf("go list ./... = %v, muốn %v", paths, want)
	}
	issues, stderr := golangciLint(t, dir, "./...")
	for _, is := range issues {
		t.Errorf("golangci-lint báo tệp ngoài mã CRM: %s:%d %s: %s", is.Pos.Filename, is.Pos.Line, is.FromLinter, is.Text)
	}
	if t.Failed() {
		t.Log(stderr)
	}
}

// Vòng quét tệp của các test kiến trúc (parseGoFiles) bỏ node_modules ở mọi tầng và thư mục ẩn như .worktrees.
func TestParseGoFilesSkipsStrayDirs(t *testing.T) {
	var got []string
	for _, g := range parseGoFiles(t, strayTree(t), func(string) bool { return true }) {
		got = append(got, g.rel)
	}
	if want := []string{"clean/clean.go"}; !slices.Equal(got, want) {
		t.Errorf("parseGoFiles = %v, muốn %v", got, want)
	}
	for _, rel := range got {
		if strings.Contains(rel, "node_modules") {
			t.Errorf("quét vào %s", rel)
		}
	}
}
