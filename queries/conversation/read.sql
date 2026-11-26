-- Đọc hộp chat (Task 159; 04 mục 7): tin theo trang, đánh dấu đã đọc, đổi trạng thái, đơn nháp, số đếm.
-- Thứ tự khóa nhóm 6 ADR-46: chỉ khóa một dòng conversations (LockConversationForAssign); tin chỉ đọc.

-- name: ListChatMessagesBefore :many
-- Tin của hội thoại trước mốc (sent_at, id), mới nhất trước (index messages_conversation). Kèm tên nhân viên gửi
-- (users_v), tin riêng đang sống của comment (index một phần messages_one_live_private_reply), tin cha của comment trả
-- lời (messages_external_key), số lần sửa nội dung (message_revisions_message: mỗi dòng giữ nội dung trước lần đổi; dòng
-- mà nội dung sau nó — dòng kế hay nội dung hiện tại — khác là một lần sửa; dòng ẩn, hiện, xóa giữ nguyên nội dung).
SELECT m.id, m.conversation_id, m.direction, m.kind, m.origin, m.body, m.attachments, m.sender_staff_id,
       coalesce(u.display_name, '')::text AS sender_staff_name, m.sender_name, m.send_status, m.fail_reason,
       m.is_hidden, (m.removed_at IS NOT NULL)::boolean AS removed,
       (SELECT count(*) FROM (SELECT r.body, lead(r.body, 1, m.body) OVER (ORDER BY r.recorded_at, r.id) AS after_body
                              FROM conversation.message_revisions AS r WHERE r.message_id = m.id) AS x
        WHERE x.body IS DISTINCT FROM x.after_body)::int AS edited_count,
       pr.id AS private_reply_id, pr.send_status AS private_reply_status, par.id AS parent_id,
       m.sent_at, m.delivered_at, m.read_at, m.quick_replies
FROM conversation.messages AS m
LEFT JOIN identity.users_v AS u ON u.id = m.sender_staff_id
LEFT JOIN conversation.messages AS pr
       ON m.kind = 'comment' AND pr.tenant_id = m.tenant_id AND pr.channel_account_id = m.channel_account_id
      AND pr.parent_external_id = m.external_id AND pr.kind = 'private_reply'
      AND pr.send_status IN ('queued', 'sent', 'checking', 'in_doubt')
LEFT JOIN conversation.messages AS par
       ON m.kind = 'comment' AND par.tenant_id = m.tenant_id AND par.channel_account_id = m.channel_account_id
      AND par.external_id = m.parent_external_id
WHERE m.conversation_id = @conversation_id
  AND (m.sent_at, m.id) < (@at::timestamptz, @msg_id::uuid)
ORDER BY m.sent_at DESC, m.id DESC
LIMIT @lim::int;

-- name: ListChatMessagesAfter :many
-- Như ListChatMessagesBefore nhưng sau mốc (sent_at, id), cũ nhất trước (lấy tin mới).
SELECT m.id, m.conversation_id, m.direction, m.kind, m.origin, m.body, m.attachments, m.sender_staff_id,
       coalesce(u.display_name, '')::text AS sender_staff_name, m.sender_name, m.send_status, m.fail_reason,
       m.is_hidden, (m.removed_at IS NOT NULL)::boolean AS removed,
       (SELECT count(*) FROM (SELECT r.body, lead(r.body, 1, m.body) OVER (ORDER BY r.recorded_at, r.id) AS after_body
                              FROM conversation.message_revisions AS r WHERE r.message_id = m.id) AS x
        WHERE x.body IS DISTINCT FROM x.after_body)::int AS edited_count,
       pr.id AS private_reply_id, pr.send_status AS private_reply_status, par.id AS parent_id,
       m.sent_at, m.delivered_at, m.read_at, m.quick_replies
FROM conversation.messages AS m
LEFT JOIN identity.users_v AS u ON u.id = m.sender_staff_id
LEFT JOIN conversation.messages AS pr
       ON m.kind = 'comment' AND pr.tenant_id = m.tenant_id AND pr.channel_account_id = m.channel_account_id
      AND pr.parent_external_id = m.external_id AND pr.kind = 'private_reply'
      AND pr.send_status IN ('queued', 'sent', 'checking', 'in_doubt')
LEFT JOIN conversation.messages AS par
       ON m.kind = 'comment' AND par.tenant_id = m.tenant_id AND par.channel_account_id = m.channel_account_id
      AND par.external_id = m.parent_external_id
WHERE m.conversation_id = @conversation_id
  AND (m.sent_at, m.id) > (@at::timestamptz, @msg_id::uuid)
ORDER BY m.sent_at, m.id
LIMIT @lim::int;

-- name: GetMessageMark :one
-- Mốc (sent_at, id) của tin id trong hội thoại conversation_id (không thuộc hội thoại → không dòng).
SELECT sent_at, id
FROM conversation.messages
WHERE id = @id
  AND conversation_id = @conversation_id;

-- name: GetUnreadCount :one
SELECT unread_count FROM conversation.conversations WHERE id = @id;

-- name: SetUnreadAfter :one
-- Số chưa đọc của hội thoại đã khóa = số tin khách sau mốc (at, msg_id) (hộp thư dùng chung), không bao giờ tăng (mốc cũ
-- hơn chỗ người khác đã đọc tới không làm hội thoại chưa đọc lại); không tăng version.
UPDATE conversation.conversations AS c
SET unread_count = LEAST(c.unread_count,
                         (SELECT count(*) FROM conversation.messages AS m
                          WHERE m.conversation_id = c.id AND m.direction = 'in'
                            AND (m.sent_at, m.id) > (@at::timestamptz, @msg_id::uuid))::int),
    updated_at   = now()
WHERE c.id = @id
RETURNING unread_count;

-- name: SetConversationStatus :one
-- Đổi trạng thái hội thoại đã khóa khi khác trạng thái hiện tại, tăng version; cùng trạng thái → không dòng.
UPDATE conversation.conversations
SET status     = @status,
    version    = version + 1,
    updated_at = now()
WHERE id = @id
  AND status <> @status
RETURNING status, version;

-- name: TagAlive :one
-- Nhãn id còn sống (bộ lọc theo nhãn đã xóa → danh sách rỗng).
SELECT EXISTS (SELECT 1 FROM conversation.tags WHERE id = @id AND deleted_at IS NULL)::boolean;

-- name: ConversationRowExtras :many
-- Phần hiển thị của các dòng danh sách ids: tên khách của hồ sơ gắn hội thoại (customers_v), tên người phụ trách
-- (users_v), đoạn bài viết, có đơn nháp gắn (ord.orders_v, mọi người bán; index một phần orders_draft_expires).
SELECT c.id, coalesce(cu.full_name, '')::text AS customer_name, coalesce(u.display_name, '')::text AS assignee_name,
       p.snippet AS post_snippet,
       EXISTS (SELECT 1 FROM ord.orders_v AS o WHERE o.conversation_id = c.id AND o.status = 'draft')::boolean AS has_open_draft
FROM conversation.conversations AS c
LEFT JOIN customer.customers_v AS cu ON cu.id = c.customer_id
LEFT JOIN identity.users_v AS u ON u.id = c.assignee_id
LEFT JOIN conversation.posts AS p ON p.tenant_id = c.tenant_id AND p.id = c.post_id
WHERE c.id = ANY(@ids::uuid[]);

-- name: CountConversationsByPage :many
-- Theo Page được xem: số hội thoại còn tin chưa đọc (trừ spam), số hội thoại đang mở.
SELECT c.channel_account_id,
       count(*) FILTER (WHERE c.unread_count > 0 AND c.status <> 'spam')::int AS unread,
       count(*) FILTER (WHERE c.status = 'open')::int AS open
FROM conversation.conversations AS c
WHERE (@all_pages::boolean OR c.channel_account_id = ANY(@pages::uuid[]))
  AND c.status IN ('open', 'pending')
GROUP BY c.channel_account_id
ORDER BY c.channel_account_id;

-- name: CountMineAndUnassigned :one
-- Hội thoại đang mở trên các Page được xem: của người dùng, chưa ai nhận.
SELECT count(*) FILTER (WHERE c.assignee_id = @user_id::uuid)::int AS mine,
       count(*) FILTER (WHERE c.assignee_id IS NULL)::int AS unassigned
FROM conversation.conversations AS c
WHERE (@all_pages::boolean OR c.channel_account_id = ANY(@pages::uuid[]))
  AND c.status = 'open';
