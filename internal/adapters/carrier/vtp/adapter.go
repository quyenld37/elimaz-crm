// Package vtp là adapter hãng Viettel Post (khóa "vtp") theo tài liệu công khai partner2.viettelpost.vn (ghi ở
// docs/carriers/vtp.md): đăng nhập lấy mã truy cập (giữ trong bộ nhớ theo tài khoản, lấy lại một lần khi hết hạn),
// địa giới mới V3, báo phí (getPriceAll, getPrice), tạo vận đơn (createOrder, mã vận đơn CRM ở ORDER_NUMBER, bật
// CHECK_UNIQUE), đổi mã trạng thái (Task 125); hủy và phát tiếp (UpdateOrder), nhãn (printing-code + máy in
// DigitalizePrint), kiểm và tách webhook (Task 126). Không có: tra theo mã, hành trình, sửa địa chỉ, bảng kê (chờ tệp
// mẫu của VTP) — trả ErrNotSupported.
//
// Khi nào không dùng: không gọi từ module (cmd/crm nối qua internal/adapters/carrier); chưa nối vào carrier.All tới
// khi chủ shop OK gọi thật (Task 49); test dùng máy chủ giả vtp/vtptest, không bao giờ gọi hãng thật.
package vtp

import (
	"cmp"
	"net/http"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Key là carrier_key của Viettel Post.
const Key = "vtp"

// Base URL theo môi trường của tài khoản (trang "Môi trường và tham số chung") và máy in nhãn theo môi trường (trang
// "Lấy link in vận đơn": dev-release-print cho Development, digitalize cho Production).
const (
	sandboxURL         = "https://partnerdev.viettelpost.vn"
	productionURL      = "https://partner.viettelpost.vn"
	sandboxPrintURL    = "https://dev-release-print.viettelpost.vn"
	productionPrintURL = "https://digitalize.viettelpost.vn"
)

// Config là cấu hình adapter. BaseURL rỗng → chọn theo Account.Environment (sandbox → partnerdev, production →
// partner, docs/carriers/vtp.md); khác rỗng thì dùng cho mọi tài khoản (test, máy chủ giả). HTTPClient nil → client
// mặc định (hạn giờ theo từng việc qua ctx). Now nil → time.Now (hạn của mã truy cập, hạn mã in). PrintURL là máy in
// nhãn: rỗng → theo môi trường, hoặc BaseURL khi BaseURL khác rỗng (test không bao giờ chạm máy in thật).
type Config struct {
	BaseURL    string
	PrintURL   string
	HTTPClient *http.Client
	Now        func() time.Time
}

// Adapter là adapter Viettel Post; an toàn khi nhiều worker dùng chung.
type Adapter struct {
	fulfillment.Unsupported

	base   string
	print  string
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
	return &Adapter{base: cfg.BaseURL, print: cmp.Or(cfg.PrintURL, cfg.BaseURL), hc: hc, now: now, tokens: newTokenCache()}
}

// Key trả "vtp".
func (*Adapter) Key() string { return Key }

// Capabilities theo tài liệu (docs/carriers/vtp.md): báo phí, tạo vận đơn, địa giới, giá trị hàng (PRODUCT_PRICE);
// hủy, phát tiếp (UpdateOrder), nhãn nhiều mã một lần (printing-code, tối đa 100), webhook (token trong header).
// Không có: tra theo mã của shop, hành trình (không có API đọc), sửa địa chỉ (API edit đòi gửi lại cả đơn), bảng kê
// (chờ tệp mẫu của VTP); cho xem hàng, phí khi khách không nhận chưa có mã dịch vụ cộng thêm; hủy lại đơn đã hủy chưa
// kiểm chứng (CancelIdempotent false).
func (*Adapter) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{
		Quote: true, Create: true, Locations: true, DeclaredValue: true,
		Cancel: true, Webhook: true, Labels: true, LabelsBatch: true, Redeliver: true,
	}
}
