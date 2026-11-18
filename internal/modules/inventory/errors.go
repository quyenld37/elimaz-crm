package inventory

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Lỗi nghiệp vụ của inventory. Lớp HTTP đổi sang mã ở internal/httpapi/errors.go.
var (
	// ErrInsufficient: không đủ hàng có thể bán (hoặc chưa có dòng số dư) cho một dòng; lỗi cụ thể là
	// *InsufficientError. Transaction của bên gọi phải lùi.
	ErrInsufficient = errors.New("inventory: không đủ hàng")
	// ErrStateConflict: dòng giữ hàng không ở trạng thái cần (đã nhả, đã đi giao, đã giao, hoặc không có). Như mọi lỗi
	// của gói, bên gọi lùi transaction (không commit tiếp sau lỗi).
	ErrStateConflict = errors.New("inventory: giữ hàng không ở trạng thái cần")
	// ErrNoWarehouse: tenant chưa có kho MAIN.
	ErrNoWarehouse = errors.New("inventory: chưa có kho")
	// ErrInvalidInput: số lượng ≤ 0, thiếu mã dòng đơn, biến thể hay kho, tác nhân lạ, lý do nhả sai mẫu.
	ErrInvalidInput = errors.New("inventory: dữ liệu không hợp lệ")
)

// InsufficientError là chi tiết của ErrInsufficient: dòng đầu tiên (theo thứ tự khóa) không đủ hàng.
type InsufficientError struct {
	VariantID, WarehouseID uuid.UUID
	Requested, Available   int32
}

func (e *InsufficientError) Error() string {
	return fmt.Sprintf("inventory: không đủ hàng biến thể %s kho %s: cần %d, có thể bán %d",
		e.VariantID, e.WarehouseID, e.Requested, e.Available)
}

// Is cho errors.Is(err, ErrInsufficient).
func (e *InsufficientError) Is(target error) bool { return target == ErrInsufficient }
