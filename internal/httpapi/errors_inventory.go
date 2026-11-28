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
	codeLegacyReturnExists = "legacy_return_exists"
	codeAmbiguousCode      = "ambiguous_code"
	codeUnknownCode        = "unknown_code"
	codeNegativeCount      = "negative_count"
	codeImportApplied      = "import_already_applied"
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
		{inventory.ErrAmbiguousCode, http.StatusUnprocessableEntity, codeAmbiguousCode},
		{inventory.ErrUnknownCode, http.StatusUnprocessableEntity, codeUnknownCode},
		{inventory.ErrNegativeCount, http.StatusUnprocessableEntity, codeNegativeCount},
		{inventory.ErrImportAlreadyApplied, http.StatusConflict, codeImportApplied},
		{inventory.ErrSupplierNameTaken, http.StatusConflict, codeSupplierNameTaken},
		{inventory.ErrSupplierArchived, http.StatusUnprocessableEntity, codeSupplierArchived},
		{inventory.ErrSupplierNotForKind, http.StatusUnprocessableEntity, codeSupplierNotForKind},
		{inventory.ErrDuplicateReference, http.StatusConflict, codeLegacyReturnExists},
		{inventory.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
	})
}

// ambiguousCandidateBody là một biến thể trùng mã quét (AmbiguousCodeCandidate của openapi).
type ambiguousCandidateBody struct {
	VariantID string `json:"variantId"`
	SellerID  string `json:"sellerId"`
	SKU       string `json:"sku"`
}

// inventoryDetail: lỗi kho có trường chi tiết trong thân — uncounted_lines kèm count (Task 262), ambiguous_code kèm
// candidates (Task 263), kiện hàng hoàn đã nhập (409 legacy_return_exists, Task 264) kèm existingId để màn hình liên
// kết tới phiếu đã có.
func inventoryDetail(err error) (errorBody, int, bool) {
	if e, ok := errors.AsType[*inventory.AmbiguousCodeError](err); ok {
		cs := make([]ambiguousCandidateBody, 0, len(e.Candidates))
		for _, c := range e.Candidates {
			cs = append(cs, ambiguousCandidateBody{VariantID: c.VariantID.String(), SellerID: c.SellerID.String(), SKU: c.SKU})
		}
		return errorBody{Error: codeAmbiguousCode, Candidates: cs}, http.StatusUnprocessableEntity, true
	}
	if e, ok := errors.AsType[*inventory.UncountedLinesError](err); ok {
		n := int32(min(e.N, math.MaxInt32)) //nolint:gosec // đã chặn trên
		return errorBody{Error: codeUncountedLines, Count: &n}, http.StatusUnprocessableEntity, true
	}
	if e, ok := errors.AsType[*inventory.DuplicateReferenceError](err); ok {
		id := e.ExistingID.String()
		return errorBody{Error: codeLegacyReturnExists, ExistingID: &id}, http.StatusConflict, true
	}
	return errorBody{}, 0, false
}
