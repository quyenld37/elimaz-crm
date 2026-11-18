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
	// ErrCategoryCycle: cha mới của danh mục là chính nó hay nằm trong cây con của nó.
	ErrCategoryCycle = errors.New("catalog: danh mục thành con của chính nó")
	// ErrCategoryTooDeep: danh mục (hay cây con sau khi chuyển) quá MaxCategoryDepth cấp.
	ErrCategoryTooDeep = errors.New("catalog: danh mục quá số cấp cho phép")
	// ErrCategoryInUse: danh mục còn danh mục con active hay mẫu active, chưa lưu trữ được.
	ErrCategoryInUse = errors.New("catalog: danh mục đang được dùng")
	// ErrAlreadyExists: trùng slug hay tên cùng cha của danh mục, mã thuộc tính, giá trị trong một thuộc tính, mã nhãn.
	ErrAlreadyExists = errors.New("catalog: đã có mục cùng tên hoặc cùng mã")
	// ErrFileNotReady: tệp gắn vào mẫu chưa xử lý xong, bị từ chối hay không phải ảnh sản phẩm (Task M1-44); tệp nhập
	// Excel không phải mục đích catalog_import hay chưa tải lên xong (Task M1-47).
	ErrFileNotReady = errors.New("catalog: tệp chưa sẵn sàng hoặc không phải ảnh sản phẩm")
	// ErrImportNotReady: bản nhập không ở trạng thái ready (còn kiểm, có lỗi, trùng, đang hay đã áp; Task M1-47b).
	ErrImportNotReady = errors.New("catalog: bản nhập chưa sẵn sàng để áp")
	// ErrRestoreExpired: ảnh đã xóa quá RestoreDays ngày, không khôi phục được (TP06).
	ErrRestoreExpired = errors.New("catalog: quá hạn khôi phục")
	// ErrInvalidArticle: bài viết sai lược đồ hay vượt giới hạn (cùng giá trị với lỗi của gói article).
	ErrInvalidArticle = article.ErrInvalid
)

// Tên ràng buộc unique trong migrations/0114_catalog.sql, 0120_catalog_classification.sql → sentinel.
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
		case "categories_slug_key", "categories_name_key", "attributes_code_key", "attribute_values_value_key",
			"tags_code_key":
			return ErrAlreadyExists
		}
	case codeFKViolation:
		return ErrNotFound // người bán không có trong tenant
	}
	return err
}
