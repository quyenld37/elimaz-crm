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

// Ca kiểm thử không chạy trong `make ci` (Task 414, phát hiện E-04 của review độc lập M1): test có nhãn build riêng
// (drill, filesync, imager…) và test cần libvips (SKIP trên máy không có, chỉ chạy đủ trong `make test-imager`). Mỗi
// loại phải có một dòng trong docs/ca-chay-tay.md với lệnh và lịch chạy, để bằng chứng của TP01, TP03, TP08–TP10 và
// tiêu chí Xong 7 không phụ thuộc người nhớ. Ca libvips thì CI chạy ở mọi lần push (job imager-vips).

const manualSchedule = "../../docs/ca-chay-tay.md"

// buildTags trả mọi nhãn build của tệp *_test.go trong repo (dòng //go:build đầu tệp), bỏ thư mục node_modules và
// testdata.
func buildTags(t *testing.T) []string {
	t.Helper()
	re := regexp.MustCompile(`^//go:build\s+([a-z][a-z0-9_]*)\s*$`)
	var tags []string
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "testdata" || d.Name() == ".git" || d.Name() == ".worktrees") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := os.Open(path) //nolint:gosec // G304: tệp trong repo
		if err != nil {
			return err //nolint:wrapcheck // test: lỗi duyệt báo nguyên ở dưới
		}
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		if sc.Scan() {
			if m := re.FindStringSubmatch(sc.Text()); m != nil && !slices.Contains(tags, m[1]) {
				tags = append(tags, m[1])
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatalf("duyệt repo: %v", err)
	}
	slices.Sort(tags)
	return tags
}

// scheduleRows đọc bảng của docs/ca-chay-tay.md: cột đầu là nhãn trong dấu `…` → các cột còn lại.
func scheduleRows(t *testing.T) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(manualSchedule)
	if err != nil {
		t.Fatalf("đọc %s: %v", manualSchedule, err)
	}
	rows := map[string][]string{}
	for line := range strings.SplitSeq(string(b), "\n") {
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 2 {
			continue
		}
		first := strings.TrimSpace(cells[0])
		if tag, ok := strings.CutPrefix(first, "`"); ok && strings.HasSuffix(tag, "`") {
			rest := make([]string, 0, len(cells)-1)
			for _, c := range cells[1:] {
				rest = append(rest, strings.TrimSpace(c))
			}
			rows[strings.TrimSuffix(tag, "`")] = rest
		}
	}
	return rows
}

func TestManualSuites_EveryBuildTagScheduled(t *testing.T) {
	tags := buildTags(t)
	for _, want := range []string{"drill", "filesync", "imager"} { // TP08–TP10, TP01: phải tìm thấy (quét đúng chỗ)
		if !slices.Contains(tags, want) {
			t.Fatalf("không thấy nhãn build %q trong test của repo (quét sai?): %v", want, tags)
		}
	}
	rows := scheduleRows(t)
	// Cột: Ca | Lệnh | Lịch | Lần chạy gần nhất.
	for _, tag := range append(tags, "libvips") {
		r, ok := rows[tag]
		switch {
		case !ok:
			t.Errorf("%s thiếu dòng cho nhãn `%s` (lệnh, lịch chạy)", manualSchedule, tag)
		case len(r) < 4 || r[1] == "" || r[2] == "" || r[3] == "":
			t.Errorf("%s dòng `%s` phải có đủ ca, lệnh, lịch, lần chạy gần nhất: %v", manualSchedule, tag, r)
		case tag != "libvips" && !strings.Contains(r[1], "-tags "+tag) && !strings.Contains(r[1], "make ") &&
			!strings.Contains(r[1], ".sh"):
			t.Errorf("%s dòng `%s`: lệnh phải là `go test -tags %s …`, đích make hay script, đang là %q", manualSchedule, tag, tag, r[1])
		}
	}
	if r := rows["libvips"]; len(r) > 1 && !strings.Contains(r[1], "make test-imager") {
		t.Errorf("dòng `libvips` phải chạy make test-imager, đang là %q", r[1])
	}
}

// Ca libvips (bom điểm ảnh TP03, hết bộ nhớ của tiêu chí Xong 7) chạy ở mọi lần push: job imager-vips chạy
// make test-imager (IMAGER_REQUIRE_VIPS=1: thiếu công cụ thì đỏ, không SKIP).
func TestCI_ImagerVipsJob(t *testing.T) {
	w := readWorkflow(t)
	j := needJob(t, w, "imager-vips")
	if len(runs(j, regexp.MustCompile(`^make test-imager$`))) != 1 {
		t.Error("job imager-vips phải chạy make test-imager")
	}
	if !strings.Contains(j.If, "github.event_name != 'schedule'") {
		t.Errorf("job imager-vips chạy ở push và pull request, không theo lịch mutation; if đang là %q", j.If)
	}
	if !strings.Contains(makeDry(t, "test-imager"), "IMAGER_REQUIRE_VIPS=1") {
		t.Error("make test-imager phải đặt IMAGER_REQUIRE_VIPS=1 (thiếu libvips thì đỏ, không SKIP)")
	}
}
