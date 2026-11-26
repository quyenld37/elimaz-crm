// Package deps giữ phụ thuộc và kiểu kết quả chung của các phần trong module conversation (inbound, send, automation,
// live…): gói gốc conversation nhập các phần, nên kiểu dùng chung phải ở gói riêng để các phần nhập được mà không vòng.
package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/observability"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/schedules"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Deps là phụ thuộc phần chạy nền của conversation (kế hoạch M4, Task 143), dựng một lần ở cmd/crm.
type Deps struct {
	DB    *db.DB
	Queue queue.JobQueue
	Bus   realtime.Bus
	// Events là sổ sự kiện nội bộ (conversation.assigned, Q8).
	Events   *events.Registry
	Registry *channel.Registry
	// Keys giải mã token Page (nil khi kênh Facebook chưa bật).
	Keys    *fieldcrypt.Keyring
	Files   files.FileStore
	Alerter observability.Alerter
	Metrics *metrics.Registry
	// Now là đồng hồ (nil → time.Now).
	Now    func() time.Time
	Config config.Config
}

// Clock là đồng hồ của Deps: Now, hoặc time.Now khi Now nil.
func (d Deps) Clock() func() time.Time {
	if d.Now == nil {
		return time.Now
	}
	return d.Now
}

// Parts là phần chạy nền một phần của conversation khai; phần tử nil được bỏ khi gom.
type Parts struct {
	Inbound    []webhooks.InboundHandler
	Commands   []commands.Handler
	Jobs       []queue.Handler
	Schedules  []schedules.Schedule
	Background []func(context.Context) error
}

// Báo hiệu "hội thoại đổi" (Global Constraints "Báo hiệu realtime" của M4): chủ đề và loại thực thể; phần nào của
// module phát cũng dùng đúng hai hằng này (gói gốc công bố lại chủ đề là conversation.TopicConversationChanged).
const (
	TopicConversationChanged = "conversation.changed"
	EntityConversation       = "conversation"
)

const (
	// snippetRunes là độ dài đoạn trích tin cuối của hội thoại.
	snippetRunes = 80
	// attachmentSnippet là đoạn trích của tin chỉ có tệp đính kèm.
	attachmentSnippet = "[Tệp đính kèm]"
)

// Snippet là đoạn trích tin cuối của hội thoại (cột last_snippet; tin vào, echo, tin nhân viên gửi đều dùng):
// snippetRunes ký tự đầu của nội dung (bỏ khoảng trắng hai đầu); không có chữ mà có đính kèm → attachmentSnippet;
// không gì → rỗng (NULL).
func Snippet(body string, hasAttachments bool) string {
	s := strings.TrimSpace(body)
	if s == "" && hasAttachments {
		return attachmentSnippet
	}
	if r := []rune(s); len(r) > snippetRunes {
		return string(r[:snippetRunes])
	}
	return s
}

// PendingAttachment là một mục đính kèm chờ tải (Task 155 chép về kho rồi đổi status). URL (đường dẫn có chữ ký của
// kênh, dữ liệu cá nhân) chỉ nằm trong mục tới khi lệnh tải xong (stored hay lỗi thì xóa; tệp task-155, Quyết định 4);
// không bao giờ trả ra API.
type PendingAttachment struct {
	Index     int     `json:"index"`
	Type      string  `json:"type"`
	Status    string  `json:"status"`
	FileID    *string `json:"fileId"`
	StickerID string  `json:"stickerId,omitempty"`
	URL       string  `json:"url,omitempty"`
}

// PendingAttachments là danh sách đính kèm chờ tải của tin (JSON mảng, rỗng → []): mọi đường ghi tin từ kênh (tin vào,
// echo, comment, tin lịch sử) dùng chung định dạng này.
func PendingAttachments(refs []channel.AttachmentRef) ([]byte, error) {
	out := make([]PendingAttachment, 0, len(refs))
	for i, r := range refs {
		out = append(out, PendingAttachment{Index: i, Type: r.Type, Status: "pending", StickerID: r.StickerID, URL: r.URL})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("mã hóa danh sách đính kèm: %w", err)
	}
	return b, nil
}

// WithoutURLs là bản sao refs không có URL (mục giữ pending, không lệnh tải): đường không xếp lệnh tải tệp thì không
// giữ đường dẫn của kênh.
func WithoutURLs(refs []channel.AttachmentRef) []channel.AttachmentRef {
	out := make([]channel.AttachmentRef, len(refs))
	for i, r := range refs {
		r.URL = ""
		out[i] = r
	}
	return out
}

// AdapterAs trả adapter của kênh key trong r dưới dạng năng lực tùy chọn T (channel.PostFetcher,
// channel.HistoryFetcher…); false khi không có bộ adapter, không có kênh hay adapter không hiện thực T.
func AdapterAs[T any](r *channel.Registry, key string) (T, bool) { //nolint:ireturn // năng lực tùy chọn của adapter là interface
	var zero T
	if r == nil {
		return zero, false
	}
	a, ok := r.Get(key)
	if !ok {
		return zero, false
	}
	t, ok := a.(T)
	return t, ok
}
