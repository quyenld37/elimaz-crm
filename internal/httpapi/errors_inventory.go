package httpapi

// Bảng đổi lỗi của vùng inventory sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"errors"
	"net/http"

	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Mã lỗi chỉ vùng inventory trả.
const (
	codeInsufficientStock  = "insufficient_stock"
	codeDocumentNotDraft   = "document_not_draft"
	codeStocktakeOpen      = "stocktake_open"
	codeStocktakeNotOpen   = "stocktake_not_open"
	codeSupplierNameTaken  = "supplier_name_taken"
	codeSupplierArchived   = "supplier_archived"
	codeSupplierNotForKind = "supplier_not_for_kind"
	codeLegacyReturnExists = "legacy_return_exists"
)

// inventoryError đổi lỗi nghiệp vụ của kho (và thiếu quyền ở kho của yêu cầu) sang mã HTTP. ErrNotFound: phiếu, nhà
// cung cấp, kho không có hoặc của tenant khác (không cho biết là có). InsufficientError có thân riêng ở
// writeHandlerError (kèm variantId, available).
func inventoryError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{errForbiddenTarget, http.StatusForbidden, codeForbidden},
		{inventory.ErrInsufficient, http.StatusConflict, codeInsufficientStock},
		{inventory.ErrStockShort, http.StatusConflict, codeStockShort},
		{inventory.ErrNotFound, http.StatusNotFound, codeNotFound},
		{inventory.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{inventory.ErrDocumentNotDraft, http.StatusConflict, codeDocumentNotDraft},
		{inventory.ErrStocktakeOpen, http.StatusConflict, codeStocktakeOpen},
		{inventory.ErrStocktakeNotOpen, http.StatusConflict, codeStocktakeNotOpen},
		{inventory.ErrSupplierNameTaken, http.StatusConflict, codeSupplierNameTaken},
		{inventory.ErrSupplierArchived, http.StatusUnprocessableEntity, codeSupplierArchived},
		{inventory.ErrSupplierNotForKind, http.StatusUnprocessableEntity, codeSupplierNotForKind},
		{inventory.ErrDuplicateReference, http.StatusConflict, codeLegacyReturnExists},
		{inventory.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
	})
}

// inventoryDetail: kiện hàng hoàn đã nhập (409 legacy_return_exists, Task 264) kèm existingId để màn hình liên kết
// tới phiếu đã có.
func inventoryDetail(err error) (errorBody, int, bool) {
	e, ok := errors.AsType[*inventory.DuplicateReferenceError](err)
	if !ok {
		return errorBody{}, 0, false
	}
	id := e.ExistingID.String()
	return errorBody{Error: codeLegacyReturnExists, ExistingID: &id}, http.StatusConflict, true
}
