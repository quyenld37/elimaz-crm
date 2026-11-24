package arch

import (
	"go/ast"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"elimaz.com.vn/crm/internal/httpapi"
)

// webExemptForbidden: lời gọi làm route exempt (không khóa chống trùng) ghi dữ liệu hay gửi việc ra ngoài.
func webExemptForbidden() []string { return []string{"WriteTx", "RecordAudit", "Enqueue", "Emit"} }

// handlerName: tên phương thức handler ogen sinh từ operationId (chữ đầu viết hoa).
func handlerName(operationID string) string {
	r, n := utf8.DecodeRuneInString(operationID)
	return string(unicode.ToUpper(r)) + operationID[n:]
}

// webExemptWrites (M5-05): với mỗi route service_key khai x-idempotency: exempt, tìm phương thức handler cùng tên
// trong các tệp Go (không _test) của dir, quét AST thân hàm cùng mọi hàm, phương thức cùng gói nó gọi hay truyền làm
// giá trị (theo tên, bảo thủ: mọi phương thức trùng tên); báo lời gọi WriteTx, RecordAudit, Enqueue, Emit. Route
// không tìm thấy handler cũng bị báo (không quét được thì không coi là sạch).
func webExemptWrites(t *testing.T, dir string, routes []httpapi.Route) []string {
	t.Helper()
	funcs, handlers := packageFuncs(t, dir)
	var out []string
	for _, r := range routes {
		if r.Auth != "service_key" || r.IdempotencyMode != "exempt" {
			continue
		}
		h, ok := handlers[handlerName(r.OperationID)]
		if !ok {
			out = append(out, r.OperationID+": không tìm thấy handler "+handlerName(r.OperationID))
			continue
		}
		if found := forbiddenReach(h, funcs); len(found) > 0 {
			out = append(out, r.OperationID+": route exempt của website gọi "+strings.Join(found, ", "))
		}
	}
	return out
}

// packageFuncs: mọi hàm, phương thức có thân trong các tệp Go (không _test, không thư mục con) của dir theo tên, và
// phương thức của kiểu handler theo tên.
func packageFuncs(t *testing.T, dir string) (funcs map[string][]*ast.FuncDecl, handlers map[string]*ast.FuncDecl) {
	t.Helper()
	files := parseGoFiles(t, dir, func(rel string) bool {
		return !strings.Contains(rel, "/") && !strings.HasSuffix(rel, "_test.go")
	})
	funcs, handlers = map[string][]*ast.FuncDecl{}, map[string]*ast.FuncDecl{}
	for _, g := range files {
		for _, d := range g.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			funcs[fd.Name.Name] = append(funcs[fd.Name.Name], fd)
			if fd.Recv != nil && len(fd.Recv.List) == 1 && recvName(fd.Recv.List[0].Type) == "handler" {
				handlers[fd.Name.Name] = fd
			}
		}
	}
	return funcs, handlers
}

// forbiddenReach trả các tên cấm (webExemptForbidden) xuất hiện trong thân start hoặc thân mọi hàm cùng gói mà nó
// nhắc tới (gọi hay truyền làm giá trị), theo thứ tự gặp.
func forbiddenReach(start *ast.FuncDecl, funcs map[string][]*ast.FuncDecl) []string {
	seen := map[*ast.FuncDecl]bool{}
	var found []string
	stack := []*ast.FuncDecl{start}
	for len(stack) > 0 {
		fd := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[fd] {
			continue
		}
		seen[fd] = true
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			name := nodeName(n)
			if slices.Contains(webExemptForbidden(), name) && !slices.Contains(found, name) {
				found = append(found, name)
			}
			stack = append(stack, funcs[name]...)
			return true
		})
	}
	return found
}

// nodeName: tên của x.Tên hoặc Tên; rỗng với nút khác.
func nodeName(n ast.Node) string {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// recvName: tên kiểu của receiver (bỏ con trỏ).
func recvName(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func TestWebExemptSample(t *testing.T) {
	routes := []httpapi.Route{
		{OperationID: "webQuote", Auth: "service_key", Write: true, IdempotencyMode: "exempt"},
		{OperationID: "webAudit", Auth: "service_key", Write: true, IdempotencyMode: "exempt"},
		{OperationID: "webPrices", Auth: "service_key", Write: true, IdempotencyMode: "exempt"},
		{OperationID: "webMissing", Auth: "service_key", Write: true, IdempotencyMode: "exempt"},
		// Không thuộc phạm vi: route nhân viên exempt, route web required.
		{OperationID: "saveQuote", Auth: "access", Write: true, IdempotencyMode: "exempt"},
		{OperationID: "webQuote", Auth: "service_key", Write: true, IdempotencyMode: "required"},
	}
	got := webExemptWrites(t, filepath.Join("testdata", "webexempt"), routes)
	want := []string{
		"webQuote: route exempt của website gọi WriteTx",
		"webAudit: route exempt của website gọi RecordAudit",
		"webMissing: không tìm thấy handler WebMissing",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("phát hiện trên tệp mẫu:\n%s\nmong:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestWebExemptRealCode(t *testing.T) {
	t.Parallel()
	if got := webExemptWrites(t, filepath.Join(repoRoot(t), "internal", "httpapi"), httpapi.Routes); len(got) != 0 {
		t.Fatalf("route exempt của website ghi dữ liệu:\n%s", strings.Join(got, "\n"))
	}
}
