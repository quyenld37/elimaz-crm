package httpapi

// Bảng đổi lỗi của vùng files sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi cho vùng chỉ sửa tệp
// này và x-error-codes của openapi.yaml khi có mã mới; thứ tự kiểm giữa các vùng ở domainErrorMappers, errors.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/platform/files"
)

// Mã lỗi chỉ vùng files trả.
const (
	codeFileTooLarge          = "file_too_large"
	codeFileTypeNotAllowed    = "file_type_not_allowed"
	codeFilePurposeNotAllowed = "file_purpose_not_allowed"
	codeUploadMissing         = "upload_missing"
	codeUnknownPreset         = "unknown_preset"
)

// filesError đổi lỗi nghiệp vụ của kho tệp sang mã HTTP. ErrNotFound: không có, của tenant khác hay đã xóa, không cho
// biết là có (PQ08).
func filesError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{files.ErrTooLarge, http.StatusUnprocessableEntity, codeFileTooLarge},
		{files.ErrTypeNotAllowed, http.StatusUnprocessableEntity, codeFileTypeNotAllowed},
		{files.ErrUnknownPurpose, http.StatusUnprocessableEntity, codeFilePurposeNotAllowed},
		{files.ErrUploadNotAllowed, http.StatusUnprocessableEntity, codeFilePurposeNotAllowed},
		{files.ErrInvalidFile, http.StatusBadRequest, codeInvalidInput},
		{files.ErrUploadMissing, http.StatusConflict, codeUploadMissing},
		{files.ErrNotFound, http.StatusNotFound, codeNotFound},
		{files.ErrUnknownPreset, http.StatusUnprocessableEntity, codeUnknownPreset},
	})
}
