-- Tìm biến thể với từ khóa rộng (kế hoạch M1, Task 390 / M1-39c): catalog.search_variants của 0123 nối biến thể với
-- MỌI mẫu khớp rồi mới cắt lim dòng, nên tên rộng ("dam" khớp 2.000 mẫu) đọc cả bảng catalog.variants (Hash Join, Seq
-- Scan). Thân mới cắt từng nhóm khớp còn tối đa lim dòng TRƯỚC khi gộp:
--   barcode: đúng cả chuỗi (index unique), tối đa 1 dòng;
--   SKU: đi index variants_sku_prefix theo thứ tự byte (USING ~<~), dừng sau lim biến thể của mẫu chưa xóa;
--   mã mẫu: đi index products_code_prefix theo thứ tự byte, mỗi mẫu đọc biến thể qua LATERAL (vòng lặp lồng trên index
--     (tenant_id, seller_id, product_id, …)), dừng sau lim biến thể;
--   tên: các mẫu khớp (index trigram) xếp theo độ liên quan — tên không dấu ngắn hơn trước (các từ của q chiếm phần lớn
--     tên), cùng độ dài theo mã mẫu —, đọc biến thể như trên, dừng sau lim biến thể.
-- Rồi như cũ: mỗi biến thể lấy hạng nhỏ nhất, sắp theo hạng rồi SKU, tối đa lim dòng. Khi không nhóm nào vượt lim, kết
-- quả đúng như 0123; khi một nhóm vượt, nhóm đó cho lim dòng liên quan nhất của nó (không còn là lim SKU nhỏ nhất của
-- cả nhóm). Chữ ký, cột trả về, chủ (crm_search), quyền EXECUTE giữ nguyên (CREATE OR REPLACE giữ chủ và quyền; crm_owner
-- là thành viên crm_search). Pha mở rộng: chỉ thay thân hàm.
--
-- Hàm SECURITY DEFINER chạy bằng crm_search (BYPASSRLS): MỌI câu đọc bảng, kể cả câu con LATERAL, tự lọc tenant_id theo
-- app.tenant_id (ngoài ngữ cảnh: NULL, 0 dòng) và seller_id theo p_seller (ADR-29).

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION catalog.search_variants(p_seller uuid, q text, raw text, lim int)
RETURNS TABLE (
    variant_id uuid, product_id uuid, sku text, barcode text, product_code text, product_name text, color text,
    size text, price bigint, sellable boolean
)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $fn$
    WITH hits AS (
        (SELECT v.id, 0 AS rank
        FROM catalog.variants AS v
        WHERE v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND v.seller_id = p_seller
            AND v.barcode = raw)
        UNION ALL
        (SELECT v.id, 1
        FROM catalog.variants AS v
        JOIN catalog.products AS p ON p.tenant_id = v.tenant_id AND p.seller_id = v.seller_id AND p.id = v.product_id
        WHERE v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND v.seller_id = p_seller
            AND v.sku ~>=~ upper(q) AND v.sku ~<~ (upper(q) || chr(1114111)) AND p.deleted_at IS NULL
        ORDER BY v.sku USING ~<~
        LIMIT least(greatest(lim, 0), 100))
        UNION ALL
        (SELECT pv.id, 2
        FROM catalog.products AS p
        CROSS JOIN LATERAL (
            SELECT v.id, v.sku
            FROM catalog.variants AS v
            WHERE v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND v.seller_id = p_seller
                AND v.product_id = p.id
            ORDER BY v.sku
            LIMIT least(greatest(lim, 0), 100)
        ) AS pv
        WHERE p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND p.seller_id = p_seller
            AND p.code ~>=~ upper(q) AND p.code ~<~ (upper(q) || chr(1114111)) AND p.deleted_at IS NULL
        ORDER BY p.code USING ~<~, pv.sku
        LIMIT least(greatest(lim, 0), 100))
        UNION ALL
        (SELECT pv.id, 3
        FROM (
            SELECT p.id, length(p.name_search) AS len, p.code
            FROM catalog.products AS p
            WHERE p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND p.seller_id = p_seller
                AND p.name_search LIKE '%' || replace(replace(replace(replace(q, '\', '\\'), '%', '\%'), '_', '\_'), ' ', '%') || '%'
                AND p.deleted_at IS NULL
            ORDER BY length(p.name_search), p.code
        ) AS p
        CROSS JOIN LATERAL (
            SELECT v.id, v.sku
            FROM catalog.variants AS v
            WHERE v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND v.seller_id = p_seller
                AND v.product_id = p.id
            ORDER BY v.sku
            LIMIT least(greatest(lim, 0), 100)
        ) AS pv
        ORDER BY p.len, p.code, pv.sku
        LIMIT least(greatest(lim, 0), 100))
    )
    SELECT v.id, v.product_id, v.sku, v.barcode, p.code, p.name, v.color, v.size, v.price,
           (p.status = 'active' AND v.status = 'active')
    FROM (SELECT hits.id, min(hits.rank) AS rank FROM hits GROUP BY hits.id) AS h
    JOIN catalog.variants AS v ON v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND v.seller_id = p_seller AND v.id = h.id
    JOIN catalog.products AS p ON p.tenant_id = v.tenant_id AND p.seller_id = v.seller_id AND p.id = v.product_id
    WHERE q <> '' AND p.deleted_at IS NULL
    ORDER BY h.rank, v.sku
    LIMIT least(greatest(lim, 0), 100)
$fn$;
-- +goose StatementEnd
