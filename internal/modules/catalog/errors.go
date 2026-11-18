package catalog

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/article"
)

// Lỗi nghiệp vụ của catalog. Lớp HTTP đổi sang mã ở internal/httpapi/errors.go. Lỗi không mang tên hay nội dung mẫu.
var (
	// ErrNotFound: không có mẫu, biến thể hay người bán này (hoặc của người bán, tenant khác).
	ErrNotFound = errors.New("catalog: không có")
	// ErrSKUTaken: SKU đã có ở người bán này.
	ErrSKUTaken = errors.New("catalog: SKU đã được dùng")
	// ErrBarcodeTaken: barcode đã có ở người bán này.
	ErrBarcodeTaken = errors.New("catalog: barcode đã được dùng")
	// ErrDuplicateOption: mẫu đã có biến thể cùng màu và size.
	ErrDuplicateOption = errors.New("catalog: mẫu đã có biến thể cùng màu và size")
	// ErrCodeTaken: mã mẫu đã có ở người bán này.
	ErrCodeTaken = errors.New("catalog: mã mẫu đã được dùng")
	// ErrVersionConflict: version gửi lên khác version hiện tại (người khác vừa sửa).
	ErrVersionConflict = errors.New("catalog: vừa được người khác sửa")
	// ErrNotForSale: biến thể có nhưng không bán được (mẫu nháp, mẫu hay biến thể ngừng bán, mẫu đã xóa; KH12).
	ErrNotForSale = errors.New("catalog: biến thể không bán được")
	// ErrInvalidPrice: giá bán hay giá nhập ngoài [0, 9007199254740991].
	ErrInvalidPrice = errors.New("catalog: giá không hợp lệ")
	// ErrInvalidInput: mã, tên, màu, size, SKU, barcode, màu hex hay trạng thái không hợp lệ.
	ErrInvalidInput = errors.New("catalog: dữ liệu mẫu không hợp lệ")
	// ErrInvalidArticle: bài viết sai lược đồ hay vượt giới hạn (cùng giá trị với lỗi của gói article).
	ErrInvalidArticle = article.ErrInvalid
)

// Tên ràng buộc unique trong migrations/0114_catalog.sql → sentinel.
const (
	codeUniqueViolation = "23505"
	codeFKViolation     = "23503"
)

// mapWriteError đổi lỗi unique và khóa ngoại của câu ghi sang sentinel theo tên ràng buộc; lỗi khác trả nguyên.
func mapWriteError(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return err
	}
	switch pgErr.Code {
	case codeUniqueViolation:
		switch pgErr.ConstraintName {
		case "variants_sku_key":
			return ErrSKUTaken
		case "variants_barcode_key":
			return ErrBarcodeTaken
		case "variants_option_key":
			return ErrDuplicateOption
		case "products_code_key":
			return ErrCodeTaken
		}
	case codeFKViolation:
		return ErrNotFound // người bán không có trong tenant
	}
	return err
}
