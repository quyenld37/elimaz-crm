package fulfillment

import (
	"errors"
	"fmt"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
)

// ErrNotSupported là lỗi khi adapter không có khả năng này (Capabilities khai false). Không gửi lại.
var ErrNotSupported = errors.New("carrier: not supported")

// Header là header HTTP của webhook (cùng kiểu nền với http.Header: bên gọi truyền r.Header thẳng vào). Module không
// nhập net/http (luật kiến trúc (b)); adapter đọc bằng http.Header(h).Get.
type Header = map[string][]string

// Capabilities là những việc adapter làm được (04 mục 4). Khai false thì phương thức tương ứng trả ErrNotSupported;
// Inspection, DeclaredValue, FeeOnRefuse là tùy chọn của Quote và CreateShipment; Statement khai true khi adapter cài
// StatementFormatProvider.
type Capabilities struct {
	Quote, Create, LookupByRef, Cancel, CancelIdempotent, Track, Webhook, Labels, LabelsBatch, UpdateAddress, Redeliver,
	Locations, Statement, Inspection, DeclaredValue, FeeOnRefuse bool
}

// Address là địa chỉ theo mã địa giới của CRM; adapter tự đổi sang mã hãng qua LocationMap.
type Address struct {
	Name         string
	Phone        logging.Phone
	Line         string
	ProvinceCode string
	WardCode     string
}

// LocationMap đổi mã địa giới của CRM (level: province, ward) sang mã của hãng.
type LocationMap interface {
	Carrier(level, ourCode string) (string, bool)
}

// Parcel là kích thước, khối lượng, giá trị khai của kiện.
type Parcel struct {
	WeightGrams, LengthCm, WidthCm, HeightCm int
	DeclaredValue                            int64
	ItemCount                                int
}

// Options là tùy chọn giao: Inspection none | view | try; FeeOnRefuse là phí thu khi khách không nhận; Payer shop |
// receiver; Pickup là hãng tới lấy hàng.
type Options struct {
	Inspection  string
	FeeOnRefuse int64
	Payer       string
	Pickup      bool
	Note        string
}

// QuoteInput là dữ liệu hỏi phí.
type QuoteInput struct {
	From, To Address
	Parcel   Parcel
	COD      int64
	Service  string
	Options  Options
}

// QuoteResult là một báo phí của một dịch vụ.
type QuoteResult struct {
	Service, ServiceName string
	Total                int64
	Breakdown            map[string]int64
	ExpectedDays         int
	ValidUntil           *time.Time
}

// CarrierEvent là một sự kiện hành trình đã chuẩn hóa; không có trường nào chứa tên, SĐT, địa chỉ.
type CarrierEvent struct {
	Key, RawStatus    string
	Canonical         Canonical
	SubStatus, Reason string
	OccurredAt        time.Time
}

// Tracking là hành trình của vận đơn; CODCollected, FeeActual nil khi hãng chưa báo.
type Tracking struct {
	TrackingCode            string
	Events                  []CarrierEvent
	CODCollected, FeeActual *int64
}

// CancelResult là kết quả hủy ở hãng.
type CancelResult string

// Kết quả hủy.
const (
	CancelDone             CancelResult = "done"
	CancelRejectedPickedUp CancelResult = "rejected_picked_up"
	CancelPending          CancelResult = "pending"
)

// UpdateResult là kết quả sửa địa chỉ, giao lại.
type UpdateResult string

// Kết quả sửa.
const (
	UpdateDone     UpdateResult = "done"
	UpdateRejected UpdateResult = "rejected"
	UpdatePending  UpdateResult = "pending"
)

// Label là nhãn in (PDF).
type Label struct {
	PDF   []byte
	Pages int
}

// Location là một đơn vị địa giới của hãng.
type Location struct {
	Level, CarrierCode, Name, ParentCarrierCode string
}

// Health là kết quả kiểm kết nối tài khoản hãng.
type Health struct {
	OK     bool
	Detail string
}

// WebhookSignal là một tín hiệu tách từ thân webhook: chỉ khóa để tra lại qua API (B10), không tin nội dung.
type WebhookSignal struct {
	EventKey, TrackingCode, ShipmentCode string
}

// ErrorClass là lớp lỗi khi gọi hãng, quyết định lệnh gửi lại, chờ, hay ra in_doubt.
type ErrorClass int

// Lớp lỗi. ClassNotSentRetry: chắc chắn hãng chưa nhận, gửi lại được; ClassRateLimited: hãng bảo chờ RetryAfter rồi
// gửi lại; ClassAuth: khóa sai hay bị thu hồi, cần người sửa; ClassRejected: hãng từ chối dữ liệu; ClassPermanent: lỗi
// không sửa được bằng gửi lại; ClassUnknownOutcome: không rõ hãng đã làm chưa (in_doubt, không tự gửi lại).
const (
	ClassUnknownOutcome ErrorClass = iota
	ClassNotSentRetry
	ClassRateLimited
	ClassAuth
	ClassRejected
	ClassPermanent
)

// String trả tên lớp lỗi.
func (c ErrorClass) String() string {
	switch c {
	case ClassNotSentRetry:
		return "not_sent_retry"
	case ClassRateLimited:
		return "rate_limited"
	case ClassAuth:
		return "auth"
	case ClassRejected:
		return "rejected"
	case ClassPermanent:
		return "permanent"
	case ClassUnknownOutcome:
	}
	return "unknown_outcome"
}

// CarrierError là lỗi gọi hãng đã phân lớp. Code là mã lỗi của hãng (không chứa dữ liệu cá nhân); Err thường là
// ErrNotSent, ErrRejected của M1 để errors.Is cũ vẫn đúng.
type CarrierError struct {
	Class      ErrorClass
	Code       string
	RetryAfter time.Duration
	Err        error
}

// Error mô tả lỗi: lớp, mã hãng, lỗi bọc.
func (e *CarrierError) Error() string {
	s := "carrier " + e.Class.String()
	if e.Code != "" {
		s += " " + e.Code
	}
	if e.RetryAfter > 0 {
		s += fmt.Sprintf(" (chờ %s)", e.RetryAfter)
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

// Unwrap trả lỗi bọc.
func (e *CarrierError) Unwrap() error { return e.Err }

// StatementField là một cột của bảng kê đối soát COD.
type StatementField string

// Cột bảng kê.
const (
	FieldTrackingCode StatementField = "tracking_code"
	FieldCOD          StatementField = "cod"
	FieldFee          StatementField = "fee"
	FieldOtherFee     StatementField = "other_fee"
	FieldNet          StatementField = "net"
	FieldPaidOn       StatementField = "paid_on"
)

// StatementFormat là định dạng tệp bảng kê của một hãng: Columns là tên cột chấp nhận (đã bỏ dấu, chữ thường).
type StatementFormat struct {
	Key, CarrierKey    string
	Version, HeaderRow int
	Columns            map[StatementField][]string
	Required           []StatementField
	DateLayouts        []string
}

// StatementFormatProvider là interface tùy chọn của adapter có bảng kê (Task 31 dùng qua Registry.StatementFormats).
type StatementFormatProvider interface {
	StatementFormats() []StatementFormat
}
