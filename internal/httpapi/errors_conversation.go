package httpapi

// Bảng đổi lỗi của vùng conversation (hộp chat M4) sang mã HTTP (Task 441: mỗi vùng một tệp errors_<vùng>.go; thêm lỗi
// cho vùng chỉ sửa tệp này và x-error-codes của openapi.yaml khi có mã mới).

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"elimaz.com.vn/crm/internal/modules/conversation"
)

// Mã lỗi chỉ vùng conversation trả.
const (
	codeAssigneeNotAllowed      = "assignee_not_allowed"
	codeAlreadyAssigned         = "already_assigned"
	codeTemplateVariableMissing = "template_variable_missing" // kèm name (detailedError, errors.go)
	codeChannelReadOnly         = "channel_read_only"         // Page chỉ xem (Task 156, MO03)
	codeSendNotAllowed          = "send_not_allowed"          // chính sách gửi của kênh không cho (Task 156, CH09)
	codeAttachmentNotReady      = "attachment_not_ready"      // ảnh gửi kèm, tệp đính kèm chưa xử lý xong (Task 156, 155)
	codeAttachmentRejected      = "attachment_rejected"       // tệp sai mục đích hay bị kho tệp từ chối (Task 156, 155)
	codePrivateReplyUsed        = "private_reply_used"        // comment đã được nhắn riêng (Task 158, CH05)
	codeCommentClaimed          = "comment_claimed"           // comment live đã có đơn (Task 172, LV07), kèm claimedBy…
	codeOpenDraftExists         = "open_draft_exists"         // khách đã có đơn nháp trong phiên (Task 172, LV09), kèm drafts
	codeSellerMismatch          = "seller_mismatch"           // hàng không thuộc người bán của Page (Task 172, ADR-29)
)

// conversationError đổi lỗi nghiệp vụ của hộp chat sang mã HTTP. ErrNotFound: không có, hay trên Page người gọi không
// có quyền (PQ04: 404 để không lộ sự tồn tại). Nhắc đã đóng: 409 version_conflict (màn hình đang cũ, tải lại).
func conversationError(err error) (status int, code string, ok bool) {
	return matchError(err, slices.Concat(chatErrorMappings(), sendErrorMappings()))
}

// chatErrorMappings: lỗi của hội thoại, nhắc, gọi, live, người phụ trách, nhãn, ghi chú, mẫu trả lời (đúng thứ tự kiểm).
func chatErrorMappings() []errorMapping {
	return []errorMapping{
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
	}
}

// sendErrorMappings: lỗi của đường gửi tin, đổi trạng thái, gửi lại, bỏ tin (kiểm sau chatErrorMappings).
func sendErrorMappings() []errorMapping {
	return []errorMapping{
		// Gửi tin (Task 156): yêu cầu sai → 400; Page chỉ xem → 409; ảnh chưa xong, sai mục đích → 422 (kênh không cho
		// gửi: thân chi tiết ở conversationDetail). Đường dẫn tệp đính kèm (Task 155) dùng cùng hai lỗi 422.
		{conversation.ErrSendInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrChannelReadOnly, http.StatusConflict, codeChannelReadOnly},
		{conversation.ErrAttachmentNotReady, http.StatusUnprocessableEntity, codeAttachmentNotReady},
		{conversation.ErrAttachmentRejected, http.StatusUnprocessableEntity, codeAttachmentRejected},
		// Đọc hộp chat, đổi trạng thái (Task 159): trạng thái lạ → 422 invalid_input.
		{conversation.ErrInvalidStatus, http.StatusUnprocessableEntity, codeInvalidInput},
		// Gửi lại, bỏ tin (Task 157): trạng thái đã đổi (màn hình đang cũ) hay lệnh đang chạy → 409.
		{conversation.ErrMessageNotRecoverable, http.StatusConflict, codeVersionConflict},
		// Comment (Task 158): đã nhắn riêng → 409; không phải comment của khách trong hội thoại → 422.
		{conversation.ErrPrivateReplyUsed, http.StatusConflict, codePrivateReplyUsed},
		{conversation.ErrNotCustomerComment, http.StatusUnprocessableEntity, codeInvalidInput},
		// Chốt comment live (Task 172): yêu cầu, mốc sai → 400; đã chốt, khách có nháp → 409 kèm chi tiết
		// (conversationDetail); hàng khác người bán của Page → 422; đơn chọn đã không còn nháp → 409 (màn hình cũ); đơn
		// không của khách trong phiên → 422.
		{conversation.ErrClaimInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrCursorInvalid, http.StatusBadRequest, codeInvalidInput},
		{conversation.ErrCommentClaimed, http.StatusConflict, codeCommentClaimed},
		{conversation.ErrOpenDraftExists, http.StatusConflict, codeOpenDraftExists},
		{conversation.ErrSellerMismatch, http.StatusUnprocessableEntity, codeSellerMismatch},
		{conversation.ErrDraftNotOpen, http.StatusConflict, codeVersionConflict},
		{conversation.ErrDraftMismatch, http.StatusUnprocessableEntity, codeInvalidInput},
	}
}

// conversationDetail: send_not_allowed kèm denyReason và until (hạn của cửa sổ, hay hạn 7 ngày còn gửi được bằng thẻ
// nhân viên); chỉ mã, mốc, không nội dung.
func conversationDetail(err error) (errorBody, int, bool) {
	if body, ok := liveClaimDetail(err); ok {
		return body, http.StatusConflict, true
	}
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

// liveDraftBody là một đơn nháp trong thân lỗi open_draft_exists (dạng LiveDraftRef của openapi).
type liveDraftBody struct {
	OrderID    string          `json:"orderId"`
	Code       string          `json:"code"`
	Items      []liveDraftItem `json:"items"`
	GrandTotal int64           `json:"grandTotal"`
	Version    int64           `json:"version"`
	ExpiresAt  *time.Time      `json:"expiresAt,omitempty"`
}

type liveDraftItem struct {
	VariantID string `json:"variantId"`
	Name      string `json:"name"`
	Quantity  int32  `json:"quantity"`
}

// liveClaimDetail: comment_claimed kèm tên người chốt, đơn, mã đơn; open_draft_exists kèm các đơn nháp (Task 172).
// Không dữ liệu cá nhân của khách.
func liveClaimDetail(err error) (errorBody, bool) {
	if e, ok := errors.AsType[*conversation.CommentClaimedError](err); ok {
		by, id, code := e.ByName, e.OrderID.String(), e.OrderCode
		return errorBody{Error: codeCommentClaimed, ClaimedBy: &by, OrderID: &id, OrderCode: &code}, true
	}
	if e, ok := errors.AsType[*conversation.OpenDraftExistsError](err); ok {
		body := errorBody{Error: codeOpenDraftExists, Drafts: make([]liveDraftBody, 0, len(e.Drafts))}
		for _, d := range e.Drafts {
			body.Drafts = append(body.Drafts, liveDraftOut(d))
		}
		return body, true
	}
	return errorBody{}, false
}

func liveDraftOut(d conversation.LiveDraft) liveDraftBody {
	out := liveDraftBody{
		OrderID: d.OrderID.String(), Code: d.Code, GrandTotal: d.GrandTotal, Version: d.Version, ExpiresAt: d.ExpiresAt,
		Items: make([]liveDraftItem, 0, len(d.Items)),
	}
	for _, it := range d.Items {
		out.Items = append(out.Items, liveDraftItem{VariantID: it.VariantID.String(), Name: it.Name, Quantity: it.Qty})
	}
	return out
}
