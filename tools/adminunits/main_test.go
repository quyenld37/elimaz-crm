package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runTool chạy tool với tệp in và trả mã thoát, stdout, stderr, đường tệp SQL (có thể chưa được ghi).
func runTool(t *testing.T, in string, extra ...string) (code int, stdout, stderr, out string) {
	t.Helper()
	out = filepath.Join(t.TempDir(), "0203_admin_units_data.sql")
	var so, se bytes.Buffer
	args := append([]string{"-in", in, "-out", out, "-source", "Danh mục thử"}, extra...)
	code = run(args, &so, &se)
	return code, so.String(), se.String(), out
}

// Tệp mẫu 2 tỉnh, 5 xã: SQL có đúng 7 dòng giá trị, in số tỉnh và số xã, tên đã bỏ dấu, dấu nháy được thoát.
func TestGenerateOK(t *testing.T) {
	code, stdout, stderr, out := runTool(t, "testdata/ok.csv")
	if code != 0 {
		t.Fatalf("mã thoát %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "2 tỉnh, 5 xã") {
		t.Fatalf("stdout thiếu số tỉnh, số xã: %q", stdout)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if n := strings.Count(s, "\n    ('"); n != 7 {
		t.Fatalf("muốn 7 dòng giá trị, có %d:\n%s", n, s)
	}
	if n := strings.Count(s, "INSERT INTO customer.admin_units"); n != 1 {
		t.Fatalf("muốn 1 câu INSERT, có %d", n)
	}
	for _, want := range []string{
		"-- +goose Up",
		"('T01', 'province', NULL, 'Thành phố Thử Một', 'thanh pho thu mot', DATE '2025-07-01', 'Danh mục thử')",
		"('T02002', 'ward', 'T02', 'Xã Ô''Môn Thử', 'xa o''mon thu', DATE '2025-07-01', 'Danh mục thử')",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("SQL thiếu %q:\n%s", want, s)
		}
	}
	// Tỉnh đứng trước xã (khóa ngoại parent_code tự trỏ, kiểm từng dòng).
	if strings.Index(s, "'T02', 'province'") > strings.Index(s, "'T01001', 'ward'") {
		t.Fatalf("tỉnh phải đứng trước xã:\n%s", s)
	}
}

// Một xã trỏ tỉnh không có: thoát khác 0, nêu mã xã, không ghi tệp.
func TestGenerateOrphanWard(t *testing.T) {
	code, _, stderr, out := runTool(t, "testdata/orphan.csv")
	if code == 0 {
		t.Fatal("muốn lỗi khi xã trỏ tỉnh không có")
	}
	if !strings.Contains(stderr, "T09003") {
		t.Fatalf("lỗi phải nêu mã xã T09003: %s", stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("không được ghi tệp khi lỗi: %v", err)
	}
}

// Các lỗi khác của tệp gốc: mỗi ca thoát khác 0 và nêu mã (hoặc dòng) sai.
func TestGenerateInvalid(t *testing.T) {
	head := "code,level,parent_code,name\n"
	cases := []struct{ name, body, want string }{
		{"trùng mã", "T01,province,,A\nT01,province,,B\n", "T01"},
		{"xã trỏ xã", "T01,province,,A\nT01001,ward,T01,X\nT01002,ward,T01001,Y\n", "T01002"},
		{"tỉnh có cha", "T01,province,,A\nT02,province,T01,B\n", "T02"},
		{"xã không có cha", "T01,province,,A\nT01001,ward,,X\n", "T01001"},
		{"cấp lạ", "T01,district,,A\n", "T01"},
		{"tên rỗng", "T01,province,,  \n", "T01"},
		{"mã rỗng", "T01,province,,A\n,ward,T01,X\n", "dòng 3"},
		{"thiếu cột", "T01,province,A\n", "dòng 2"},
		{"không có tỉnh", "", "không có tỉnh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := filepath.Join(t.TempDir(), "in.csv")
			if err := os.WriteFile(in, []byte(head+c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			code, _, stderr, _ := runTool(t, in)
			if code == 0 || !strings.Contains(stderr, c.want) {
				t.Fatalf("mã thoát %d, muốn lỗi nêu %q: %s", code, c.want, stderr)
			}
		})
	}
	t.Run("sai tiêu đề", func(t *testing.T) {
		in := filepath.Join(t.TempDir(), "in.csv")
		if err := os.WriteFile(in, []byte("ma,cap,cha,ten\nT01,province,,A\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code, _, stderr, _ := runTool(t, in); code == 0 || !strings.Contains(stderr, "tiêu đề") {
			t.Fatalf("mã thoát %d: %s", code, stderr)
		}
	})
	t.Run("thiếu cờ source", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		args := []string{"-in", "testdata/ok.csv", "-out", filepath.Join(t.TempDir(), "x.sql")}
		if code := run(args, &stdout, &stderr); code == 0 {
			t.Fatal("thiếu -source phải lỗi")
		}
	})
	t.Run("ngày hiệu lực sai", func(t *testing.T) {
		if code, _, _, _ := runTool(t, "testdata/ok.csv", "-effective-from", "01/07/2025"); code == 0 {
			t.Fatal("ngày không dạng YYYY-MM-DD phải lỗi")
		}
	})
}

// 1 tỉnh + 1000 xã = 1001 dòng: ba câu INSERT (500, 500, 1); cờ -effective-from được dùng.
func TestGenerateBatches(t *testing.T) {
	var b strings.Builder
	b.WriteString("code,level,parent_code,name\nT01,province,,Tỉnh Một\n")
	for i := range 1000 {
		fmt.Fprintf(&b, "T01%04d,ward,T01,Xã %d\n", i, i)
	}
	in := filepath.Join(t.TempDir(), "in.csv")
	if err := os.WriteFile(in, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, out := runTool(t, in, "-effective-from", "2025-06-12")
	if code != 0 {
		t.Fatalf("mã thoát %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "1 tỉnh, 1000 xã") {
		t.Fatalf("stdout: %q", stdout)
	}
	sb, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(sb)
	stmts := strings.Split(s, "INSERT INTO customer.admin_units")[1:]
	if len(stmts) != 3 {
		t.Fatalf("muốn 3 câu INSERT, có %d", len(stmts))
	}
	for i, want := range []int{500, 500, 1} {
		if n := strings.Count(stmts[i], "\n    ('"); n != want {
			t.Fatalf("câu %d có %d dòng, muốn %d", i+1, n, want)
		}
		if !strings.HasSuffix(strings.TrimSpace(stmts[i]), ";") {
			t.Fatalf("câu %d không kết thúc bằng ;", i+1)
		}
	}
	if strings.Count(s, "DATE '2025-06-12'") != 1001 {
		t.Fatal("effective_from phải lấy từ cờ")
	}
}
