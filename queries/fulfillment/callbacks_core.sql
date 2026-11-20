-- Việc gọi lại khách sau giao thất bại, phần lõi do áp kết quả tra ghi (kế hoạch M3, Task M3-08). Chạy trong
-- tenancy.WithTenant, sau khi đã khóa đơn và vận đơn (ADR-46 nhóm 7 → 8).

-- name: NextCallbackAttempt :one
-- Lần giao thất bại kế tiếp của vận đơn (số việc gọi lại đã có + 1).
SELECT (COALESCE(max(attempt_no), 0) + 1)::int AS attempt_no
FROM fulfillment.callback_tasks
WHERE shipment_id = @shipment_id;

-- name: InsertCallbackTask :one
-- Đã có việc cùng (vận đơn, lần) → pgx.ErrNoRows.
INSERT INTO fulfillment.callback_tasks
    (tenant_id, shipment_id, order_id, attempt_no, failed_event_id, failed_reason, failed_at, due_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @shipment_id, @order_id, @attempt_no,
        @failed_event_id, @failed_reason, @failed_at, @due_at)
ON CONFLICT (tenant_id, shipment_id, attempt_no) DO NOTHING
RETURNING id;

-- name: CloseOpenCallbacks :many
-- Đóng mọi việc gọi lại còn mở của vận đơn với kết quả outcome; trả mã các việc đã đóng.
UPDATE fulfillment.callback_tasks
SET status = 'closed', outcome = @outcome, updated_at = now(), version = version + 1
WHERE shipment_id = @shipment_id AND status IN ('open', 'in_progress')
RETURNING id;
