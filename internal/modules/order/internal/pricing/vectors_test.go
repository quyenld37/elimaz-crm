package pricing_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"elimaz.com.vn/crm/internal/modules/order/internal/pricing"
)

// Bộ vector chung Go ↔ màn hình (Task M2-27 chạy cùng tệp bằng pricing.ts). Sinh lại (chỉ khi đổi thuật toán, đổi
// cả hai phía cùng lúc):
//
//	go test ./internal/modules/order/internal/pricing -run TestVectors -update-vectors
//
// Định dạng: mảng 200 phần tử {"input": vectorInput, "totals": vectorTotals}; mọi số tiền là số nguyên đồng
// ≤ 2^53 − 1 (JavaScript đọc đúng). Đầu vào sinh bằng validInputGen().Example(i), i = 0…199 (hạt giống cố định).
var updateVectors = flag.Bool("update-vectors", false, "ghi lại testdata/vectors.json") //nolint:gochecknoglobals // cờ test

const vectorCount = 200

var vectorsPath = filepath.Join("testdata", "vectors.json") //nolint:gochecknoglobals // đường dẫn cố định của test

type vectorLine struct {
	Key          int   `json:"key"`
	Qty          int64 `json:"qty"`
	UnitPrice    int64 `json:"unitPrice"`
	LineDiscount int64 `json:"lineDiscount"`
}

type vectorInput struct {
	Lines          []vectorLine `json:"lines"`
	OrderDiscount  int64        `json:"orderDiscount"`
	CouponDiscount int64        `json:"couponDiscount"`
	ShippingFee    int64        `json:"shippingFee"`
}

type vectorLineTotals struct {
	Key       int   `json:"key"`
	Gross     int64 `json:"gross"`
	Base      int64 `json:"base"`
	Allocated int64 `json:"allocated"`
	Net       int64 `json:"net"`
}

type vectorTotals struct {
	Subtotal          int64              `json:"subtotal"`
	LineDiscountTotal int64              `json:"lineDiscountTotal"`
	OrderDiscount     int64              `json:"orderDiscount"`
	CouponDiscount    int64              `json:"couponDiscount"`
	DiscountTotal     int64              `json:"discountTotal"`
	ShippingFee       int64              `json:"shippingFee"`
	GrandTotal        int64              `json:"grandTotal"`
	Lines             []vectorLineTotals `json:"lines"`
}

type vector struct {
	Input  vectorInput  `json:"input"`
	Totals vectorTotals `json:"totals"`
}

func (v vectorInput) toInput() pricing.Input {
	in := pricing.Input{OrderDiscount: v.OrderDiscount, CouponDiscount: v.CouponDiscount, ShippingFee: v.ShippingFee}
	for _, l := range v.Lines {
		in.Lines = append(in.Lines, pricing.Line(l))
	}
	return in
}

func fromInput(in pricing.Input) vectorInput {
	v := vectorInput{
		Lines:         make([]vectorLine, 0, len(in.Lines)),
		OrderDiscount: in.OrderDiscount, CouponDiscount: in.CouponDiscount, ShippingFee: in.ShippingFee,
	}
	for _, l := range in.Lines {
		v.Lines = append(v.Lines, vectorLine(l))
	}
	return v
}

func fromTotals(t pricing.Totals) vectorTotals {
	v := vectorTotals{
		Subtotal: t.Subtotal, LineDiscountTotal: t.LineDiscountTotal, OrderDiscount: t.OrderDiscount,
		CouponDiscount: t.CouponDiscount, DiscountTotal: t.DiscountTotal, ShippingFee: t.ShippingFee,
		GrandTotal: t.GrandTotal, Lines: make([]vectorLineTotals, 0, len(t.Lines)),
	}
	for _, l := range t.Lines {
		v.Lines = append(v.Lines, vectorLineTotals(l))
	}
	return v
}

func writeVectors(t *testing.T) {
	t.Helper()
	gen := validInputGen()
	vs := make([]vector, vectorCount)
	for i := range vs {
		in := gen.Example(i)
		vs[i] = vector{Input: fromInput(in), Totals: fromTotals(mustCompute(t, in))}
	}
	b, err := json.MarshalIndent(vs, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vectorsPath, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestVectors: mỗi bộ trong testdata/vectors.json cho đúng totals đã lưu.
func TestVectors(t *testing.T) {
	if *updateVectors {
		writeVectors(t)
	}
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var vs []vector
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&vs); err != nil {
		t.Fatal(err)
	}
	if len(vs) != vectorCount {
		t.Fatalf("có %d bộ, muốn %d", len(vs), vectorCount)
	}
	for i, v := range vs {
		got, err := pricing.Compute(v.Input.toInput())
		if err != nil {
			t.Fatalf("bộ %d: %v", i, err)
		}
		want, _ := json.Marshal(v.Totals)
		have, _ := json.Marshal(fromTotals(got))
		if !bytes.Equal(want, have) {
			t.Errorf("bộ %d:\n có  %s\n muốn %s", i, have, want)
		}
	}
}
