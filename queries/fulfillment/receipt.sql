-- Kho nhận kiện hoàn (kế hoạch M3, Task M3-23): consumer fulfillment_return_received đưa vận đơn sang returned. Thứ tự
-- khóa như ApplyTracking: order.LockSnapshot (nhóm 7) → LockShipmentForReceipt (nhóm 8). Chạy trong tenancy.WithTenant.

-- name: LockShipmentForReceipt :one
-- Khóa vận đơn (chỉ dòng vận đơn) kèm kho của tài khoản hãng (phạm vi báo hiệu); không có → pgx.ErrNoRows.
SELECT s.id, s.tenant_id, s.order_id, s.status, s.partial, s.cod_amount, s.version, a.warehouse_id
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.id = @id
FOR UPDATE OF s;

-- name: MarkShipmentReturned :one
-- returning → returned khi kho đã nhận kiện hoàn (Q-9: chỉ kho nhận mới thành returned); trả version mới. Vận đơn
-- không còn returning → pgx.ErrNoRows.
UPDATE fulfillment.shipments
SET status = 'returned', returned_at = @now::timestamptz, last_status_at = @now::timestamptz, updated_at = now(),
    version = version + 1
WHERE id = @id AND status = 'returning'
RETURNING version;
