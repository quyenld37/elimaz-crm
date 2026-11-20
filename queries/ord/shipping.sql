-- Hàm đơn hàng cho vận chuyển (kế hoạch M3, Task M3-06): đóng gói chỉ đi tới, đổi địa chỉ khi hãng đã nhận. Chạy trong
-- tenancy.WithTenant, trong tx của bên gọi. Không gán status (đổi trạng thái chỉ ở transition.sql).

-- name: LockOrdersForPacking :many
-- Khóa nhiều đơn của người bán theo id tăng dần (ADR-46 nhóm 7, luật khóa nhiều dòng một bảng: một câu ORDER BY id).
SELECT id, tenant_id, seller_id, customer_id, status, pending_reason, current_shipment_id, address_complete, version,
       packing_status
FROM ord.orders
WHERE seller_id = @seller_id AND id = ANY(@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: SetPackingStatus :one
-- Ghi trạng thái đóng gói của đơn đã khóa (bên gọi đã kiểm chỉ đi tới); trả version mới.
UPDATE ord.orders
SET packing_status = @packing_status::text, version = version + 1, updated_at = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;

-- name: SetShipAddress :one
-- Ghi địa chỉ giao của đơn đã khóa (đường đổi địa chỉ khi hãng đã nhận, không qua Editable); trả version mới.
UPDATE ord.orders
SET ship_address        = sqlc.arg(ship_address)::jsonb,
    ship_phone          = @ship_phone::text,
    ship_phone_reversed = @ship_phone_reversed::text,
    address_complete    = @address_complete::boolean,
    version             = version + 1,
    updated_at          = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;
