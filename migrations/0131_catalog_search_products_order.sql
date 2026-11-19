-- Danh sách mẫu với từ khóa rộng (Task 413, phát hiện B-07 của review độc lập M1): catalog.search_products của 0123
-- gộp ba nhánh khớp bằng UNION rồi cắt `lim` (≤ 1.000) mẫu KHÔNG thứ tự, sau đó internal/queries.ListProducts mới
-- sắp theo (updated_at, id) giảm dần — từ khóa khớp hơn 1.000 mẫu ("dam" khớp 2.000) lấy 1.000 mẫu bất kỳ, có thể
-- thiếu mẫu vừa sửa, và mỗi trang tính lại tập đó. Thân mới: cùng ba nhánh khớp, mỗi nhánh mang thêm updated_at của
-- mẫu (đã đọc sẵn dòng mẫu; UNION vốn đã phải tính đủ tập khớp để khử trùng), sắp (updated_at, id) giảm dần — đúng
-- thứ tự của danh sách — RỒI mới cắt: tập trả về là `lim` mẫu khớp sửa gần nhất, các trang (con trỏ (updated_at, id))
-- đi trong cùng một tập.
-- Chữ ký, kiểu trả về, chủ (crm_search), quyền EXECUTE giữ nguyên (CREATE OR REPLACE giữ chủ và quyền). Pha mở rộng:
-- chỉ thay thân hàm.
--
-- Hàm SECURITY DEFINER chạy bằng crm_search (BYPASSRLS): MỌI câu đọc bảng tự lọc tenant_id theo
-- app.tenant_id (ngoài ngữ cảnh: NULL, 0 dòng) và seller_id theo p_seller (ADR-29).

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION catalog.search_products(p_seller uuid, q text, raw text, lim int)
RETURNS SETOF uuid
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $fn$
    SELECT m.id
    FROM (
        SELECT p.id, p.updated_at
        FROM catalog.variants AS v
        JOIN catalog.products AS p ON p.tenant_id = v.tenant_id AND p.seller_id = v.seller_id AND p.id = v.product_id
        WHERE v.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND v.seller_id = p_seller
            AND (v.barcode = raw OR (v.sku ~>=~ upper(q) AND v.sku ~<~ (upper(q) || chr(1114111))))
            AND p.deleted_at IS NULL AND q <> ''
        UNION
        SELECT p.id, p.updated_at
        FROM catalog.products AS p
        WHERE p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND p.seller_id = p_seller
            AND p.code ~>=~ upper(q) AND p.code ~<~ (upper(q) || chr(1114111)) AND p.deleted_at IS NULL AND q <> ''
        UNION
        SELECT p.id, p.updated_at
        FROM catalog.products AS p
        WHERE p.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND p.seller_id = p_seller
            AND p.name_search LIKE '%' || replace(replace(replace(replace(q, '\', '\\'), '%', '\%'), '_', '\_'), ' ', '%') || '%'
            AND p.deleted_at IS NULL AND q <> ''
    ) AS m
    ORDER BY m.updated_at DESC, m.id DESC
    LIMIT least(greatest(lim, 0), 1000)
$fn$;
-- +goose StatementEnd
