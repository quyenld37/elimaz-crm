-- Kết quả cuộc gọi khách và hội thoại của cả nhóm khách (Task 169; Q14, CH17). Chạy trong tenancy.WithTenant. Nhóm
-- khách là mọi hồ sơ cùng root_id (customer.GroupMembers). Lọc Page: all = true (grant phạm vi all) hoặc Page thuộc
-- pages; cuộc gọi không gắn hội thoại thì ai có conversation.read cũng thấy. note là dữ liệu cá nhân.

-- name: InsertCallLog :one
-- created_at là giờ của ứng dụng (cùng đồng hồ với tin hệ thống và nhắc gọi lại).
INSERT INTO conversation.call_logs (tenant_id, customer_id, conversation_id, order_id, outcome, note, callback_at,
                                    created_by, created_at)
VALUES (current_setting('app.tenant_id')::uuid, @customer_id, sqlc.narg(conversation_id), sqlc.narg(order_id), @outcome,
        sqlc.narg(note), sqlc.narg(callback_at), @created_by, @created_at)
RETURNING id, customer_id, conversation_id, order_id, outcome, note, callback_at, created_by, created_at;

-- name: InsertSystemMessage :one
-- Tin hệ thống trong hội thoại (ví dụ "Đã gọi: không nghe máy"); không gửi ra ngoài.
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, direction, kind, origin, body,
                                   sender_staff_id, sent_at)
VALUES (current_setting('app.tenant_id')::uuid, @conversation_id, @channel_account_id, 'out', 'system', 'system', @body,
        @sender_staff_id, @sent_at)
RETURNING id;

-- name: OrderCustomer :one
-- Khách của đơn (đọc view của order, Q1).
SELECT customer_id
FROM ord.orders_v
WHERE id = @id;

-- name: ListGroupCalls :many
-- Cuộc gọi của nhóm khách, mới nhất trước, trang theo (created_at, id) giảm dần (index call_logs_customer).
SELECT l.id, l.customer_id, l.conversation_id, l.order_id, l.outcome, l.note, l.callback_at, l.created_by, l.created_at
FROM conversation.call_logs AS l
LEFT JOIN conversation.conversations AS c ON c.tenant_id = l.tenant_id AND c.id = l.conversation_id
WHERE l.customer_id = ANY(@members::uuid[])
  AND (l.conversation_id IS NULL OR @all_pages::boolean OR c.channel_account_id = ANY(@pages::uuid[]))
  AND (l.created_at, l.id) < (@before_at::timestamptz, @before_id::uuid)
ORDER BY l.created_at DESC, l.id DESC
LIMIT @lim::int;

-- name: ListGroupConversations :many
-- Hội thoại của nhóm khách trên các Page được xem, mới nhất trước (index conversations_customer).
SELECT c.id, c.channel_account_id, c.customer_id, c.kind, c.status, c.assignee_id, c.unread_count, c.last_message_at,
       c.last_snippet, c.post_id, c.version, cu.full_name AS customer_name, p.snippet AS post_snippet,
       EXISTS (SELECT 1 FROM ord.orders_v AS o
               WHERE o.conversation_id = c.id AND o.status = 'draft')::boolean AS has_open_draft
FROM conversation.conversations AS c
JOIN customer.customers_v AS cu ON cu.id = c.customer_id
LEFT JOIN conversation.posts AS p ON p.tenant_id = c.tenant_id AND p.id = c.post_id
WHERE c.customer_id = ANY(@members::uuid[])
  AND (@all_pages::boolean OR c.channel_account_id = ANY(@pages::uuid[]))
  AND (c.last_message_at, c.id) < (@before_at::timestamptz, @before_id::uuid)
ORDER BY c.last_message_at DESC, c.id DESC
LIMIT @lim::int;

-- name: ConversationTagsOf :many
-- Nhãn còn sống của các hội thoại.
SELECT l.conversation_id, t.id, t.name, t.color, t.sort, t.version
FROM conversation.conversation_tag_links AS l
JOIN conversation.tags AS t ON t.tenant_id = l.tenant_id AND t.id = l.tag_id
WHERE l.conversation_id = ANY(@ids::uuid[])
  AND t.deleted_at IS NULL
ORDER BY l.conversation_id, t.sort, t.id;
