package order

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Lỗi nghiệp vụ của order. Lớp HTTP đổi sang mã ở internal/httpapi/errors.go. Mọi lỗi: bên gọi lùi transaction.
var (
	// ErrOutOfStock: một dòng không đủ hàng có thể bán; lỗi cụ thể là *OutOfStockError.
	ErrOutOfStock = errors.New("order: hết hàng")
	// ErrNotForSale: biến thể của người bán của đơn nhưng ngừng bán (hoặc mẫu ngừng bán, đã xóa; KH12).
	ErrNotForSale = errors.New("order: biến thể không bán được")
	// ErrMixedSellers: biến thể không thuộc người bán của đơn (của người bán khác, hoặc không có; KH14, ADR-29).
	ErrMixedSellers = errors.New("order: biến thể không thuộc người bán của đơn")
	// ErrTotalOverflow: tiền dòng, tạm tính hay tổng đơn vượt 9007199254740991 đồng.
	ErrTotalOverflow = errors.New("order: tổng tiền vượt giới hạn")
	// ErrInvalidInput: thiếu khóa, không có dòng, số lượng ≤ 0, phí âm, địa chỉ giao sai.
	ErrInvalidInput = errors.New("order: dữ liệu đơn không hợp lệ")
	// ErrDuplicateOrder: khóa nghiệp vụ idempotency_key đã có đơn (04 mục 5.5).
	ErrDuplicateOrder = errors.New("order: khóa tạo đơn đã được dùng")
	// ErrNotFound: không có đơn, hoặc của người bán, tenant khác (không cho biết là có).
	ErrNotFound = errors.New("order: không có đơn")
	// ErrShipmentAlreadyAttached: đơn đã có vận đơn hiện hành khác (05 mục 2, B1).
	ErrShipmentAlreadyAttached = errors.New("order: đơn đã có vận đơn hiện hành khác")
	// ErrStatusConflict: đơn không ở trạng thái cho việc này, hoặc giữ hàng của đơn lệch trạng thái đơn.
	ErrStatusConflict = errors.New("order: đơn không ở trạng thái cần")

	// Sentinel của M2 (kế hoạch M2, Task M2-02 khai một lần; task sau chỉ dùng).

	// ErrVersionConflict: version gửi lên khác version hiện tại của đơn (người khác vừa sửa).
	ErrVersionConflict = errors.New("order: đơn đã đổi phiên bản")
	// ErrInvalidTransition: cặp trạng thái không có trong sơ đồ (05 mục 2), hoặc trường không sửa được ở trạng thái này.
	ErrInvalidTransition = errors.New("order: không chuyển được trạng thái")
	// ErrAddressIncomplete: chốt đơn khi địa chỉ giao chưa đủ tỉnh, xã, số nhà.
	ErrAddressIncomplete = errors.New("order: địa chỉ giao chưa đủ")
	// ErrNeedsApproval: đơn đang chờ duyệt giảm giá, chưa xác nhận được.
	ErrNeedsApproval = errors.New("order: đơn chờ duyệt giảm giá")
	// ErrDiscountNeedsApproval: sửa đơn đã xác nhận làm giảm giá vượt ngưỡng mà người sửa không có quyền duyệt.
	ErrDiscountNeedsApproval = errors.New("order: giảm giá vượt ngưỡng cần người có quyền duyệt")
	// ErrVariantNotFound: biến thể không có (kể cả của tenant khác).
	ErrVariantNotFound = errors.New("order: không có biến thể")
	// ErrLineDiscountExceeds: giảm giá dòng lớn hơn thành tiền dòng.
	ErrLineDiscountExceeds = errors.New("order: giảm giá dòng vượt thành tiền dòng")
	// ErrOrderDiscountExceeds: giảm cấp đơn cộng giảm mã lớn hơn tạm tính sau giảm dòng.
	ErrOrderDiscountExceeds = errors.New("order: giảm giá đơn vượt tạm tính")
	// ErrLockedByShipment: đơn có vận đơn hiện hành, trường này không sửa được (Q25).
	ErrLockedByShipment = errors.New("order: đơn đã gửi hãng, không sửa được trường này")
	// ErrTrafficSourceInactive: nguồn đơn không có hoặc đã tắt.
	ErrTrafficSourceInactive = errors.New("order: nguồn đơn không dùng được")
	// ErrReturnQtyExceeds: số trả vượt số đã mua trừ số đã trả (kể cả phiếu đang mở).
	ErrReturnQtyExceeds = errors.New("order: số lượng trả vượt số còn trả được")
	// ErrReturnNotAllowed: đơn không ở trạng thái cho tạo phiếu trả, hoặc phiếu không ở trạng thái cho việc này.
	ErrReturnNotAllowed = errors.New("order: không trả hàng được")
	// ErrSettingOutOfRange: cài đặt đơn ngoài khoảng cho phép; chi tiết (tên trường) là *SettingOutOfRangeError.
	ErrSettingOutOfRange = errors.New("order: cài đặt ngoài khoảng cho phép")
	// ErrTrafficSourceNameTaken: đã có nguồn đơn cùng tên trong shop (Task M2-15).
	ErrTrafficSourceNameTaken = errors.New("order: đã có nguồn đơn cùng tên")
)

// OutOfStockError là chi tiết của ErrOutOfStock: dòng đầu tiên (theo thứ tự khóa) không đủ hàng.
type OutOfStockError struct {
	VariantID uuid.UUID
	Available int32
}

func (e *OutOfStockError) Error() string {
	return fmt.Sprintf("order: hết hàng biến thể %s (có thể bán %d)", e.VariantID, e.Available)
}

// Is cho errors.Is(err, ErrOutOfStock).
func (e *OutOfStockError) Is(target error) bool { return target == ErrOutOfStock }
