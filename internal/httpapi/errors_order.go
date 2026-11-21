package httpapi

// Bảng đổi lỗi của vùng order sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Mã lỗi chỉ vùng order trả.
const (
	codeOutOfStock              = "out_of_stock"
	codeVariantNotForSale       = "variant_not_for_sale"
	codeMixedSellers            = "mixed_sellers"
	codeTotalOverflow           = "total_overflow"
	codeShipmentAlreadyAttached = "shipment_already_attached"
	codeOrderLockedByShipment   = "order_locked_by_shipment"
	codeAddressIncomplete       = "address_incomplete"
	codeNeedsApproval           = "needs_approval"
	codeDiscountNeedsApproval   = "discount_needs_approval"
	codeVariantNotFound         = "variant_not_found"
	codeLineDiscountExceeds     = "line_discount_exceeds"
	codeOrderDiscountExceeds    = "order_discount_exceeds"
	codeTrafficSourceInactive   = "traffic_source_inactive"
	codeReturnQtyExceeds        = "return_qty_exceeds"
	codeReturnNotAllowed        = "return_not_allowed"
)

// orderError đổi lỗi nghiệp vụ của lần tạo, đọc đơn (và khách tạo kèm) sang mã HTTP. OutOfStockError có thân riêng ở
// writeHandlerError (kèm variantId, available). ErrDuplicateOrder: khóa nghiệp vụ của đơn đã dùng — cùng nghĩa với khóa
// yêu cầu dùng lại. ErrNotFound: không có, của người bán hay tenant khác (không cho biết là có).
func orderError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{order.ErrOutOfStock, http.StatusConflict, codeOutOfStock},
		{order.ErrNotForSale, http.StatusUnprocessableEntity, codeVariantNotForSale},
		{order.ErrMixedSellers, http.StatusUnprocessableEntity, codeMixedSellers},
		{order.ErrTotalOverflow, http.StatusUnprocessableEntity, codeTotalOverflow},
		{order.ErrDuplicateOrder, http.StatusUnprocessableEntity, codeIdempotencyKeyReused},
		{order.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
		{order.ErrStockShort, http.StatusConflict, codeStockShort}, // cùng giá trị inventory.ErrStockShort (M3-08)
		{order.ErrNotFound, http.StatusNotFound, codeNotFound},
		{customer.ErrInvalidInput, http.StatusBadRequest, codeInvalidInput},
		// M1 chưa ánh xạ (trước đây 500); M2 thêm.
		{order.ErrShipmentAlreadyAttached, http.StatusConflict, codeShipmentAlreadyAttached},
		// M2 (Task M2-02).
		{order.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{order.ErrLockedByShipment, http.StatusConflict, codeOrderLockedByShipment},
		{order.ErrInvalidTransition, http.StatusUnprocessableEntity, codeInvalidTransition},
		{order.ErrAddressIncomplete, http.StatusUnprocessableEntity, codeAddressIncomplete},
		{order.ErrNeedsApproval, http.StatusUnprocessableEntity, codeNeedsApproval},
		{order.ErrDiscountNeedsApproval, http.StatusForbidden, codeDiscountNeedsApproval},
		{order.ErrVariantNotFound, http.StatusUnprocessableEntity, codeVariantNotFound},
		{order.ErrLineDiscountExceeds, http.StatusUnprocessableEntity, codeLineDiscountExceeds},
		{order.ErrOrderDiscountExceeds, http.StatusUnprocessableEntity, codeOrderDiscountExceeds},
		{order.ErrTrafficSourceInactive, http.StatusUnprocessableEntity, codeTrafficSourceInactive},
		{order.ErrReturnQtyExceeds, http.StatusUnprocessableEntity, codeReturnQtyExceeds},
		{order.ErrQtyMismatch, http.StatusUnprocessableEntity, codeQtyMismatch},
		{order.ErrReturnNotAllowed, http.StatusUnprocessableEntity, codeReturnNotAllowed},
		{order.ErrSettingOutOfRange, http.StatusUnprocessableEntity, codeSettingOutOfRange},
		// Task M2-15: tên nguồn đơn trùng (Q-T72-1: mã chung already_exists).
		{order.ErrTrafficSourceNameTaken, http.StatusConflict, codeAlreadyExists},
	})
}
