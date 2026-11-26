package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Phí cố định (Task 511, FAKECARRIER_QUOTE_FEE cho e2e website): một dịch vụ std đúng QuoteFee bất kể cân nặng,
// COD, giá trị, thử hàng; hỏi dịch vụ khác → không có báo phí; QuoteFee 0 → công thức cũ (hai dịch vụ).
func TestQuoteFixedFee(t *testing.T) {
	t.Parallel()
	cli := func(fee int64) *fake.Client {
		srv := httptest.NewServer(New(Options{Secret: secret, QuoteFee: fee, Logger: slog.New(slog.DiscardHandler)}))
		t.Cleanup(srv.Close)
		return fake.NewClient(srv.URL, secret, &http.Client{})
	}
	in := func(grams int, cod int64, svc string) fulfillment.QuoteInput {
		return fulfillment.QuoteInput{
			To: fulfillment.Address{ProvinceCode: "T01", WardCode: "T0101"}, Parcel: fulfillment.Parcel{WeightGrams: grams, DeclaredValue: cod},
			COD: cod, Service: svc, Options: fulfillment.Options{Inspection: "try"},
		}
	}
	fixed := cli(30000)
	for _, c := range []fulfillment.QuoteInput{in(300, 499000, ""), in(4200, 2500000, ""), in(300, 0, "std")} {
		qs, err := fixed.Quote(t.Context(), account(), c)
		if err != nil || len(qs) != 1 || qs[0].Service != "std" || qs[0].Total != 30000 || qs[0].Breakdown["main"] != 30000 {
			t.Fatalf("phí cố định với %+v = %+v %v", c, qs, err)
		}
	}
	if qs, err := fixed.Quote(t.Context(), account(), in(300, 0, "fast")); err != nil || len(qs) != 0 {
		t.Fatalf("phí cố định, hỏi fast = %+v %v, muốn rỗng", qs, err)
	}
	qs, err := cli(0).Quote(t.Context(), account(), in(300, 0, ""))
	if err != nil || len(qs) != 2 || qs[0].Total != quoteBase+quoteTryFee {
		t.Fatalf("công thức cũ = %+v %v", qs, err)
	}
}
