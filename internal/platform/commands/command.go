// Package commands là đường ra của CRM: lệnh ra ngoài (gửi tin, trả lời comment, tạo/hủy vận đơn) chạy như một việc
// mode command của hàng đợi (spec 04 mục 5.3; vùng rủi ro cao). Điểm cốt lõi: không biết chắc đã gửi hay chưa thì
// không gửi lại. Lệnh được ghi bằng queue.JobQueue.Enqueue với Kind từ CommandKind, trong transaction nghiệp vụ;
// Worker giao lệnh cho Runner, Runner gọi Handler.Execute ngoài transaction rồi ghi kết quả; lệnh in_doubt chỉ về hàng
// chờ khi ResolveInDoubt tra thấy chắc chắn chưa gửi (Looker) hoặc khi người có quyền bấm chạy lại.
package commands

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// Kết quả của một lần gửi (Outcome.Status) và của một lần tra (Looker.Lookup).
const (
	// StatusDone: bên ngoài xác nhận đã thực hiện.
	StatusDone = "done"
	// StatusNotSent: chắc chắn chưa gửi (lỗi kết nối trước khi gửi, 429).
	StatusNotSent = "not_sent"
	// StatusRejected: bên ngoài từ chối (chính sách, dữ liệu sai); không gửi lại.
	StatusRejected = "rejected"
	// StatusUnknown: không biết đã gửi chưa (quá giờ, mất kết nối sau khi gửi).
	StatusUnknown = "unknown"
)

// maxAttempts của mọi loại lệnh (spec 04 mục 5.1).
const maxAttempts = 8

// Outcome là kết quả Handler.Execute báo về.
type Outcome struct {
	// Status: StatusDone, StatusNotSent, StatusRejected hoặc StatusUnknown; giá trị khác coi như StatusUnknown.
	Status string
	// Reason: lý do ngắn (mã lỗi của bên ngoài), ghi vào last_error; không chứa nội dung hay dữ liệu cá nhân.
	Reason string
	// RetryAfter > 0 (not_sent): chờ đúng chừng này thay cho lịch thử lại (bên ngoài báo quá tải, 04 mục 5.3).
	RetryAfter time.Duration
	// KeepAttempt (not_sent): lần chạy này không tính vào MaxAttempts — lượt tăng lúc nhận việc được trả lại (bên ngoài
	// bận, không tới được: lỗi hạ tầng tạm, không do chính lệnh). Handler dùng nó phải tự giới hạn tổng thời gian thử
	// (trần riêng hoặc ExpiresAfter của loại) và đặt RetryAfter, vì lịch thử lại theo lượt không tiến lên.
	KeepAttempt bool
	// Result (done): mã bên ngoài trả về (message_id, mã vận đơn…), lưu vào result và truyền cho DoneHook.
	Result json.RawMessage
}

// Handler là một loại lệnh ra ngoài.
type Handler interface {
	// Kind là loại lệnh, dựng bằng CommandKind (Mode command, OnLeaseExpired in_doubt).
	Kind() queue.Kind
	// AutoRetryNotSent: tự gửi lại khi chắc chắn chưa gửi (chính sách theo loại lệnh, 04 mục 5.3).
	AutoRetryNotSent() bool
	// Execute gửi lệnh j ra ngoài, ngoài mọi transaction, với ctx hết hạn sau Policy.Timeout. Chỉ trả StatusNotSent khi
	// chắc chắn bên ngoài chưa nhận; trả lỗi hoặc ctx hết hạn được hiểu là không biết (in_doubt).
	Execute(ctx context.Context, j queue.Job) (Outcome, error)
}

// Looker (tùy chọn, Handler hiện thực thêm) tra bên ngoài xem lệnh j đã được thực hiện chưa: StatusDone,
// StatusNotSent (chắc chắn chưa có) hoặc StatusUnknown. Chạy ngoài transaction.
type Looker interface {
	Lookup(ctx context.Context, j queue.Job) (string, error)
}

// DoneHook (tùy chọn) được gọi trong transaction đổi lệnh sang done (ngữ cảnh tenant của lệnh), trước câu đổi trạng
// thái; result là Outcome.Result (nil khi done do tra lại). Trả lỗi thì lệnh không thành done.
type DoneHook interface {
	OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error
}

// CommandKind dựng Kind của một loại lệnh: Mode command, giữ chỗ hết hạn thì in_doubt (không tự chạy lại), 8 lượt;
// expiresAfter > 0: lệnh còn chờ quá chừng này kể từ lúc ghi thì expired (tin nhắn 3 phút…); timeout: hạn của một
// lần Execute (≤ 30 s; 0 lấy mặc định của hàng đợi).
func CommandKind(name string, expiresAfter, timeout time.Duration) queue.Kind {
	return queue.Kind{Name: name, Policy: queue.Policy{
		Mode: queue.ModeCommand, MaxAttempts: maxAttempts, OnLeaseExpired: queue.LeaseInDoubt,
		ExpiresAfter: expiresAfter, Timeout: timeout,
	}}
}
