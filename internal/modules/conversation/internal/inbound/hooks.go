package inbound

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Hook của đường tin vào mà task sau hiện thực (khung của Task 153; chạy trong tx của sự kiện vào, sau khi đã ghi tin
// và cập nhật hội thoại, trước báo hiệu). Nhắn lại khách (Task 168) không cần hook: danh sách tự loại hội thoại khách
// đã trả lời.

// onNewConversation chạy khi hội thoại inbox vừa được tạo: giao việc xoay vòng (assign.OnNewConversation, Task 160),
// lấy lịch sử tin cũ (history.OnNewConversation, Task 164). Khung: không làm gì.
func onNewConversation(context.Context, pgx.Tx, uuid.UUID) error { return nil }

// onInboxMessage chạy sau mỗi tin khách mới (inserted: hội thoại vừa tạo): lời chào tự động
// (automation.OnInboxMessage, Task 167). Khung: không làm gì.
func onInboxMessage(context.Context, pgx.Tx, uuid.UUID, bool) error { return nil }

// onCommentCreated chạy sau mỗi comment khách mới msgID (chưa xóa): tự ẩn (Task 165), tự trả lời, tự nhắn riêng
// (Task 166) qua automation.OnCommentCreated. Sự kiện tới bằng đường lịch sử (ViaHistory) không kích hoạt quy tắc.
func onCommentCreated(ctx context.Context, tx pgx.Tx, d deps.Deps, e webhooks.Inbound, acc channel.ChannelAccount,
	p channel.CommentPayload, msgID, convID uuid.UUID, isLive bool,
) error {
	return automation.OnCommentCreated(ctx, tx, d, msgID, automation.Trigger{ //nolint:wrapcheck // automation đã bọc
		Account: acc, ConversationID: convID, OccurredAt: p.CreatedAt.UTC(), FromPage: p.FromPage,
		IsHistory: e.Via == webhooks.ViaHistory, IsLive: isLive, Text: p.Text, CommentExternalID: p.CommentID,
		PostExternalID: p.PostID,
	})
}

// onLiveComment chạy sau comment khách mới thuộc phiên live: chốt từ comment live (live.OnComment, Task 172). Khung:
// không làm gì.
func onLiveComment(context.Context, pgx.Tx, uuid.UUID) error { return nil }
