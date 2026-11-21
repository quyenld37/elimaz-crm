package httpapi

// Bảng đổi lỗi của vùng customer sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// Mã lỗi chỉ vùng customer trả.
const (
	codeInvalidPhone       = "invalid_phone"
	codeIdentityTaken      = "identity_taken"
	codeWardNotInProvince  = "ward_not_in_province"
	codeUnknownAdminUnit   = "unknown_admin_unit"
	codeQueryTooShort      = "query_too_short"
	codeMergeSameCustomer  = "merge_same_customer"
	codeMergeNotLatest     = "merge_not_latest"
	codeMergeWindowExpired = "merge_window_expired"
	codeImportTooLarge     = "import_too_large"
	codeImportFileInvalid  = "import_file_invalid"
	codeInvalidTagRule     = "invalid_tag_rule"
)

// customerError đổi lỗi nghiệp vụ của khách (M2, Task M2-02) sang mã HTTP. ErrNotFound: không có hoặc của tenant khác
// (không cho biết là có). IdentityTakenError, MergeNotLatestError có thân riêng ở detailedError.
func customerError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{customer.ErrNotFound, http.StatusNotFound, codeNotFound},
		{customer.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{customer.ErrInvalidPhone, http.StatusUnprocessableEntity, codeInvalidPhone},
		{customer.ErrIdentityTaken, http.StatusConflict, codeIdentityTaken},
		{customer.ErrWardNotInProvince, http.StatusUnprocessableEntity, codeWardNotInProvince},
		{customer.ErrUnknownAdminUnit, http.StatusUnprocessableEntity, codeUnknownAdminUnit},
		{customer.ErrQueryTooShort, http.StatusUnprocessableEntity, codeQueryTooShort},
		{customer.ErrMergeSameCustomer, http.StatusUnprocessableEntity, codeMergeSameCustomer},
		{customer.ErrMergeNotLatest, http.StatusConflict, codeMergeNotLatest},
		{customer.ErrMergeWindowExpired, http.StatusUnprocessableEntity, codeMergeWindowExpired},
		{customer.ErrImportTooLarge, http.StatusUnprocessableEntity, codeImportTooLarge},
		{customer.ErrImportFileInvalid, http.StatusUnprocessableEntity, codeImportFileInvalid},
		{customer.ErrInvalidTagRule, http.StatusUnprocessableEntity, codeInvalidTagRule},
		// Task M2-12: tên nhãn trùng dùng mã chung already_exists (như Q-T72-1); gỡ nhãn do quy tắc gắn → màn hình đang
		// cũ, tải lại (version_conflict). Không thêm mã mới vào hợp đồng (tệp task 69, QĐ1, QĐ2).
		{customer.ErrTagNameTaken, http.StatusConflict, codeAlreadyExists},
		{customer.ErrTagRuleManaged, http.StatusConflict, codeVersionConflict},
	})
}
