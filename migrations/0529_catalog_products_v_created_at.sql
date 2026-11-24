-- Ngày tạo của mẫu cho website (Task 489; website Task 211 sắp xếp "Mới nhất"). Pha mở rộng: chỉ thêm.
-- - catalog.products_v nối created_at (cột có sẵn của catalog.products từ 0114) vào cuối; giữ mọi cột cũ, thứ tự, kiểu,
--   security_invoker (CREATE OR REPLACE giữ GRANT).

-- +goose Up
CREATE OR REPLACE VIEW catalog.products_v WITH (security_invoker = true) AS
SELECT id AS product_id, seller_id, code, name, name_search, description, article, status, category_id, deleted_at,
       updated_at, version, created_at
FROM catalog.products;
