-- Hội thoại (Task 151; 04 mục 7). Trang theo khóa (last_message_at, id) giảm dần: trang đầu gửi mốc rất xa và
-- uuid lớn nhất.

-- name: ListConversationsByPage :many
-- Hội thoại của một Page theo trạng thái, mới nhất trước (index conversations_page_status).
SELECT id, channel_account_id, customer_id, kind, external_thread_ref, post_id, status, assignee_id, last_message_at,
       last_customer_message_at, unread_count, last_snippet, version
FROM conversation.conversations
WHERE channel_account_id = @channel_account_id
  AND status = @status
  AND (last_message_at, id) < (@before_at::timestamptz, @before_id::uuid)
ORDER BY last_message_at DESC, id DESC
LIMIT @lim::int;

-- name: ListConversationsByAssignee :many
-- Hội thoại được giao cho một nhân viên theo trạng thái, mới nhất trước (index conversations_assignee_status).
SELECT id, channel_account_id, customer_id, kind, external_thread_ref, post_id, status, assignee_id, last_message_at,
       last_customer_message_at, unread_count, last_snippet, version
FROM conversation.conversations
WHERE assignee_id = @assignee_id::uuid
  AND status = @status
  AND (last_message_at, id) < (@before_at::timestamptz, @before_id::uuid)
ORDER BY last_message_at DESC, id DESC
LIMIT @lim::int;
