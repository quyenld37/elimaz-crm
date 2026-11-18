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
