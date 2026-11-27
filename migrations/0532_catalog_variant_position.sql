-- Thứ tự biến thể do nhân viên đặt (Task 513; chủ shop 27/11 chọn «Kéo thả tự xếp»). Pha mở rộng: chỉ thêm.
-- - catalog.variants.position: vị trí của biến thể trong mẫu (1, 2, …); đọc theo (position, id). Biến thể mới vào cuối:
--   trigger variants_position điền cuối + 1 khi câu chèn không đặt vị trí (để 0) — câu InsertVariant không nhắc cột nên
--   bản cũ của máy chủ lúc triển khai và test dựng dữ liệu trên schema cũ vẫn chạy; dòng chèn thẳng cũng vào cuối. Writer
--   đã khóa dòng mẫu khi thêm nên hai lần thêm không cùng vị trí. Sắp lại bằng reorderProductVariants. Không unique
--   (tenant_id, product_id, position): chèn thẳng không khóa mẫu có thể trùng, thứ tự vẫn ổn định theo id (Quyết định 3).
-- - Dòng có sẵn: vị trí theo thứ tự tạo (created_at, id) trong từng mẫu, điền một lần. RLS bị ép cả với chủ bảng (FORCE):
--   tạm bỏ ép trong transaction của migration để chủ bảng thấy mọi tenant, rồi ép lại ngay (như 0201).
-- - catalog.variants_v nối position vào cuối (giữ mọi cột cũ, thứ tự, kiểu, security_invoker; CREATE OR REPLACE giữ
--   GRANT); website đọc biến thể theo vị trí.

-- +goose Up
ALTER TABLE catalog.variants ADD COLUMN position int NOT NULL DEFAULT 0 CHECK (position >= 0);
GRANT UPDATE (position) ON catalog.variants TO crm_app;

ALTER TABLE catalog.variants NO FORCE ROW LEVEL SECURITY;
UPDATE catalog.variants AS v
SET position = r.rn
FROM (SELECT id, row_number() OVER (PARTITION BY tenant_id, product_id ORDER BY created_at, id) AS rn
      FROM catalog.variants) AS r
WHERE v.id = r.id;
ALTER TABLE catalog.variants FORCE ROW LEVEL SECURITY;

-- Trigger chạy với quyền của người chèn (RLS: chỉ thấy dòng của tenant đang chèn, cùng tenant với NEW).
-- +goose StatementBegin
CREATE FUNCTION catalog.variants_position_default()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    IF NEW.position = 0 THEN
        SELECT coalesce(max(v.position), 0) + 1 INTO NEW.position
        FROM catalog.variants AS v
        WHERE v.tenant_id = NEW.tenant_id AND v.product_id = NEW.product_id;
    END IF;
    RETURN NEW;
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER variants_position
    BEFORE INSERT ON catalog.variants
    FOR EACH ROW EXECUTE FUNCTION catalog.variants_position_default();

CREATE OR REPLACE VIEW catalog.variants_v WITH (security_invoker = true) AS
SELECT v.id AS variant_id, v.seller_id, v.product_id, v.sku, v.barcode, p.code AS product_code, p.name AS product_name,
       v.color, v.color_hex, v.size, v.price, v.cost_price, v.status AS variant_status, p.status AS product_status,
       (p.status = 'active' AND v.status = 'active' AND p.deleted_at IS NULL) AS sellable, v.updated_at,
       v.weight_grams, v.position
FROM catalog.variants AS v
JOIN catalog.products AS p ON p.tenant_id = v.tenant_id AND p.seller_id = v.seller_id AND p.id = v.product_id;
