package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/send"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Ghi chú nội bộ trong hội thoại (Task 161; CH07): không bao giờ gửi ra ngoài. Hiện thực ở internal/send.

// Note là một ghi chú nội bộ vừa ghi; Body là dữ liệu cá nhân.
type Note = send.Note

// ErrNoteInvalid là lỗi: ghi chú rỗng hay quá 4.000 ký tự (400).
var ErrNoteInvalid = send.ErrInvalidNote

// AddNote ghi ghi chú nội bộ text của u vào hội thoại conversationID lúc now (xem send.AddNote): cần conversation.reply
// trên Page của hội thoại (không có → ErrNotFound); không tạo lệnh, không job; báo hiệu conversation.changed qua bus.
func AddNote(ctx context.Context, tx pgx.Tx, bus realtime.Bus, u identity.AuthUser, conversationID uuid.UUID, text string,
	now time.Time,
) (Note, error) {
	return send.AddNote(ctx, tx, bus, u, conversationID, text, now) //nolint:wrapcheck // sentinel của phần
}
