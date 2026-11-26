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
		"content":      {"catalog", "promotion"}, // M5 Task 453: chỉ đọc view, không nhập order
		"channel":      {"identity"},
		"conversation": {"channel", "customer", "order", "identity"}, // M4 Q1: conversation điều phối chốt live
		"affiliate":    nil,                                          // chỉ nghe sự kiện order.*
		"reporting":    nil,                                          // chỉ đọc view
	}
}

type listedPackage struct {
	ImportPath string
	Imports    []string
	Deps       []string // mọi gói nó cần, cả bắc cầu (luật (g) xét cả tệp chạy imager liên kết gì, Task 414)
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

// imagerForbidden: imager chỉ ra mạng tới kho tệp, không nối database, không giữ bí mật (Global Constraints M1).
func imagerForbidden(imp string) bool {
	if hasPathPrefix(imp, "github.com/jackc/pgx") {
		return true
	}
	rel, ok := strings.CutPrefix(imp, modPrefix)
	if !ok {
		return false
	}
	if hasPathPrefix(rel, "internal/modules") {
		return true
	}
	for _, p := range []string{"db", "tenancy", "config", "idempotency", "queue", "commands"} {
		if hasPathPrefix(rel, "internal/platform/"+p) {
			return true
		}
	}
	return false
}

// isStdlib: đường nhập của thư viện chuẩn (phần đầu không có dấu chấm).
func isStdlib(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".")
}

// readerViolation: luật (f), (g) của M1 (Task M1-25) cho gói chỉ đọc view và cho imager; luật (h) của Task M1-38
// cho gói Excel dùng chung (không biết nghiệp vụ, là chỗ duy nhất nhập excelize); luật (i) của M2 (Task M2-02, Q32)
// cho gói nhãn đơn internal/queries/orderview: chỉ thư viện chuẩn, để module order và gói internal/queries cùng nhập
// (ngoại lệ có lý do: một nguồn nhãn cho màn đơn M2, cảnh báo M3, báo cáo M6); luật (l) của M5 (Task 184) cho lớp nối
// Valkey.
func readerViolation(pkg, imp string) string {
	if v := carrierViolation(pkg, imp); v != "" {
		return v
	}
	if v := wiringViolation(pkg, imp); v != "" {
		return v
	}
	if v := messagingAdapterViolation(pkg, imp); v != "" {
		return v
	}
	switch {
	case strings.HasPrefix(pkg, "internal/modules/") && hasPathPrefix(imp, modPrefix+"internal/platform/cache"):
		// Luật (l) của M5 (Task 184, ADR-39): Valkey không quyết định nghiệp vụ nào; module không chạm lớp nối Valkey.
		return "(l) module nghiệp vụ không nhập internal/platform/cache"
	case hasPathPrefix(pkg, "internal/queries/orderview") && !isStdlib(imp):
		return "(i) internal/queries/orderview chỉ nhập thư viện chuẩn"
	case hasPathPrefix(pkg, "internal/platform/xlsx") && hasPathPrefix(imp, modPrefix+"internal/modules"):
		return "(h) internal/platform/xlsx không nhập internal/modules/**"
	case !hasPathPrefix(pkg, "internal/platform/xlsx") && hasPathPrefix(imp, "github.com/xuri/excelize"):
		return "(h) chỉ internal/platform/xlsx được nhập excelize"
	case hasPathPrefix(pkg, "internal/queries") && hasPathPrefix(imp, modPrefix+"internal/modules"):
		return "(f) internal/queries chỉ đọc view, không nhập internal/modules/**"
	case (hasPathPrefix(pkg, "internal/imager") || hasPathPrefix(pkg, "cmd/imager")) && imagerForbidden(imp):
		return "(g) imager không nhập internal/modules/**, database, cấu hình, hàng đợi, pgx"
	}
	return ""
}

// carrierAllowed: gói nội bộ adapter hãng được nhập (luật (j) của M3, Task M3-04): phần công khai của fulfillment (kiểu,
// interface CarrierAdapter), log, dấu vết, cấu hình, kiểu nguồn webhook (Source của ingest, Q-T91-7) và các gói adapter
// hãng khác.
func carrierAllowed(rel string) bool {
	if rel == "internal/modules/fulfillment" || hasPathPrefix(rel, "internal/adapters/carrier") {
		return true
	}
	for _, p := range []string{"logging", "telemetry", "config", "webhooks"} {
		if hasPathPrefix(rel, "internal/platform/"+p) {
			return true
		}
	}
	return false
}

// carrierViolation: luật (j), (k) của M3 (Task M3-04; Global Constraints "Ranh giới module"). (j) adapter hãng
// (internal/adapters/carrier/**) chỉ nhập thư viện chuẩn và carrierAllowed; (k) không module nào nhập
// internal/adapters/** (cmd/crm nối adapter vào module).
func carrierViolation(pkg, imp string) string {
	rel, internal := strings.CutPrefix(imp, modPrefix)
	switch {
	case strings.HasPrefix(pkg, "internal/modules/") && internal && hasPathPrefix(rel, "internal/adapters"):
		return "(k) module không nhập internal/adapters/**"
	case hasPathPrefix(pkg, "internal/adapters/carrier") && !isStdlib(imp) && (!internal || !carrierAllowed(rel)):
		return "(j) adapter hãng chỉ nhập phần công khai của fulfillment, platform/{logging,telemetry,config,webhooks}, thư viện chuẩn"
	}
	return ""
}

// messagingAdapterViolation: luật (n) của M4 (Task 143; kế hoạch ghi "(f)", chữ đó đã là luật của internal/queries):
// adapter kênh nhắn tin (mọi gói internal/adapters/** trừ adapter hãng carrier/** của luật (j) và cfaccess của
// identity) chỉ nhập thư viện chuẩn, thư viện ngoài, internal/platform/**, phần công khai internal/modules/channel và
// gói trong chính adapter đó. Mọi kiểu dữ liệu của Meta ở lại trong adapter; adapter không biết module nào khác.
func messagingAdapterViolation(pkg, imp string) string {
	rest, ok := strings.CutPrefix(pkg, "internal/adapters/")
	if !ok || hasPathPrefix(pkg, "internal/adapters/carrier") || hasPathPrefix(pkg, "internal/adapters/cfaccess") {
		return ""
	}
	rel, internal := strings.CutPrefix(imp, modPrefix)
	if !internal {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	if rel == "internal/modules/channel" || hasPathPrefix(rel, "internal/platform") ||
		hasPathPrefix(rel, "internal/adapters/"+name) {
		return ""
	}
	return "(n) adapter nhắn tin chỉ nhập internal/platform/**, phần công khai internal/modules/channel và gói của chính nó"
}

// wiringViolation: luật (m) của M5 (Task 185; kế hoạch ghi "(g)", chữ đó đã là luật của imager): gói nối
// internal/wiring/** chỉ nhập phần công khai của internal/modules/*, internal/platform/** và gói wiring khác; chỉ cmd/
// (và chính internal/wiring) được nhập internal/wiring/**.
func wiringViolation(pkg, imp string) string {
	rel, internal := strings.CutPrefix(imp, modPrefix)
	if !internal {
		return ""
	}
	fromWiring := hasPathPrefix(pkg, "internal/wiring")
	switch {
	case !fromWiring && !hasPathPrefix(pkg, "cmd") && hasPathPrefix(rel, "internal/wiring"):
		return "(m) chỉ cmd/ được nhập internal/wiring/**"
	case fromWiring && !wiringAllowed(rel):
		return "(m) internal/wiring chỉ nhập phần công khai của internal/modules/*, internal/platform/**, internal/wiring/**"
	}
	return ""
}

// wiringAllowed: gói nội bộ mà internal/wiring/** được nhập.
func wiringAllowed(rel string) bool {
	if hasPathPrefix(rel, "internal/platform") || hasPathPrefix(rel, "internal/wiring") {
		return true
	}
	_, isMod := moduleOf(rel)
	return isMod && !strings.Contains(strings.TrimPrefix(rel, "internal/modules/"), "/internal")
}

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
	if v := readerViolation(pkg, imp); v != "" {
		return v
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

// imagerDepViolation: luật (g) ở mức tệp chạy (Task 414, C-05 của review độc lập M1) — gói của imager không được
// liên kết, kể cả gián tiếp qua gói khác, tới database, cấu hình có bí mật, hàng đợi, pgx. Lần nhập trực tiếp đã báo
// ở boundaryViolation nên bỏ qua ở đây.
func imagerDepViolation(p listedPackage, pkg, dep string) string {
	if !hasPathPrefix(pkg, "internal/imager") && !hasPathPrefix(pkg, "cmd/imager") {
		return ""
	}
	if slices.Contains(p.Imports, dep) || !imagerForbidden(dep) {
		return ""
	}
	return fmt.Sprintf("%s phụ thuộc gián tiếp %s: (g) imager không liên kết database, cấu hình, hàng đợi, pgx", pkg, dep)
}

// checkBoundaries trả mọi vi phạm luật a–m trong dir (một module Go có đường dẫn elimaz.com.vn/crm).
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
		for _, dep := range p.Deps {
			if v := imagerDepViolation(p, pkg, dep); v != "" {
				out = append(out, v)
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestBoundariesTestdata(t *testing.T) {
	t.Parallel()
	got := checkBoundaries(t, filepath.Join("testdata", "boundaries"))
	want := []string{
		"cmd/imager nhập elimaz.com.vn/crm/internal/platform/config: (g)",
		"cmd/imager phụ thuộc gián tiếp github.com/jackc/pgx/v5: (g)",
		"internal/httpapi nhập elimaz.com.vn/crm/internal/wiring/webcache: (m)",
		"internal/imager nhập github.com/jackc/pgx/v5: (g)",
		"internal/imager/viahealth phụ thuộc gián tiếp elimaz.com.vn/crm/internal/platform/db: (g)",
		"internal/modules/catalog nhập elimaz.com.vn/crm/internal/modules/order/internal/store: (a)",
		"internal/modules/customer nhập github.com/valkey-io/valkey-go: (d)",
		"internal/modules/customer/callorder nhập elimaz.com.vn/crm/internal/modules/order: (a)",
		"internal/modules/identity nhập net/http: (b)",
		"internal/modules/inventory nhập elimaz.com.vn/crm/internal/modules/order: (a)",
		"internal/modules/modcache nhập elimaz.com.vn/crm/internal/platform/cache: (l)",
		"internal/platform/queue nhập elimaz.com.vn/crm/internal/modules/order: (c)",
		"internal/queries nhập elimaz.com.vn/crm/internal/modules/catalog: (f)",
		"internal/queries/orderview nhập elimaz.com.vn/crm/internal/modules/order: (i)",
		"internal/wiring/badwire nhập elimaz.com.vn/crm/internal/modules/order/internal/store: (m)",
		"tools/probe nhập elimaz.com.vn/crm/spikes/flashsale: (e)",
	}
	checkViolations(t, got, want)
}

func TestBoundariesNotCalledModule(t *testing.T) {
	cases := []boundaryCase{
		{"internal/modules/catalog", modPrefix + "internal/modules/order", "(a)"},
		{"internal/modules/affiliate", modPrefix + "internal/modules/order", "(a)"},
		{"internal/modules/moi", modPrefix + "internal/modules/identity", "(a)"},
		{"internal/modules/order", modPrefix + "internal/modules/catalog", ""},
		{"internal/modules/order/internal/x", modPrefix + "internal/modules/order/internal/store", ""},
		{"internal/platform/queue", modPrefix + "internal/modules/identity", ""},
		{"internal/platform/queue", modPrefix + "internal/modules/identity/internal/store", "(c)"},
		{"internal/platform/cache", "github.com/valkey-io/valkey-go", ""},
		// Task 184 (M5-01): chỉ platform, httpapi, cmd nhập lớp nối Valkey; module nghiệp vụ, kể cả phần internal, thì không.
		{"internal/modules/order", modPrefix + "internal/platform/cache", "(l)"},
		{"internal/modules/promotion/internal/flash", modPrefix + "internal/platform/cache/x", "(l)"},
		{"internal/modules/order", modPrefix + "internal/platform/cachex", ""},
		{"internal/httpapi", modPrefix + "internal/platform/cache", ""},
		{"internal/platform/observability", modPrefix + "internal/platform/cache", ""},
		{"internal/httpapi", "github.com/aws/aws-sdk-go-v2/service/s3", "(d)"},
		{"internal/modules/order", modPrefix + "internal/httpapi/gen", "(b)"},
		{"internal/modules/order", "net/http/httptest", "(b)"},
		{"internal/httpapi", "net/http", ""},
		{"internal/modules/inventory", modPrefix + "internal/modules/order", "(a)"},
		{"internal/modules/inventory", modPrefix + "internal/modules/catalog", ""},
		{"internal/modules/cash", modPrefix + "internal/modules/fulfillment", ""},
		{"internal/modules/fulfillment", modPrefix + "internal/modules/inventory", "(a)"},
		// Task M1-31: handler tín hiệu webhook và lệnh của fulfillment không gọi mạng trực tiếp; client hãng ở adapters.
		{"internal/modules/fulfillment", "net/http", "(b)"},
		{"internal/modules/fulfillment/internal/carriers/fake", "net/http", "(b)"},
		{"internal/modules/fulfillment", modPrefix + "internal/modules/order", ""},
		{"internal/adapters/carrier/fake", "net/http", ""},
		{"internal/queries", modPrefix + "internal/modules/catalog", "(f)"},
		{"internal/queries/x", modPrefix + "internal/modules/identity/internal/store", "(f)"},
		{"internal/queries", modPrefix + "internal/platform/db", ""},
		{"internal/imager", modPrefix + "internal/modules/catalog", "(g)"},
		{"internal/imager/pipe", "github.com/jackc/pgx/v5/pgxpool", "(g)"},
		{"cmd/imager", modPrefix + "internal/platform/tenancy", "(g)"},
		{"cmd/imager", modPrefix + "internal/platform/idempotency", "(g)"},
		{"cmd/imager", modPrefix + "internal/platform/queue", "(g)"},
		{"cmd/imager", modPrefix + "internal/platform/commands", "(g)"},
		{"cmd/imager", modPrefix + "internal/platform/db", "(g)"},
		{"cmd/imager", modPrefix + "internal/imager", ""},
		{"cmd/imager", modPrefix + "internal/platform/logging", ""},
		{"cmd/imagerx", "github.com/jackc/pgx/v5", ""},
		{"internal/platform/xlsx", modPrefix + "internal/modules/identity", "(h)"},
		{"internal/platform/xlsx", modPrefix + "internal/modules/catalog/internal/importer", "(h)"},
		{"internal/platform/xlsx", "github.com/xuri/excelize/v2", ""},
		{"internal/modules/catalog/internal/importer", "github.com/xuri/excelize/v2", "(h)"},
		{"internal/modules/catalog/internal/importer", modPrefix + "internal/platform/xlsx", ""},
		{"cmd/crm", "github.com/xuri/excelize/v2", "(h)"},
		{"internal/platform/xlsxstream", "github.com/xuri/excelize/v2", "(h)"},
		// Kế hoạch M2 (Task M2-02): customer không nhập order (chỉ đọc view ord); gói nhãn orderview (Q32) chỉ nhập
		// thư viện chuẩn, được order và internal/queries nhập.
		{"internal/modules/customer", modPrefix + "internal/modules/order", "(a)"},
		{"internal/modules/order", modPrefix + "internal/modules/promotion", ""},
		{"internal/modules/promotion", modPrefix + "internal/modules/customer", ""},
		{"internal/modules/promotion", modPrefix + "internal/modules/order", "(a)"},
		{"internal/queries/orderview", modPrefix + "internal/modules/order", "(i)"},
		{"internal/queries/orderview", modPrefix + "internal/platform/db", "(i)"},
		{"internal/queries/orderview", "github.com/google/uuid", "(i)"},
		{"internal/queries/orderview", "strings", ""},
		{"internal/queries/orderview/x", "slices", ""},
		{"internal/modules/order", modPrefix + "internal/queries/orderview", ""},
		{"internal/modules/order/internal/state", modPrefix + "internal/queries/orderview", ""},
		{"internal/queries", modPrefix + "internal/queries/orderview", ""},
	}
	checkCases(t, cases)
}

// Module mẫu của M3 (Task M3-04): fulfillment nhập cash (a), adapter hãng nhập order (j), module nhập adapter (k) —
// đúng ba vi phạm; gói hợp lệ (adapter nhập fulfillment, cash nhập fulfillment) không báo.
func TestBoundariesTestdataM3(t *testing.T) {
	t.Parallel()
	got := checkBoundaries(t, filepath.Join("testdata", "boundaries_m3"))
	want := []string{
		"internal/adapters/carrier/bad nhập elimaz.com.vn/crm/internal/modules/order: (j)",
		"internal/modules/fulfillment nhập elimaz.com.vn/crm/internal/modules/cash: (a)",
		"internal/modules/order nhập elimaz.com.vn/crm/internal/adapters/carrier/good: (k)",
	}
	checkViolations(t, got, want)
}

func TestBoundariesM3Rules(t *testing.T) {
	cases := []boundaryCase{
		// (a) theo bảng 04 mục 3: fulfillment chỉ gọi order; cash gọi order, fulfillment (phần công khai).
		{"internal/modules/fulfillment", modPrefix + "internal/modules/cash", "(a)"},
		{"internal/modules/fulfillment/internal/apply", modPrefix + "internal/modules/inventory", "(a)"},
		{"internal/modules/fulfillment", modPrefix + "internal/modules/customer", "(a)"},
		{"internal/modules/cash", modPrefix + "internal/modules/fulfillment/internal/store", "(a)"},
		{"internal/modules/cash/internal/ledger", modPrefix + "internal/modules/inventory", "(a)"},
		{"internal/modules/cash", modPrefix + "internal/modules/order", ""},
		// (j) adapter hãng.
		{"internal/adapters/carrier/vtp", modPrefix + "internal/modules/fulfillment", ""},
		{"internal/adapters/carrier/vtp", modPrefix + "internal/modules/fulfillment/internal/store", "(j)"},
		{"internal/adapters/carrier/vtp", modPrefix + "internal/modules/order", "(j)"},
		{"internal/adapters/carrier/vtp", modPrefix + "internal/platform/db", "(j)"},
		{"internal/adapters/carrier/vtp", modPrefix + "internal/platform/logging", ""},
		{"internal/adapters/carrier/vtp", modPrefix + "internal/platform/telemetry", ""},
		{"internal/adapters/carrier/vtp", modPrefix + "internal/platform/config", ""},
		{"internal/adapters/carrier/fake", modPrefix + "internal/platform/webhooks", ""},
		{"internal/adapters/carrier", modPrefix + "internal/adapters/carrier/vtp", ""},
		{"internal/adapters/carrier/vtp", "github.com/jackc/pgx/v5", "(j)"},
		{"internal/adapters/carrier/vtp", "net/http", ""},
		{"internal/adapters/cfaccess", modPrefix + "internal/modules/identity", ""},
		// (k) module không nhập adapter.
		{"internal/modules/fulfillment", modPrefix + "internal/adapters/carrier", "(k)"},
		{"internal/modules/order/internal/x", modPrefix + "internal/adapters/carrier/fake", "(k)"},
		{"internal/modules/identity", modPrefix + "internal/adapters/cfaccess", "(k)"},
		{"cmd/crm", modPrefix + "internal/adapters/carrier", ""},
	}
	checkCases(t, cases)
}

// Mẫu M4 (Task 143): adapter nhắn tin nhập module conversation (n), module nhập adapter facebook (k), order nhập
// conversation (a) — đúng ba vi phạm; adapter facebook nhập channel, conversation nhập channel không báo.
func TestBoundariesTestdataM4(t *testing.T) {
	t.Parallel()
	got := checkBoundaries(t, filepath.Join("testdata", "boundaries_m4"))
	want := []string{
		"internal/adapters/badchat nhập elimaz.com.vn/crm/internal/modules/conversation: (n)",
		"internal/modules/customer nhập elimaz.com.vn/crm/internal/adapters/facebook: (k)",
		"internal/modules/order nhập elimaz.com.vn/crm/internal/modules/conversation: (a)",
	}
	checkViolations(t, got, want)
}

// Luật M4 (kế hoạch Task 143; tệp task 143 Quyết định 4): (n) adapter nhắn tin chỉ nhập internal/platform/**, phần
// công khai channel, gói trong chính adapter đó và thư viện ngoài; (a) conversation gọi channel, customer, order,
// identity (Q1), channel gọi identity, order không gọi conversation.
func TestBoundariesM4Rules(t *testing.T) {
	cases := []boundaryCase{
		{"internal/adapters/facebook", modPrefix + "internal/modules/channel", ""},
		{"internal/adapters/facebook", modPrefix + "internal/platform/webhooks", ""},
		{"internal/adapters/facebook", modPrefix + "internal/platform/logging", ""},
		{"internal/adapters/facebook/graph", modPrefix + "internal/adapters/facebook", ""},
		{"internal/adapters/facebook", "github.com/google/uuid", ""},
		{"internal/adapters/facebook", "net/http", ""},
		{"internal/adapters/fake", modPrefix + "internal/modules/channel", ""},
		{"internal/adapters/facebook", modPrefix + "internal/modules/conversation", "(n)"},
		{"internal/adapters/facebook", modPrefix + "internal/modules/channel/internal/store", "(n)"},
		{"internal/adapters/facebook", modPrefix + "internal/modules/identity", "(n)"},
		{"internal/adapters/facebook", modPrefix + "internal/adapters/fake", "(n)"},
		{"internal/adapters/facebook", modPrefix + "internal/httpapi", "(n)"},
		{"internal/adapters/fake", modPrefix + "internal/testkit", "(n)"},
		// Adapter có trước giữ luật của mình: hãng (j), cfaccess nhập identity.
		{"internal/adapters/cfaccess", modPrefix + "internal/modules/identity", ""},
		{"internal/adapters/carrier/vtp", modPrefix + "internal/modules/fulfillment", ""},
		// (a) theo Q1 và bảng 04 mục 3.
		{"internal/modules/conversation", modPrefix + "internal/modules/channel", ""},
		{"internal/modules/conversation", modPrefix + "internal/modules/customer", ""},
		{"internal/modules/conversation/internal/live", modPrefix + "internal/modules/order", ""},
		{"internal/modules/conversation", modPrefix + "internal/modules/identity", ""},
		{"internal/modules/conversation", modPrefix + "internal/modules/inventory", "(a)"},
		{"internal/modules/conversation", modPrefix + "internal/modules/channel/internal/store", "(a)"},
		{"internal/modules/channel", modPrefix + "internal/modules/identity", ""},
		{"internal/modules/channel", modPrefix + "internal/modules/conversation", "(a)"},
		{"internal/modules/order", modPrefix + "internal/modules/conversation", "(a)"},
		{"internal/modules/channel", "net/http", "(b)"},
		{"internal/modules/conversation", modPrefix + "internal/adapters/facebook", "(k)"},
		{"cmd/crm", modPrefix + "internal/adapters/facebook", ""},
	}
	checkCases(t, cases)
}

// Luật (m) của M5 (Task 185): internal/wiring/** nhập phần công khai của module và internal/platform; chỉ cmd/ nhập
// internal/wiring/**.
func TestBoundariesWiringRule(t *testing.T) {
	cases := []boundaryCase{
		{"internal/wiring/webcache", modPrefix + "internal/modules/catalog", ""},
		{"internal/wiring/webcache", modPrefix + "internal/modules/order", ""},
		{"internal/wiring/webcache", modPrefix + "internal/platform/cache", ""},
		{"internal/wiring/webcache", modPrefix + "internal/platform/realtime/realtimetest", ""},
		{"internal/wiring/flashstate", modPrefix + "internal/wiring/webcache", ""},
		{"internal/wiring/webcache", modPrefix + "internal/modules/catalog/internal/store", "(m)"},
		{"internal/wiring/webcache", modPrefix + "internal/httpapi", "(m)"},
		{"internal/wiring/webcache", modPrefix + "internal/adapters/cfaccess", "(m)"},
		{"internal/wiring/webcache", modPrefix + "internal/queries", "(m)"},
		{"internal/wiring/webcache", modPrefix + "cmd/crm", "(m)"},
		{"internal/wiring/webcache", "github.com/jackc/pgx/v5", ""},
		{"cmd/crm", modPrefix + "internal/wiring/webcache", ""},
		{"cmd/crm/sub", modPrefix + "internal/wiring/flashstate", ""},
		{"internal/httpapi", modPrefix + "internal/wiring/webcache", "(m)"},
		{"internal/httpapi/web", modPrefix + "internal/wiring/webcache", "(m)"},
		{"internal/modules/order", modPrefix + "internal/wiring/webcache", "(m)"},
		{"internal/platform/queue", modPrefix + "internal/wiring", "(m)"},
		{"internal/httpapi", modPrefix + "internal/wiringx", ""},
		{"cmdx", modPrefix + "internal/wiring/webcache", "(m)"},
	}
	checkCases(t, cases)
}

func TestBoundariesRealCode(t *testing.T) {
	t.Parallel()
	if got := checkBoundaries(t, repoRoot(t)); len(got) > 0 {
		t.Fatalf("mã thật vi phạm ranh giới:\n%s", strings.Join(got, "\n"))
	}
}

// checkViolations: got có đúng len(want) vi phạm, vi phạm thứ i bắt đầu bằng want[i].
func checkViolations(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("mong đúng %d vi phạm, có %d:\n%s", len(want), len(got), strings.Join(got, "\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("vi phạm %d: mong bắt đầu %q, có %q", i, want[i], got[i])
		}
	}
}

// boundaryCase: gói pkg nhập imp thì boundaryViolation bắt đầu bằng want (rỗng: hợp lệ).
type boundaryCase struct{ pkg, imp, want string }

func checkCases(t *testing.T, cases []boundaryCase) {
	t.Helper()
	for _, c := range cases {
		got := boundaryViolation(c.pkg, c.imp)
		if (c.want == "") != (got == "") || !strings.HasPrefix(got, c.want) {
			t.Errorf("%s nhập %s: mong %q, có %q", c.pkg, c.imp, c.want, got)
		}
	}
}
