-- Nhập mã tay, hãng "tự giao", nhân viên cập nhật tay trạng thái (kế hoạch M3, Task M3-15). Chạy trong
-- tenancy.WithTenant / WriteTx. Thứ tự khóa ADR-46: ord.orders (nhóm 7) → shipments (nhóm 8) → shipment_anomalies.

-- name: MarkShipmentManual :exec
-- Vận đơn tay vừa gắn mã (dòng đã khóa, awaiting_pickup): create_state manual (không có lệnh gửi hãng), phí thật nhân
-- viên nhập (NULL: chưa biết). Không tăng version: cùng transaction với lần gắn mã, chưa ai thấy bản trước.
UPDATE fulfillment.shipments
SET create_state = 'manual', fee_actual = sqlc.narg(fee_actual)::bigint, updated_at = now()
WHERE id = @id AND status = 'awaiting_pickup';

-- name: StaffCancelTaskOpen :one
-- Vận đơn có việc hủy tay mở không: manual_cancel (hãng không hủy qua API) hay in_doubt_cancel (không rõ hãng đã hủy).
SELECT EXISTS (
    SELECT 1 FROM fulfillment.shipment_anomalies
    WHERE shipment_id = @shipment_id AND kind IN ('manual_cancel', 'in_doubt_cancel') AND status = 'open'
)::bool;

-- name: ResolveStaffCancelAnomalies :exec
-- Nhân viên ghi tay kết quả hủy ở hãng: đóng việc manual_cancel, in_doubt_cancel còn mở (người làm, kết quả) rồi tính
-- lại needs_review theo việc còn mở.
WITH closed AS (
    UPDATE fulfillment.shipment_anomalies
    SET status = 'resolved', resolution = @resolution, resolved_by = sqlc.narg(resolved_by)::uuid, resolved_at = now(),
        updated_at = now()
    WHERE shipment_id = @shipment_id AND kind IN ('manual_cancel', 'in_doubt_cancel') AND status = 'open'
    RETURNING id
)
UPDATE fulfillment.shipments s
SET needs_review = EXISTS (
        SELECT 1 FROM fulfillment.shipment_anomalies x
        WHERE x.shipment_id = s.id AND x.status = 'open' AND x.id NOT IN (SELECT id FROM closed)
    ),
    updated_at = now()
WHERE s.id = @shipment_id;
