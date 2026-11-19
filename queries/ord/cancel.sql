-- Hủy đơn (kế hoạch M2, Task M2-21). Chạy trong tenancy.WithTenant, trong tx của bên gọi; đơn đã khóa bằng LockOrder
-- (ADR-46 nhóm 7) và version là version sau lần đổi trạng thái vừa xong trong cùng tx (applyTransition). Không gán
-- status (đổi trạng thái chỉ ở transition.sql) và không tăng version (cùng một lần đổi của đơn).

-- name: SetCancelReason :execrows
-- Ghi lý do, ghi chú hủy (yêu cầu hủy, hủy) hoặc bỏ cả hai (NULL: hãng từ chối hủy, đơn về confirmed).
UPDATE ord.orders
SET cancel_reason = sqlc.narg(reason)::text,
    cancel_note   = sqlc.narg(note)::text
WHERE seller_id = @seller_id AND id = @id AND version = @version;

-- name: ClearShipmentOfCancelled :execrows
-- Đơn vừa sang cancelled thì không còn vận đơn hiện hành (WB12: không có đơn cancelled còn vận đơn).
UPDATE ord.orders
SET current_shipment_id = NULL
WHERE seller_id = @seller_id AND id = @id AND version = @version AND status = 'cancelled'
  AND current_shipment_id IS NOT NULL;
