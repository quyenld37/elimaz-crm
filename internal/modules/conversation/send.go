package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

// OutgoingImagePurpose là mục đích tệp của ảnh nhân viên tải lên để gửi khách.
const OutgoingImagePurpose = send.OutgoingImagePurpose

// Lỗi của đường gửi.
var (
	// ErrSendInvalid: không đúng một trong chữ, ảnh; chữ rỗng, quá 2 000 ký tự; hội thoại không phải hộp thư (400).
	ErrSendInvalid = send.ErrInvalidInput
	// ErrChannelReadOnly: Page ở chế độ chỉ xem (409 channel_read_only).
	ErrChannelReadOnly = send.ErrReadOnly
	// ErrAttachmentNotReady: ảnh chưa xử lý xong (422 attachment_not_ready).
	ErrAttachmentNotReady = send.ErrAttachmentNotReady
	// ErrAttachmentRejected: tệp sai mục đích hay bị từ chối (422 attachment_rejected).
	ErrAttachmentRejected = send.ErrAttachmentRejected
)

// SendMessage gửi tin của nhân viên u vào hội thoại conversationID (xem send.Send), trong tx tenant của bên gọi:
// cần conversation.reply trên Page của hội thoại (không có → ErrNotFound).
func SendMessage(ctx context.Context, tx pgx.Tx, env SendEnv, u identity.AuthUser, conversationID uuid.UUID, in SendInput,
	now time.Time,
) (Message, error) {
	return send.Send(ctx, tx, env, u, conversationID, in, now) //nolint:wrapcheck // sentinel của phần
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

// RegisterFilePurposes đăng ký mục đích tệp của conversation (chat_outgoing) vào sổ của tiến trình; cmd/crm gọi lúc
// khởi động, trước files.CheckPurposes. Gọi lại nhiều lần không sao.
func RegisterFilePurposes() { files.RegisterPurpose(OutgoingImageFilePurpose()) }
