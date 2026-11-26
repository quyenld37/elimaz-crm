package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/reminders"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Nhắc việc trên hội thoại (Task 169; 01 mục 6 "nhắc tôi lúc…"). Hiện thực ở internal/reminders; lịch
// conversation.reminders bắn nhắc tới hạn (Schedules).

// Reminder là một nhắc việc; Note là dữ liệu cá nhân, chỉ trả cho người được nhắc.
type Reminder = reminders.Reminder

// ReminderInput là yêu cầu tạo nhắc (UserID nil = chính người gọi).
type ReminderInput = reminders.Input

// Trạng thái nhắc dùng ở bộ lọc /me/reminders.
const (
	ReminderPending = reminders.StatusPending
	ReminderFired   = reminders.StatusFired
)

// Lỗi của nhắc việc.
var (
	// ErrReminderInvalid là lỗi: giờ nhắc không ở tương lai hay quá 90 ngày, ghi chú quá dài, trạng thái lọc lạ (400).
	ErrReminderInvalid = reminders.ErrInvalidInput
	// ErrNotFound là lỗi: đối tượng không có hay trên Page người gọi không có quyền (404, không lộ sự tồn tại).
	ErrNotFound = reminders.ErrNotFound
	// ErrReminderClosed là lỗi: nhắc đã xong hay đã hủy (409).
	ErrReminderClosed = reminders.ErrClosed
	// ErrReminderAssignForbidden là lỗi: giao nhắc cho người khác mà không có conversation.assign trên Page (403).
	ErrReminderAssignForbidden = reminders.ErrAssignForbidden
	// ErrAssigneeNotAllowed là lỗi: người được giao không phải nhân viên đang làm có quyền trả lời hội thoại (422).
	ErrAssigneeNotAllowed = reminders.ErrAssigneeNotAllowed
)

// CreateReminder tạo nhắc "manual" cho hội thoại conversationID (xem reminders.Create): cần conversation.reply trên Page
// của hội thoại, giao người khác cần conversation.assign; remindAt sau now, tối đa 90 ngày.
func CreateReminder(ctx context.Context, tx pgx.Tx, u identity.AuthUser, conversationID uuid.UUID, in ReminderInput,
	now time.Time,
) (Reminder, error) {
	return reminders.Create(ctx, tx, u, conversationID, in, reminders.SourceManual, now) //nolint:wrapcheck // sentinel của phần
}

// CompleteReminder đánh dấu nhắc id đã xong (cập nhật có điều kiện; đã đóng → ErrReminderClosed).
func CompleteReminder(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) (Reminder, error) {
	return reminders.Close(ctx, tx, u, id, reminders.StatusDone) //nolint:wrapcheck // sentinel của phần
}

// CancelReminder hủy nhắc id (cập nhật có điều kiện; đã đóng → ErrReminderClosed).
func CancelReminder(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) (Reminder, error) {
	return reminders.Close(ctx, tx, u, id, reminders.StatusCancelled) //nolint:wrapcheck // sentinel của phần
}

// ListMyReminders trả nhắc của chính userID ở trạng thái status (ReminderPending, ReminderFired), sớm nhất trước.
func ListMyReminders(ctx context.Context, tx pgx.Tx, userID uuid.UUID, status string) ([]Reminder, error) {
	return reminders.ListMine(ctx, tx, userID, status) //nolint:wrapcheck // sentinel của phần
}
