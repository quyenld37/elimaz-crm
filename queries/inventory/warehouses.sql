-- Kho (kế hoạch M1, Task M1-28). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant.

-- name: InsertWarehouseIfMissing :exec
-- Chủ bảng (seed-dev): crm_app không ghi được warehouses.
INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name)
VALUES (@tenant_id, @operated_by_seller_id, @code, @name)
ON CONFLICT (tenant_id, code) DO NOTHING;

-- name: GetWarehouseByCode :one
SELECT id FROM inventory.warehouses WHERE code = @code;

-- name: ListWarehouses :many
-- Mọi kho của tenant (cả kho đã đóng), kho MAIN trước rồi theo mã (Task M1-51).
SELECT id, code, name, status FROM inventory.warehouses
ORDER BY (code = 'MAIN') DESC, code;
