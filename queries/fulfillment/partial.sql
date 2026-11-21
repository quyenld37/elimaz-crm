-- Giao một phần bằng ngoại lệ có quyền (kế hoạch M3, Task M3-26). Chạy trong tenancy.WithTenant, sau khi đã khóa đơn
-- rồi vận đơn (LockShipmentForApply).

-- name: MarkShipmentDeliveredPartial :one
-- Vận đơn (dòng đã khóa) đang giao, giao thất bại hay chờ thu gom → delivered, partial, tiền hãng đã thu; mốc
-- delivered_at, picked_up_at giữ lần đầu; trả version mới. Trạng thái khác → pgx.ErrNoRows.
UPDATE fulfillment.shipments
SET status = 'delivered',
    partial = true,
    cod_collected_amount = @cod_collected,
    delivered_at = COALESCE(delivered_at, @now::timestamptz),
    picked_up_at = COALESCE(picked_up_at, @now::timestamptz),
    last_status_at = @now::timestamptz,
    updated_at = now(),
    version = version + 1
WHERE id = @id AND status IN ('shipping', 'delivery_failed', 'awaiting_pickup')
RETURNING version;

-- name: HasOpenAnomaly :one
SELECT EXISTS (
    SELECT 1 FROM fulfillment.shipment_anomalies
    WHERE shipment_id = @shipment_id AND kind = @kind AND status = 'open'
)::boolean AS open;

-- name: ResolvePartialAnomaly :exec
-- Đóng việc "hãng báo giao một phần" (partial_reported) khi nhân viên đã ghi giao một phần, rồi tính lại needs_review
-- theo việc còn mở.
WITH closed AS (
    UPDATE fulfillment.shipment_anomalies
    SET status = 'resolved', resolution = @resolution, resolved_by = sqlc.narg(resolved_by)::uuid, resolved_at = now(),
        updated_at = now()
    WHERE shipment_id = @shipment_id AND kind = 'partial_reported' AND status = 'open'
    RETURNING id
)
UPDATE fulfillment.shipments s
SET needs_review = EXISTS (
        SELECT 1 FROM fulfillment.shipment_anomalies x
        WHERE x.shipment_id = s.id AND x.status = 'open' AND x.id NOT IN (SELECT id FROM closed)
    ),
    updated_at = now()
WHERE s.id = @shipment_id;
