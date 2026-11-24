-- Còn/hết cho website (kế hoạch M5, Task 191 / M5-08). Chỉ đọc view _v đã công bố. available của
-- inventory.stock_available_v = on_hand − damaged − held − in_transit: phần đã giữ cho đơn và cho đợt flash sale (nằm
-- trong held) không tính (FS09).

-- name: AvailabilityOf :many
-- Số có thể bán ở kho warehouse_id của các biến thể có trong variant_ids (mã lạ không có dòng; không có dòng số dư →
-- 0), theo mã biến thể. Đọc theo khóa chính biến thể và khóa chính số dư.
SELECT v.variant_id,
       coalesce((SELECT sa.available FROM inventory.stock_available_v AS sa
                 WHERE sa.variant_id = v.variant_id AND sa.warehouse_id = @warehouse_id::uuid), 0)::int AS available
FROM catalog.variants_v AS v
WHERE v.variant_id = ANY(@variant_ids::uuid[])
ORDER BY v.variant_id;

-- name: InStockProducts :many
-- Mẫu trong product_ids có ít nhất một biến thể bán được còn có thể bán (> 0) ở kho warehouse_id (Task 454: bộ phối
-- "hết hàng" khi một món không còn cỡ nào). Mã lạ, mẫu không còn hàng → không có dòng.
SELECT DISTINCT v.product_id
FROM catalog.variants_v AS v
JOIN inventory.stock_available_v AS sa ON sa.variant_id = v.variant_id AND sa.warehouse_id = @warehouse_id::uuid
WHERE v.product_id = ANY(@product_ids::uuid[]) AND v.sellable AND sa.available > 0
ORDER BY v.product_id;
