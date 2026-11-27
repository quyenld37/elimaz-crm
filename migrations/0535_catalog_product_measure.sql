-- Số đo của mẫu sản phẩm (Task 516; chốt thay chủ shop 27/11 «Số đo sản phẩm: LÀM»). Pha mở rộng: chỉ thêm.
-- - catalog.products.length_cm, width_cm, height_cm: Dài, Rộng, Cao (cm, số nguyên 1–1000), mỗi số tùy chọn; rỗng là
--   chưa nhập. Website hiện khối «Số đo» chỉ với các số đã nhập (measureCm của API web).
-- - crm_app có INSERT cả bảng (0114); thêm UPDATE ba cột.
-- - catalog.products_v nối ba cột vào cuối; giữ mọi cột cũ, thứ tự, kiểu, security_invoker (CREATE OR REPLACE giữ GRANT).

-- +goose Up
ALTER TABLE catalog.products
    ADD COLUMN length_cm int CHECK (length_cm BETWEEN 1 AND 1000),
    ADD COLUMN width_cm int CHECK (width_cm BETWEEN 1 AND 1000),
    ADD COLUMN height_cm int CHECK (height_cm BETWEEN 1 AND 1000);
GRANT UPDATE (length_cm, width_cm, height_cm) ON catalog.products TO crm_app;

CREATE OR REPLACE VIEW catalog.products_v WITH (security_invoker = true) AS
SELECT id AS product_id, seller_id, code, name, name_search, description, article, status, category_id, deleted_at,
       updated_at, version, created_at, length_cm, width_cm, height_cm
FROM catalog.products;
