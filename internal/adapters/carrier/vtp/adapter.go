// Package vtp là adapter hãng Viettel Post (khóa "vtp") theo tài liệu công khai partner2.viettelpost.vn (ghi ở
// docs/carriers/vtp.md): đăng nhập lấy mã truy cập (giữ trong bộ nhớ theo tài khoản, lấy lại một lần khi hết hạn),
// địa giới mới V3, báo phí (getPriceAll, getPrice), tạo vận đơn (createOrder, mã vận đơn CRM ở ORDER_NUMBER, bật
// CHECK_UNIQUE), đổi mã trạng thái. Hủy, webhook, nhãn, bảng kê ở Task 126; khả năng chưa có trả ErrNotSupported.
//
// Khi nào không dùng: không gọi từ module (cmd/crm nối qua internal/adapters/carrier); chưa nối vào carrier.All tới
// khi chủ shop OK gọi thật (Task 49); test dùng máy chủ giả vtp/vtptest, không bao giờ gọi hãng thật.
package vtp

import (
	"net/http"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Key là carrier_key của Viettel Post.
const Key = "vtp"

// Base URL theo môi trường của tài khoản (trang "Môi trường và tham số chung").
const (
	sandboxURL    = "https://partnerdev.viettelpost.vn"
	productionURL = "https://partner.viettelpost.vn"
)

// Config là cấu hình adapter. BaseURL rỗng → chọn theo Account.Environment (sandbox → partnerdev, production →
// partner, docs/carriers/vtp.md); khác rỗng thì dùng cho mọi tài khoản (test, máy chủ giả). HTTPClient nil → client
// mặc định (hạn giờ theo từng việc qua ctx). Now nil → time.Now (hạn của mã truy cập).
type Config struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

// Adapter là adapter Viettel Post; an toàn khi nhiều worker dùng chung.
type Adapter struct {
	fulfillment.Unsupported

	base   string
	hc     *http.Client
	now    func() time.Time
	tokens *tokenCache
}

// New dựng adapter từ cfg.
func New(cfg Config) *Adapter {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Adapter{base: cfg.BaseURL, hc: hc, now: now, tokens: newTokenCache()}
}

// Key trả "vtp".
func (*Adapter) Key() string { return Key }

// Capabilities theo tài liệu (docs/carriers/vtp.md): báo phí, tạo vận đơn, địa giới, giá trị hàng (PRODUCT_PRICE).
// Không có tra theo mã của shop, hành trình; cho xem hàng, phí khi khách không nhận chưa có mã dịch vụ cộng thêm.
func (*Adapter) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Quote: true, Create: true, Locations: true, DeclaredValue: true}
}
