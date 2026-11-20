-- Địa chỉ lấy hàng của kho (kế hoạch M3, Task M3-09). Chạy trong tenancy.WithTenant (crm_app). Một câu UPDATE có điều
-- kiện version trên một dòng kho (không đọc ra Go rồi ghi lại).

-- name: GetWarehousePickup :one
SELECT pickup_address, pickup_phone, version FROM inventory.warehouses WHERE id = @id;

-- name: SetWarehousePickup :one
UPDATE inventory.warehouses
SET pickup_address = @pickup_address, pickup_phone = @pickup_phone, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING version;
