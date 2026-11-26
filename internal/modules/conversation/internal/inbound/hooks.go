package inbound

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/assign"
)

// Hook của đường tin vào mà task sau hiện thực (khung của Task 153; chạy trong tx của sự kiện vào, sau khi đã ghi tin
// và cập nhật hội thoại, trước báo hiệu). Nhắn lại khách (Task 168) không cần hook: danh sách tự loại hội thoại khách
// đã trả lời.

// onNewConversation chạy khi hội thoại (inbox hay comment) vừa được tạo: giao xoay vòng nếu Page bật (assign.RoundRobin, Task 160;
// nối ở Task 159; chưa có người phụ trách là điều kiện bên trong); lấy lịch sử tin cũ (history.OnNewConversation, Task
// 164) thêm sau. Sự kiện conversation.assigned ghi bằng sổ sự kiện, hàng đợi của đường vào; báo hiệu do đường vào phát.
func (h *handler) onNewConversation(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	rr := assign.RoundRobin{W: assign.Writer{Events: h.d.Events, Queue: h.d.Queue}}
	return rr.OnNewConversation(ctx, tx, id) //nolint:wrapcheck // assign đã bọc việc đang làm
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
