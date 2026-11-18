-- Lịch sử trạng thái đơn, chỉ ghi thêm (kế hoạch M1, Task M1-30).

-- name: InsertStatusHistory :exec
INSERT INTO ord.order_status_history
    (tenant_id, order_id, from_status, to_status, actor_type, actor_id, reason, occurred_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id, sqlc.narg(from_status)::text, @to_status, @actor_type, sqlc.narg(actor_id)::uuid,
        sqlc.narg(reason)::text, @occurred_at);
