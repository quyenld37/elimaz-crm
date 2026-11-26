package inbound

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/automation"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Hook của đường tin vào mà task sau hiện thực (khung của Task 153; chạy trong tx của sự kiện vào, sau khi đã ghi tin
// và cập nhật hội thoại, trước báo hiệu). Nhắn lại khách (Task 168) không cần hook: danh sách tự loại hội thoại khách
// đã trả lời.

// onNewConversation chạy khi hội thoại id (inbox: hộp thư; ngược lại comment) của Page acc vừa được tạo: giao xoay vòng
// nếu Page bật (assign.RoundRobin, Task 160; nối ở Task 159; chưa có người phụ trách là điều kiện bên trong), rồi hội
// thoại hộp thư lấy lịch sử tin cũ (history.OnNewConversation, Task 164). Sự kiện conversation.assigned ghi bằng sổ sự
// kiện, hàng đợi của đường vào; báo hiệu do đường vào phát.
func (h *handler) onNewConversation(ctx context.Context, tx pgx.Tx, acc channel.ChannelAccount, id uuid.UUID, inbox bool) error {
	rr := assign.RoundRobin{W: assign.Writer{Events: h.d.Events, Queue: h.d.Queue}}
	if err := rr.OnNewConversation(ctx, tx, id); err != nil {
		return err //nolint:wrapcheck // assign đã bọc việc đang làm
	}
	if !inbox {
		return nil
	}
	return history.OnNewConversation(ctx, tx, h.d, acc, id) //nolint:wrapcheck // history đã bọc việc đang làm
}

// onInboxMessage chạy sau mỗi tin khách mới msgID của hội thoại hộp thư convID: tin chào tự động
// (automation.OnInboxMessage, Task 167; chỉ khi khách nhắn Page lần đầu). Sự kiện tới bằng đường lịch sử (ViaHistory)
// không kích hoạt quy tắc.
func onInboxMessage(ctx context.Context, tx pgx.Tx, d deps.Deps, e webhooks.Inbound, acc channel.ChannelAccount,
	p channel.MessagePayload, msgID, convID uuid.UUID,
) error {
	return automation.OnInboxMessage(ctx, tx, d, msgID, automation.Trigger{ //nolint:wrapcheck // automation đã bọc
		Account: acc, ConversationID: convID, OccurredAt: p.SentAt.UTC(), IsHistory: e.Via == webhooks.ViaHistory,
		Text: p.Text,
	})
}

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
