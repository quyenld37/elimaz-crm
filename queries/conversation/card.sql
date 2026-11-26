-- Thẻ khách cạnh khung chat và bối cảnh lên đơn (Task 159). Đơn đọc ord.orders_v (conversation được đọc view của
-- order, Q1); nguồn tên đọc customer.customers_v.

-- name: CustomerNameSource :one
-- Nguồn tên của hồ sơ id (placeholder, channel, staff — Q26).
SELECT name_source FROM customer.customers_v WHERE id = @id;

-- name: RecentGroupOrders :many
-- Đơn (trừ đơn nháp) của mọi hồ sơ trong nhóm khách, mới nhất trước.
SELECT o.id, o.code, o.status, o.grand_total, o.created_at, o.draft_expires_at, o.version
FROM ord.orders_v AS o
WHERE o.customer_id = ANY(@members::uuid[])
  AND o.status <> 'draft'
ORDER BY o.created_at DESC, o.id DESC
LIMIT @lim::int;

-- name: GroupDraftOrders :many
-- Đơn nháp đang mở của mọi hồ sơ trong nhóm khách, mới nhất trước.
SELECT o.id, o.code, o.status, o.grand_total, o.created_at, o.draft_expires_at, o.version
FROM ord.orders_v AS o
WHERE o.customer_id = ANY(@members::uuid[])
  AND o.status = 'draft'
ORDER BY o.created_at DESC, o.id DESC
LIMIT @lim::int;

-- name: GetOrderContextRow :one
-- Hội thoại id kèm loại và giờ đăng của bài viết (comment) — dữ liệu chọn nguồn đơn tự điền.
SELECT c.id, c.channel_account_id, c.customer_id, c.kind,
       coalesce(p.kind, '')::text AS post_kind, p.created_time AS post_created_time
FROM conversation.conversations AS c
LEFT JOIN conversation.posts AS p ON p.tenant_id = c.tenant_id AND p.id = c.post_id
WHERE c.id = @id;
