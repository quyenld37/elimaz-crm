-- Người phụ trách hội thoại (Task 160; 04 mục 7). Thứ tự khóa nhóm 6 ADR-46: conversations → conversation_assignments.

-- name: LockConversationForAssign :one
-- Khóa dòng hội thoại trước khi đổi người phụ trách (so người đang nhận, version dưới khóa).
SELECT id, tenant_id, channel_account_id, assignee_id, version
FROM conversation.conversations
WHERE id = @id
FOR UPDATE;

-- name: SetConversationAssignee :one
-- Đổi người phụ trách (NULL: bỏ giao), tăng version (Seq của conversation.assigned).
UPDATE conversation.conversations
SET assignee_id = sqlc.narg(assignee_id)::uuid,
    version     = version + 1,
    updated_at  = now()
WHERE id = @id
RETURNING version;

-- name: InsertConversationAssignment :exec
INSERT INTO conversation.conversation_assignments (tenant_id, conversation_id, from_user_id, to_user_id, actor_id, reason)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, sqlc.narg(from_user_id)::uuid,
        sqlc.narg(to_user_id)::uuid, sqlc.narg(actor_id)::uuid, @reason);

-- name: ListConversationAssignments :many
-- Lịch sử giao, nhận của một hội thoại, cũ trước; tên nhân viên từ identity.users_v (người đã xóa: tên rỗng).
SELECT a.id, a.from_user_id, a.to_user_id, a.actor_id, a.reason, a.created_at,
       coalesce(f.display_name, '')::text AS from_name,
       coalesce(t.display_name, '')::text AS to_name,
       coalesce(x.display_name, '')::text AS actor_name
FROM conversation.conversation_assignments AS a
LEFT JOIN identity.users_v AS f ON f.id = a.from_user_id
LEFT JOIN identity.users_v AS t ON t.id = a.to_user_id
LEFT JOIN identity.users_v AS x ON x.id = a.actor_id
WHERE a.conversation_id = @conversation_id
ORDER BY a.created_at, a.id;

-- name: GetConversationRow :one
-- Một hội thoại kèm tên khách (customers_v), tên người phụ trách (users_v), đoạn bài viết, có đơn nháp đang mở
-- (ord.orders_v) — dữ liệu của chi tiết hội thoại.
SELECT c.id, c.channel_account_id, c.customer_id, c.kind, c.status, c.assignee_id, c.post_id, c.last_message_at,
       c.last_customer_message_at, c.unread_count, c.last_snippet, c.blocked_by_user, c.version,
       coalesce(cu.full_name, '')::text AS customer_name,
       coalesce(u.display_name, '')::text AS assignee_name,
       p.snippet AS post_snippet,
       EXISTS (SELECT 1 FROM ord.orders_v AS o WHERE o.conversation_id = c.id AND o.status = 'draft') AS has_open_draft
FROM conversation.conversations AS c
LEFT JOIN customer.customers_v AS cu ON cu.id = c.customer_id
LEFT JOIN identity.users_v AS u ON u.id = c.assignee_id
LEFT JOIN conversation.posts AS p ON p.id = c.post_id
WHERE c.id = @id;

-- name: ListConversationTagsOf :many
-- Nhãn còn sống của một hội thoại theo thứ tự hiển thị.
SELECT t.id, t.name, t.color, t.sort, t.version
FROM conversation.conversation_tag_links AS l
JOIN conversation.tags AS t ON t.id = l.tag_id AND t.deleted_at IS NULL
WHERE l.conversation_id = @conversation_id
ORDER BY t.sort, lower(t.name), t.id;
