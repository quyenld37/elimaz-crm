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
	codeChannelReadOnly         = "channel_read_only"         // Page chỉ xem (Task 156, MO03)
	codeSendNotAllowed          = "send_not_allowed"          // chính sách gửi của kênh không cho (Task 156, CH09)
	codeAttachmentNotReady      = "attachment_not_ready"      // ảnh gửi kèm chưa xử lý xong (Task 156)
	codeAttachmentRejected      = "attachment_rejected"       // tệp gửi kèm sai mục đích hay bị từ chối (Task 156)
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
		// Gửi tin (Task 156): yêu cầu sai → 400; Page chỉ xem → 409; ảnh chưa xong, sai mục đích → 422 (kênh không cho
		// gửi: thân chi tiết ở conversationDetail).
		{conversation.ErrSendInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrChannelReadOnly, http.StatusConflict, codeChannelReadOnly},
		{conversation.ErrAttachmentNotReady, http.StatusUnprocessableEntity, codeAttachmentNotReady},
		{conversation.ErrAttachmentRejected, http.StatusUnprocessableEntity, codeAttachmentRejected},
	})
}

// conversationDetail: send_not_allowed kèm denyReason và until (hạn của cửa sổ, hay hạn 7 ngày còn gửi được bằng thẻ
// nhân viên); chỉ mã, mốc, không nội dung.
func conversationDetail(err error) (errorBody, int, bool) {
	e, ok := errors.AsType[*conversation.SendNotAllowedError](err)
	if !ok {
		return errorBody{}, 0, false
	}
	reason := string(e.Reason)
	body := errorBody{Error: codeSendNotAllowed, DenyReason: &reason}
	if e.Until != nil {
		u := e.Until.UTC()
		body.Until = &u
	}
	return body, http.StatusConflict, true
}

// isConversationRefusal: lỗi nghiệp vụ của đường ghi hộp chat (kể cả khách không có của customer; Page không có của
// channel, nguồn đơn đã tắt của order và nhãn chung thiếu quyền toàn shop, Task 160; biến của mẫu thiếu giá trị, Task
// 161; kênh không cho gửi, Task 156) — không lưu theo khóa chống trùng.
func isConversationRefusal(err error) bool {
	_, _, ok := conversationError(err)
	_, _, chOK := channelError(err)
	_, _, ordOK := orderError(err)
	_, missing := errors.AsType[*conversation.VariableMissingError](err)
	_, _, detailed := conversationDetail(err)
	return ok || chOK || ordOK || missing || detailed || isCustomerRefusal(err) || errors.Is(err, errForbiddenTarget)
}
