-- Tìm khách theo tên đưa mọi từ vào điều kiện index trigram (kế hoạch M2 Task 88, thử tải M2-31; DH02: tìm bằng tên
-- không dấu dưới 0,3 giây). Pha mở rộng: chỉ thay thân hàm của 0207 (cùng chữ ký, cùng chủ crm_search, cùng quyền).
--
-- Đo 27/11 trên 1 triệu khách: hàm 0207 chỉ đưa từ dài nhất vào index (GIN tenant_id, name_search gin_trgm_ops); từ dài
-- nhất thường là họ ("nguyen" ~38% khách) nên index trả hàng trăm nghìn dòng, LIKE ALL lọc sau khi đọc bảng: p50 239 ms,
-- p95 580 ms. Nay ba từ dài nhất đều là điều kiện LIKE riêng trên name_search nên GIN lấy giao trigram của cả ba ngay
-- trong index; một hay hai từ thì điều kiện lặp lại từ dài nhất (coalesce), không đổi kết quả. Từ thứ tư trở đi và mọi
-- từ vẫn qua LIKE ALL như trước. Cách khớp, thứ tự, giới hạn, tự lọc tenant theo app.tenant_id: như 0207.

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION customer.search_by_name(q_words text[], lim int)
RETURNS TABLE (id uuid, root_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $fn$
    WITH pats AS (
        SELECT array_agg(p.pat ORDER BY p.len DESC, p.pat) AS a
        FROM (SELECT '%' || replace(replace(replace(w, '\', '\\'), '%', '\%'), '_', '\_') || '%' AS pat, length(w) AS len
              FROM unnest(q_words) AS w
              WHERE w <> '') AS p
    )
    SELECT c.id, c.root_id
    FROM customer.customers AS c
    WHERE c.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
        AND c.name_search LIKE (SELECT pats.a[1] FROM pats)
        AND c.name_search LIKE (SELECT coalesce(pats.a[2], pats.a[1]) FROM pats)
        AND c.name_search LIKE (SELECT coalesce(pats.a[3], pats.a[1]) FROM pats)
        AND c.name_search LIKE ALL (SELECT unnest(pats.a) FROM pats)
    ORDER BY c.last_order_at DESC NULLS LAST, c.id
    LIMIT least(greatest(lim, 0), 500)
$fn$;
-- +goose StatementEnd
