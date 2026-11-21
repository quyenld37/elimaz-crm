package fulfillment

import "errors"

// Lỗi nghiệp vụ của fulfillment. Lớp HTTP đổi sang mã ở internal/httpapi/errors.go. Mọi lỗi: bên gọi lùi transaction.
var (
	// ErrShipmentExists: đơn đã có vận đơn chưa hủy (unique một phần, B1).
	ErrShipmentExists = errors.New("fulfillment: đơn đã có vận đơn")
	// ErrAccountNotFound: không có tài khoản hãng (hay tenant khác), hoặc tài khoản đã tắt.
	ErrAccountNotFound = errors.New("fulfillment: không có tài khoản hãng")
	// ErrNotFound: không có vận đơn (hay tenant khác).
	ErrNotFound = errors.New("fulfillment: không có vận đơn")
	// ErrOrderNotShippable: đơn không có, của người bán khác tài khoản hãng, giữ hàng ở kho khác kho của tài khoản hãng,
	// hoặc không ở trạng thái giao được.
	ErrOrderNotShippable = errors.New("fulfillment: đơn không giao được")
	// errCodeExhausted: sinh mã vận đơn 3 lần đều trùng.
	errCodeExhausted = errors.New("fulfillment: sinh mã vận đơn trùng 3 lần")
)

// Lỗi của gửi hãng (kế hoạch M3, Task M3-12).
var (
	// ErrOrderNotFound: không có đơn (hay tenant khác), hoặc người gọi không có fulfillment.ship ở kho của đơn.
	ErrOrderNotFound = errors.New("fulfillment: không có đơn")
	// ErrQuoteStale: báo phí đã chọn không còn dùng được (không ok, hết hạn, của đơn hay tài khoản khác, hoặc địa chỉ,
	// cân nặng, COD, dịch vụ, tùy chọn đã đổi sau khi báo phí).
	ErrQuoteStale = errors.New("fulfillment: báo phí không còn khớp")
	// ErrAddressUnmapped: địa chỉ giao chưa khớp mã địa giới của hãng (carrier_locations), chưa gọi hãng.
	ErrAddressUnmapped = errors.New("fulfillment: địa chỉ chưa khớp mã địa giới của hãng")
	// ErrCreateInProgress: vận đơn không ở trạng thái cho "Gửi lại" (creating, chắc chưa gửi) hay cho xử lý "không rõ"
	// (creating, unknown): lần gửi đang chạy, đã có kết quả, hay vận đơn đã qua creating.
	ErrCreateInProgress = errors.New("fulfillment: lệnh tạo vận đơn đang chạy hoặc đã có kết quả")
	// ErrShipInvalid: dữ liệu gửi hãng sai (mã hãng, phí nhập tay cho tài khoản api; kết quả xử lý "không rõ" thiếu mã
	// hãng hay lý do).
	ErrShipInvalid = errors.New("fulfillment: dữ liệu gửi hãng không hợp lệ")
	// ErrTrackingCodeConflict: mã hãng nhân viên nhập đã thuộc vận đơn khác cùng hãng.
	ErrTrackingCodeConflict = errors.New("fulfillment: mã hãng đã thuộc vận đơn khác")
)
