// Package deps giữ phụ thuộc và kiểu kết quả chung của các phần trong module conversation (inbound, send, automation,
// live…): gói gốc conversation nhập các phần, nên kiểu dùng chung phải ở gói riêng để các phần nhập được mà không vòng.
package deps

import (
	"context"
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
