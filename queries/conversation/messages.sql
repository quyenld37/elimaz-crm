-- Tin (Task 151; 04 mục 7).

-- name: ListMessages :many
-- Tin của một hội thoại, mới nhất trước, trang theo (sent_at, id) giảm dần (index messages_conversation).
SELECT id, conversation_id, channel_account_id, external_id, client_ref, direction, kind, origin, parent_external_id,
       post_id, body, attachments, sender_staff_id, sender_name, send_status, fail_reason, is_hidden, removed_at,
       edited_at, is_live, delivered_at, read_at, sent_at
FROM conversation.messages
WHERE conversation_id = @conversation_id
  AND (sent_at, id) < (@before_at::timestamptz, @before_id::uuid)
ORDER BY sent_at DESC, id DESC
LIMIT @lim::int;

-- name: ListLiveComments :many
-- Comment live của một bài theo thứ tự đến, sau mốc (sent_at, id) (index một phần messages_live_by_post).
SELECT id, conversation_id, channel_account_id, external_id, parent_external_id, body, sender_name, is_hidden,
       removed_at, sent_at
FROM conversation.messages
WHERE post_id = @post_id::uuid
  AND is_live
  AND (sent_at, id) > (@after_at::timestamptz, @after_id::uuid)
ORDER BY sent_at, id
LIMIT @lim::int;
