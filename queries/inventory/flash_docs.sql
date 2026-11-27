-- Chứng từ giữ cho đợt flash sale (kế hoạch M5, Task 200 / M5-17; ADR-40, QĐ31). Chạy trong tx tenant của bên gọi
-- (RLS lọc theo tenant). Đổi số dư vẫn chỉ qua ApplyDelta (balances.sql); tệp này khóa số dư và ghi đơn vị suất.
-- Thứ tự khóa ADR-46 nhóm 11: stock_balances theo (variant_id, warehouse_id) tăng dần → flash_sale_units.

-- name: LockFlashBalances :many
-- Khóa các dòng số dư của biến thể variant_ids ở kho warehouse_ids theo khóa tăng dần và trả số có thể bán của bản mới
-- nhất (READ COMMITTED: FOR UPDATE trả bản sau khi chờ khóa). Lọc theo hai mảng riêng (sqlc không đọc được unnest hai
-- mảng): nhiều kho thì có thể khóa thêm cặp chéo, vẫn đúng thứ tự khóa; bên gọi bỏ cặp không cần. Cặp chưa có dòng số
-- dư không có trong kết quả.
SELECT b.variant_id, b.warehouse_id, (b.on_hand - b.damaged - b.held - b.in_transit)::int AS available
FROM inventory.stock_balances AS b
WHERE b.variant_id = ANY (@variant_ids::uuid[]) AND b.warehouse_id = ANY (@warehouse_ids::uuid[])
ORDER BY b.variant_id, b.warehouse_id
FOR UPDATE;

-- name: InsertFlashUnits :execrows
-- Trải n đơn vị suất trống của món (mỗi đơn vị một dòng, ADR-40). Dòng mới: không khóa dòng có sẵn.
INSERT INTO inventory.flash_sale_units (tenant_id, flash_sale_item_id)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, @flash_sale_item_id::uuid
FROM generate_series(1, @n::int);

-- name: ReleaseAvailableFlashUnits :execrows
-- Đóng món: mọi đơn vị còn trống → released (bên gọi đã khóa dòng số dư của món). Đơn vị đang bị lượt mua khóa thì
-- chờ; lượt mua commit held thì dòng không còn khớp điều kiện (READ COMMITTED đánh giá lại).
UPDATE inventory.flash_sale_units
SET status = 'released', updated_at = now()
WHERE flash_sale_item_id = @flash_sale_item_id AND status = 'available';
