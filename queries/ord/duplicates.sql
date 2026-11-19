-- Đơn trùng (kế hoạch M2, Task M2-23; Q7). Chạy trong tenancy.WithTenant (consumer order_duplicates, handler). Gốc
-- khách đọc qua view customer.customers_v (module customer công bố). Thứ tự khóa ADR-46 nhóm 7: đơn (LockOrder, theo
-- id tăng dần) → order_duplicate_links. Không gán status, không tăng version của đơn.

-- name: DuplicateSource :one
-- Đơn X của sự kiện: trạng thái, giờ tạo, SĐT giao chuẩn hóa, gốc khách.
SELECT o.id, o.status, o.created_at, o.ship_phone, c.root_id
FROM ord.orders o
JOIN customer.customers_v c ON c.id = o.customer_id
WHERE o.seller_id = @seller_id AND o.id = @id;

-- name: FindDuplicateCandidates :many
-- Đơn khác cancelled của người bán, tạo trong [from, to], cùng gốc khách root_id hoặc cùng SĐT giao (khác rỗng), có ít
-- nhất một biến thể trong variant_ids. matched_by: customer khi cùng gốc, còn lại phone. exclude_id: đơn X (uuid rỗng
-- khi kiểm ở form trước khi tạo). Mới nhất trước.
SELECT o.id, (CASE WHEN c.root_id = sqlc.narg(root_id)::uuid THEN 'customer' ELSE 'phone' END)::text AS matched_by
FROM ord.orders o
JOIN customer.customers_v c ON c.id = o.customer_id
WHERE o.seller_id = @seller_id
  AND o.id <> @exclude_id::uuid
  AND o.status <> 'cancelled'
  AND o.created_at BETWEEN @from_at::timestamptz AND @to_at::timestamptz
  AND (c.root_id = sqlc.narg(root_id)::uuid OR (@ship_phone::text <> '' AND o.ship_phone = @ship_phone::text))
  AND EXISTS (SELECT 1 FROM ord.order_items i
              WHERE i.seller_id = @seller_id AND i.order_id = o.id AND i.variant_id = ANY(@variant_ids::uuid[]))
ORDER BY o.created_at DESC, o.id DESC
LIMIT 100;

-- name: OrderVariantIDs :many
-- Biến thể của đơn (không trùng lặp).
SELECT DISTINCT variant_id
FROM ord.order_items
WHERE seller_id = @seller_id AND order_id = @order_id;

-- name: CustomerRoot :one
-- Gốc nhóm của khách id (pgx.ErrNoRows: khách không có).
SELECT root_id FROM customer.customers_v WHERE id = @id;

-- name: InsertDuplicateLink :execrows
-- Cặp mới (order_id < other_order_id); cặp đã có (mở hay đã xử lý) → 0 dòng, không đụng.
INSERT INTO ord.order_duplicate_links (tenant_id, order_id, other_order_id, matched_by, created_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id, @other_order_id, @matched_by, @now::timestamptz)
ON CONFLICT DO NOTHING;

-- name: SetDuplicateSuspect :exec
-- Bật hay gỡ cờ nghi trùng của các đơn (đã khóa); không tăng version, không phát sự kiện.
UPDATE ord.orders
SET duplicate_suspect = @suspect::boolean
WHERE seller_id = @seller_id AND id = ANY(@ids::uuid[]) AND duplicate_suspect IS DISTINCT FROM @suspect::boolean;

-- name: LockDuplicateLink :one
-- Khóa dòng cặp (hai đơn đã khóa trước).
SELECT order_id, other_order_id, matched_by, created_at, resolution, resolved_at
FROM ord.order_duplicate_links
WHERE order_id = @order_id AND other_order_id = @other_order_id
FOR UPDATE;

-- name: ResolveDuplicateLink :execrows
UPDATE ord.order_duplicate_links
SET resolution = @resolution::text, resolved_at = @now::timestamptz, resolved_by = sqlc.narg(resolved_by)::uuid
WHERE order_id = @order_id AND other_order_id = @other_order_id AND resolution IS NULL;

-- name: HasOpenDuplicateLink :one
-- Đơn id còn cặp chưa xử lý mà cả hai đơn đều chưa hủy.
SELECT EXISTS (
    SELECT 1
    FROM ord.order_duplicate_links l
    JOIN ord.orders a ON a.id = l.order_id
    JOIN ord.orders b ON b.id = l.other_order_id
    WHERE (l.order_id = @id::uuid OR l.other_order_id = @id::uuid) AND l.resolution IS NULL
      AND a.seller_id = @seller_id AND b.seller_id = @seller_id
      AND a.status <> 'cancelled' AND b.status <> 'cancelled'
)::boolean AS open;

-- name: GetDuplicateLink :one
SELECT l.order_id, l.other_order_id, l.matched_by, l.created_at, l.resolution, l.resolved_at
FROM ord.order_duplicate_links l
JOIN ord.orders o ON o.id = l.order_id
WHERE o.seller_id = @seller_id AND l.order_id = @order_id AND l.other_order_id = @other_order_id;

-- name: ListOpenDuplicateLinks :many
-- Cặp chưa xử lý của người bán, hai đơn đều chưa hủy, mới nhất trước; keyset (created_at, order_id, other_order_id)
-- giảm dần (after_* rỗng: trang đầu).
SELECT l.order_id, l.other_order_id, l.matched_by, l.created_at, l.resolution, l.resolved_at
FROM ord.order_duplicate_links l
JOIN ord.orders a ON a.id = l.order_id
JOIN ord.orders b ON b.id = l.other_order_id
WHERE a.seller_id = @seller_id AND b.seller_id = @seller_id AND l.resolution IS NULL AND a.status <> 'cancelled' AND b.status <> 'cancelled'
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (l.created_at, l.order_id, l.other_order_id)
          < (sqlc.narg(after_at)::timestamptz, @after_order_id::uuid, @after_other_order_id::uuid))
ORDER BY l.created_at DESC, l.order_id DESC, l.other_order_id DESC
LIMIT @max_rows;

-- name: OrderSummaries :many
-- Tóm tắt các đơn ids của người bán cho danh sách (nhãn, khách gốc, số dòng).
SELECT o.id, o.code, o.status, o.pending_reason, o.packing_status, o.customer_id, c.root_id, r.full_name AS root_name,
       o.ship_phone, o.grand_total, o.cod_amount, o.channel, o.traffic_source_id, o.created_at, o.duplicate_suspect,
       (SELECT count(*) FROM ord.order_items i WHERE i.seller_id = @seller_id AND i.order_id = o.id)::int AS item_count
FROM ord.orders o
JOIN customer.customers_v c ON c.id = o.customer_id
JOIN customer.customers_v r ON r.id = c.root_id
WHERE o.seller_id = @seller_id AND o.id = ANY(@ids::uuid[]);
