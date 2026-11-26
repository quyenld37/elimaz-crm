package httpapi

// Bảng đổi lỗi của vùng conversation (hộp chat M4) sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi
// cho vùng chỉ sửa tệp này và x-error-codes của openapi.yaml khi có mã mới).

import (
	"net/http"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// Mã lỗi chỉ vùng conversation trả.
const (
	codeAssigneeNotAllowed = "assignee_not_allowed"
)

// conversationError đổi lỗi nghiệp vụ của hộp chat sang mã HTTP. ErrNotFound: không có, hay trên Page người gọi không
// có quyền (PQ04: 404 để không lộ sự tồn tại). Nhắc đã đóng: 409 version_conflict (màn hình đang cũ, tải lại).
func conversationError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{conversation.ErrNotFound, http.StatusNotFound, codeNotFound},
		{conversation.ErrReminderInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrCallInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrLiveVideoInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrLiveStatusInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrReminderClosed, http.StatusConflict, codeVersionConflict},
		{conversation.ErrReminderAssignForbidden, http.StatusForbidden, codeForbidden},
		{conversation.ErrAssigneeNotAllowed, http.StatusUnprocessableEntity, codeAssigneeNotAllowed},
	})
}

// isConversationRefusal: lỗi nghiệp vụ của đường ghi hộp chat (kể cả khách không có của customer) — không lưu theo khóa
// chống trùng.
func isConversationRefusal(err error) bool {
	_, _, ok := conversationError(err)
	return ok || isCustomerRefusal(err)
}
