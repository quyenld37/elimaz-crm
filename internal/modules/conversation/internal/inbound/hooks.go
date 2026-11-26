package inbound

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// onCommentCreated chạy sau mỗi comment khách mới (chưa xóa): tự ẩn, tự trả lời, tự nhắn riêng
// (automation.OnCommentCreated, Task 165, 166). Khung: không làm gì.
func onCommentCreated(context.Context, pgx.Tx, uuid.UUID) error { return nil }

// onLiveComment chạy sau comment khách mới thuộc phiên live: chốt từ comment live (live.OnComment, Task 172). Khung:
// không làm gì.
func onLiveComment(context.Context, pgx.Tx, uuid.UUID) error { return nil }
