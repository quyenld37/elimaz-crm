package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func check(t *testing.T, base, rev, approved string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	args := []string{"-base", filepath.Join("testdata", base), "-rev", filepath.Join("testdata", rev)}
	if approved != "" {
		args = append(args, "-approved", filepath.Join("testdata", approved))
	}
	code := run(t.Context(), args, &out)
	return code, out.String()
}

func TestBreakingChanges(t *testing.T) {
	cases := []struct {
		name, rev, approved string
		wantCode            int
		wantOut             []string
	}{
		{"xóa trường bắt buộc, chưa duyệt", "removed-required.yaml", "", 1, []string{"getMe", "response-required-property-removed"}},
		{"duyệt cho operation khác", "removed-required.yaml", "approved-other.yaml", 1, []string{"getMe"}},
		{"dòng duyệt thiếu ngày không tính", "removed-required.yaml", "approved-no-date.yaml", 1, []string{"approvedOn"}},
		{"xóa trường bắt buộc, đã duyệt", "removed-required.yaml", "approved-email.yaml", 0, []string{"getMe"}},
		{"thêm trường tùy chọn", "added-optional.yaml", "", 0, nil},
		{"không đổi", "base.yaml", "", 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out := check(t, "base.yaml", c.rev, c.approved)
			if code != c.wantCode {
				t.Fatalf("mong thoát %d, có %d:\n%s", c.wantCode, code, out)
			}
			for _, w := range c.wantOut {
				if !strings.Contains(out, w) {
					t.Errorf("đầu ra phải nêu %q:\n%s", w, out)
				}
			}
		})
	}
}

// Lần đầu (nhánh gốc chưa có tệp hợp đồng) thì bỏ qua, thoát 0.
func TestMissingBaseSkips(t *testing.T) {
	var out bytes.Buffer
	code := run(t.Context(), []string{"-base-ref", "HEAD", "-rev", filepath.Join("testdata", "khong-co-trong-git.yaml")}, &out)
	if code != 0 || !strings.Contains(out.String(), "bỏ qua") {
		t.Fatalf("nhánh có mà chưa có tệp phải bỏ qua, thoát 0; có %d:\n%s", code, out.String())
	}
}

// Nhánh gốc không tồn tại là lỗi, không được bỏ qua kiểm.
func TestMissingRefFails(t *testing.T) {
	var out bytes.Buffer
	code := run(t.Context(), []string{"-base-ref", "khong-co-nhanh-nay-xyz", "-rev", filepath.Join("testdata", "base.yaml")}, &out)
	if code == 0 || !strings.Contains(out.String(), "khong-co-nhanh-nay-xyz") {
		t.Fatalf("nhánh không có phải thoát khác 0 và nêu tên nhánh; có %d:\n%s", code, out.String())
	}
}

// Lấy bản gốc từ git: tệp đã có ở HEAD thì được đem so, không bị bỏ qua.
func TestBaseFromRef(t *testing.T) {
	var out bytes.Buffer
	code := run(t.Context(), []string{"-base-ref", "HEAD", "-rev", filepath.Join("..", "catalog", "main.go")}, &out)
	// tools/catalog/main.go (đã có ở HEAD) không phải OpenAPI: oasdiff báo lỗi đọc, chứng tỏ đã lấy được tệp từ HEAD và gọi oasdiff.
	if code == 0 || strings.Contains(out.String(), "bỏ qua") || !strings.Contains(out.String(), "oasdiff breaking") {
		t.Fatalf("tệp có ở HEAD phải được đem so; có %d:\n%s", code, out.String())
	}
}

func TestParseApprovedRejectsIncomplete(t *testing.T) {
	ok, problems := parseApproved([]byte("approved:\n  - {operationId: a, reason: \"x\", approvedOn: 2026-11-18}\n" +
		"  - {operationId: b, reason: \"\", approvedOn: 2026-11-18}\n  - {operationId: c, reason: \"y\", approvedOn: hôm qua}\n"))
	if !ok["a"] || ok["b"] || ok["c"] || len(problems) != 2 {
		t.Fatalf("chỉ a hợp lệ; có %v, vấn đề %v", ok, problems)
	}
}
