-- Đọc comment định kỳ (kế hoạch M4, Task 150, Q4): chế độ của các Page; Page không có dòng cài đặt dùng mặc định
-- 'auto' của cột (bên gọi tự điền).

-- name: CommentPollModes :many
SELECT channel_account_id, comment_poll_mode
FROM conversation.page_settings
WHERE channel_account_id = ANY(@ids::uuid[]);
