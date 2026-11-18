// Lệnh routegen đọc hợp đồng openapi/openapi.yaml và sinh internal/httpapi/routes_gen.go: bảng Routes mà
// httpapi.NewServer đăng ký vào ServeMux (kế hoạch M0 bản 5, Task 6; luật .claude/rules/openapi.md).
//
//	go run ./tools/routegen -in openapi/openapi.yaml -out internal/httpapi/routes_gen.go
//
// Thoát khác 0, không ghi tệp, và nêu operationId khi một operation thiếu operationId, không có đúng một trong
// `x-permission: <khóa quyền>` hoặc `x-public: true`, hoặc là operation ghi (POST, PUT, PATCH, DELETE) mà không có
// tham số `$ref: '#/components/parameters/IdempotencyKey'` (ở operation hoặc ở đường dẫn).
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ogen-go/ogen"
)

const idempotencyKeyRef = "#/components/parameters/IdempotencyKey"

var errInvalidContract = errors.New("hợp đồng vi phạm luật route")

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("routegen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "tệp hợp đồng OpenAPI")
	out := fs.String("out", "", "tệp Go sinh ra")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *out == "" {
		_, _ = fmt.Fprintln(stderr, "routegen: cần -in và -out")
		return 2
	}
	spec, err := os.ReadFile(*in)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "routegen:", err)
		return 1
	}
	src, err := generate(spec)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "routegen: %s: %v\n", *in, err)
		return 1
	}
	if err := os.WriteFile(*out, src, 0o600); err != nil {
		_, _ = fmt.Fprintln(stderr, "routegen:", err)
		return 1
	}
	return 0
}

type route struct {
	Method, Path, OperationID, Permission string
	Public, Write, Stream                 bool
}

// generate kiểm mọi operation và trả mã Go đã định dạng; lỗi liệt kê mọi vi phạm cùng lúc.
func generate(specYAML []byte) ([]byte, error) {
	spec, err := ogen.Parse(specYAML)
	if err != nil {
		return nil, fmt.Errorf("đọc hợp đồng: %w", err)
	}
	var routes []route
	var problems []string
	for path, item := range spec.Paths {
		if item == nil {
			continue
		}
		for _, mo := range operations(item) {
			r, errs := checkOperation(path, mo.method, item, mo.op)
			problems = append(problems, errs...)
			routes = append(routes, r)
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, fmt.Errorf("%w:\n  %s", errInvalidContract, strings.Join(problems, "\n  "))
	}
	slices.SortFunc(routes, func(a, b route) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.Method, b.Method)
	})
	return render(routes)
}

type methodOp struct {
	method string
	op     *ogen.Operation
}

func operations(item *ogen.PathItem) []methodOp {
	all := []methodOp{
		{"GET", item.Get},
		{"PUT", item.Put},
		{"POST", item.Post},
		{"DELETE", item.Delete},
		{"OPTIONS", item.Options},
		{"HEAD", item.Head},
		{"PATCH", item.Patch},
		{"TRACE", item.Trace},
	}
	return slices.DeleteFunc(all, func(m methodOp) bool { return m.op == nil })
}

func isWrite(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// checkOperation dựng route của một operation và trả các vi phạm (mỗi dòng nêu operationId).
func checkOperation(path, method string, item *ogen.PathItem, op *ogen.Operation) (route, []string) {
	r := route{Method: method, Path: path, OperationID: op.OperationID, Write: isWrite(method), Stream: isStream(op)}
	name := op.OperationID
	var problems []string
	if name == "" {
		name = method + " " + path
		problems = append(problems, name+": thiếu operationId")
	}
	ext := op.Common.Extensions
	permNode, hasPerm := ext["x-permission"]
	publicNode, hasPublic := ext["x-public"]
	switch {
	case hasPerm && hasPublic:
		problems = append(problems, name+": có cả x-permission lẫn x-public; chỉ được một")
	case !hasPerm && !hasPublic:
		problems = append(problems, name+": thiếu cả x-permission lẫn x-public")
	case hasPerm:
		if permNode.Tag != "!!str" || strings.TrimSpace(permNode.Value) == "" {
			problems = append(problems, name+": x-permission phải là một khóa quyền không rỗng")
		}
		r.Permission = strings.TrimSpace(permNode.Value)
	default:
		if b, err := strconv.ParseBool(publicNode.Value); publicNode.Tag != "!!bool" || err != nil || !b {
			problems = append(problems, name+": x-public chỉ nhận giá trị true")
		}
		r.Public = true
	}
	if r.Write && !hasIdempotencyKey(item.Parameters) && !hasIdempotencyKey(op.Parameters) {
		problems = append(problems, name+": operation ghi thiếu tham số $ref '"+idempotencyKeyRef+"' (IdempotencyKey)")
	}
	return r, problems
}

func hasIdempotencyKey(params []*ogen.Parameter) bool {
	return slices.ContainsFunc(params, func(p *ogen.Parameter) bool { return p != nil && p.Ref == idempotencyKeyRef })
}

// isStream: câu trả lời 200 là text/event-stream (handler SSE riêng, không đếm vào giới hạn tần suất).
func isStream(op *ogen.Operation) bool {
	resp, ok := op.Responses["200"]
	if !ok || resp == nil {
		return false
	}
	_, ok = resp.Content["text/event-stream"]
	return ok
}

const header = `// Code generated by tools/routegen. DO NOT EDIT.
// Nguồn: openapi/openapi.yaml. Sửa hợp đồng rồi chạy make gen.

package httpapi

// Route là một operation của hợp đồng, đăng ký vào ServeMux bằng Pattern.
type Route struct {
	Method      string // GET, POST…
	Pattern     string // mẫu của ServeMux: "POST /api/v1/platform/jobs/{id}/retry"
	OperationID string
	Permission  string // khóa quyền (x-permission); rỗng khi Public
	Public      bool   // x-public: true, bỏ qua auth và guard
	Write       bool   // POST, PUT, PATCH, DELETE: kiểm origin và khóa chống trùng
	Stream      bool   // câu trả lời text/event-stream: không đếm vào giới hạn tần suất
}

// Routes là mọi operation của hợp đồng, theo thứ tự đường dẫn rồi method.
var Routes = []Route{
`

func render(routes []route) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(header)
	for _, r := range routes {
		fields := []string{
			"Method: " + strconv.Quote(r.Method),
			"Pattern: " + strconv.Quote(r.Method+" "+r.Path),
			"OperationID: " + strconv.Quote(r.OperationID),
		}
		if r.Permission != "" {
			fields = append(fields, "Permission: "+strconv.Quote(r.Permission))
		}
		for _, f := range []struct {
			name string
			on   bool
		}{{"Public", r.Public}, {"Write", r.Write}, {"Stream", r.Stream}} {
			if f.on {
				fields = append(fields, f.name+": true")
			}
		}
		b.WriteString("\t{" + strings.Join(fields, ", ") + "},\n")
	}
	b.WriteString("}\n")
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("định dạng mã sinh: %w", err)
	}
	return src, nil
}
