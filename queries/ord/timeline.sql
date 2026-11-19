-- Lịch sử thao tác của đơn (kế hoạch M2, Task M2-24): lịch sử trạng thái và ghi chú nội bộ. Chạy trong
-- tenancy.WithTenant, sau khi đã kiểm đơn thuộc người bán (OrderExists).

-- name: ListOrderHistory :many
SELECT id, COALESCE(from_status, '')::text AS from_status, to_status, from_pending, to_pending, actor_type, actor_id,
       COALESCE(reason, '')::text AS reason, occurred_at
FROM ord.order_status_history
WHERE order_id = @order_id
ORDER BY occurred_at, id;

-- name: ListOrderNotes :many
SELECT id, body, author_id, created_at
FROM ord.order_notes
WHERE order_id = @order_id
ORDER BY created_at, id;
