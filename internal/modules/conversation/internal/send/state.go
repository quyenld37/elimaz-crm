package send

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// conversationRow là phần của hội thoại SendPolicy cần (câu khóa LockConversationForSend cùng cột, đổi kiểu được).
type conversationRow = store.GetConversationForSendRow

// state dựng trạng thái hội thoại cho SendPolicy của tin thường (Purpose message): chỉ xem theo chế độ gửi của Page,
// Human Agent theo cờ của Page; automated: tin do quy tắc tự động gửi (không được dùng thẻ nhân viên).
func state(c conversationRow, acc channel.ChannelAccount, automated bool) channel.ConversationState {
	s := channel.ConversationState{
		Purpose: channel.PurposeMessage, Kind: c.Kind, BlockedByUser: c.BlockedByUser,
		ReadOnly: acc.SendMode != channel.SendModeEnabled, HumanAgentEnabled: acc.HumanAgentEnabled, Automated: automated,
	}
	if c.LastCustomerMessageAt.Valid {
		t := c.LastCustomerMessageAt.Time.UTC()
		s.LastCustomerMessageAt = &t
	}
	return s
}

// Mã lý do của tin failed, expired (cột fail_reason; tệp task-156, Quyết định 3): DenyReason của kênh hoặc mã dưới.
const (
	reasonAuth          = "auth"            // kênh báo token hỏng
	reasonRateLimited   = "rate_limited"    // kênh báo quá giới hạn, chờ rồi gửi lại
	reasonNotSent       = "not_sent"        // chưa tới được kênh, gửi lại
	reasonRejected      = "rejected"        // kênh từ chối yêu cầu (dữ liệu sai…)
	reasonNoAdapter     = "no_adapter"      // tiến trình không có adapter của kênh
	reasonNoAttachment  = "attachment_gone" // ảnh không còn biến thể công khai
	reasonInDoubt       = "in_doubt"        // không rõ kênh đã nhận chưa
	reasonExpiredQueued = "expired"         // quá 3 phút chưa gửi được
)

// failTexts là câu tiếng Việt của từng mã lý do (màn hình hiện dưới tin).
//
//nolint:gochecknoglobals // bảng tra hằng, chỉ đọc
var failTexts = map[string]string{
	string(channel.DenyWindowClosed):   "Đã quá 24 giờ kể từ tin cuối của khách: Facebook không cho gửi tin thường",
	string(channel.DenyBlockedByUser):  "Khách đã chặn tin nhắn từ Page",
	string(channel.DenyReadOnly):       "Page đang ở chế độ chỉ xem, tin chưa được gửi",
	string(channel.DenyNoPermission):   "Page chưa có quyền gửi tin (cần kết nối lại Page)",
	string(channel.DenyCommentRemoved): "Comment đã bị xóa",
	reasonAuth:                         "Token của Page hết hiệu lực: quản trị cần kết nối lại Page",
	reasonRateLimited:                  "Facebook đang giới hạn số tin gửi, thử lại sau ít phút",
	reasonNotSent:                      "Chưa gửi được tới Facebook",
	reasonRejected:                     "Facebook từ chối tin này",
	reasonNoAdapter:                    "Kênh của Page chưa được bật trên máy chủ",
	reasonNoAttachment:                 "Ảnh không còn trên kho tệp",
	reasonInDoubt:                      "Không rõ tin đã tới khách chưa",
	reasonExpiredQueued:                "Quá 3 phút chưa gửi được",
}

// FailReasonText trả câu tiếng Việt của mã lý do code (fail_reason của tin); mã lạ → câu chung.
func FailReasonText(code string) string {
	if s, ok := failTexts[code]; ok {
		return s
	}
	return "Chưa gửi được tin"
}

// publish phát conversation.changed cho hội thoại id của Page account (đòi conversation.read trên Page, chỉ đi khi tx
// commit; thực thể là mã hội thoại, không nội dung).
func publish(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenant, account, id uuid.UUID) error {
	if bus == nil {
		return nil
	}
	if err := bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: deps.TopicConversationChanged, Requires: permRead, Scope: access.PageScope(account),
		Entity: realtime.Entity{Type: deps.EntityConversation, ID: id.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu hội thoại đổi: %w", err)
	}
	return nil
}
