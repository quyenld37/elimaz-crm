// Package carrier gom adapter của mọi hãng vận chuyển (internal/adapters/carrier/<hãng>) để cmd/crm nối vào
// fulfillment: một chỗ duy nhất quyết định hãng nào có mặt theo cấu hình.
//
// Khi nào không dùng: module không nhập gói này (chỉ cmd/crm và test); tra adapter theo khóa dùng fulfillment.Registry.
package carrier

import (
	"net/http"

	"elimaz.com.vn/crm/internal/adapters/carrier/best"
	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/adapters/carrier/manual"
	"elimaz.com.vn/crm/internal/adapters/carrier/spx"
	"elimaz.com.vn/crm/internal/adapters/carrier/vtp"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
)

// SelfKey là khóa hãng của giao tay (shop tự giao).
const SelfKey = "self"

// All trả adapter của mọi hãng: giao tay (self), Viettel Post (chưa nối API: khung giao tay khóa "vtp" tới khi chủ
// shop OK gọi thật, Task 49), BEST, SPX; cộng hãng giả khi CRM_ENV khác production và
// có FAKECARRIER_URL (đúng điều kiện của M1). Khóa không trùng nhau (fulfillment.NewRegistry dựng được).
func All(cfg config.Config) []fulfillment.CarrierAdapter {
	as := []fulfillment.CarrierAdapter{manual.New(SelfKey), manual.New(vtp.Key), best.New(cfg), spx.New(cfg)}
	if FakeOn(cfg) {
		as = append(as, fake.NewClient(cfg.FakeCarrierURL, cfg.FakeCarrierWebhookSecret, &http.Client{}))
	}
	return as
}

// FakeOn cho biết hãng giả được nối: chỉ ngoài production và khi có FAKECARRIER_URL (Q2 của M1).
func FakeOn(cfg config.Config) bool { return cfg.Env != "production" && cfg.FakeCarrierURL != "" }
