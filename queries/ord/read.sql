-- Đọc chi tiết đơn (kế hoạch M2, Task M2-24). Chạy trong tenancy.WithTenant; điều kiện seller_id ở tầng dưới cùng
-- (ADR-29) qua OrderExists/GetOrderBase trước khi đọc bảng con của đơn.

-- name: OrderExists :one
SELECT EXISTS (SELECT 1 FROM ord.orders WHERE seller_id = @seller_id AND id = @id);

-- name: ListOrderDuplicateLinks :many
-- Cặp đơn nghi trùng chưa xử lý của đơn @id (đơn ở bên nào của cặp cũng được), cũ trước.
SELECT CASE WHEN order_id = @id THEN other_order_id ELSE order_id END::uuid AS other_id, matched_by, created_at
FROM ord.order_duplicate_links
WHERE (order_id = @id OR other_order_id = @id) AND resolved_at IS NULL
ORDER BY created_at, other_id;

-- name: ListOrderReturns :many
SELECT id, code, kind, status
FROM ord.returns
WHERE order_id = @order_id
ORDER BY created_at, id;
