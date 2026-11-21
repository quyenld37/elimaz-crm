package manual_test

import (
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/carrier/carriertest"
	"elimaz.com.vn/crm/internal/adapters/carrier/manual"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Adapter giao tay giữ đúng khóa được cho (self, hay khóa của hãng có tài khoản tay), khai mọi khả năng false, mọi lời
// gọi hãng trả ErrNotSupported (lõi không bao giờ gọi nó cho tài khoản tay; nếu có ai gọi nhầm thì không có gì gửi đi)
// và qua bộ contract chung của adapter hãng.
func TestAdapter(t *testing.T) {
	for _, key := range []string{fulfillment.SelfCarrierKey, "best"} {
		t.Run(key, func(t *testing.T) {
			a := manual.New(key)
			if a.Key() != key {
				t.Fatalf("Key = %q, muốn %q", a.Key(), key)
			}
			if a.Capabilities() != (fulfillment.Capabilities{}) {
				t.Fatalf("khả năng = %+v, muốn mọi khả năng false", a.Capabilities())
			}
			if _, err := a.CreateShipment(t.Context(), fulfillment.Account{}, fulfillment.CreateRequest{Code: "SH1"}); !errors.Is(err, fulfillment.ErrNotSupported) {
				t.Fatalf("CreateShipment = %v, muốn ErrNotSupported", err)
			}
			if _, err := a.Track(t.Context(), fulfillment.Account{}, "BEST1", "SH1"); !errors.Is(err, fulfillment.ErrNotSupported) {
				t.Fatalf("Track = %v, muốn ErrNotSupported", err)
			}
			if _, err := a.Cancel(t.Context(), fulfillment.Account{}, "BEST1"); !errors.Is(err, fulfillment.ErrNotSupported) {
				t.Fatalf("Cancel = %v, muốn ErrNotSupported", err)
			}
			carriertest.Run(t, carriertest.Factory{Adapter: a})
		})
	}
}
