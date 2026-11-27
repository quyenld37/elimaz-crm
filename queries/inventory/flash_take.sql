-- Lượt mua flash sale, phần của inventory (kế hoạch M5, Task 201 / M5-18; ADR-40). Chạy trong tx tenant của bên gọi.
-- Đơn vị suất lấy bằng SKIP LOCKED (ADR-46 nhóm 11: flash_sale_units, sau flash_sale_claims của nhóm 10). Giữ hàng của
-- lượt mua không đổi stock_balances: phần held đó đã cộng từ lúc lên lịch (flash_hold, Task 200).

-- name: TakeFlashUnits :many
-- Khóa tối đa n đơn vị trống của món theo id tăng dần, bỏ qua đơn vị tx khác đang khóa (index flash_sale_units_pick).
SELECT id
FROM inventory.flash_sale_units
WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
  AND flash_sale_item_id = @flash_sale_item_id AND status = 'available'
ORDER BY id
LIMIT @n
FOR UPDATE SKIP LOCKED;

-- name: AnyOtherFlashUnitAvailable :one
-- Còn đơn vị trống nào của món ngoài các đơn vị đã lấy (taken) không (không khóa): có → đang bị tx khác khóa.
SELECT EXISTS (
    SELECT 1
    FROM inventory.flash_sale_units
    WHERE tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
      AND flash_sale_item_id = @flash_sale_item_id AND status = 'available' AND id <> ALL (@taken::uuid[])
)::boolean AS found;

-- name: HoldFlashUnits :execrows
-- Đơn vị đã do chính tx này khóa (TakeFlashUnits) → held cho dòng đơn.
UPDATE inventory.flash_sale_units
SET status = 'held', order_item_id = @order_item_id::uuid, updated_at = now()
WHERE id = ANY (@ids::uuid[]) AND status = 'available';

-- name: InsertFlashReservation :exec
-- Dòng giữ hàng held của dòng đơn flash; không đổi số dư (dòng mới, không khóa dòng có sẵn).
INSERT INTO inventory.stock_reservations
    (id, tenant_id, order_item_id, variant_id, warehouse_id, quantity, expires_at)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_item_id::uuid, @variant_id, @warehouse_id,
        @quantity, @expires_at::timestamptz);
