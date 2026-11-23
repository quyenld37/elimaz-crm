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
