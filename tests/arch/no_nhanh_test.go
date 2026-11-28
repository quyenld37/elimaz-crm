package arch

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Task 265 (M7-20, ADR-11, MO07): CRM không gọi Nhanh, không đọc dữ liệu Nhanh. Quét mọi tệp git theo dõi của repo (trừ docs/, *.md,
// testdata/no_nhanh và chính tệp này) tìm địa chỉ API, tên miền, tên biến môi trường của Nhanh. Mẫu ghép lúc chạy để
// tệp này không tự chứa chuỗi bị cấm (C20).

// nhanhPattern là mẫu bị cấm, không phân biệt hoa thường: tên miền nhanh.vn, nhanh.group, open.nhanh, tiền tố biến
// môi trường NHANH_.
func nhanhPattern() *regexp.Regexp {
	n := "nh" + "anh"
	return regexp.MustCompile(`(?i)` + n + `\.(vn|group)|open\.` + n + `|` + strings.ToUpper(n) + "_")
}

// noNhanhMaxFile: tệp lớn hơn (ảnh, bản dựng) không phải mã viết tay, bỏ qua.
const noNhanhMaxFile = 8 << 20

// trackedFiles trả các tệp git theo dõi dưới root (kể cả tệp đã stage), đường dẫn tương đối root, dấu /. Chỉ tệp của
// repo: tệp sinh lúc chạy (log, bản dựng, node_modules, worktree lồng) không bị quét.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "-C", root, "ls-files", "-z", "--cached").Output()
	if err != nil {
		t.Fatalf("git ls-files %s: %v", root, err)
	}
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 })
}

// scanNhanh trả đường dẫn (tương đối root, dấu /) của các tệp git theo dõi dưới root có chuỗi bị cấm, trừ *.md và tệp
// skip(rel) trả true.
func scanNhanh(t *testing.T, root string, skip func(rel string) bool) []string {
	t.Helper()
	re := nhanhPattern()
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("mở %s: %v", root, err)
	}
	defer func() { _ = r.Close() }()
	var hits []string
	for _, rel := range trackedFiles(t, root) {
		if strings.HasSuffix(rel, ".md") || skip(rel) {
			continue
		}
		if nhanhIn(t, r, rel, re) {
			hits = append(hits, rel)
		}
	}
	return hits
}

// nhanhIn: tệp rel (thường, không quá noNhanhMaxFile) dưới r có chuỗi khớp re. Tệp đã xóa khỏi cây (còn trong
// index) bỏ qua.
func nhanhIn(t *testing.T, r *os.Root, rel string, re *regexp.Regexp) bool {
	t.Helper()
	info, err := r.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatalf("stat %s: %v", rel, err)
	}
	if !info.Mode().IsRegular() || info.Size() > noNhanhMaxFile {
		return false
	}
	b, err := r.ReadFile(rel)
	if err != nil {
		t.Fatalf("đọc %s: %v", rel, err)
	}
	return re.Match(b)
}

// skipRepo là luật bỏ qua của repo: docs/ ở gốc, mẫu vi phạm testdata/no_nhanh, chính tệp test này.
func skipRepo(rel string) bool {
	return strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, "tests/arch/testdata/no_nhanh/") ||
		rel == "tests/arch/no_nhanh_test.go"
}

// Mã thật của repo: 0 tệp có chuỗi Nhanh bị cấm.
func TestNoNhanh_RealCode(t *testing.T) {
	t.Parallel()
	if hits := scanNhanh(t, repoRoot(t), skipRepo); len(hits) != 0 {
		t.Errorf("tệp có chuỗi gọi hay đọc Nhanh (ADR-11): %v", hits)
	}
}

// Chạy trên chính thư mục tests/arch (gốc là tests/arch): vẫn 0, kể cả tệp test này và các testdata khác.
func TestNoNhanh_ArchDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(repoRoot(t), "tests", "arch")
	hits := scanNhanh(t, dir, func(rel string) bool { return skipRepo("tests/arch/" + rel) })
	if len(hits) != 0 {
		t.Errorf("tests/arch có chuỗi Nhanh: %v", hits)
	}
}

// Trên testdata/no_nhanh: báo đúng sample.go; mẫu bắt cả chữ hoa, chữ thường, không bắt chữ "nhanh" thường.
func TestNoNhanh_FindsSample(t *testing.T) {
	t.Parallel()
	hits := scanNhanh(t, filepath.Join(repoRoot(t), "tests", "arch", "testdata", "no_nhanh"), func(string) bool { return false })
	if !slices.Equal(hits, []string{"sample.go"}) {
		t.Fatalf("quét testdata = %v, muốn [sample.go]", hits)
	}
	re := nhanhPattern()
	n := "nh" + "anh"
	for s, want := range map[string]bool{
		"https://" + n + ".vn":              true,
		"api." + strings.ToUpper(n) + ".VN": true,
		"pos." + n + ".group":               true,
		"open." + n + "/x":                  true,
		strings.ToUpper(n) + "_TOKEN":       true,
		"lib/" + n + "_client.go":           true,
		strings.ToUpper(n) + "9999":         false,
		"giao " + n + " trong ngày":         false,
	} {
		if got := re.MatchString(s); got != want {
			t.Errorf("mẫu với %q = %v, muốn %v", s, got, want)
		}
	}
}
