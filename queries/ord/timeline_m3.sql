-- Phiếu trả của đơn cho lịch sử thao tác (kế hoạch M3, Task M3-28): mỗi phiếu một mục lúc tạo, một mục lúc nhận hàng.
-- Chạy trong tenancy.WithTenant, sau khi đã kiểm đơn thuộc người bán (OrderExists).

-- name: ListOrderReturnsForTimeline :many
SELECT id, code, kind, status, created_by, created_at, received_by, received_at
FROM ord.returns
WHERE order_id = @order_id
ORDER BY created_at, id;
