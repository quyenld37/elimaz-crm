-- Đổi trạng thái đơn và lịch sử trạng thái (kế hoạch M2, Task M2-16: chỗ duy nhất có câu đổi trạng thái đơn; dời từ
-- orders.sql, history.sql của M1 Task M1-30, M1-30b). Chạy trong tenancy.WithTenant. Lịch sử chỉ ghi thêm.

-- name: SetOrderStatus :one
-- Đổi trạng thái chỉ khi đang ở trạng thái cũ; trả version mới (không dòng nào → pgx.ErrNoRows).
UPDATE ord.orders
SET status = @to_status, version = version + 1, updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND status = @from_status
RETURNING version;

-- name: InsertStatusHistory :exec
INSERT INTO ord.order_status_history
    (tenant_id, order_id, from_status, to_status, actor_type, actor_id, reason, occurred_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id, sqlc.narg(from_status)::text, @to_status, @actor_type, sqlc.narg(actor_id)::uuid,
        sqlc.narg(reason)::text, @occurred_at);
