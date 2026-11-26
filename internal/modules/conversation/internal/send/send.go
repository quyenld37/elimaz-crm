// Package send là phần của module conversation: gửi tin, trả lời comment, nhắn riêng, ẩn comment; không rõ đã gửi (Task 156, 157, 158).
//
// Gửi tin (Task 156): API Send khóa hội thoại, hỏi SendPolicy của adapter, chèn tin queued và ghi lệnh
// command.fb.send_message (hạn 3 phút) trong transaction của bên gọi; lệnh hỏi lại chính sách, lấy lượt gửi của Page
// rồi gọi kênh ngoài mọi transaction; kết quả (sent, failed, expired, checking) ghi trong transaction của Runner.
//
// Comment (Task 158): ReplyToComment (command.fb.reply_comment), SendPrivateReply (command.fb.private_reply, một tin
// riêng mỗi comment, nằm trong hội thoại hộp thư của khách), SetCommentHidden (command.fb.hide_comment) theo cùng khuôn.
package send

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// KindSendMessage là lệnh gửi một tin của nhân viên ra kênh (tên theo kế hoạch M4; dùng cho mọi adapter trong
// Registry, adapter chọn theo kênh của tài khoản).
const KindSendMessage = "command.fb.send_message"

// Hạn của lệnh gửi tin (04 mục 5.3): còn chờ quá 3 phút kể từ lúc ghi thì expired ("chưa gửi được"); mỗi lần Execute
// tối đa 30 giây.
const (
	sendExpiresAfter = 3 * time.Minute
	sendTimeout      = 30 * time.Second
)

// CommandKind là Kind của lệnh gửi tin.
func CommandKind() queue.Kind {
	return commands.CommandKind(KindSendMessage, sendExpiresAfter, sendTimeout)
}

// Lệnh ra kênh của comment (kế hoạch M4-18): trả lời công khai, nhắn riêng người comment, ẩn/hiện comment.
const (
	KindReplyComment = "command.fb.reply_comment"
	KindPrivateReply = "command.fb.private_reply"
	KindHideComment  = "command.fb.hide_comment"
)

// Hạn của lệnh comment: trả lời, nhắn riêng còn chờ quá 5 phút thì expired; ẩn/hiện 1 giờ (tự thử lại khi chắc chắn
// chưa tới Meta).
const (
	commentExpiresAfter = 5 * time.Minute
	hideExpiresAfter    = time.Hour
)

// ReplyCommentKind là Kind của lệnh trả lời comment.
func ReplyCommentKind() queue.Kind {
	return commands.CommandKind(KindReplyComment, commentExpiresAfter, sendTimeout)
}

// PrivateReplyKind là Kind của lệnh nhắn riêng người comment.
func PrivateReplyKind() queue.Kind {
	return commands.CommandKind(KindPrivateReply, commentExpiresAfter, sendTimeout)
}

// HideCommentKind là Kind của lệnh ẩn/hiện comment.
func HideCommentKind() queue.Kind {
	return commands.CommandKind(KindHideComment, hideExpiresAfter, sendTimeout)
}

// Giá trị cột của conversation.messages.
const (
	statusQueued   = "queued"
	statusSent     = "sent"
	statusFailed   = "failed"
	statusExpired  = "expired"
	statusChecking = "checking"
	kindInbox      = "inbox"
	kindComment    = "comment"
	kindMessage    = "message"
	kindPrivate    = "private_reply"
	originCustomer = "customer"
	originAuto     = "automation"
	attachImage    = "image"
	attachReady    = "ready"
)

// OutgoingImagePurpose là mục đích tệp của ảnh nhân viên tải lên để gửi khách (migration 0413).
const OutgoingImagePurpose = "chat_outgoing"

// maxText là độ dài tối đa (ký tự) của một tin chữ (hợp đồng sendMessage, sendPrivateReply, giới hạn của Messenger);
// maxCommentText của câu trả lời comment (hợp đồng replyToComment).
const (
	maxText        = 2000
	maxCommentText = 8000
)

// Lỗi của đường gửi.
var (
	// ErrInvalidInput: không đúng một trong chữ, ảnh; chữ rỗng hay quá 2 000 ký tự; hội thoại không phải hộp thư (400).
	ErrInvalidInput = errors.New("conversation: yêu cầu gửi không hợp lệ")
	// ErrReadOnly: Page đang ở chế độ chỉ xem (409 channel_read_only, MO03).
	ErrReadOnly = errors.New("conversation: Page đang ở chế độ chỉ xem")
	// ErrAttachmentNotReady: ảnh chưa xử lý xong (422 attachment_not_ready).
	ErrAttachmentNotReady = errors.New("conversation: ảnh chưa sẵn sàng")
	// ErrAttachmentRejected: tệp không dùng gửi được (sai mục đích, bị kho tệp từ chối; 422 attachment_rejected).
	ErrAttachmentRejected = errors.New("conversation: tệp không gửi được")
	// ErrPrivateReplyUsed: comment đã được nhắn riêng (tin riêng đang sống, hay Meta báo đã nhắn; 409 private_reply_used).
	ErrPrivateReplyUsed = errors.New("conversation: comment đã được nhắn riêng")
	// ErrNotCustomerComment: tin không phải comment của khách trong hội thoại (422 invalid_input).
	ErrNotCustomerComment = errors.New("conversation: không phải comment của khách trong hội thoại")
)

// NotAllowedError là lỗi: chính sách gửi của kênh không cho gửi lúc này (409 send_not_allowed kèm lý do và hạn). Until khác
// nil với lý do window_closed nghĩa là còn gửi được bằng thẻ nhân viên (humanAgent: true) tới mốc đó.
type NotAllowedError struct {
	Reason channel.DenyReason
	Until  *time.Time
}

func (e *NotAllowedError) Error() string {
	return fmt.Sprintf("conversation: kênh không cho gửi (%s)", e.Reason)
}

// Env là phụ thuộc của API gửi (lớp HTTP nối).
type Env struct {
	Registry *channel.Registry
	Queue    queue.JobQueue
	Bus      realtime.Bus
}

// Input là yêu cầu gửi: đúng một trong Text, AttachmentFileID; HumanAgent: nhân viên bấm "Trả lời (nhân viên)".
type Input struct {
	Text             string
	AttachmentFileID *uuid.UUID
	HumanAgent       bool
}

func (in Input) validate() error {
	hasText := strings.TrimSpace(in.Text) != ""
	if hasText == (in.AttachmentFileID != nil) || (in.Text != "" && !hasText) || len([]rune(in.Text)) > maxText {
		return ErrInvalidInput
	}
	return nil
}

// Attachment là một mục đính kèm của tin (cột messages.attachments; cùng dạng với tin vào).
type Attachment struct {
	Index  int        `json:"index"`
	Type   string     `json:"type"`
	Status string     `json:"status"`
	FileID *uuid.UUID `json:"fileId"`
}

// Message là tin nhân viên vừa gửi (trạng thái queued). Kind: message, comment (trả lời comment), private_reply;
// ParentMessageID: comment được trả lời hay nhắn riêng.
type Message struct {
	ID, ConversationID, ClientRef, SenderStaffID uuid.UUID
	Kind                                         string
	ParentMessageID                              *uuid.UUID
	Body                                         string
	Attachments                                  []Attachment
	SendStatus                                   string
	SentAt                                       time.Time
}

// CommentInput là yêu cầu trả lời hay nhắn riêng comment CommentMessageID của hội thoại comment bằng chữ Text.
type CommentInput struct {
	CommentMessageID uuid.UUID
	Text             string
}

// validText: chữ không rỗng sau khi bỏ khoảng trắng, tối đa limit ký tự.
func validText(text string, limit int) error {
	if strings.TrimSpace(text) == "" || len([]rune(text)) > limit {
		return ErrInvalidInput
	}
	return nil
}

// Parts là phần chạy nền của phần send: lệnh gửi tin, trả lời comment, nhắn riêng, ẩn/hiện comment.
func Parts(d deps.Deps) deps.Parts {
	base := newSendCommand(d)
	return deps.Parts{Commands: []commands.Handler{
		base, replyCommand{base}, privateCommand{base}, hideCommand{base},
	}}
}
