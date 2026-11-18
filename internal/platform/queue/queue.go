// Package queue là lõi hàng đợi việc nền trên bảng platform.jobs (spec 04 mục 5.1): ghi việc trong transaction của
// bên gọi (JobQueue), worker nhận việc bằng FOR UPDATE SKIP LOCKED, thử lại theo lịch, thứ tự theo đối tượng
// (OrderKey), hết hạn, in_doubt khi giữ chỗ hết hạn, tắt êm, dấu vết đi qua hàng đợi.
//
// Module nghiệp vụ không viết SQL lên platform.jobs; chỉ dùng JobQueue, Handler, TerminalHook. Danh sách tính năng
// của lõi là đóng (spec 04 mục 5.1); cần thêm thì xét thư viện làm lớp dưới trước.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrPermanent đánh dấu lỗi vĩnh viễn (payload sai…): việc thành dead ngay, không thử lại. Handler bọc bằng
// fmt.Errorf("…: %w", queue.ErrPermanent).
var ErrPermanent = errors.New("permanent")

// NewJob là một việc cần ghi.
type NewJob struct {
	// TenantID nil: việc chưa định tuyến (tách gói webhook) hoặc toàn hệ thống; handler chạy ngoài ngữ cảnh tenant.
	TenantID *uuid.UUID
	// Payload được mã hóa JSON (nil → {}); chỉ mang mã bản ghi, không mang nội dung hay dữ liệu cá nhân.
	Payload any
	// DedupeKey (bắt buộc): chống trùng tầng 1, duy nhất theo (TenantID, Kind.Name, DedupeKey).
	DedupeKey string
	// OrderKey khác rỗng: các việc cùng (TenantID, OrderKey) chạy lần lượt theo (OrderSeq, ID).
	OrderKey string
	OrderSeq int64
	// RunAt rỗng: chạy ngay; khác rỗng: hoãn tới giờ đó.
	RunAt time.Time
}

// JobQueue ghi việc vào hàng đợi.
type JobQueue interface {
	// Enqueue ghi j trong tx của bên gọi (việc chỉ có khi tx commit; NOTIFY cũng vậy). Trùng khóa chống trùng thì
	// không ghi, trả id của việc đã có với created = false. Span trong ctx được ghi vào trace_context.
	Enqueue(ctx context.Context, tx pgx.Tx, k Kind, j NewJob) (id uuid.UUID, created bool, err error)
}

// Job là việc worker giao cho handler.
type Job struct {
	ID       uuid.UUID
	TenantID *uuid.UUID
	Kind     string
	Payload  json.RawMessage
	// Attempts là số lần chạy tính cả lần này (lần đầu là 1).
	Attempts  int
	CreatedAt time.Time
}

// Handler xử lý một loại việc.
type Handler interface {
	// Kind là loại việc và chính sách của nó.
	Kind() Kind
	// Handle chạy trong tx (WithTenant khi việc có tenant) với ctx hết hạn sau Policy.Timeout. Kết quả trả về được
	// ghi vào result cùng transaction với done. Trả lỗi thì mọi thay đổi trong tx bị lùi và việc được hẹn chạy lại
	// (bọc ErrPermanent để dead ngay). Handle có thể bị gọi lại (lỗi tạm của database, chết giữa chừng) nên phải
	// chạy lại được; không gọi mạng, không gửi gì ra ngoài database (việc ra ngoài là lệnh, Task 12).
	Handle(ctx context.Context, j Job, tx pgx.Tx) (json.RawMessage, error)
}

// TerminalHook (tùy chọn, handler hiện thực thêm) được gọi trong transaction đổi việc sang dead, in_doubt hoặc
// expired, trước câu đổi trạng thái; trả lỗi thì việc giữ nguyên trạng thái cũ.
type TerminalHook interface {
	OnTerminal(ctx context.Context, j Job, status string, tx pgx.Tx) error
}

// CommandRunner chạy việc mode command (lệnh ra ngoài, spec 04 mục 5.3; commands.Runner hiện thực). Worker nhận việc
// command chỉ khi WorkerOptions.Commands khác nil; giữ chỗ hết hạn và hết hạn sống của lệnh vẫn do Worker quét.
type CommandRunner interface {
	// Kinds là các loại lệnh runner chạy được (Policy.Mode = ModeCommand).
	Kinds() []Kind
	// TerminalHook của loại kind, nil khi không có; Worker gọi khi lệnh thành in_doubt vì hết giữ chỗ hoặc expired.
	TerminalHook(kind string) TerminalHook
	// Run chạy lệnh j đang running do workerID giữ chỗ và tự ghi trạng thái sau đó; ctx hết hạn sau Policy.Timeout và
	// bị hủy khi Stop quá grace. Trả trạng thái đã ghi ("done", "pending", "dead", "in_doubt", "expired"; rỗng khi
	// mất giữ chỗ).
	Run(ctx context.Context, j Job, workerID string) (status string, err error)
}
