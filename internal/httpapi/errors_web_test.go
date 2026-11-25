package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/order"
)

// Task 194: thân lỗi chi tiết của đơn web (cả khi đã bọc): quote_changed kèm báo giá CRM theo hình dạng WebQuote,
// item_unavailable kèm variantIds theo thứ tự, coupon_invalid kèm reason; too_fast kèm Retry-After làm tròn lên.
// webOrderRefusal (lưu theo khóa) cho cùng thân như writeDomainError, thêm out_of_stock, mixed_sellers; lỗi khác false.
func TestWebOrderErrors(t *testing.T) {
	t.Parallel()
	v1, v2 := uuid.New(), uuid.New()
	q := order.WebQuote{
		Lines:       []order.QuotedLine{{VariantID: v1, Qty: 1, UnitPrice: 350000, Net: 350000, Problem: "price_changed"}},
		Merchandise: 350000, ShippingFee: 30000, GrandTotal: 380000, Fingerprint: "ab", Changed: true,
	}
	type body struct {
		Error      string          `json:"error"`
		Reason     string          `json:"reason"`
		VariantIDs []string        `json:"variantIds"`
		Quote      json.RawMessage `json:"quote"`
		RequestID  string          `json:"request_id"`
	}
	cases := []struct {
		err  error
		want string
	}{
		{&order.QuoteChangedError{Quote: q}, `"grandTotal":380000`},
		{&order.ItemUnavailableError{VariantIDs: []uuid.UUID{v2, v1}}, v2.String() + `","` + v1.String()},
		{&order.CouponInvalidError{Reason: "not_found"}, `"reason":"not_found"`},
	}
	for _, c := range cases {
		rec := domainErrorRecorder(t, fmt.Errorf("tạo đơn web: %w", c.err))
		var b body
		if rec.Code != http.StatusUnprocessableEntity || json.Unmarshal(rec.Body.Bytes(), &b) != nil ||
			!json.Valid(rec.Body.Bytes()) || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%T: %d %s", c.err, rec.Code, rec.Body.String())
		}
		stored, status, ok := webOrderRefusal(c.err)
		raw, _ := json.Marshal(stored)
		if !ok || status != rec.Code || !strings.Contains(string(raw), c.want) {
			t.Errorf("%T: webOrderRefusal %v %d %s", c.err, ok, status, raw)
		}
	}
	oos := &order.OutOfStockError{VariantID: v1, Available: 0}
	if b, status, ok := webOrderRefusal(fmt.Errorf("x: %w", oos)); !ok || status != http.StatusConflict ||
		b.Error != "out_of_stock" || *b.VariantID != v1.String() || *b.Available != 0 {
		t.Errorf("out_of_stock: %v %d %+v", ok, status, b)
	}
	if b, status, ok := webOrderRefusal(order.ErrMixedSellers); !ok || status != http.StatusUnprocessableEntity || b.Error != "mixed_sellers" {
		t.Errorf("mixed_sellers: %v %d %+v", ok, status, b)
	}
	for err, code := range map[error]string{
		order.ErrShipCarrierUnavailable: "shipping_carrier_unavailable", order.ErrShipFeeMismatch: "shipping_fee_mismatch",
	} {
		if b, status, ok := webOrderRefusal(fmt.Errorf("đơn web: %w", err)); !ok || status != http.StatusUnprocessableEntity || b.Error != code {
			t.Errorf("%v: %v %d %+v", err, ok, status, b)
		}
	}
	for _, err := range []error{order.ErrInvalidInput, order.ErrTotalOverflow, order.ErrShipUnverified, nil} {
		if _, _, ok := webOrderRefusal(err); ok {
			t.Errorf("%v không phải từ chối được lưu", err)
		}
	}
	rec := domainErrorRecorder(t, fmt.Errorf("x: %w", &tooFastError{retryAfter: 1500 * time.Millisecond}))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" {
		t.Errorf("too_fast: %d %v", rec.Code, rec.Header())
	}
}
