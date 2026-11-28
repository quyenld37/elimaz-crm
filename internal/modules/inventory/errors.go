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
	// ErrReasonRequired: đảo phiếu xuất hay ghi hàng thất lạc không có lý do (kế hoạch M3, Task M3-05).
	ErrReasonRequired = errors.New("inventory: thiếu lý do")
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

// Lỗi của kiểm kho mở sổ (kế hoạch M7, Task 262 = M7-17).
var (
	// ErrOpeningExists: kho đã có phiên mở sổ chưa hủy (đang mở hoặc đã đăng); mỗi kho mở sổ một lần.
	ErrOpeningExists = errors.New("inventory: kho đã có phiên kiểm kho mở sổ")
	// ErrWarehouseNotEmpty: kho đã có dòng sổ, số dư khác 0 hay giữ hàng chưa xong; mở sổ chỉ làm trên kho trống.
	ErrWarehouseNotEmpty = errors.New("inventory: kho không còn trống")
	// ErrUncountedLines: còn biến thể phải đếm chưa có dòng đếm; lỗi cụ thể là *UncountedLinesError.
	ErrUncountedLines = errors.New("inventory: còn biến thể chưa đếm")
	// ErrOpeningNotPosted: kiểm lại một phiên mở sổ chưa đăng (đang mở hoặc đã hủy).
	ErrOpeningNotPosted = errors.New("inventory: phiên mở sổ chưa đăng")
)

// UncountedLinesError là chi tiết của ErrUncountedLines: N biến thể phải đếm mà chưa có dòng.
type UncountedLinesError struct{ N int }

func (e *UncountedLinesError) Error() string {
	return fmt.Sprintf("inventory: còn %d biến thể chưa đếm", e.N)
}

// Is cho errors.Is(err, ErrUncountedLines).
func (e *UncountedLinesError) Is(target error) bool { return target == ErrUncountedLines }

// Lỗi của đếm cộng dồn và nhập số đếm bằng Excel (kế hoạch M7, Task 263).
var (
	// ErrUnknownCode: mã quét không khớp SKU hay barcode của biến thể nào (mẫu chưa xóa) trong tenant.
	ErrUnknownCode = errors.New("inventory: mã quét không có")
	// ErrAmbiguousCode: mã quét khớp nhiều biến thể (trùng giữa người bán); lỗi cụ thể là *AmbiguousCodeError.
	ErrAmbiguousCode = errors.New("inventory: mã quét trùng nhiều biến thể")
	// ErrNegativeCount: lần trừ làm số đếm âm, hoặc trừ dòng chưa đếm.
	ErrNegativeCount = errors.New("inventory: số đếm không được âm")
	// ErrImportAlreadyApplied: cùng tệp đã nhập chế độ add vào phiên này (cộng lần hai sẽ đếm đôi).
	ErrImportAlreadyApplied = errors.New("inventory: tệp đã cộng vào phiên kiểm")
)

// CodeCandidate là một biến thể khớp mã quét: mã biến thể, người bán, SKU.
type CodeCandidate struct {
	VariantID, SellerID uuid.UUID
	SKU                 string
}

// AmbiguousCodeError là chi tiết của ErrAmbiguousCode: mọi biến thể khớp mã (theo mã biến thể tăng dần).
type AmbiguousCodeError struct{ Candidates []CodeCandidate }

func (e *AmbiguousCodeError) Error() string {
	return fmt.Sprintf("inventory: mã quét trùng %d biến thể", len(e.Candidates))
}

// Is cho errors.Is(err, ErrAmbiguousCode).
func (e *AmbiguousCodeError) Is(target error) bool { return target == ErrAmbiguousCode }
