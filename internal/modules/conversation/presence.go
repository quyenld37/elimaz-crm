package conversation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/presence"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// ErrNotFound là lỗi khi hội thoại không có, của tenant khác, hoặc người gọi không có quyền trên Page của nó (lớp HTTP trả 404
// để không lộ sự tồn tại).
var ErrNotFound = access.ErrNotFound

// PresenceState là việc một nhân viên đang làm trong hội thoại (Task 163).
type PresenceState = presence.State

// Ba trạng thái có mặt.
const (
	PresenceViewing       = presence.Viewing
	PresenceTyping        = presence.Typing
	PresenceDraftingOrder = presence.DraftingOrder
)

// PresenceEntry là một người khác đang có mặt trong hội thoại.
type PresenceEntry = presence.Entry

// PresenceCleanupSchedule là tên lịch xóa dòng có mặt hết hạn (60 s, theo tenant).
const PresenceCleanupSchedule = presence.CleanupSchedule

// SetPresence ghi nhịp tim của u trong hội thoại convID (trạng thái s, hạn now + 30 s) trong tx của bên gọi; phát báo
// hiệu TopicPresence qua bus chỉ khi trạng thái của u đổi hoặc u mới xuất hiện. Không có quyền → ErrNotFound.
func SetPresence(ctx context.Context, tx pgx.Tx, bus realtime.Bus, u identity.AuthUser, convID uuid.UUID,
	s PresenceState, now time.Time,
) error {
	if err := presence.Set(ctx, tx, bus, u, convID, s, now); err != nil {
		return fmt.Errorf("ghi có mặt trong hội thoại: %w", err)
	}
	return nil
}

// ListPresence trả những người khác (trừ u) còn hạn lúc now trong hội thoại convID. Không có quyền → ErrNotFound.
func ListPresence(ctx context.Context, tx pgx.Tx, u identity.AuthUser, convID uuid.UUID, now time.Time) ([]PresenceEntry, error) {
	out, err := presence.List(ctx, tx, u, convID, now)
	if err != nil {
		return nil, fmt.Errorf("đọc người có mặt trong hội thoại: %w", err)
	}
	return out, nil
}
