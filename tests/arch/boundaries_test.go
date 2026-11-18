package arch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const modPrefix = "elimaz.com.vn/crm/"

// moduleCalls: cột "Được gọi" của 04-kien-truc.md mục 3 — module ở khóa chỉ được nhập phần công khai của các
// module trong danh sách. Module chưa có trong bảng thì không được nhập module nào (thêm dòng khi spec thêm).
func moduleCalls() map[string][]string {
	return map[string][]string{
		"identity":     nil,
		"catalog":      {"identity"},
		"inventory":    {"catalog"},
		"customer":     {"identity"},
		"order":        {"customer", "catalog", "inventory", "promotion"},
		"fulfillment":  {"order"},
		"cash":         {"order", "fulfillment"},
		"promotion":    {"catalog", "customer", "inventory"},
		"channel":      {"identity"},
		"conversation": {"channel", "customer"},
		"affiliate":    nil, // chỉ nghe sự kiện order.*
		"reporting":    nil, // chỉ đọc view
	}
}

type listedPackage struct {
	ImportPath string
	Imports    []string
}

// goListPackages chạy `go list -e -json ./...` trong dir (không mạng: GOPROXY=off; -e để vẫn có danh sách nhập khi
// có lần nhập không phân giải được, như trong testdata).
func goListPackages(t *testing.T, dir string) []listedPackage {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-e", "-json", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list trong %s: %v\n%s", dir, err, stderr.String())
	}
	var pkgs []listedPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("đọc kết quả go list: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// moduleOf: "internal/modules/<tên>/..." → tên module, ok.
func moduleOf(rel string) (string, bool) {
	rest, ok := strings.CutPrefix(rel, "internal/modules/")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, "/")
	return name, true
}

func hasPathPrefix(p, prefix string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }

// boundaryViolation trả lời vi phạm (rỗng nếu không) của một lần nhập imp từ gói pkg (đường dẫn tương đối so với
// gốc module). Mỗi lần nhập báo tối đa một luật.
func boundaryViolation(pkg, imp string) string {
	impRel, internalImp := strings.CutPrefix(imp, modPrefix)
	srcMod, srcIsMod := moduleOf(pkg)
	switch {
	case internalImp && hasPathPrefix(impRel, "spikes"):
		return "(e) không gói nào được nhập spikes/"
	case srcIsMod && (imp == "net/http" || strings.HasPrefix(imp, "net/http/") ||
		(internalImp && hasPathPrefix(impRel, "internal/httpapi")) || hasPathPrefix(imp, "github.com/ogen-go/ogen")):
		return "(b) module nghiệp vụ không nhập net/http, internal/httpapi, mã ogen"
	case !strings.HasPrefix(pkg, "internal/platform/") &&
		(hasPathPrefix(imp, "github.com/valkey-io/valkey-go") || hasPathPrefix(imp, "github.com/aws/aws-sdk-go-v2")):
		return "(d) chỉ internal/platform được nhập valkey-go, aws-sdk-go-v2"
	}
	if !internalImp {
		return ""
	}
	dstMod, dstIsMod := moduleOf(impRel)
	if !dstIsMod {
		return ""
	}
	switch {
	case strings.HasPrefix(pkg, "internal/platform/"):
		if dstMod != "identity" || strings.Contains(impRel, "/internal/") {
			return "(c) internal/platform chỉ được nhập phần công khai của internal/modules/identity"
		}
	case srcIsMod && dstMod != srcMod:
		if strings.HasPrefix(impRel, "internal/modules/"+dstMod+"/internal") {
			return "(a) module không nhập phần riêng (internal/) của module khác"
		}
		if !slices.Contains(moduleCalls()[srcMod], dstMod) {
			return fmt.Sprintf("(a) module %s không được gọi %s (cột \"Được gọi\" của 04 mục 3)", srcMod, dstMod)
		}
	}
	return ""
}

// checkBoundaries trả mọi vi phạm luật a–e trong dir (một module Go có đường dẫn elimaz.com.vn/crm).
func checkBoundaries(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for _, p := range goListPackages(t, dir) {
		pkg, ok := strings.CutPrefix(p.ImportPath, modPrefix)
		if !ok {
			continue
		}
		for _, imp := range p.Imports {
			if v := boundaryViolation(pkg, imp); v != "" {
				out = append(out, fmt.Sprintf("%s nhập %s: %s", pkg, imp, v))
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestBoundariesTestdata(t *testing.T) {
	got := checkBoundaries(t, filepath.Join("testdata", "boundaries"))
	want := []string{
		"internal/modules/catalog nhập elimaz.com.vn/crm/internal/modules/order/internal/store: (a)",
		"internal/modules/customer nhập github.com/valkey-io/valkey-go: (d)",
		"internal/modules/identity nhập net/http: (b)",
		"internal/platform/queue nhập elimaz.com.vn/crm/internal/modules/order: (c)",
		"tools/probe nhập elimaz.com.vn/crm/spikes/flashsale: (e)",
	}
	if len(got) != len(want) {
		t.Fatalf("mong đúng %d vi phạm, có %d:\n%s", len(want), len(got), strings.Join(got, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("vi phạm %d: mong bắt đầu %q, có %q", i, want[i], got[i])
		}
	}
}

func TestBoundariesNotCalledModule(t *testing.T) {
	cases := []struct{ pkg, imp, want string }{
		{"internal/modules/catalog", modPrefix + "internal/modules/order", "(a)"},
		{"internal/modules/affiliate", modPrefix + "internal/modules/order", "(a)"},
		{"internal/modules/moi", modPrefix + "internal/modules/identity", "(a)"},
		{"internal/modules/order", modPrefix + "internal/modules/catalog", ""},
		{"internal/modules/order/internal/x", modPrefix + "internal/modules/order/internal/store", ""},
		{"internal/platform/queue", modPrefix + "internal/modules/identity", ""},
		{"internal/platform/queue", modPrefix + "internal/modules/identity/internal/store", "(c)"},
		{"internal/platform/cache", "github.com/valkey-io/valkey-go", ""},
		{"internal/httpapi", "github.com/aws/aws-sdk-go-v2/service/s3", "(d)"},
		{"internal/modules/order", modPrefix + "internal/httpapi/gen", "(b)"},
		{"internal/modules/order", "net/http/httptest", "(b)"},
		{"internal/httpapi", "net/http", ""},
	}
	for _, c := range cases {
		got := boundaryViolation(c.pkg, c.imp)
		if (c.want == "") != (got == "") || !strings.HasPrefix(got, c.want) {
			t.Errorf("%s nhập %s: mong %q, có %q", c.pkg, c.imp, c.want, got)
		}
	}
}

func TestBoundariesRealCode(t *testing.T) {
	if got := checkBoundaries(t, repoRoot(t)); len(got) > 0 {
		t.Fatalf("mã thật vi phạm ranh giới:\n%s", strings.Join(got, "\n"))
	}
}
