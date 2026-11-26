package conversation

import (
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
)

// Chủ đề báo hiệu realtime của M4 (Global Constraints "Báo hiệu realtime"): mọi báo hiệu đòi conversation.read với
// phạm vi page:<channel_accounts.id> (Q36), thực thể là mã bản ghi, không có nội dung.
const (
	TopicConversationChanged = deps.TopicConversationChanged
	TopicPresence            = "conversation.presence"
	TopicLiveComments        = "live.comments"
	TopicReminderFired       = "reminder.fired"
	TopicChannelStatus       = channel.TopicStatus // tài khoản kênh đổi cài đặt, trạng thái (channel phát)
)

// Sự kiện nội bộ của module (Q8): chỉ conversation.assigned được phát ở M4; hai sự kiện còn lại chỉ phát khi có module
// đăng ký nghe (báo cáo, AI).
const (
	EventConversationAssigned = "conversation.assigned"
	EventMessageReceived      = "message.received"
	EventCommentReceived      = "comment.received"
)
