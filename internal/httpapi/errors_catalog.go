package httpapi

// Bảng đổi lỗi của vùng catalog sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/catalog"
)

// Mã lỗi chỉ vùng catalog trả.
const (
	codeSKUTaken        = "sku_taken"
	codeBarcodeTaken    = "barcode_taken"
	codeDuplicateOption = "duplicate_option"
	codeNotForSale      = "not_for_sale"
	codeInvalidPrice    = "invalid_price"
	codeInvalidArticle  = "invalid_article"
	codeCategoryCycle   = "category_cycle"
	codeCategoryTooDeep = "category_too_deep"
	codeCategoryInUse   = "category_in_use"
	codeImportNotReady  = "import_not_ready"
	codeRestoreExpired  = "restore_expired"
)

// catalogError đổi lỗi nghiệp vụ của catalog sang mã HTTP. ErrNotFound: không có, của người bán hay tenant khác (không
// cho biết là có).
func catalogError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{catalog.ErrNotFound, http.StatusNotFound, codeNotFound},
		{catalog.ErrSKUTaken, http.StatusConflict, codeSKUTaken},
		{catalog.ErrBarcodeTaken, http.StatusConflict, codeBarcodeTaken},
		{catalog.ErrDuplicateOption, http.StatusConflict, codeDuplicateOption},
		{catalog.ErrCodeTaken, http.StatusConflict, codeCodeTaken},
		{catalog.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{catalog.ErrNotForSale, http.StatusConflict, codeNotForSale},
		{catalog.ErrInvalidPrice, http.StatusUnprocessableEntity, codeInvalidPrice},
		{catalog.ErrInvalidArticle, http.StatusUnprocessableEntity, codeInvalidArticle},
		{catalog.ErrCategoryCycle, http.StatusUnprocessableEntity, codeCategoryCycle},
		{catalog.ErrCategoryTooDeep, http.StatusUnprocessableEntity, codeCategoryTooDeep},
		{catalog.ErrCategoryInUse, http.StatusConflict, codeCategoryInUse},
		{catalog.ErrAlreadyExists, http.StatusConflict, codeAlreadyExists},
		{catalog.ErrFileNotReady, http.StatusConflict, codeFileNotReady},
		{catalog.ErrImportNotReady, http.StatusConflict, codeImportNotReady},
		{catalog.ErrRestoreExpired, http.StatusGone, codeRestoreExpired},
		{catalog.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
	})
}
