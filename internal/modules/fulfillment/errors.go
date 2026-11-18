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
	// ErrOrderNotShippable: đơn không có, của người bán khác tài khoản hãng, hoặc không ở trạng thái giao được.
	ErrOrderNotShippable = errors.New("fulfillment: đơn không giao được")
	// errCodeExhausted: sinh mã vận đơn 3 lần đều trùng.
	errCodeExhausted = errors.New("fulfillment: sinh mã vận đơn trùng 3 lần")
)
