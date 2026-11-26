package httpapi

// Bảng đổi lỗi của vùng channel (tài khoản kênh, kế hoạch M4 Task 145) sang mã HTTP (luật Task 441: thêm lỗi cho vùng
// chỉ sửa tệp này, x-error-codes của openapi.yaml và errors_snapshot_test.go).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/channel"
)

// Mã lỗi chỉ vùng channel trả (kết nối Page, Task 149).
const (
	codeOAuthStateInvalid      = "oauth_state_invalid"
	codePageNotAccessible      = "page_not_accessible"
	codePageConnectedElsewhere = "page_connected_elsewhere"
)

// channelError: không có tài khoản kênh → 404 not_found; version cũ → 409 version_conflict; chế độ gửi lạ (hợp đồng đã
// chặn bằng enum, chốt cuối của module) → 400 invalid_input. Kết nối Page (Task 149): phiên đăng nhập sai, hết hạn, đã
// dùng → 400 oauth_state_invalid; không quản trị được Page → 422 page_not_accessible; Page của shop khác → 409
// page_connected_elsewhere; Meta lỗi tạm, hay tiến trình chưa bật kết nối (thiếu adapter, khóa mã hóa) → 503
// retry_later (không lưu; 501 dành cho operation chưa làm); thiếu hay thừa nguồn token → 422 invalid_input.
func channelError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{channel.ErrAccountNotFound, http.StatusNotFound, codeNotFound},
		{channel.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{channel.ErrInvalidSendMode, http.StatusBadRequest, codeInvalidInput},
		{channel.ErrOAuthStateInvalid, http.StatusBadRequest, codeOAuthStateInvalid},
		{channel.ErrPageNotAccessible, http.StatusUnprocessableEntity, codePageNotAccessible},
		{channel.ErrPageConnectedElsewhere, http.StatusConflict, codePageConnectedElsewhere},
		{channel.ErrConnectUnavailable, http.StatusServiceUnavailable, codeRetryLater},
		{channel.ErrNoConnector, http.StatusServiceUnavailable, codeRetryLater},
		{channel.ErrNoKeyring, http.StatusServiceUnavailable, codeRetryLater},
		{errConnectSource, http.StatusUnprocessableEntity, codeInvalidInput},
	})
}
