-- Tệp xuất Excel của báo cáo (kế hoạch M6, Task 237 = M6-14). Chạy trong tenancy.WithTenant, transaction chỉ đọc
-- của báo cáo.

-- name: WarehouseNames :many
-- Tên các kho (dòng phạm vi đầu tệp, cột Kho của xuất nhập tồn), theo tên.
SELECT warehouse_id, name FROM inventory.warehouses_v
WHERE warehouse_id = ANY(@ids::uuid[])
ORDER BY name, warehouse_id;
