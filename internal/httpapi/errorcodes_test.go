package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ogen-go/ogen"
)

// TestErrorCodes_DeclaredInContract: mọi mã lỗi errors.go trả cho client (hằng code*) có trong danh sách
// x-error-codes của components.schemas.Error trong openapi/openapi.yaml. Màn hình đọc danh sách đó để có câu tiếng
// Việt cho từng mã (console/src/app/errors.test.ts), nên mã mới mà quên khai thì test này đỏ trước khi nhân viên thấy
// mã máy. Hợp đồng được khai trước mã của mốc sau (chiều ngược lại không bắt buộc).
func TestErrorCodes_DeclaredInContract(t *testing.T) {
	t.Parallel()
	declared := contractErrorCodes(t)
	for _, code := range errorsGoCodes(t) {
		if !slices.Contains(declared, code) {
			t.Errorf("mã lỗi %q có trong internal/httpapi/errors*.go nhưng thiếu ở x-error-codes của openapi.yaml", code)
		}
	}
}

func TestErrorCodes_ContractListWellFormed(t *testing.T) {
	t.Parallel()
	declared := contractErrorCodes(t)
	if len(declared) == 0 {
		t.Fatal("x-error-codes rỗng")
	}
	seen := map[string]bool{}
	for _, c := range declared {
		if seen[c] {
			t.Errorf("x-error-codes có %q hai lần", c)
		}
		seen[c] = true
		if c == "" || strings.Trim(c, "abcdefghijklmnopqrstuvwxyz_") != "" {
			t.Errorf("mã %q không phải chữ thường và gạch dưới", c)
		}
	}
}

func contractErrorCodes(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../openapi/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ogen.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	schema, ok := spec.Components.Schemas["Error"]
	if !ok || schema == nil {
		t.Fatal("thiếu components.schemas.Error")
	}
	node, ok := schema.Common.Extensions["x-error-codes"]
	if !ok {
		t.Fatal("components.schemas.Error thiếu x-error-codes")
	}
	var codes []string
	if err := node.Decode(&codes); err != nil {
		t.Fatalf("x-error-codes phải là danh sách chuỗi: %v", err)
	}
	return codes
}

// errorsGoCodes đọc giá trị của mọi hằng chuỗi tên code* trong errors.go và các tệp theo vùng errors_<vùng>.go (Task
// 441), không gồm tệp test.
func errorsGoCodes(t *testing.T) []string {
	t.Helper()
	names, err := filepath.Glob("errors*.go")
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range vs.Names {
				if v, ok := codeLiteral(name.Name, vs.Values, i); ok {
					codes = append(codes, v)
				}
			}
			return false
		})
	}
	// Trước khi tách (Task 441) errors.go có 104 mã; errors.go một mình nay chỉ còn mã chung (29).
	if len(codes) < 100 {
		t.Fatalf("chỉ đọc được %d mã lỗi từ errors*.go; cách đọc đã sai", len(codes))
	}
	return codes
}

// codeLiteral trả chuỗi của values[i] khi tên bắt đầu bằng "code" và giá trị là chuỗi viết thẳng.
func codeLiteral(name string, values []ast.Expr, i int) (string, bool) {
	if !strings.HasPrefix(name, "code") || i >= len(values) {
		return "", false
	}
	lit, ok := values[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}
