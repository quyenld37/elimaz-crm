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
	// ErrInvalidInput: số lượng ≤ 0, thiếu mã dòng đơn, biến thể hay kho, tác nhân lạ, lý do nhả sai mẫu; dòng phiếu
	// sai (số lượng theo loại, trùng biến thể, biến thể không có), phiếu không dòng khi ghi sổ, tên hay SĐT NCC sai.
	ErrInvalidInput = errors.New("inventory: dữ liệu không hợp lệ")
	// ErrNotFound: phiếu, nhà cung cấp hay kho không có hoặc của tenant khác (không cho biết là có).
	ErrNotFound = errors.New("inventory: không có")
	// ErrVersionConflict: version gửi lên không còn khớp (người khác vừa sửa).
	ErrVersionConflict = errors.New("inventory: version không khớp")
	// ErrDocumentNotDraft: phiếu đã ghi sổ hoặc đã hủy; không sửa, ghi sổ, hủy được nữa.
	ErrDocumentNotDraft = errors.New("inventory: phiếu không còn nháp")
	// ErrSupplierNameTaken: tenant đã có nhà cung cấp cùng tên.
	ErrSupplierNameTaken = errors.New("inventory: tên nhà cung cấp đã có")
	// ErrSupplierArchived: nhà cung cấp đã lưu trữ, không dùng cho phiếu mới.
	ErrSupplierArchived = errors.New("inventory: nhà cung cấp đã lưu trữ")
	// ErrSupplierNotForKind: nhà cung cấp chỉ gắn với phiếu nhập.
	ErrSupplierNotForKind = errors.New("inventory: nhà cung cấp chỉ cho phiếu nhập")
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

// Lỗi của kiểm kho (Task M1-35).
var (
	// ErrStocktakeOpen: kho đã có một phiên kiểm đang mở.
	ErrStocktakeOpen = errors.New("inventory: kho đang có phiên kiểm mở")
	// ErrStocktakeNotOpen: phiên kiểm đã áp dụng hoặc đã hủy; không đếm, áp dụng, hủy được nữa.
	ErrStocktakeNotOpen = errors.New("inventory: phiên kiểm không còn mở")
)

// ErrStockShort là lỗi tồn thực không đủ cho một dòng giữ hàng đã có ở bước lấy hàng hay giao (Task 406): kiểm kho đã
// ghi số đếm thấp hơn phần đang giữ (Q8). Lỗi cụ thể là *StockShortError. Không trừ âm, không tự nhả giữ hay hủy đơn:
// nhân viên điều chỉnh tồn, đổi kho hay hủy đơn rồi làm lại. Bên gọi lùi transaction.
var ErrStockShort = errors.New("inventory: tồn thực không đủ cho phần giữ")

// StockShortError là chi tiết của ErrStockShort: dòng giữ hàng đầu tiên (theo thứ tự khóa) bị chặn. OnShelf là phần
// tồn thực còn cho dòng đó: on_hand − in_transit ở bước lấy hàng, on_hand ở bước giao.
type StockShortError struct {
	ReservationID, VariantID, WarehouseID uuid.UUID
	Requested, OnShelf                    int32
}

func (e *StockShortError) Error() string {
	return fmt.Sprintf("inventory: tồn thực không đủ cho giữ hàng %s (biến thể %s kho %s): cần %d, trên kệ %d",
		e.ReservationID, e.VariantID, e.WarehouseID, e.Requested, e.OnShelf)
}

// Is cho errors.Is(err, ErrStockShort).
func (e *StockShortError) Is(target error) bool { return target == ErrStockShort }
