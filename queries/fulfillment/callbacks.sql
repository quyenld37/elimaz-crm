-- Danh sách "đơn giao thất bại cần gọi lại", ghi kết quả, giao việc (kế hoạch M3, Task M3-19). Chạy trong
-- tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 7 → 8: đơn (order.LockSnapshot) → vận đơn → callback_tasks →
-- address_change_requests; callback_task_logs chỉ chèn.

-- name: ListCallbackTasks :many
-- Một trang việc gọi lại có trạng thái thuộc statuses, hạn sớm nhất trước (due_at, id): việc còn mở quá hạn có
-- due_at < now nên tự đứng trước việc chưa tới hạn. due_before: chỉ việc có hạn trước mốc đó; assignee_id: chỉ việc
-- giao cho người đó; after_at/after_id: con trỏ trang trước; lim = cỡ trang + 1. Chọn trang trên riêng callback_tasks
-- (index callback_tasks_status_due) rồi mới nối vận đơn, đơn cho đúng các dòng của trang. Tên khách do bên gọi đọc qua
-- public API của customer (fulfillment không gọi customer).
WITH page AS (
    SELECT c.id
    FROM fulfillment.callback_tasks c
    WHERE c.status = ANY (sqlc.arg(statuses)::text[])
      AND (sqlc.narg(due_before)::timestamptz IS NULL OR c.due_at < sqlc.narg(due_before)::timestamptz)
      AND (sqlc.narg(assignee_id)::uuid IS NULL OR c.assignee_id = sqlc.narg(assignee_id)::uuid)
      AND (sqlc.narg(after_at)::timestamptz IS NULL
           OR (c.due_at, c.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
    ORDER BY c.due_at, c.id
    LIMIT sqlc.arg(lim)::int
)
SELECT c.id, c.shipment_id, c.order_id, o.code AS order_code, o.customer_id,
       s.carrier_key, s.tracking_code, c.attempt_no, c.failed_reason, c.failed_at, c.due_at, c.status, c.outcome,
       c.next_call_at, c.assignee_id, c.version
FROM page
JOIN fulfillment.callback_tasks c ON c.id = page.id
JOIN fulfillment.shipments s ON s.tenant_id = c.tenant_id AND s.id = c.shipment_id
JOIN ord.orders_v o ON o.tenant_id = c.tenant_id AND o.id = c.order_id
ORDER BY c.due_at, c.id;

-- name: CountOpenCallbacks :one
-- Số việc gọi lại còn mở (open, in_progress), đếm tối đa 1.000 (đếm có trần).
SELECT count(*)::int AS n
FROM (SELECT 1 FROM fulfillment.callback_tasks c WHERE c.status IN ('open', 'in_progress') LIMIT 1000) t;

-- name: CallbackTaskShipment :one
-- Vận đơn của việc gọi lại id (đọc không khóa: bên gọi khóa đơn, vận đơn rồi khóa việc và đọc lại).
SELECT shipment_id FROM fulfillment.callback_tasks WHERE id = @id;

-- name: LockCallbackTask :one
-- Khóa việc gọi lại id (sau đơn, vận đơn).
SELECT id, shipment_id, order_id, status, outcome, next_call_at, assignee_id, version
FROM fulfillment.callback_tasks
WHERE id = @id
FOR UPDATE;

-- name: SetCallbackOutcome :exec
-- Ghi kết quả lên việc đã khóa: trạng thái mới, kết quả, hẹn gọi (NULL: giữ hẹn cũ khi vẫn in_progress, xóa khi xong);
-- tăng version.
UPDATE fulfillment.callback_tasks
SET status = @status, outcome = @outcome,
    next_call_at = CASE WHEN @status = 'in_progress' THEN coalesce(sqlc.narg(next_call_at)::timestamptz, next_call_at) END,
    updated_at = now(), version = version + 1
WHERE id = @id;

-- name: InsertCallbackLog :one
-- Một dòng nhật ký gọi lại (chỉ ghi thêm).
INSERT INTO fulfillment.callback_task_logs (tenant_id, task_id, outcome, note, next_call_at, actor_id)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @task_id, @outcome, @note,
        sqlc.narg(next_call_at)::timestamptz, @actor_id)
RETURNING id;

-- name: AssignCallbackTask :exec
-- Giao việc đã khóa cho assignee_id; tăng version.
UPDATE fulfillment.callback_tasks
SET assignee_id = @assignee_id, updated_at = now(), version = version + 1
WHERE id = @id;

-- name: CallbackTaskView :one
-- Một việc gọi lại cho câu trả lời (cùng cột với ListCallbackTasks).
SELECT c.id, c.shipment_id, c.order_id, o.code AS order_code, o.customer_id,
       s.carrier_key, s.tracking_code, c.attempt_no, c.failed_reason, c.failed_at, c.due_at, c.status, c.outcome,
       c.next_call_at, c.assignee_id, c.version
FROM fulfillment.callback_tasks c
JOIN fulfillment.shipments s ON s.tenant_id = c.tenant_id AND s.id = c.shipment_id
JOIN ord.orders_v o ON o.tenant_id = c.tenant_id AND o.id = c.order_id
WHERE c.id = @id;

-- name: RedeliverTarget :one
-- Dòng nhật ký của lệnh giao lại kèm ghi chú, vận đơn, tài khoản hãng (lệnh đọc ngoài transaction nghiệp vụ).
SELECT l.note, s.id AS shipment_id, s.status AS shipment_status, s.mode, s.carrier_key, s.carrier_account_id,
       s.tracking_code
FROM fulfillment.callback_task_logs l
JOIN fulfillment.callback_tasks c ON c.tenant_id = l.tenant_id AND c.id = l.task_id
JOIN fulfillment.shipments s ON s.tenant_id = c.tenant_id AND s.id = c.shipment_id
WHERE l.id = @id;
