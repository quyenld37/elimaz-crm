-- Đợt flash sale cho website (kế hoạch M5, Task 204 / M5-21). Chỉ đọc view _v đã công bố.

-- name: FlashCurrentItems :many
-- Món của đợt scheduled/open chưa quá ends_at (now), kèm đợt, mẫu và giá thường của biến thể; theo giờ mở, mã đợt,
-- mã món. Đợt không còn món nào thì không có dòng.
SELECT s.id AS sale_id, s.name, s.starts_at, s.ends_at,
       i.id AS item_id, i.variant_id, v.product_id, i.sale_price, v.price AS regular_price, i.quantity,
       i.per_customer_limit, i.hold_minutes
FROM promotion.flash_sales_v AS s
JOIN promotion.flash_sale_items_v AS i ON i.flash_sale_id = s.id
JOIN catalog.variants_v AS v ON v.variant_id = i.variant_id
WHERE s.status IN ('scheduled', 'open') AND s.ends_at > @now::timestamptz
ORDER BY s.starts_at, s.id, i.id;

-- name: FlashAvailableOf :many
-- Số đơn vị suất còn trống của các món trong item_ids (món chưa trải đơn vị → không có dòng).
SELECT u.flash_sale_item_id, u.available::int AS available
FROM inventory.flash_units_summary_v AS u
WHERE u.flash_sale_item_id = ANY(@item_ids::uuid[])
ORDER BY u.flash_sale_item_id;
