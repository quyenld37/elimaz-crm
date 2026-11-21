-- Vùng labels (kế hoạch M3, Task M3-16): in nhãn theo lô mỗi tài khoản hãng. In không đổi trạng thái vận đơn hay
-- đóng gói (AT08): chỉ label_file_id, label_printed_at của vận đơn được ghi khi lô ready.

-- name: LabelShipments :many
-- Vận đơn được chọn in kèm kho của đơn (phạm vi quyền), theo id tăng dần. Không khóa: in không đổi vận đơn.
SELECT s.id, s.carrier_account_id, s.carrier_key, s.mode, s.tracking_code, s.status, o.warehouse_id
FROM fulfillment.shipments s
JOIN ord.orders_v o ON o.tenant_id = s.tenant_id AND o.id = s.order_id
WHERE s.id = ANY(@ids::uuid[])
ORDER BY s.id;

-- name: InsertLabelBatch :one
INSERT INTO fulfillment.label_batches (tenant_id, carrier_account_id, shipment_ids, status, created_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @carrier_account_id, @shipment_ids::uuid[],
    'pending', @created_by)
RETURNING id;

-- name: GetLabelBatch :one
SELECT id, carrier_account_id, shipment_ids, status, file_id, error_code, created_by, created_at
FROM fulfillment.label_batches
WHERE id = @id;

-- name: LabelBatchWarehouses :many
-- Kho của các đơn có vận đơn trong lô (phạm vi quyền khi đọc lô, báo hiệu), không trùng.
SELECT DISTINCT o.warehouse_id
FROM fulfillment.shipments s
JOIN ord.orders_v o ON o.tenant_id = s.tenant_id AND o.id = s.order_id
WHERE s.id = ANY(@ids::uuid[]);

-- name: LabelTrackingCodes :many
-- Mã hãng của vận đơn trong lô theo id tăng dần (vận đơn mất mã hãng thì bỏ).
SELECT id, tracking_code::text AS tracking_code
FROM fulfillment.shipments
WHERE id = ANY(@ids::uuid[]) AND tracking_code IS NOT NULL
ORDER BY id;

-- name: LockLabelShipments :many
-- Khóa vận đơn của lô theo id tăng dần (ADR-46 nhóm 8: shipments trước label_batches; hai lô chung vận đơn không
-- deadlock).
SELECT id
FROM fulfillment.shipments
WHERE id = ANY(@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: SetShipmentsLabel :exec
-- Gắn tệp nhãn của lô cho mọi vận đơn trong lô (đã khóa); không đổi status, version (AT08).
UPDATE fulfillment.shipments
SET label_file_id = @file_id, label_printed_at = now()
WHERE id = ANY(@ids::uuid[]);

-- name: ReadyLabelBatch :execrows
UPDATE fulfillment.label_batches
SET status = 'ready', file_id = @file_id, updated_at = now()
WHERE id = @id AND status = 'pending';

-- name: FailLabelBatch :execrows
UPDATE fulfillment.label_batches
SET status = 'failed', error_code = @error_code, updated_at = now()
WHERE id = @id AND status = 'pending';
