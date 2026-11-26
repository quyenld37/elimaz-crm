// Package send là phần của module conversation: gửi tin, trả lời comment, nhắn riêng, ẩn comment; không rõ đã gửi (Task 156, 157, 158).
//
// Không rõ đã gửi, chưa gửi được (Task 157): indoubt.go (Looker, lịch send_reveal), resend.go (gửi lại, bỏ).
//
// Gửi tin (Task 156): API Send khóa hội thoại, hỏi SendPolicy của adapter, chèn tin queued và ghi lệnh
// command.fb.send_message (hạn 3 phút) trong transaction của bên gọi; lệnh hỏi lại chính sách, lấy lượt gửi của Page
// rồi gọi kênh ngoài mọi transaction; kết quả (sent, failed, expired, checking) ghi trong transaction của Runner.
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
	"elimaz.com.vn/crm/internal/platform/schedules"
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

// Giá trị cột của conversation.messages.
const (
	statusQueued   = "queued"
	statusSent     = "sent"
	statusFailed   = "failed"
	statusExpired  = "expired"
	statusChecking = "checking"
	kindInbox      = "inbox"
	originAuto     = "automation"
	originStaff    = "staff"
	attachImage    = "image"
	attachReady    = "ready"
)

// OutgoingImagePurpose là mục đích tệp của ảnh nhân viên tải lên để gửi khách (migration 0413).
const OutgoingImagePurpose = "chat_outgoing"

// maxText là độ dài tối đa (ký tự) của một tin chữ (hợp đồng sendMessage, giới hạn của Messenger).
const maxText = 2000

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

// Message là tin nhân viên vừa gửi (queued), hay tin vừa bỏ (discarded, Task 157). Origin: staff hay automation;
// FailReason: mã lý do (cột fail_reason), rỗng khi không có.
type Message struct {
	ID, ConversationID, ClientRef, SenderStaffID uuid.UUID
	Origin                                       string
	Body                                         string
	Attachments                                  []Attachment
	SendStatus                                   string
	FailReason                                   string
	SentAt                                       time.Time
}

// Parts là phần chạy nền của phần send: lệnh gửi tin (có Looker tra lại in_doubt), lịch send_reveal và việc của nó
// (Task 157).
func Parts(d deps.Deps) deps.Parts {
	return deps.Parts{
		Commands:  []commands.Handler{newSendCommand(d)},
		Jobs:      []queue.Handler{revealHandler{d: d}},
		Schedules: []schedules.Schedule{revealSchedule()},
	}
}
