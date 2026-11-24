package deploy_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Thư mục chỉ có tệp mang nhãn build riêng (Task 483; phát hiện ở Task 481: tests/drills "drill", tests/probe "probe")
// bị `go vet ./...`, `go build ./...`, `go test ./...` bỏ trắng, nên lỗi biên dịch chỉ lộ khi diễn tập tay. `make vet`
// (chạy trong make ci và scripts/agent/ci-task.sh) phải vet chúng với đúng nhãn — chỉ biên dịch, không chạy test:
// drill làm hỏng thật (docker kill, xóa volume).

// tagOnlyDirs trả thư mục (tương đối gốc repo, dấu /) mà MỌI tệp .go đều mở đầu bằng `//go:build <một nhãn>`, kèm
// các nhãn đó. Bỏ node_modules, testdata, .git, .worktrees và module Go riêng (thư mục con có go.mod).
func tagOnlyDirs(t *testing.T) map[string][]string {
	t.Helper()
	const root = "../.."
	tags := map[string][]string{} // thư mục → nhãn
	untagged := map[string]bool{} // thư mục có tệp không nhãn: bản dựng thường đã biên dịch
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && skipScanDir(root, path, d.Name()):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go"):
			return nil
		}
		dir := filepath.ToSlash(strings.TrimPrefix(filepath.Dir(path), root+string(filepath.Separator)))
		tag, err := firstLineTag(path)
		switch {
		case err != nil:
			return err
		case tag == "":
			untagged[dir] = true
		case !slices.Contains(tags[dir], tag):
			tags[dir] = append(tags[dir], tag)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("duyệt repo: %v", err)
	}
	for dir := range untagged {
		delete(tags, dir)
	}
	return tags
}

// skipScanDir: thư mục không thuộc `go vet ./...` của module này (phụ thuộc console, dữ liệu test, worktree, module
// Go riêng như spikes/…).
func skipScanDir(root, path, name string) bool {
	switch name {
	case "node_modules", "testdata", ".git", ".worktrees":
		return true
	}
	_, err := os.Stat(filepath.Join(path, "go.mod"))
	return path != root && err == nil
}

var buildTagLine = regexp.MustCompile(`^//go:build\s+([a-z][a-z0-9_]*)\s*$`)

// firstLineTag trả nhãn của dòng `//go:build <nhãn>` đầu tệp, "" khi tệp không mở đầu như vậy.
func firstLineTag(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err //nolint:wrapcheck // test: lỗi duyệt báo nguyên ở trên
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if sc.Scan() {
		if m := buildTagLine.FindStringSubmatch(sc.Text()); m != nil {
			return m[1], nil
		}
	}
	return "", sc.Err() //nolint:wrapcheck // test
}

func TestMakeVet_CompilesTagOnlyDirs(t *testing.T) {
	want := tagOnlyDirs(t)
	for _, d := range []string{"tests/drills", "tests/probe"} { // phải tìm thấy (quét đúng chỗ)
		if _, ok := want[d]; !ok {
			t.Fatalf("không thấy %s trong thư mục chỉ có tệp mang nhãn (quét sai?): %v", d, want)
		}
	}
	out := makeDry(t, "vet")
	var tags, pkgs []string
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == "go" && f[1] == "vet" && f[2] == "-tags" {
			tags = append(tags, strings.Split(f[3], ",")...)
			pkgs = append(pkgs, f[4:]...)
		}
	}
	for dir, dirTags := range want {
		if !slices.Contains(pkgs, "./"+dir+"/...") {
			t.Errorf("make vet phải vet %s (chỉ có tệp nhãn %v, go vet ./... bỏ qua): có gói %v", dir, dirTags, pkgs)
		}
		for _, tag := range dirTags {
			if !slices.Contains(tags, tag) {
				t.Errorf("make vet phải dùng nhãn %q cho %s, đang có %v", tag, dir, tags)
			}
		}
	}
	if strings.Contains(out, "go test") {
		t.Errorf("make vet chỉ biên dịch và vet, không chạy test (drill làm hỏng thật):\n%s", out)
	}
}
