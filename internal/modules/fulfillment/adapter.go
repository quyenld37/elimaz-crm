package fulfillment

import (
	"context"
	"errors"
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

// Account là tài khoản hãng mà adapter dùng: mã trong CRM và mã tài khoản ở hãng.
type Account struct {
	ID                 uuid.UUID
	ExternalAccountRef string
}

// CreateRequest là yêu cầu tạo vận đơn ở hãng; Code là mã vận đơn của CRM (khóa tra lại khi in_doubt).
type CreateRequest struct {
	Code          string
	CODAmount     int64
	ReceiverName  string
	ReceiverPhone logging.Phone
	Address       string
}

// CreateResult là mã hãng và phí hãng báo khi tạo.
type CreateResult struct {
	TrackingCode string
	Fee          int64
}

// LookupResult là kết quả tra vận đơn theo mã của CRM.
type LookupResult struct {
	Found                   bool
	TrackingCode, RawStatus string
}

// StatusResult là trạng thái thật của vận đơn đọc qua API có xác thực (B10); EventKey chống trùng sự kiện.
type StatusResult struct {
	RawStatus  string
	OccurredAt time.Time
	EventKey   string
}

// CarrierAdapter là phần dùng ở M1 của adapter hãng vận chuyển (04 mục 4). M3 mở rộng interface này (thêm phương
// thức), không tạo interface thứ hai. Bản hiện thực gọi mạng nằm ở internal/adapters/<hãng>.
type CarrierAdapter interface {
	Key() string
	// CreateShipment: ErrRejected, ErrNotSent, lỗi khác = không rõ.
	CreateShipment(ctx context.Context, a Account, r CreateRequest) (CreateResult, error)
	LookupByCode(ctx context.Context, a Account, code string) (LookupResult, error)
	GetStatus(ctx context.Context, a Account, trackingCode string) (StatusResult, error)
	MapStatus(raw string) (Status, bool)
}
