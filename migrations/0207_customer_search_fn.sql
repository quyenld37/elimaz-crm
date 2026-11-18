-- Tìm khách dưới RLS (kế hoạch M2, Task M2-10; spec 04 mục 7, ADR-08). Pha mở rộng: chỉ CREATE FUNCTION, CREATE
-- INDEX, quyền.
--
-- Đuôi SĐT, SĐT đủ, mã khách: câu thẳng bằng crm_app (toán tử so khoảng ~>=~ ~<~ và = trên text là leakproof nên
-- index (tenant_id, kind, value_reversed text_pattern_ops), unique (tenant_id, kind, scope, value), unique
-- (tenant_id, code) dùng được dưới RLS) — queries/customer/search.sql.
--
-- Tên: LIKE không leakproof nên câu thẳng dưới RLS là Seq Scan. Hàm SECURITY DEFINER do crm_search (BYPASSRLS, không
-- đăng nhập) làm chủ dùng index GIN (tenant_id, name_search gin_trgm_ops) của 0201, nên TỰ lọc tenant_id theo
-- app.tenant_id (ngoài ngữ cảnh: NULL, 0 dòng). search_path cố định (pg_temp cuối cùng), mọi tên viết đủ schema.
--
-- Cách khớp: q_words là các từ của chuỗi tìm đã qua textnorm.Fold ở Go (chữ thường, bỏ dấu); mỗi từ phải có trong
-- name_search, không theo thứ tự (LIKE ALL '%từ%', đã thoát \ % _). GIN không nhận LIKE ALL (mảng) làm điều kiện index,
-- nên thêm một điều kiện LIKE trên từ dài nhất (nhiều trigram nhất) để đi index; LIKE ALL lọc phần còn lại.
-- Trả (id, root_id) của hồ sơ khớp (gốc hay con), mua gần nhất trước, chưa mua sau cùng, rồi theo id; tối đa
-- least(lim, 500) dòng. Bên gọi gom theo root_id.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION customer.search_by_name(q_words text[], lim int)
RETURNS TABLE (id uuid, root_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $fn$
    WITH pats AS (
        SELECT '%' || replace(replace(replace(w, '\', '\\'), '%', '\%'), '_', '\_') || '%' AS pat, length(w) AS len
        FROM unnest(q_words) AS w
        WHERE w <> ''
    )
    SELECT c.id, c.root_id
    FROM customer.customers AS c
    WHERE c.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND c.name_search LIKE (SELECT pats.pat FROM pats ORDER BY pats.len DESC, pats.pat LIMIT 1)
        AND c.name_search LIKE ALL (SELECT pats.pat FROM pats)
    ORDER BY c.last_order_at DESC NULLS LAST, c.id
    LIMIT least(greatest(lim, 0), 500)
$fn$;
-- +goose StatementEnd

-- Đổi chủ cần chủ mới có CREATE trên schema: cấp tạm rồi thu lại ngay (như 0123).
GRANT CREATE ON SCHEMA customer TO crm_search;
ALTER FUNCTION customer.search_by_name(text[], int) OWNER TO crm_search;
REVOKE CREATE ON SCHEMA customer FROM crm_search;
REVOKE ALL ON FUNCTION customer.search_by_name(text[], int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION customer.search_by_name(text[], int) TO crm_app;
-- crm_search chỉ đọc bảng khách (USAGE trên schema customer đã cấp ở 0112).
GRANT SELECT ON customer.customers TO crm_search;

-- Danh sách khách gốc mới nhất trước, trang theo (created_at, id) (queries/customer/search.sql ListRoots).
CREATE INDEX customers_roots_created ON customer.customers (tenant_id, created_at DESC, id DESC) WHERE id = root_id;
