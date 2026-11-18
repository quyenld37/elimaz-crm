-- Tìm và danh sách sản phẩm (kế hoạch M1, Task M1-39; spec 04 mục 7, cách đã đo ở Task M0-05): index cho danh sách
-- mẫu trang theo (updated_at, id), index tìm (trigram trên tên không dấu, tiền tố mã mẫu và SKU), hai hàm tìm
-- SECURITY DEFINER do crm_search làm chủ. Pha mở rộng: chỉ CREATE INDEX, CREATE FUNCTION, quyền.
--
-- Vì sao hàm SECURITY DEFINER: tìm thẳng trên bảng có RLS là Seq Scan (LIKE không leakproof nên không đẩy được vào
-- index trước policy). Hàm chạy bằng crm_search (BYPASSRLS) nên TỰ lọc tenant_id theo app.tenant_id (ngoài ngữ cảnh:
-- NULL, 0 dòng) và theo người bán gửi vào (ADR-29). search_path cố định, mọi tên viết đủ schema.
--
-- Cách khớp (q đã qua textnorm.Fold ở Go: chữ thường, bỏ dấu, gộp khoảng trắng; raw là chuỗi gốc chỉ bỏ khoảng trắng
-- đầu cuối):
--   barcode đúng cả chuỗi raw, phân biệt hoa thường như lúc ghi (index unique sẵn có (tenant_id, seller_id, barcode));
--   SKU, mã mẫu theo tiền tố, không phân biệt hoa thường (mã lưu viết hoa; khoảng [upper(q), upper(q) || U+10FFFF)
--   theo thứ tự byte của text_pattern_ops, vì LIKE 'x%' chỉ dùng được index khi mẫu là hằng lúc lập kế hoạch);
--   tên: các từ của q xuất hiện theo thứ tự trong name_search (LIKE '%w1%w2%', index GIN trigram).

-- +goose Up
CREATE INDEX products_name_search_trgm ON catalog.products USING gin (name_search gin_trgm_ops);
CREATE INDEX products_code_prefix ON catalog.products (tenant_id, seller_id, code text_pattern_ops);
CREATE INDEX variants_sku_prefix ON catalog.variants (tenant_id, seller_id, sku text_pattern_ops);
-- Danh sách mẫu (internal/queries.ListProducts): sửa gần nhất trước, trang theo con trỏ (updated_at, id).
CREATE INDEX products_list_updated ON catalog.products (tenant_id, seller_id, updated_at DESC, id DESC)
    WHERE deleted_at IS NULL;

-- Biến thể khớp q của người bán p_seller trong tenant đang đặt, tối đa least(lim, 100) dòng: barcode đúng trước, rồi
-- SKU, mã mẫu, tên; cùng hạng theo SKU. Không trả biến thể của mẫu đã xóa. sellable như catalog.variants_v.
-- +goose StatementBegin
CREATE FUNCTION catalog.search_variants(p_seller uuid, q text, raw text, lim int)
RETURNS TABLE (
    variant_id uuid, product_id uuid, sku text, barcode text, product_code text, product_name text, color text,
    size text, price bigint, sellable boolean
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $fn$
    WITH hits AS (
        SELECT v.id, 0 AS rank
        FROM catalog.variants AS v
        WHERE v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND v.seller_id = p_seller
            AND v.barcode = raw
        UNION ALL
        SELECT v.id, 1
        FROM catalog.variants AS v
        WHERE v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND v.seller_id = p_seller
            AND v.sku ~>=~ upper(q) AND v.sku ~<~ (upper(q) || chr(1114111))
        UNION ALL
        SELECT v.id, 2
        FROM catalog.products AS p
        JOIN catalog.variants AS v ON v.tenant_id = p.tenant_id AND v.seller_id = p.seller_id AND v.product_id = p.id
        WHERE p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND p.seller_id = p_seller
            AND p.code ~>=~ upper(q) AND p.code ~<~ (upper(q) || chr(1114111))
        UNION ALL
        SELECT v.id, 3
        FROM catalog.products AS p
        JOIN catalog.variants AS v ON v.tenant_id = p.tenant_id AND v.seller_id = p.seller_id AND v.product_id = p.id
        WHERE p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND p.seller_id = p_seller
            AND p.name_search LIKE '%' || replace(replace(replace(replace(q, '\', '\\'), '%', '\%'), '_', '\_'), ' ', '%') || '%'
    )
    SELECT v.id, v.product_id, v.sku, v.barcode, p.code, p.name, v.color, v.size, v.price,
           (p.status = 'active' AND v.status = 'active')
    FROM (SELECT hits.id, min(hits.rank) AS rank FROM hits GROUP BY hits.id) AS h
    JOIN catalog.variants AS v ON v.id = h.id
    JOIN catalog.products AS p ON p.tenant_id = v.tenant_id AND p.seller_id = v.seller_id AND p.id = v.product_id
    WHERE q <> '' AND p.deleted_at IS NULL
    ORDER BY h.rank, v.sku
    LIMIT least(greatest(lim, 0), 100)
$fn$;
-- +goose StatementEnd

-- Mã các mẫu (chưa xóa) của người bán p_seller trong tenant đang đặt có mã mẫu, SKU (tiền tố), barcode (đúng) hoặc tên
-- khớp q như search_variants; tối đa least(lim, 1000) mẫu, không theo thứ tự nào (bên gọi tự sắp).
-- +goose StatementBegin
CREATE FUNCTION catalog.search_products(p_seller uuid, q text, raw text, lim int)
RETURNS SETOF uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $fn$
    SELECT p.id
    FROM catalog.variants AS v
    JOIN catalog.products AS p ON p.tenant_id = v.tenant_id AND p.seller_id = v.seller_id AND p.id = v.product_id
    WHERE v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND v.seller_id = p_seller
        AND (v.barcode = raw OR (v.sku ~>=~ upper(q) AND v.sku ~<~ (upper(q) || chr(1114111))))
        AND p.deleted_at IS NULL AND q <> ''
    UNION
    SELECT p.id
    FROM catalog.products AS p
    WHERE p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND p.seller_id = p_seller
        AND p.code ~>=~ upper(q) AND p.code ~<~ (upper(q) || chr(1114111)) AND p.deleted_at IS NULL AND q <> ''
    UNION
    SELECT p.id
    FROM catalog.products AS p
    WHERE p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND p.seller_id = p_seller
        AND p.name_search LIKE '%' || replace(replace(replace(replace(q, '\', '\\'), '%', '\%'), '_', '\_'), ' ', '%') || '%'
        AND p.deleted_at IS NULL AND q <> ''
    LIMIT least(greatest(lim, 0), 1000)
$fn$;
-- +goose StatementEnd

-- Đổi chủ cần chủ mới có CREATE trên schema: cấp tạm rồi thu lại ngay trong cùng migration (crm_search không giữ
-- quyền tạo đối tượng nào).
GRANT CREATE ON SCHEMA catalog TO crm_search;
ALTER FUNCTION catalog.search_variants(uuid, text, text, int) OWNER TO crm_search;
ALTER FUNCTION catalog.search_products(uuid, text, text, int) OWNER TO crm_search;
REVOKE CREATE ON SCHEMA catalog FROM crm_search;
REVOKE EXECUTE ON FUNCTION catalog.search_variants(uuid, text, text, int), catalog.search_products(uuid, text, text, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION catalog.search_variants(uuid, text, text, int), catalog.search_products(uuid, text, text, int) TO crm_app;
-- crm_search đọc hai bảng tìm (USAGE trên schema catalog đã cấp ở 0112).
GRANT SELECT ON catalog.products, catalog.variants TO crm_search;
