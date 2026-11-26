package assign

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// RoundRobin là xoay vòng người phụ trách (Q15, 09 mục E; tắt mặc định): hook của đường tin vào khi hội thoại vừa
// được tạo. Chạy trong tx của sự kiện vào, sau khi dòng hội thoại mới đã chèn (ngoại lệ thứ tự khóa ghi ở ADR-46:
// page_settings khóa sau dòng hội thoại mới, chưa đường nào khác thấy).
type RoundRobin struct {
	W Writer
}

// OnNewConversation giao hội thoại id cho người kế tiếp của danh sách xoay vòng Page (ứng viên: trong danh sách, active,
// có conversation.read trên Page), sau người vừa được giao; không bật, không ai → không giao. Hội thoại đã có người
// phụ trách → không đổi gì.
func (r RoundRobin) OnNewConversation(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	q := store.New(tx)
	c, err := lockConversation(ctx, q, id)
	if err != nil {
		return err
	}
	if c.AssigneeID.Valid {
		return nil
	}
	s, err := q.LockPageSettings(ctx, c.ChannelAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("khóa cài đặt Page: %w", err)
	}
	if !s.RoundRobinEnabled || len(s.RoundRobinUserIds) == 0 {
		return nil
	}
	staff, err := identity.StaffWithPermissionAt(ctx, tx, PermRead, *access.PageScope(c.ChannelAccountID))
	if err != nil {
		return fmt.Errorf("đọc ứng viên xoay vòng: %w", err)
	}
	ok := make(map[uuid.UUID]bool, len(staff))
	for _, u := range staff {
		ok[u.ID] = true
	}
	next, found := NextInRotation(s.RoundRobinUserIds, PtrUUID(s.RoundRobinLastUserID), func(u uuid.UUID) bool { return ok[u] })
	if !found {
		return nil
	}
	if err := q.SetRoundRobinLast(ctx, store.SetRoundRobinLastParams{
		UserID: pgtype.UUID{Bytes: next, Valid: true}, ChannelAccountID: c.ChannelAccountID,
	}); err != nil {
		return fmt.Errorf("ghi con trỏ xoay vòng: %w", err)
	}
	return r.W.change(ctx, tx, c, &next, nil, ReasonRoundRobin)
}

// NextInRotation trả người đầu tiên thỏa eligible trong users, bắt đầu ngay sau last (last nil hay không còn trong
// danh sách → từ đầu), vòng lại đầu danh sách; không ai thỏa → false.
func NextInRotation(users []uuid.UUID, last *uuid.UUID, eligible func(uuid.UUID) bool) (uuid.UUID, bool) {
	start := 0
	if last != nil {
		if i := slices.Index(users, *last); i >= 0 {
			start = i + 1
		}
	}
	for k := range users {
		u := users[(start+k)%len(users)]
		if eligible(u) {
			return u, true
		}
	}
	return uuid.Nil, false
}
