-- Móc nối cho M3 (kế hoạch M2, Task M2-18): vận đơn hiện hành và tổng đã thanh toán của đơn. Chạy trong
-- tenancy.WithTenant, trong tx của bên gọi (fulfillment, cash); đơn đã khóa bằng LockOrder (ADR-46 nhóm 7) và version
-- là version đọc dưới khóa (không khớp → pgx.ErrNoRows). Không gán status (đổi trạng thái chỉ ở transition.sql).

-- name: AttachCurrentShipment :one
-- Gắn vận đơn hiện hành (bên gọi đã kiểm confirmed, chưa có vận đơn); trả version mới (Seq của order.updated).
UPDATE ord.orders
SET current_shipment_id = @shipment_id, version = version + 1, updated_at = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version AND current_shipment_id IS NULL
RETURNING version;

-- name: DetachCurrentShipment :one
-- Gỡ vận đơn hiện hành khi đúng vận đơn đó; trả version mới.
UPDATE ord.orders
SET current_shipment_id = NULL, version = version + 1, updated_at = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version AND current_shipment_id = @shipment_id
RETURNING version;

-- name: SetPaymentTotals :one
-- Ghi tổng đã thanh toán, COD còn thu, trạng thái thanh toán (bên gọi tính; CHECK orders_cod_prepaid_check kiểm lại);
-- trả version mới.
UPDATE ord.orders
SET prepaid_total  = @prepaid_total,
    cod_amount     = @cod_amount,
    payment_status = @payment_status::text,
    version        = version + 1,
    updated_at     = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;
