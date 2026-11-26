package inbound

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/history"
)

// Hook của đường tin vào mà task sau hiện thực (khung của Task 153; chạy trong tx của sự kiện vào, sau khi đã ghi tin
// và cập nhật hội thoại, trước báo hiệu). Nhắn lại khách (Task 168) không cần hook: danh sách tự loại hội thoại khách
// đã trả lời.

// onNewConversation chạy khi hội thoại id (inbox: hộp thư; ngược lại comment) của Page acc vừa được tạo: hội thoại hộp
// thư lấy lịch sử tin cũ (history.OnNewConversation, Task 164). Giao việc xoay vòng (assign.OnNewConversation, Task
// 160) chưa nối: tệp task-160, Việc phát hiện thêm 1.
func (h *handler) onNewConversation(ctx context.Context, tx pgx.Tx, acc channel.ChannelAccount, id uuid.UUID, inbox bool) error {
	if !inbox {
		return nil
	}
	return history.OnNewConversation(ctx, tx, h.d, acc, id) //nolint:wrapcheck // history đã bọc việc đang làm
}

// onInboxMessage chạy sau mỗi tin khách mới (inserted: hội thoại vừa tạo): lời chào tự động
// (automation.OnInboxMessage, Task 167). Khung: không làm gì.
func onInboxMessage(context.Context, pgx.Tx, uuid.UUID, bool) error { return nil }

// isLiveComment cho biết comment lúc at trên bài postID có thuộc một phiên live đang mở không
// (live.IsLiveComment, Task 170). Khung: không phải comment live.
func isLiveComment(context.Context, pgx.Tx, uuid.UUID, time.Time) (bool, error) { return false, nil }

// onCommentCreated chạy sau mỗi comment khách mới (chưa xóa): tự ẩn, tự trả lời, tự nhắn riêng
// (automation.OnCommentCreated, Task 165, 166). Khung: không làm gì.
func onCommentCreated(context.Context, pgx.Tx, uuid.UUID) error { return nil }

// onLiveComment chạy sau comment khách mới thuộc phiên live: chốt từ comment live (live.OnComment, Task 172). Khung:
// không làm gì.
func onLiveComment(context.Context, pgx.Tx, uuid.UUID) error { return nil }
