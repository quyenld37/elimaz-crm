package httpapi

// Bảng đổi lỗi của vùng channel (tài khoản kênh, kế hoạch M4 Task 145) sang mã HTTP (luật Task 441: thêm lỗi cho vùng
// chỉ sửa tệp này, x-error-codes của openapi.yaml và errors_snapshot_test.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// channelError: không có tài khoản kênh → 404 not_found; version cũ → 409 version_conflict; chế độ gửi lạ (hợp đồng đã
// chặn bằng enum, chốt cuối của module) → 400 invalid_input.
func channelError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{channel.ErrAccountNotFound, http.StatusNotFound, codeNotFound},
		{channel.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{channel.ErrInvalidSendMode, http.StatusBadRequest, codeInvalidInput},
	})
}
