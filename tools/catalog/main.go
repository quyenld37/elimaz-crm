// Lệnh catalog sinh docs/danh-muc-dung-chung.md từ tài liệu Go (go/doc, cùng nguồn của `go doc -all`) của mọi gói
// trong internal/platform: tên gói, câu đầu của doc gói, chữ ký mọi hàm và kiểu xuất ra ngoài, ghép với dòng "khi nào
// không dùng" viết tay trong docs/danh-muc-ghi-chu.yaml. Gói chưa có ghi chú: sinh dòng "CẦN GHI CHÚ" và thoát 1.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const needNote = "CẦN GHI CHÚ"

var errNotes = errors.New("tệp ghi chú sai dạng")

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "internal/platform", "thư mục chứa các gói dùng chung")
	notesPath := fs.String("notes", "docs/danh-muc-ghi-chu.yaml", "tệp ghi chú viết tay")
	out := fs.String("out", "docs/danh-muc-dung-chung.md", "tệp danh mục sinh ra")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, err := os.ReadFile(*notesPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "đọc tệp ghi chú:", err)
		return 2
	}
	notes, err := parseNotes(raw)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	text, missing, err := generate(*root, notes)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	if err := os.WriteFile(*out, []byte(text), 0o600); err != nil {
		_, _ = fmt.Fprintln(stderr, "ghi danh mục:", err)
		return 2
	}
	if len(missing) > 0 {
		_, _ = fmt.Fprintf(stderr, "gói chưa có ghi chú \"khi nào không dùng\" trong %s: %s\n", *notesPath, strings.Join(missing, ", "))
		return 1
	}
	return 0
}

// parseNotes đọc một tập con của YAML: mỗi dòng `đường/dẫn/gói: câu ghi chú` (câu có thể đặt trong nháy kép),
// dòng trống và dòng bắt đầu bằng # bị bỏ qua.
func parseNotes(b []byte) (map[string]string, error) {
	notes := map[string]string{}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if !ok || key == "" || val == "" {
			return nil, fmt.Errorf("%w: dòng %d phải có dạng `gói: ghi chú`", errNotes, i+1)
		}
		if strings.HasPrefix(val, `"`) {
			unq, err := strconv.Unquote(val)
			if err != nil {
				return nil, fmt.Errorf("%w: dòng %d: %w", errNotes, i+1, err)
			}
			val = unq
		}
		notes[key] = val
	}
	return notes, nil
}

// generate trả nội dung danh mục và các gói thiếu ghi chú.
func generate(root string, notes map[string]string) (string, []string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", nil, fmt.Errorf("đọc %s: %w", root, err)
	}
	var b strings.Builder
	b.WriteString("# Danh mục dùng chung\n\n")
	b.WriteString("> Sinh tự động bằng `make catalog` (`go run ./tools/catalog`) từ tài liệu Go của `" + root + "/*`. ")
	b.WriteString("Không sửa tay; dòng \"Khi nào không dùng\" sửa ở `docs/danh-muc-ghi-chu.yaml`.\n")
	var missing []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "testdata" {
			continue
		}
		dir := filepath.ToSlash(filepath.Join(root, e.Name()))
		pkg, fset, err := loadPackage(dir)
		if err != nil {
			return "", nil, err
		}
		if pkg == nil {
			continue
		}
		note, ok := notes[dir]
		if !ok {
			note = needNote
			missing = append(missing, dir)
		}
		writePackage(&b, fset, dir, pkg, note)
	}
	return b.String(), missing, nil
}

// loadPackage đọc các tệp .go không phải test của dir; nil khi thư mục không có tệp Go.
func loadPackage(dir string) (*doc.Package, *token.FileSet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc %s: %w", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("đọc mã %s: %w", n, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil, nil
	}
	pkg, err := doc.NewFromFiles(fset, files, dir)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc tài liệu %s: %w", dir, err)
	}
	return pkg, fset, nil
}

func writePackage(b *strings.Builder, fset *token.FileSet, dir string, pkg *doc.Package, note string) {
	fmt.Fprintf(b, "\n## `%s` (gói `%s`)\n\n", dir, pkg.Name)
	if s := pkg.Synopsis(pkg.Doc); s != "" {
		b.WriteString(s + "\n\n")
	}
	fmt.Fprintf(b, "**Khi nào không dùng:** %s\n\n", note)
	var sigs []string
	for _, f := range pkg.Funcs {
		sigs = append(sigs, funcSig(fset, f.Decl))
	}
	for _, t := range pkg.Types {
		sigs = append(sigs, typeSig(fset, t.Decl))
		for _, f := range t.Funcs {
			sigs = append(sigs, funcSig(fset, f.Decl))
		}
		for _, m := range t.Methods {
			sigs = append(sigs, funcSig(fset, m.Decl))
		}
	}
	sigs = slices.DeleteFunc(sigs, func(s string) bool { return s == "" })
	if len(sigs) == 0 {
		return
	}
	b.WriteString("```go\n" + strings.Join(sigs, "\n") + "\n```\n")
}

func funcSig(fset *token.FileSet, d *ast.FuncDecl) string {
	cp := *d
	cp.Doc, cp.Body = nil, nil
	return render(fset, &cp)
}

func typeSig(fset *token.FileSet, d *ast.GenDecl) string {
	cp := *d
	cp.Doc = nil
	ast.Inspect(&cp, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			x.Doc, x.Comment = nil, nil
		case *ast.TypeSpec:
			x.Doc, x.Comment = nil, nil
		case *ast.StructType:
			x.Incomplete = false
		case *ast.InterfaceType:
			x.Incomplete = false
		}
		return true
	})
	return render(fset, &cp)
}

func render(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	if err := (&printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}).Fprint(&buf, fset, n); err != nil {
		return ""
	}
	return buf.String()
}
