-- Chốt đơn từ comment live (Task 172; LV03, LV07, LV08, LV09). Chạy trong tenancy.WithTenant / WriteTx. Đơn đọc
-- ord.orders_v, ord.order_items_v; khách đọc customer.customers_v; tên nhân viên đọc identity.users_v (view đã công bố,
-- Q1). Thứ tự khóa ở live.Claim (ADR-46: khách gốc → hội thoại → ghi nhận → tin; đơn, tồn trong order).

-- name: GetLiveComment :one
-- Comment id (không khóa) kèm hội thoại comment, khách của hội thoại và mã bài trên kênh.
SELECT m.id, m.conversation_id, m.channel_account_id, m.external_id, m.direction, m.kind, m.origin, m.body,
       m.is_hidden, m.removed_at, m.is_live, m.sent_at, c.customer_id, c.external_thread_ref,
       p.external_id AS post_external_id
FROM conversation.messages AS m
JOIN conversation.conversations AS c ON c.id = m.conversation_id
JOIN conversation.posts AS p ON p.id = m.post_id
WHERE m.id = @id
  AND m.kind = 'comment';

-- name: LockConversationsByID :many
-- Khóa các hội thoại ids theo id tăng dần (ADR-46 nhóm 6: hội thoại comment và hộp thư của cùng khách).
SELECT id
FROM conversation.conversations
WHERE id = ANY(@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: GetCommentClaim :one
-- Dòng ghi nhận của comment message_id kèm tên hiển thị người chốt và mã đơn.
SELECT cl.message_id, cl.live_session_id, cl.order_id, cl.claimed_by, cl.claimed_at, cl.ask_status, cl.ask_reason,
       cl.ask_message_id, coalesce(u.display_name, '')::text AS claimed_by_name, coalesce(o.code, '')::text AS order_code
FROM conversation.live_comment_claims AS cl
LEFT JOIN identity.users_v AS u ON u.id = cl.claimed_by
LEFT JOIN ord.orders_v AS o ON o.id = cl.order_id
WHERE cl.message_id = @message_id;

-- name: OpenDraftsOfRootInSession :many
-- Đơn nháp còn draft của nhóm khách root_id có ghi nhận trong phiên live_session_id (gợi ý thêm vào nháp, LV09); mới
-- nhất trước.
SELECT o.id, o.code, o.grand_total, o.version, o.draft_expires_at
FROM ord.orders_v AS o
JOIN customer.customers_v AS cu ON cu.id = o.customer_id
WHERE o.status = 'draft'
  AND cu.root_id = @root_id
  AND EXISTS (SELECT 1 FROM conversation.live_comment_claims AS cl
              WHERE cl.order_id = o.id AND cl.live_session_id = @live_session_id)
ORDER BY o.created_at DESC, o.id DESC
LIMIT 20;

-- name: DraftItems :many
-- Dòng của các đơn ids (tên chụp lúc đặt, số lượng) theo đơn và thứ tự dòng.
SELECT order_id, variant_id, name_snapshot, quantity
FROM ord.order_items_v
WHERE order_id = ANY(@ids::uuid[])
ORDER BY order_id, position;

-- name: OrderClaimedInSession :one
-- Đơn order_id có ít nhất một ghi nhận trong phiên live_session_id.
SELECT EXISTS (SELECT 1 FROM conversation.live_comment_claims
               WHERE order_id = @order_id AND live_session_id = @live_session_id);

-- name: CustomerRootOf :one
-- Gốc của hồ sơ khách id (customer.customers_v).
SELECT root_id FROM customer.customers_v WHERE id = @id;

-- name: InsertCommentClaim :execrows
-- Ghi nhận chốt comment; đã có dòng (comment đã chốt) → 0 dòng.
INSERT INTO conversation.live_comment_claims (tenant_id, message_id, live_session_id, conversation_id, order_id,
                                              claimed_by, claimed_at, ask_message_id, ask_status, ask_reason)
VALUES (current_setting('app.tenant_id')::uuid, @message_id, @live_session_id, @conversation_id, @order_id,
        @claimed_by, @claimed_at, sqlc.narg(ask_message_id), @ask_status, sqlc.narg(ask_reason))
ON CONFLICT (tenant_id, message_id) DO NOTHING;

-- name: ListSessionComments :many
-- Comment live của khách dưới bài của phiên (khớp mã bài, mã live hay mã video như MatchLiveComment), theo (sent_at, id)
-- tăng dần sau mốc after; lọc có SĐT (mẫu thô, Go lọc lại chính xác) và chưa chốt. Kèm khách, ghi nhận, tin riêng mới
-- nhất của comment, khách có nháp của phiên.
SELECT m.id, m.conversation_id, m.external_id, coalesce(m.body, '')::text AS body, m.is_hidden, m.sent_at,
       c.customer_id, coalesce(cu.full_name, '')::text AS customer_name,
       cl.order_id AS claim_order_id, cl.claimed_by AS claim_by, coalesce(u.display_name, '')::text AS claim_by_name,
       coalesce(o.code, '')::text AS claim_order_code,
       coalesce(pr.id, '00000000-0000-0000-0000-000000000000'::uuid) AS private_id, pr.send_status AS private_status,
       EXISTS (SELECT 1 FROM conversation.live_comment_claims AS cl2
               JOIN ord.orders_v AS o2 ON o2.id = cl2.order_id
               JOIN customer.customers_v AS cu2 ON cu2.id = o2.customer_id
               WHERE cl2.live_session_id = @live_session_id AND o2.status = 'draft'
                 AND cu2.root_id = cu.root_id) AS customer_has_open_draft
FROM conversation.messages AS m
JOIN conversation.posts AS p ON p.id = m.post_id
JOIN conversation.conversations AS c ON c.id = m.conversation_id
LEFT JOIN customer.customers_v AS cu ON cu.id = c.customer_id
LEFT JOIN conversation.live_comment_claims AS cl ON cl.message_id = m.id
LEFT JOIN identity.users_v AS u ON u.id = cl.claimed_by
LEFT JOIN ord.orders_v AS o ON o.id = cl.order_id
LEFT JOIN LATERAL (SELECT r.id, r.send_status
                   FROM conversation.messages AS r
                   WHERE r.channel_account_id = m.channel_account_id
                     AND r.kind = 'private_reply'
                     AND r.parent_external_id = m.external_id
                   ORDER BY r.sent_at DESC, r.id DESC
                   LIMIT 1) AS pr ON true
WHERE m.channel_account_id = @channel_account_id
  AND m.kind = 'comment'
  AND m.is_live
  AND m.direction = 'in'
  AND m.removed_at IS NULL
  AND p.external_id = ANY(@post_refs::text[])
  AND (m.sent_at, m.id) > (@after_at::timestamptz, @after_id::uuid)
  AND (NOT @phone_only::boolean OR m.body ~ '[0-9]')
  AND (NOT @unhandled_only::boolean OR cl.message_id IS NULL)
ORDER BY m.sent_at, m.id
LIMIT @lim::int;
