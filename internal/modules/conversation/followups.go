package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// Nhắn lại khách chưa trả lời (Task 168; CH16). Hiện thực ở internal/automation (followup.go): danh sách cho nhân viên
// nhắn một chạm (renderReplyTemplate + sendMessage, không thêm đường ghi) và lịch FollowUpSchedule của quy tắc
// follow_up (tắt mặc định) tự gửi một bước bằng lệnh AutoSendKind.

// FollowUp là một hội thoại cần nhắn lại: Page, khách (CustomerName là dữ liệu cá nhân), mốc tin nhân viên cuối, mốc
// cửa sổ 24 giờ đóng.
type FollowUp = automation.FollowUpItem

// FollowUpSchedule là lịch theo tenant (300 giây) chạy quy tắc nhắn lại.
const FollowUpSchedule = automation.FollowUpSchedule

// AutoSendKind là lệnh gửi tin tự động vào hội thoại hộp thư (hạn 5 phút, không rõ đã gửi thì bỏ).
const AutoSendKind = automation.KindAutoSendCommand

// ListFollowUps trả tối đa limit hội thoại cần nhắn lại lúc now trên các Page u được đọc (pageID khác nil: chỉ Page đó,
// không được đọc → rỗng), khách im quá afterHours giờ (1–23, ngoài → ErrInvalidInput); cửa sổ đóng sớm trước.
func ListFollowUps(ctx context.Context, tx pgx.Tx, u identity.AuthUser, pageID *uuid.UUID, afterHours, limit int,
	now time.Time,
) ([]FollowUp, error) {
	out, err := automation.ListFollowUps(ctx, tx, u, pageID, afterHours, limit, now)
	if errors.Is(err, automation.ErrInvalidFollowUp) {
		return nil, ErrInvalidInput
	}
	return out, err //nolint:wrapcheck // automation đã bọc
}
