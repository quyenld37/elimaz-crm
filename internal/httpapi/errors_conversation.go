package httpapi

// Bảng đổi lỗi của vùng conversation (hộp chat M4) sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi
// cho vùng chỉ sửa tệp này và x-error-codes của openapi.yaml khi có mã mới).

import (
	"errors"
	"net/http"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// Mã lỗi chỉ vùng conversation trả.
const (
	codeAssigneeNotAllowed      = "assignee_not_allowed"
	codeAlreadyAssigned         = "already_assigned"
	codeTemplateVariableMissing = "template_variable_missing" // kèm name (detailedError, errors.go)
)

// conversationError đổi lỗi nghiệp vụ của hộp chat sang mã HTTP. ErrNotFound: không có, hay trên Page người gọi không
// có quyền (PQ04: 404 để không lộ sự tồn tại). Nhắc đã đóng: 409 version_conflict (màn hình đang cũ, tải lại).
func conversationError(err error) (status int, code string, ok bool) {
	return matchError(err, []errorMapping{
		{conversation.ErrNotFound, http.StatusNotFound, codeNotFound},
		{conversation.ErrReminderInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrCallInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrReminderClosed, http.StatusConflict, codeVersionConflict},
		{conversation.ErrReminderAssignForbidden, http.StatusForbidden, codeForbidden},
		{conversation.ErrAssigneeNotAllowed, http.StatusUnprocessableEntity, codeAssigneeNotAllowed},
		// Người phụ trách, nhãn, cài đặt Page (Task 160): nhãn không có → 404; version cũ → 409; người khác đang nhận →
		// 409 already_assigned; tên nhãn trùng → 409 already_exists; dữ liệu nhãn, cài đặt sai → 422 invalid_input.
		{conversation.ErrTagNotFound, http.StatusNotFound, codeNotFound},
		{conversation.ErrVersionConflict, http.StatusConflict, codeVersionConflict},
		{conversation.ErrAlreadyAssigned, http.StatusConflict, codeAlreadyAssigned},
		{conversation.ErrTagNameTaken, http.StatusConflict, codeAlreadyExists},
		{conversation.ErrInvalidInput, http.StatusUnprocessableEntity, codeInvalidInput},
		// Ghi chú, mẫu trả lời (Task 161).
		{conversation.ErrNoteInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrTemplateForbidden, http.StatusForbidden, codeForbidden},
		{conversation.ErrShortcutTaken, http.StatusConflict, codeAlreadyExists},
		{conversation.ErrTemplateVersionConflict, http.StatusConflict, codeVersionConflict},
		{conversation.ErrTemplateInvalid, http.StatusUnprocessableEntity, codeInvalidInput},
		{conversation.ErrUnknownVariable, http.StatusUnprocessableEntity, codeInvalidInput},
		{conversation.ErrOrderNotOfCustomer, http.StatusUnprocessableEntity, codeInvalidInput},
	})
}

// isConversationRefusal: lỗi nghiệp vụ của đường ghi hộp chat (kể cả khách không có của customer; Page không có của
// channel, nguồn đơn đã tắt của order và nhãn chung thiếu quyền toàn shop, Task 160; biến của mẫu thiếu giá trị, Task
// 161) — không lưu theo khóa chống trùng.
func isConversationRefusal(err error) bool {
	_, _, ok := conversationError(err)
	_, _, chOK := channelError(err)
	_, _, ordOK := orderError(err)
	_, missing := errors.AsType[*conversation.VariableMissingError](err)
	return ok || chOK || ordOK || missing || isCustomerRefusal(err) || errors.Is(err, errForbiddenTarget)
}
