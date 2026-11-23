-- Danh mục cho website (kế hoạch M5, Task 190 / M5-07; QĐ30, Q-X2). Pha mở rộng: chỉ thêm.
-- - catalog.variants.weight_grams: cân nặng của biến thể (gam) cho báo phí ship; rỗng = chưa nhập (phí dùng mặc định).
-- - catalog.variants_v nối weight_grams vào cuối; catalog.products_v nối version vào cuối (giữ mọi cột cũ, thứ tự,
--   kiểu, security_invoker; CREATE OR REPLACE giữ GRANT). updated_at của variants_v, stock_available_v đã có (M1).
-- - Index cho luồng thay đổi của website (updated_at ≥ mốc của mẫu, của số dư) và đọc biến thể theo mẫu của một trang
--   (không Seq Scan). Biến thể không cần index updated_at: mọi lần ghi biến thể tăng updated_at của mẫu (bumpProduct).

-- +goose Up
ALTER TABLE catalog.variants ADD COLUMN weight_grams int CHECK (weight_grams BETWEEN 1 AND 50000);
GRANT UPDATE (weight_grams) ON catalog.variants TO crm_app;

CREATE OR REPLACE VIEW catalog.products_v WITH (security_invoker = true) AS
SELECT id AS product_id, seller_id, code, name, name_search, description, article, status, category_id, deleted_at,
       updated_at, version
FROM catalog.products;

CREATE OR REPLACE VIEW catalog.variants_v WITH (security_invoker = true) AS
SELECT v.id AS variant_id, v.seller_id, v.product_id, v.sku, v.barcode, p.code AS product_code, p.name AS product_name,
       v.color, v.color_hex, v.size, v.price, v.cost_price, v.status AS variant_status, p.status AS product_status,
       (p.status = 'active' AND v.status = 'active' AND p.deleted_at IS NULL) AS sellable, v.updated_at,
       v.weight_grams
FROM catalog.variants AS v
JOIN catalog.products AS p ON p.tenant_id = v.tenant_id AND p.seller_id = v.seller_id AND p.id = v.product_id;

CREATE INDEX products_web_changes ON catalog.products (tenant_id, updated_at);
CREATE INDEX variants_product_idx ON catalog.variants (tenant_id, product_id);
CREATE INDEX stock_balances_web_changes ON inventory.stock_balances (tenant_id, updated_at);
