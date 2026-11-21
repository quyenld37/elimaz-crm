// Lệnh routegen đọc hợp đồng openapi/openapi.yaml và sinh internal/httpapi/routes_gen.go: bảng Routes mà
// httpapi.NewServer đăng ký vào ServeMux (kế hoạch M0 bản 5, Task 6; luật .claude/rules/openapi.md).
//
//	go run ./tools/routegen -in openapi/openapi.yaml -out internal/httpapi/routes_gen.go
//
// Thoát khác 0, không ghi tệp, và nêu operationId khi một operation thiếu operationId, không có đúng một trong
// `x-permission: <khóa quyền>`, `x-public: true` hoặc `x-authenticated: true` (route đọc chỉ cần đăng nhập), hoặc là operation ghi (POST, PUT, PATCH, DELETE) mà không có
// tham số `$ref: '#/components/parameters/IdempotencyKey'` (ở operation hoặc ở đường dẫn).
//
// Ngoại lệ chống trùng (ADR-47, Q-X5): `x-idempotency: exempt | no-replay` kèm `x-idempotency-reason: "<lý do>"`.
// Route ghi không khai cờ là "required". Lỗi khi: cờ có giá trị khác; thiếu lý do hoặc lý do rỗng; cờ trên method
// khác POST; `exempt` vẫn khai IdempotencyKey; `no-replay` không khai IdempotencyKey; có lý do mà không có cờ.
//
// Đường của website (M5-05, luật openapi 6): path `/api/v1/web/*` khai `x-auth: service_key` và ngược lại (giá trị
// khác bị từ chối); route service_key khai `x-permission` bắt đầu `web.`; `x-admission: <tên>` chỉ trên route
// service_key có chế độ chống trùng required; `x-body-limit: <byte>` là số nguyên trong 1 KiB – 8 MiB. Route không
// khai x-auth: Auth "public" khi x-public, còn lại "access".
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ogen-go/ogen"
)

const idempotencyKeyRef = "#/components/parameters/IdempotencyKey"

// Chế độ chống trùng của route ghi (ADR-47). Route đọc không có chế độ ("").
const (
	modeRequired = "required"
	modeExempt   = "exempt"
	modeNoReplay = "no-replay"
)

// Cách xác thực của route (Route.Auth) và luật đường của website (M5-05).
const (
	authAccess     = "access"
	authServiceKey = "service_key"
	authPublic     = "public"
	webPathPrefix  = "/api/v1/web/"
	webPermPrefix  = "web."
	minBodyLimit   = 1 << 10
	maxBodyLimit   = 8 << 20
)

// admissionName: tên cổng nhận vào (khóa của httpapi.Deps.Admission).
var admissionName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

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
	Public, Authenticated, Write, Stream  bool
	IdempotencyMode, IdempotencyReason    string
	Auth, Admission                       string
	BodyLimit                             int64
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

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
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
	authNode, hasAuth := ext["x-authenticated"]
	switch {
	case btoi(hasPerm)+btoi(hasPublic)+btoi(hasAuth) > 1:
		problems = append(problems, name+": chỉ được một trong x-permission, x-public, x-authenticated")
	case !hasPerm && !hasPublic && !hasAuth:
		problems = append(problems, name+": thiếu cả x-permission lẫn x-public (hoặc x-authenticated cho route đọc chỉ cần đăng nhập)")
	case hasPerm:
		if permNode.Tag != "!!str" || strings.TrimSpace(permNode.Value) == "" {
			problems = append(problems, name+": x-permission phải là một khóa quyền không rỗng")
		}
		r.Permission = strings.TrimSpace(permNode.Value)
	case hasAuth:
		if b, err := strconv.ParseBool(authNode.Value); authNode.Tag != "!!bool" || err != nil || !b {
			problems = append(problems, name+": x-authenticated chỉ nhận giá trị true")
		}
		if r.Write {
			problems = append(problems, name+": x-authenticated chỉ cho route đọc; route ghi phải khai x-permission")
		}
		r.Authenticated = true
	default:
		if b, err := strconv.ParseBool(publicNode.Value); publicNode.Tag != "!!bool" || err != nil || !b {
			problems = append(problems, name+": x-public chỉ nhận giá trị true")
		}
		r.Public = true
	}
	hasKey := hasIdempotencyKey(item.Parameters) || hasIdempotencyKey(op.Parameters)
	mode, reason, errs := idempotencyMode(name, method, ext, hasKey)
	r.IdempotencyMode, r.IdempotencyReason = mode, reason
	problems = append(problems, errs...)
	problems = append(problems, webRules(name, path, ext, &r)...)
	return r, problems
}

// webRules đọc x-auth, x-admission, x-body-limit (M5-05) vào r và trả các vi phạm; chạy sau khi r đã có quyền và chế
// độ chống trùng.
func webRules(name, path string, ext ogen.Extensions, r *route) []string {
	var problems []string
	r.Auth = authAccess
	if r.Public {
		r.Auth = authPublic
	}
	authNode, hasAuth := ext["x-auth"]
	switch {
	case hasAuth && (authNode.Tag != "!!str" || authNode.Value != authServiceKey):
		problems = append(problems, name+": x-auth chỉ nhận service_key, có "+strconv.Quote(authNode.Value))
	case hasAuth:
		r.Auth = authServiceKey
	}
	onWebPath := strings.HasPrefix(path, webPathPrefix)
	switch {
	case onWebPath && r.Auth != authServiceKey:
		problems = append(problems, name+": path "+webPathPrefix+"* phải khai x-auth: service_key")
	case !onWebPath && r.Auth == authServiceKey:
		problems = append(problems, name+": x-auth: service_key chỉ cho path "+webPathPrefix+"*")
	}
	if r.Auth == authServiceKey && !strings.HasPrefix(r.Permission, webPermPrefix) {
		problems = append(problems, name+": route x-auth: service_key phải khai x-permission bắt đầu bằng "+webPermPrefix)
	}
	if node, ok := ext["x-admission"]; ok {
		switch {
		case node.Tag != "!!str" || !admissionName.MatchString(node.Value):
			problems = append(problems, name+": x-admission phải là tên chữ thường (a-z, 0-9, _), có "+strconv.Quote(node.Value))
		case r.Auth != authServiceKey || r.IdempotencyMode != modeRequired:
			problems = append(problems, name+": x-admission chỉ trên operation x-auth: service_key có khóa chống trùng bắt buộc (required)")
		default:
			r.Admission = node.Value
		}
	}
	if node, ok := ext["x-body-limit"]; ok {
		n, err := strconv.ParseInt(node.Value, 10, 64)
		if node.Tag != "!!int" || err != nil || n < minBodyLimit || n > maxBodyLimit {
			problems = append(problems, fmt.Sprintf("%s: x-body-limit phải là số byte nguyên trong %d – %d, có %q",
				name, minBodyLimit, maxBodyLimit, node.Value))
		} else {
			r.BodyLimit = n
		}
	}
	return problems
}

// idempotencyMode đọc x-idempotency, x-idempotency-reason (ADR-47) và kiểm luật cùng tham số IdempotencyKey; trả chế
// độ ("" cho route đọc), lý do đã bỏ khoảng trắng hai đầu, và các vi phạm.
func idempotencyMode(name, method string, ext ogen.Extensions, hasKey bool) (mode, reason string, problems []string) {
	modeNode, hasMode := ext["x-idempotency"]
	if reasonNode, ok := ext["x-idempotency-reason"]; ok {
		if !hasMode {
			problems = append(problems, name+": có x-idempotency-reason mà không có x-idempotency")
		}
		if reasonNode.Tag == "!!str" {
			reason = strings.TrimSpace(reasonNode.Value)
		}
	}
	if !hasMode {
		if !isWrite(method) {
			return "", "", problems
		}
		if !hasKey {
			problems = append(problems, name+": operation ghi thiếu tham số $ref '"+idempotencyKeyRef+"' (IdempotencyKey)")
		}
		return modeRequired, "", problems
	}
	mode = modeNode.Value
	if modeNode.Tag != "!!str" || (mode != modeExempt && mode != modeNoReplay) {
		return "", "", append(problems, name+": x-idempotency chỉ nhận exempt hoặc no-replay, có "+strconv.Quote(modeNode.Value))
	}
	if reason == "" {
		problems = append(problems, name+": x-idempotency: "+mode+" cần x-idempotency-reason không rỗng")
	}
	if method != "POST" {
		problems = append(problems, name+": x-idempotency: "+mode+" chỉ cho POST, có "+method)
	}
	switch {
	case mode == modeExempt && hasKey:
		problems = append(problems, name+": x-idempotency: exempt không được khai tham số IdempotencyKey")
	case mode == modeNoReplay && !hasKey:
		problems = append(problems, name+": x-idempotency: no-replay vẫn phải khai tham số $ref '"+idempotencyKeyRef+"' (IdempotencyKey)")
	}
	return mode, reason, problems
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
	Permission  string // khóa quyền (x-permission); rỗng khi Public hoặc Authenticated
	Public      bool   // x-public: true, bỏ qua auth và guard
	// Authenticated (x-authenticated: true, chỉ route đọc): qua auth (thẻ, MFA, tài khoản không khóa) là đủ, guard
	// không đòi quyền. Chỉ dùng cho route trả dữ liệu của chính người gọi (GET /api/v1/me).
	Authenticated bool
	Write       bool   // POST, PUT, PATCH, DELETE: kiểm origin và khóa chống trùng
	Stream      bool   // câu trả lời text/event-stream: không đếm vào giới hạn tần suất
	// IdempotencyMode của route ghi (ADR-41, ADR-47): "required" (mặc định: bắt buộc Idempotency-Key, lưu kết quả),
	// "exempt" (x-idempotency: exempt, không khóa), "no-replay" (bắt buộc khóa, không lưu thân trả lời). Rỗng với
	// route đọc.
	IdempotencyMode   string
	IdempotencyReason string // x-idempotency-reason khi exempt hoặc no-replay
	// Auth (M5-05): "access" (thẻ Cloudflare Access của nhân viên), "service_key" (khóa dịch vụ của website,
	// x-auth: service_key, chỉ path /api/v1/web/*) hoặc "public" (x-public).
	Auth        string
	Admission   string // x-admission: tên cổng nhận vào (httpapi.Deps.Admission); rỗng: không có cổng
	BodyLimit   int64  // x-body-limit: số byte thân tối đa; 0: mặc định 1 MiB
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
		}{{"Public", r.Public}, {"Authenticated", r.Authenticated}, {"Write", r.Write}, {"Stream", r.Stream}} {
			if f.on {
				fields = append(fields, f.name+": true")
			}
		}
		if r.IdempotencyMode != "" {
			fields = append(fields, "IdempotencyMode: "+strconv.Quote(r.IdempotencyMode))
		}
		if r.IdempotencyReason != "" {
			fields = append(fields, "IdempotencyReason: "+strconv.Quote(r.IdempotencyReason))
		}
		fields = append(fields, "Auth: "+strconv.Quote(r.Auth))
		if r.Admission != "" {
			fields = append(fields, "Admission: "+strconv.Quote(r.Admission))
		}
		if r.BodyLimit != 0 {
			fields = append(fields, "BodyLimit: "+strconv.FormatInt(r.BodyLimit, 10))
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
