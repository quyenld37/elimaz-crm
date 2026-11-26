package channel

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// permConversationRead là quyền mà báo hiệu của tài khoản kênh đòi (Q36: theo Page).
const permConversationRead = identity.Permission("conversation.read")

// Writer là đường ghi cài đặt tài khoản kênh do nhân viên đổi (module mẫu catalog.Writer): mỗi lần ghi khóa dòng rồi
// so version, ghi audit (RequestID của yêu cầu), báo hiệu channel.status theo Page và (khi bật gửi thật) lệnh cảnh báo,
// tất cả trong tx của bên gọi. Dựng mới cho mỗi yêu cầu; không giữ trạng thái giữa các lần chạy lại.
type Writer struct {
	// Realtime phát báo hiệu trong tx (nil: không phát).
	Realtime  realtime.Bus
	RequestID string
}

// SetSendMode đổi chế độ gửi của tài khoản id sang mode (read_only, enabled) khi expectedVersion khớp; version tăng
// 1, audit channel.send_mode_changed. Chuyển sang enabled ghi lệnh cảnh báo thường send-mode:<id> (Page bắt đầu gửi
// thật). Cùng giá trị hiện tại → trả tài khoản, không đổi gì. Lỗi: ErrInvalidSendMode, ErrAccountNotFound,
// ErrVersionConflict. Mốc sau (M7) mở rộng chính hàm này, không thêm đường thứ hai (B13).
func (w Writer) SetSendMode(ctx context.Context, tx pgx.Tx, id uuid.UUID, mode string, expectedVersion int32,
	actor identity.AuthUser,
) (ChannelAccount, error) {
	if mode != SendModeReadOnly && mode != SendModeEnabled {
		return ChannelAccount{}, ErrInvalidSendMode
	}
	return w.change(ctx, tx, id, expectedVersion, actor, change{
		action: "channel.send_mode_changed", field: "send_mode",
		same: func(a ChannelAccount) bool { return a.SendMode == mode },
		old:  func(a ChannelAccount) any { return a.SendMode }, val: mode,
		apply: func(q *store.Queries) (int32, error) {
			return q.SetSendMode(ctx, store.SetSendModeParams{SendMode: mode, ID: id})
		},
		after: func(a ChannelAccount) error {
			if mode != SendModeEnabled {
				return nil
			}
			return enqueueSendModeAlert(ctx, tx, a)
		},
	})
}

// SetHumanAgent bật, tắt trả lời "nhân viên" (thẻ HUMAN_AGENT, Q19) của tài khoản id khi expectedVersion khớp; như
// SetSendMode (audit channel.human_agent_changed), không có lệnh cảnh báo.
func (w Writer) SetHumanAgent(ctx context.Context, tx pgx.Tx, id uuid.UUID, enabled bool, expectedVersion int32,
	actor identity.AuthUser,
) (ChannelAccount, error) {
	return w.change(ctx, tx, id, expectedVersion, actor, change{
		action: "channel.human_agent_changed", field: "human_agent_enabled",
		same: func(a ChannelAccount) bool { return a.HumanAgentEnabled == enabled },
		old:  func(a ChannelAccount) any { return a.HumanAgentEnabled }, val: enabled,
		apply: func(q *store.Queries) (int32, error) {
			return q.SetHumanAgent(ctx, store.SetHumanAgentParams{HumanAgentEnabled: enabled, ID: id})
		},
	})
}

// change là một lần đổi một cột cài đặt: field là tên cột trong audit, old/val là giá trị trước/sau, apply ghi và trả
// version mới, after chạy sau khi ghi (nil: không có).
type change struct {
	action, field string
	same          func(ChannelAccount) bool
	old           func(ChannelAccount) any
	val           any
	apply         func(*store.Queries) (int32, error)
	after         func(ChannelAccount) error
}

func (w Writer) change(ctx context.Context, tx pgx.Tx, id uuid.UUID, expectedVersion int32, actor identity.AuthUser,
	c change,
) (ChannelAccount, error) {
	q := store.New(tx)
	r, err := q.LockAccount(ctx, id)
	if err != nil {
		return ChannelAccount{}, notFound(err, "khóa tài khoản kênh")
	}
	a := accountFrom(accountRow(r))
	if a.Version != expectedVersion {
		return ChannelAccount{}, ErrVersionConflict
	}
	if c.same(a) {
		return a, nil
	}
	before := c.old(a)
	if _, err := c.apply(q); err != nil {
		return ChannelAccount{}, fmt.Errorf("ghi %s: %w", c.field, err)
	}
	after, err := Account(ctx, tx, id)
	if err != nil {
		return ChannelAccount{}, err
	}
	if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
		TenantID: actor.TenantID, ActorType: "staff", ActorID: actor.UserID.String(), Action: c.action,
		EntityType: EntityAccount, EntityID: id.String(), Before: map[string]any{c.field: before},
		After: map[string]any{c.field: c.val}, RequestID: w.RequestID,
	}); err != nil {
		return ChannelAccount{}, fmt.Errorf("ghi audit %s: %w", c.action, err)
	}
	if err := w.signal(ctx, tx, after); err != nil {
		return ChannelAccount{}, err
	}
	if c.after != nil {
		if err := c.after(after); err != nil {
			return ChannelAccount{}, err
		}
	}
	return after, nil
}

// signal phát channel.status của tài khoản a trong tx (chỉ người có conversation.read trên Page nhận).
func (w Writer) signal(ctx context.Context, tx pgx.Tx, a ChannelAccount) error {
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: a.TenantID, Topic: TopicStatus, Requires: permConversationRead,
		Scope:  &identity.Scope{Type: identity.ScopePage, ID: a.ID.String()},
		Entity: realtime.Entity{Type: EntityAccount, ID: a.ID.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu tài khoản kênh: %w", err)
	}
	return nil
}
