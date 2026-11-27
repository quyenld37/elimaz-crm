-- Số suất còn trống của món thuộc đợt đã lên lịch hay đang mở (kế hoạch M5, Task 204 / M5-21): vòng dựng lại cờ hết
-- suất và số còn trên cổng flash sale đọc mỗi 2 s và theo báo hiệu. Chỉ đọc, không khóa. Đơn vị suất đọc qua view
-- inventory.flash_units_summary_v bằng câu con theo từng món (lọc theo món trước khi gộp: dùng index
-- flash_sale_units_pick, không gộp cả bảng đơn vị của mọi đợt cũ).

-- name: FlashActiveRemaining :many
-- Món của đợt scheduled/open; sale_id, item_id NULL là không lọc. Theo mã món.
SELECT i.id, i.flash_sale_id,
       coalesce((SELECT u.available FROM inventory.flash_units_summary_v AS u
                 WHERE u.flash_sale_item_id = i.id), 0)::int AS available
FROM promotion.flash_sale_items AS i
JOIN promotion.flash_sales AS s ON s.id = i.flash_sale_id
WHERE s.status IN ('scheduled', 'open')
  AND (sqlc.narg(sale_id)::uuid IS NULL OR i.flash_sale_id = sqlc.narg(sale_id)::uuid)
  AND (sqlc.narg(item_id)::uuid IS NULL OR i.id = sqlc.narg(item_id)::uuid)
ORDER BY i.id;
