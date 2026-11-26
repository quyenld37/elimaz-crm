package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/files"
)

// Gửi tin của nhân viên (Task 156; CH03, CH04, CH09). Hiện thực ở internal/send; lệnh command.fb.send_message nằm trong
// CommandHandlers.

// SendEnv là phụ thuộc của API gửi: bộ adapter kênh, hàng đợi, bus báo hiệu.
type SendEnv = send.Env

// SendInput là yêu cầu gửi: đúng một trong Text, AttachmentFileID (ảnh chat_outgoing đã ready); HumanAgent khi nhân
// viên bấm "Trả lời (nhân viên)".
type SendInput = send.Input

// Message là tin nhân viên vừa gửi (queued).
type Message = send.Message

// Attachment là một mục đính kèm của tin.
type Attachment = send.Attachment

// SendNotAllowedError là lỗi: chính sách gửi của kênh không cho gửi (409 send_not_allowed kèm denyReason, until).
type SendNotAllowedError = send.NotAllowedError

// SendMessageKind là tên lệnh gửi tin ra kênh.
const SendMessageKind = send.KindSendMessage

// Tên lệnh ra kênh của comment (Task 158): trả lời công khai, nhắn riêng người comment, ẩn/hiện comment.
const (
	ReplyCommentKind = send.KindReplyComment
	PrivateReplyKind = send.KindPrivateReply
	HideCommentKind  = send.KindHideComment
)

// CommentInput là yêu cầu trả lời hay nhắn riêng một comment của hội thoại comment.
type CommentInput = send.CommentInput

// Comment là comment của khách sau khi nhân viên bấm ẩn/hiện (trạng thái trong CRM lúc này).
type Comment = send.Comment

// PrivateReplyState là trạng thái nhắn riêng của một comment (available, used, expired; mốc; tin riêng).
type PrivateReplyState = send.PrivateReplyState

// Trạng thái nhắn riêng của comment.
const (
	PrivateReplyAvailable = send.PrivateReplyAvailable
	PrivateReplyUsed      = send.PrivateReplyUsed
	PrivateReplyExpired   = send.PrivateReplyExpired
)

// OutgoingImagePurpose là mục đích tệp của ảnh nhân viên tải lên để gửi khách.
const OutgoingImagePurpose = send.OutgoingImagePurpose

// Lỗi của đường gửi.
var (
	// ErrSendInvalid: không đúng một trong chữ, ảnh; chữ rỗng, quá 2 000 ký tự; hội thoại không phải hộp thư (400).
	ErrSendInvalid = send.ErrInvalidInput
	// ErrChannelReadOnly: Page ở chế độ chỉ xem (409 channel_read_only).
	ErrChannelReadOnly = send.ErrReadOnly
	// ErrAttachmentNotReady: ảnh chưa xử lý xong (422 attachment_not_ready; gửi ảnh, và AttachmentURL của Task 155).
	ErrAttachmentNotReady = send.ErrAttachmentNotReady
	// ErrAttachmentRejected: tệp sai mục đích hay bị kho tệp từ chối (422 attachment_rejected; gửi ảnh, AttachmentURL).
	ErrAttachmentRejected = send.ErrAttachmentRejected
	// ErrPrivateReplyUsed: comment đã được nhắn riêng (409 private_reply_used).
	ErrPrivateReplyUsed = send.ErrPrivateReplyUsed
	// ErrNotCustomerComment: không phải comment của khách trong hội thoại (422 invalid_input).
	ErrNotCustomerComment = send.ErrNotCustomerComment
)

// SendMessage gửi tin của nhân viên u vào hội thoại conversationID (xem send.Send), trong tx tenant của bên gọi:
// cần conversation.reply trên Page của hội thoại (không có → ErrNotFound).
func SendMessage(ctx context.Context, tx pgx.Tx, env SendEnv, u identity.AuthUser, conversationID uuid.UUID, in SendInput,
	now time.Time,
) (Message, error) {
	return send.Send(ctx, tx, env, u, conversationID, in, now) //nolint:wrapcheck // sentinel của phần
}

// ReplyToComment trả lời công khai comment của hội thoại comment conversationID (xem send.ReplyToComment), trong tx
// tenant của bên gọi: cần conversation.reply trên Page (không có → ErrNotFound).
func ReplyToComment(ctx context.Context, tx pgx.Tx, env SendEnv, u identity.AuthUser, conversationID uuid.UUID,
	in CommentInput, now time.Time,
) (Message, error) {
	return send.ReplyToComment(ctx, tx, env, u, conversationID, in, now) //nolint:wrapcheck // sentinel của phần
}

// SendPrivateReply nhắn riêng người comment một lần (xem send.PrivateReply); tin nằm trong hội thoại hộp thư của
// khách. Lần hai → ErrPrivateReplyUsed.
func SendPrivateReply(ctx context.Context, tx pgx.Tx, env SendEnv, u identity.AuthUser, conversationID uuid.UUID,
	in CommentInput, now time.Time,
) (Message, error) {
	return send.PrivateReply(ctx, tx, env, u, conversationID, in, now) //nolint:wrapcheck // sentinel của phần
}

// SetCommentHidden ẩn (hidden) hay hiện comment của khách messageID trên Page (xem send.SetCommentHidden); comment vẫn
// hiện trong CRM.
func SetCommentHidden(ctx context.Context, tx pgx.Tx, env SendEnv, u identity.AuthUser, messageID uuid.UUID, hidden bool,
	now time.Time,
) (Comment, error) {
	return send.SetCommentHidden(ctx, tx, env, u, messageID, hidden, now) //nolint:wrapcheck // sentinel của phần
}

// PrivateReplyStatus trả trạng thái nhắn riêng của comment commentID (bên gọi đã kiểm quyền đọc).
func PrivateReplyStatus(ctx context.Context, tx pgx.Tx, reg *channel.Registry, commentID uuid.UUID, now time.Time,
) (PrivateReplyState, error) {
	return send.PrivateReplyStatus(ctx, tx, reg, commentID, now) //nolint:wrapcheck // sentinel của phần
}

// FailReasonText trả câu tiếng Việt của mã lý do tin gửi không thành (cột fail_reason).
func FailReasonText(code string) string { return send.FailReasonText(code) }

// imageRulesFrom là mục đích M1 có cùng luật với ảnh nhân viên gửi khách (loại, dung lượng, bucket, xử lý).
const imageRulesFrom = "product_image"

// OutgoingImageFilePurpose là mục đích ảnh nhân viên tải lên để gửi khách: cùng luật với ảnh sản phẩm của M1 (JPEG,
// PNG, WebP ≤ 20 MiB, nhân viên tải lên qua createUpload, bucket originals, xử lý bằng file.process: biến thể công
// khai, lệnh gửi lấy JPEG lớn nhất), khác khóa; dòng của platform.file_purposes ở migration 0413.
func OutgoingImageFilePurpose() files.Purpose {
	for _, p := range files.M1Purposes() {
		if p.Key == imageRulesFrom {
			p.Key = OutgoingImagePurpose
			return p
		}
	}
	// M1Purposes là hằng của mã nên không tới đây; nếu M1 bỏ mục đích kia thì TestPurposes_GoMatchesTable của files bắt
	// (mục đích không nhận loại nào).
	return files.Purpose{Key: OutgoingImagePurpose, Bucket: files.BucketOriginals}
}
