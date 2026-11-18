// Lệnh catalog sinh docs/danh-muc-dung-chung.md từ tài liệu Go (go/doc, cùng nguồn của `go doc -all`) của mọi gói
// trong internal/platform: tên gói, câu đầu của doc gói, chữ ký mọi hàm và kiểu xuất ra ngoài, ghép với dòng "khi nào
// không dùng" viết tay trong docs/danh-muc-ghi-chu.yaml. Gói chưa có ghi chú: sinh dòng "CẦN GHI CHÚ" và thoát 1.
// Phần màn hình (Task 17): mọi câu export của console/src/shared/index.ts, kèm câu đầu của chú thích /** */ ngay trên,
// ghi chú dưới khóa là thư mục của tệp (console/src/shared).
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
	tsIndex := fs.String("ts", "console/src/shared/index.ts", "tệp export dùng chung của màn hình (không có thì bỏ qua)")
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
	tsText, tsMissing, err := generateTS(*tsIndex, notes)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	text += tsText
	missing = append(missing, tsMissing...)
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

// generateTS trả phần danh mục cho tệp export TypeScript; rỗng khi tệp không tồn tại.
func generateTS(path string, notes map[string]string) (string, []string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("đọc %s: %w", path, err)
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	note, ok := notes[dir]
	var missing []string
	if !ok {
		note = needNote
		missing = append(missing, dir)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n## `%s` (màn hình, TypeScript)\n\n", dir)
	fmt.Fprintf(&b, "Export của `%s`.\n\n", filepath.ToSlash(path))
	fmt.Fprintf(&b, "**Khi nào không dùng:** %s\n\n", note)
	if lines := tsExports(string(raw)); len(lines) > 0 {
		b.WriteString("```ts\n" + strings.Join(lines, "\n") + "\n```\n")
	}
	return b.String(), missing, nil
}

// tsExports đọc từng câu `export` ở đầu dòng: câu nhiều dòng (`export { a,\n b } from ...`) gộp thành một dòng;
// khai báo có thân (`export function f() {`) chỉ lấy chữ ký. Câu đầu của chú thích /** */ ngay trên ghi sau `//`.
func tsExports(src string) []string {
	lines := strings.Split(src, "\n")
	var out []string
	doc := ""
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "/**") {
			doc, i = tsDoc(lines, i)
			continue
		}
		if strings.HasPrefix(l, "export ") {
			var stmt string
			stmt, i = tsExport(lines, i)
			if doc != "" {
				stmt += " // " + doc
			}
			out = append(out, stmt)
		}
		doc = ""
	}
	return out
}

// tsDoc đọc chú thích /** */ bắt đầu ở dòng i; trả câu đầu và chỉ số dòng cuối của chú thích.
func tsDoc(lines []string, i int) (string, int) {
	var c strings.Builder
	for ; i < len(lines); i++ {
		c.WriteString(" " + strings.TrimSpace(lines[i]))
		if strings.Contains(lines[i], "*/") {
			break
		}
	}
	return firstSentence(c.String()), i
}

// tsExport đọc câu export bắt đầu ở dòng i; trả câu đã gộp và chỉ số dòng cuối đã đọc.
func tsExport(lines []string, i int) (string, int) {
	l := strings.TrimSpace(lines[i])
	if !strings.HasSuffix(l, "{") {
		return l, i
	}
	if strings.Contains(l, "(") || strings.Contains(l, "=") {
		return strings.TrimSpace(strings.TrimSuffix(l, "{")), i // khai báo có thân: chỉ chữ ký
	}
	stmt := l // export { a,\n b,\n } from "...": gộp tới dấu }
	for i+1 < len(lines) && !strings.Contains(stmt, "}") {
		i++
		stmt += " " + strings.TrimSpace(lines[i])
	}
	return strings.ReplaceAll(strings.Join(strings.Fields(stmt), " "), ", }", " }"), i
}

func firstSentence(c string) string {
	c = strings.TrimSpace(strings.NewReplacer("/**", "", "*/", "", " * ", " ", " *", " ").Replace(c))
	c = strings.Join(strings.Fields(c), " ")
	if i := strings.Index(c, ". "); i >= 0 {
		c = c[:i+1]
	}
	return c
}
