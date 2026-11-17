package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogWithNotes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "danh-muc.md")
	var stderr bytes.Buffer
	code := run([]string{"-root", "testdata/ok", "-notes", "testdata/ok/notes.yaml", "-out", out}, &stderr)
	if code != 0 {
		t.Fatalf("mã thoát %d, stderr: %s", code, stderr.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"testdata/ok/sample",
		"Package sample là gói mẫu cho test của tools/catalog.",
		"func Add(a, b int) int",
		"func NewGreeter(name string) *Greeter",
		"func (g *Greeter) Greet(ctx context.Context, to string) (string, error)",
		"type Greeter struct",
		"Name string",
		"Không dùng cho việc gì ngoài test.",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("thiếu %q trong:\n%s", want, s)
		}
	}
	for _, bad := range []string{"Câu thứ hai", "hidden", "secret", "CẦN GHI CHÚ"} {
		if strings.Contains(s, bad) {
			t.Errorf("không được có %q trong:\n%s", bad, s)
		}
	}
}

func TestCatalogMissingNoteFails(t *testing.T) {
	out := filepath.Join(t.TempDir(), "danh-muc.md")
	var stderr bytes.Buffer
	code := run([]string{"-root", "testdata/missing", "-notes", "testdata/missing/notes.yaml", "-out", out}, &stderr)
	if code == 0 {
		t.Fatal("gói thiếu ghi chú phải thoát khác 0")
	}
	if !strings.Contains(stderr.String(), "testdata/missing/bare") {
		t.Fatalf("stderr phải nêu gói thiếu: %s", stderr.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "CẦN GHI CHÚ") || !strings.Contains(string(b), "func Hello() string") {
		t.Fatalf("tệp sinh phải có dòng CẦN GHI CHÚ và chữ ký:\n%s", b)
	}
}

func TestCatalogDeterministic(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	var stderr bytes.Buffer
	for _, out := range []string{a, b} {
		if code := run([]string{"-root", "testdata/ok", "-notes", "testdata/ok/notes.yaml", "-out", out}, &stderr); code != 0 {
			t.Fatal(stderr.String())
		}
	}
	x, _ := os.ReadFile(a)
	y, _ := os.ReadFile(b)
	if !bytes.Equal(x, y) {
		t.Fatal("hai lần sinh phải giống hệt")
	}
}

func TestParseNotesRejectsBadLine(t *testing.T) {
	if _, err := parseNotes([]byte("không có dấu hai chấm\n")); err == nil {
		t.Fatal("dòng không đúng dạng phải lỗi")
	}
	n, err := parseNotes([]byte("# chú thích\n\na/b: \"x: y\"\nc/d: z\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n["a/b"] != "x: y" || n["c/d"] != "z" {
		t.Fatalf("parse = %v", n)
	}
}
