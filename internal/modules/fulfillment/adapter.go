package fulfillment

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/platform/logging"
)

// Lỗi của CarrierAdapter.CreateShipment (04 mục 4). Lỗi khác (quá giờ, mất kết nối sau khi gửi) = không rõ đã tạo
// chưa: lệnh thành in_doubt, không gửi lại.
var (
	// ErrRejected: hãng từ chối (dữ liệu sai, tài khoản khóa); không gửi lại.
	ErrRejected = errors.New("fulfillment: hãng từ chối")
	// ErrNotSent: chắc chắn hãng chưa nhận (lỗi trước khi gửi, hãng báo chưa tạo); được gửi lại.
	ErrNotSent = errors.New("fulfillment: chưa gửi tới hãng")
)

// Account là tài khoản hãng mà adapter dùng: mã trong CRM, mã tài khoản ở hãng (ExternalAccountRef). M3 thêm khóa hãng,
// môi trường (sandbox | production), khóa tuyến webhook, bộ khóa đã giải mã, địa chỉ lấy hàng, bảng đổi mã địa giới.
type Account struct {
	ID                 uuid.UUID
	ExternalAccountRef string
	CarrierKey         string
	Environment        string
	RouteKey           string
	Creds              Credentials
	Pickup             Address
	Locations          LocationMap
}

// LogValue chỉ ghi mã tài khoản, khóa hãng, môi trường: bộ khóa (Creds) và địa chỉ lấy hàng (tên, SĐT, địa chỉ)
// không bao giờ vào log; slog JSON không gọi String của trường lồng nên Account phải tự che.
func (a Account) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", a.ID.String()), slog.String("carrier_key", a.CarrierKey),
		slog.String("external_account_ref", a.ExternalAccountRef), slog.String("environment", a.Environment))
}

// CreateRequest là yêu cầu tạo vận đơn ở hãng; Code là mã vận đơn của CRM (khóa tra lại khi in_doubt). ReceiverName,
// ReceiverPhone, Address của M1 được điền từ To; M3 thêm nơi gửi, nơi nhận theo mã địa giới, kiện, dịch vụ, tùy chọn.
type CreateRequest struct {
	Code          string
	CODAmount     int64
	ReceiverName  string
	ReceiverPhone logging.Phone
	Address       string
	From, To      Address
	Parcel        Parcel
	Service       string
	Options       Options
}

// CreateResult là mã hãng và phí hãng báo khi tạo; FeeKnown = false khi hãng không báo phí lúc tạo (Fee là 0).
type CreateResult struct {
	TrackingCode string
	Fee          int64
	FeeKnown     bool
}

// LookupResult là kết quả tra vận đơn theo mã của CRM. Inconclusive = true: hãng không trả lời chắc chắn (không được
// coi là "không có"); giá trị rỗng giữ nghĩa của M1 (chắc chắn không có).
type LookupResult struct {
	Found                   bool
	TrackingCode, RawStatus string
	Inconclusive            bool
}

// StatusResult là trạng thái thật của vận đơn đọc qua API có xác thực (B10); EventKey chống trùng sự kiện.
type StatusResult struct {
	RawStatus  string
	OccurredAt time.Time
	EventKey   string
}

// CarrierAdapter là adapter hãng vận chuyển (04 mục 4): năm phương thức đầu của M1 giữ nguyên chữ ký, M3 thêm phần
// còn lại (không tạo interface thứ hai). Bản hiện thực nằm ở internal/adapters/carrier/<hãng>; khả năng khai false
// trong Capabilities thì phương thức tương ứng trả ErrNotSupported. Lỗi gọi hãng là *CarrierError (bọc ErrNotSent,
// ErrRejected của M1); bộ contract test ở internal/adapters/carrier/carriertest.
type CarrierAdapter interface { //nolint:interfacebloat // 04 mục 4, kế hoạch M3 Task 3: một interface cho mọi hãng, M1 mở rộng chứ không tách
	Key() string
	// CreateShipment: ErrRejected, ErrNotSent, lỗi khác = không rõ.
	CreateShipment(ctx context.Context, a Account, r CreateRequest) (CreateResult, error)
	LookupByCode(ctx context.Context, a Account, code string) (LookupResult, error)
	GetStatus(ctx context.Context, a Account, trackingCode string) (StatusResult, error)
	MapStatus(raw string) (Status, bool)

	Capabilities() Capabilities
	// MapCanonical: mã lạ → (CanonUnknown, false).
	MapCanonical(raw string) (Canonical, bool)
	// Vào: chạy ở ingest, không database.
	VerifyWebhook(raw []byte, h Header, secret string) bool
	SplitWebhook(raw []byte) ([]WebhookSignal, error)
	// Ra: chạy trong Execute của lệnh, ngoài transaction.
	CheckConnection(ctx context.Context, acc Account) (Health, error)
	Quote(ctx context.Context, acc Account, in QuoteInput) ([]QuoteResult, error)
	Cancel(ctx context.Context, acc Account, trackingCode string) (CancelResult, error)
	Track(ctx context.Context, acc Account, trackingCode, shipmentCode string) (Tracking, error)
	Labels(ctx context.Context, acc Account, trackingCodes []string) (Label, error)
	UpdateAddress(ctx context.Context, acc Account, trackingCode string, to Address) (UpdateResult, error)
	Redeliver(ctx context.Context, acc Account, trackingCode, note string) (UpdateResult, error)
	Locations(ctx context.Context, acc Account, level, parentCarrierCode string) ([]Location, error)
}
