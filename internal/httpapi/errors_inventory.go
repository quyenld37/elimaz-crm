package httpapi

// Bảng đổi lỗi của vùng inventory sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"errors"
	"math"
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
	codeOpeningExists      = "opening_exists"
	codeWarehouseNotEmpty  = "warehouse_not_empty"
	codeUncountedLines     = "uncounted_lines"
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
		{inventory.ErrOpeningExists, http.StatusConflict, codeOpeningExists},
		{inventory.ErrWarehouseNotEmpty, http.StatusConflict, codeWarehouseNotEmpty},
		{inventory.ErrOpeningNotPosted, http.StatusConflict, codeStocktakeOpen},
		{inventory.ErrUncountedLines, http.StatusUnprocessableEntity, codeUncountedLines},
		{inventory.ErrSupplierNameTaken, http.StatusConflict, codeSupplierNameTaken},
		{inventory.ErrSupplierArchived, http.StatusUnprocessableEntity, codeSupplierArchived},
		{inventory.ErrSupplierNotForKind, http.StatusUnprocessableEntity, codeSupplierNotForKind},
		{inventory.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
	})
}

// inventoryDetail: lỗi kho có trường chi tiết trong thân — uncounted_lines kèm count (Task 262).
func inventoryDetail(err error) (errorBody, int, bool) {
	if e, ok := errors.AsType[*inventory.UncountedLinesError](err); ok {
		n := int32(min(e.N, math.MaxInt32)) //nolint:gosec // đã chặn trên
		return errorBody{Error: codeUncountedLines, Count: &n}, http.StatusUnprocessableEntity, true
	}
	return errorBody{}, 0, false
}
