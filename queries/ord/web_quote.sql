-- Báo giá cho website (kế hoạch M5, Task 192 / M5-09). Chỉ đọc view đã công bố.

-- name: ProductCategories :many
-- Danh mục của các mẫu product_ids của người bán seller_id (phạm vi "categories" của chiến dịch web); mẫu không có
-- danh mục trả category_id NULL; mẫu lạ hay của người bán khác không có dòng.
SELECT product_id, category_id
FROM catalog.products_v
WHERE seller_id = @seller_id AND product_id = ANY(@product_ids::uuid[]);
