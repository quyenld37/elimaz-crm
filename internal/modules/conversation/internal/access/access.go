// Package access là phần của module conversation kiểm quyền theo Page (conversation.read, conversation.reply…, phạm vi
// page:<channel_accounts.id>, PQ04): danh sách Page người dùng được xem, kiểm quyền trên Page của đối tượng; không có
// quyền thì trả "không có" (404) để không lộ sự tồn tại. Khung của Task 143; hiện thực ở Task 151.
package access

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// ErrNotFound là lỗi khi người dùng không có quyền trên Page của đối tượng; lớp HTTP trả 404 như khi đối tượng không có, để
// không lộ sự tồn tại.
var ErrNotFound = errors.New("conversation: không có")

// ErrAssigneeNotAllowed là lỗi khi người được giao (hội thoại, nhắc việc) không phải nhân viên đang làm có quyền trên
// Page của hội thoại (422); một sentinel chung cho phần assign (Task 160) và reminders (Task 169).
var ErrAssigneeNotAllowed = errors.New("conversation: người được giao không có quyền trên hội thoại")

// PageScope trả phạm vi page:<accountID> để kiểm quyền theo Page.
func PageScope(accountID uuid.UUID) *identity.Scope {
	return &identity.Scope{Type: identity.ScopePage, ID: accountID.String()}
}

// Require trả nil khi u có quyền p trên Page accountID (grant phạm vi all hoặc đúng Page), ngược lại ErrNotFound
// (mặc định từ chối, ADR-18).
func Require(u identity.AuthUser, p identity.Permission, accountID uuid.UUID) error {
	if identity.Can(u, p, PageScope(accountID)) {
		return nil
	}
	return ErrNotFound
}

// VisiblePages trả các Page u được đọc hội thoại (conversation.read): all = true khi có grant phạm vi all; ngược lại
// ids là mã Page (đã sắp, không trùng) từ grant phạm vi page. Grant phạm vi khác (kho) và mã Page hỏng bị bỏ. Không
// grant nào: false, rỗng.
func VisiblePages(u identity.AuthUser) (all bool, ids []uuid.UUID) {
	const read identity.Permission = "conversation.read"
	for _, g := range u.Grants {
		if g.Permission != read {
			continue
		}
		switch {
		case g.Scope == identity.Scope{Type: identity.ScopeAll}:
			return true, nil
		case g.Scope.Type == identity.ScopePage:
			if id, err := uuid.Parse(g.Scope.ID); err == nil {
				ids = append(ids, id)
			}
		}
	}
	slices.SortFunc(ids, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	return false, slices.Compact(ids)
}

// Signal phát báo hiệu topic cho thực thể (kind, id) trên Page pageID: người có conversation.read trên Page đó nhận,
// không mang nội dung. bus nil: không làm gì (dùng chung cho các phần của conversation).
func Signal(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenantID uuid.UUID, topic string, pageID uuid.UUID,
	kind string, id uuid.UUID,
) error {
	if bus == nil {
		return nil
	}
	if err := bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenantID, Topic: topic, Requires: "conversation.read", Scope: PageScope(pageID),
		Entity: realtime.Entity{Type: kind, ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", topic, err)
	}
	return nil
}
