package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ogen-go/ogen"
)

// moneyNameRe: thuộc tính mang tiền theo tên (kế hoạch M2, Task M2-02): kết thúc bằng Amount, Total, Price, Discount,
// Fee, Spent, Base, Subtotal (chữ đầu viết hoa: grandTotal, unitPrice, minSubtotal).
var moneyNameRe = regexp.MustCompile(`(Amount|Total|Price|Discount|Fee|Spent|Base|Subtotal)$`)

const maxMoneyLiteral = "9007199254740991"

// moneyChecker đi qua mọi schema của hợp đồng (components và nội tuyến trong operation) và ghi các thuộc tính tiền
// không phải số nguyên int64 có maximum 2^53−1 (trực tiếp hoặc qua $ref tới schema như Money).
type moneyChecker struct {
	components map[string]*ogen.Schema
	seen       map[*ogen.Schema]bool
	out        []string
}

func (c *moneyChecker) resolve(s *ogen.Schema) *ogen.Schema {
	for i := 0; s != nil && s.Ref != "" && i < 8; i++ {
		s = c.components[strings.TrimPrefix(s.Ref, "#/components/schemas/")]
	}
	return s
}

func isMoneySchema(s *ogen.Schema) bool {
	return s != nil && s.Type == "integer" && s.Format == "int64" && strings.TrimSpace(string(s.Maximum)) == maxMoneyLiteral
}

func (c *moneyChecker) walk(where string, s *ogen.Schema) {
	if s == nil || c.seen[s] {
		return
	}
	c.seen[s] = true
	if s.Ref != "" {
		c.walk(where, c.resolve(s))
		return
	}
	for _, p := range s.Properties {
		if moneyNameRe.MatchString(p.Name) && !isMoneySchema(c.resolve(p.Schema)) {
			c.out = append(c.out, where+"."+p.Name+": trường tiền phải $ref Money hoặc integer/int64/maximum "+maxMoneyLiteral)
		}
		c.walk(where+"."+p.Name, p.Schema)
	}
	if s.Items != nil {
		c.walk(where+"[]", s.Items.Item)
		for _, it := range s.Items.Items {
			c.walk(where+"[]", it)
		}
	}
	for _, group := range [][]*ogen.Schema{s.AllOf, s.OneOf, s.AnyOf} {
		for _, sub := range group {
			c.walk(where, sub)
		}
	}
}

// walkOperation đi qua thân yêu cầu và mọi câu trả lời của op (nil: bỏ qua).
func (c *moneyChecker) walkOperation(path string, op *ogen.Operation) {
	if op == nil {
		return
	}
	if op.RequestBody != nil {
		for _, m := range op.RequestBody.Content {
			c.walk(op.OperationID+" "+path+" body", m.Schema)
		}
	}
	for code, r := range op.Responses {
		if r == nil {
			continue
		}
		for _, m := range r.Content {
			c.walk(op.OperationID+" "+path+" "+code, m.Schema)
		}
	}
}

// moneyFieldProblems đọc hợp đồng ở path và trả mọi trường tiền sai kiểu (tên đầy đủ theo đường đi).
func moneyFieldProblems(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ogen.Parse(data)
	if err != nil {
		t.Fatalf("đọc %s: %v", path, err)
	}
	c := &moneyChecker{components: map[string]*ogen.Schema{}, seen: map[*ogen.Schema]bool{}}
	if spec.Components != nil {
		c.components = spec.Components.Schemas
	}
	names := make([]string, 0, len(c.components))
	for n := range c.components {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		c.walk(n, c.components[n])
	}
	for p, item := range spec.Paths {
		for _, op := range []*ogen.Operation{item.Get, item.Put, item.Post, item.Delete, item.Patch} {
			c.walkOperation(p, op)
		}
	}
	slices.Sort(c.out)
	return c.out
}

func TestMoneyFieldsSample(t *testing.T) {
	got := moneyFieldProblems(t, filepath.Join("testdata", "moneyfields", "openapi.yaml"))
	want := []string{"grandTotal", "items[].lineDiscount", "refundAmount"}
	if len(got) != len(want) {
		t.Fatalf("mong %d trường sai, có %d:\n%s", len(want), len(got), strings.Join(got, "\n"))
	}
	for _, w := range want {
		if !slices.ContainsFunc(got, func(g string) bool { return strings.Contains(g, "."+w+":") }) {
			t.Errorf("không báo %s: %q", w, got)
		}
	}
	for _, ok := range []string{"subtotal", "shippingFee", "couponDiscount", "codAmount", "paidTotal"} {
		if slices.ContainsFunc(got, func(g string) bool { return strings.Contains(g, "."+ok+":") }) {
			t.Errorf("trường đúng luật %s bị báo: %q", ok, got)
		}
	}
}

func TestMoneyFieldsContract(t *testing.T) {
	t.Parallel()
	if got := moneyFieldProblems(t, filepath.Join(repoRoot(t), "openapi", "openapi.yaml")); len(got) != 0 {
		t.Fatalf("trường tiền sai kiểu trong openapi.yaml:\n%s", strings.Join(got, "\n"))
	}
}
