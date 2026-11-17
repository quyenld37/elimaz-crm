-- Phép thử ngắn số 1 (ADR-42, ADR-29 điều 5, 04 mục 9 "Tìm kiếm dưới RLS"):
-- kế hoạch chạy của câu tìm tên khi chạy bằng role ứng dụng crm_app (NOBYPASSRLS) trong ngữ cảnh tenant.
--   (a) LIKE '%x%' thẳng trên bảng có RLS  -> mong đợi Seq Scan (RLS chặn index cho toán tử không leakproof)
--   (b) qua hàm SECURITY DEFINER do crm_search (NOLOGIN BYPASSRLS) làm chủ, hàm tự lọc tenant -> mong đợi dùng index trigram
-- Chạy bằng superuser để dựng (database riêng search_probe), rồi \connect sang crm_app để đo. Cần role crm_app, crm_search.
-- psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres -f search-plan-probe.sql
-- Mốc đoạn in ra cả stdout (\echo, kết quả EXPLAIN) và stderr (\warn, NOTICE của auto_explain), vì hai luồng không giữ thứ tự với nhau.

\set QUIET on
DROP DATABASE IF EXISTS search_probe WITH (FORCE);
CREATE DATABASE search_probe;
\connect search_probe postgres

CREATE EXTENSION pg_trgm;
CREATE SCHEMA probe;

CREATE TABLE probe.names (
  id uuid NOT NULL DEFAULT uuidv7() PRIMARY KEY,
  tenant_id uuid NOT NULL,
  name_search text NOT NULL,
  UNIQUE (tenant_id, id)
);
ALTER TABLE probe.names ENABLE ROW LEVEL SECURITY;
ALTER TABLE probe.names FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON probe.names
  USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- 200.000 tên không dấu, chia đều hai tenant. Họ, đệm, tên lấy vòng từ danh sách; thêm số thứ tự để câu tìm hẹp.
INSERT INTO probe.names (tenant_id, name_search)
SELECT CASE WHEN g % 2 = 0 THEN '00000000-0000-0000-0000-00000000000a'::uuid
            ELSE '00000000-0000-0000-0000-00000000000b'::uuid END,
       (ARRAY['nguyen','tran','le','pham','hoang','huynh','phan','vu','vo','dang'])[1 + g % 10] || ' ' ||
       (ARRAY['thi','van','ngoc','minh','thu','hoang','thanh','duc'])[1 + (g / 10) % 8] || ' ' ||
       (ARRAY['anh','hoa','lan','tuyet','mai','huong','linh','trang','phuong','nga','hung','nam'])[1 + (g / 80) % 12] || ' ' || g
FROM generate_series(1, 200000) AS g;
CREATE INDEX names_name_search_trgm ON probe.names USING gin (name_search gin_trgm_ops);

-- Hàm tìm: chủ là crm_search (BYPASSRLS) nên index dùng được; tự lọc tenant theo ngữ cảnh, thiếu ngữ cảnh thì 0 dòng.
-- search_path cố định để không ai chiếm hàm bằng đối tượng cùng tên; ký tự đại diện của LIKE trong q bị thoát.
CREATE FUNCTION probe.search(q text, lim int)
RETURNS TABLE (id uuid, name_search text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $fn$
  SELECT n.id, n.name_search
  FROM probe.names AS n
  WHERE n.tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND n.name_search LIKE '%' || replace(replace(replace(q, '\', '\\'), '%', '\%'), '_', '\_') || '%'
  ORDER BY n.id
  LIMIT least(lim, 100)
$fn$;
GRANT USAGE ON SCHEMA probe TO crm_search, crm_app;
GRANT SELECT ON probe.names TO crm_search, crm_app;
ALTER FUNCTION probe.search(text, int) OWNER TO crm_search;
REVOKE EXECUTE ON FUNCTION probe.search(text, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION probe.search(text, int) TO crm_app;
VACUUM ANALYZE probe.names;

-- Kế hoạch của câu bên trong hàm chỉ thấy được qua auto_explain (EXPLAIN chỉ ra "Function Scan").
-- Bật riêng cho crm_app trong riêng database thử này, mức NOTICE để psql in ra.
ALTER ROLE crm_app IN DATABASE search_probe SET session_preload_libraries = 'auto_explain';
ALTER ROLE crm_app IN DATABASE search_probe SET auto_explain.log_min_duration = 0;
ALTER ROLE crm_app IN DATABASE search_probe SET auto_explain.log_nested_statements = on;
ALTER ROLE crm_app IN DATABASE search_probe SET auto_explain.log_analyze = on;
ALTER ROLE crm_app IN DATABASE search_probe SET auto_explain.log_level = 'notice';
ALTER ROLE crm_app IN DATABASE search_probe SET auto_explain.log_format = 'text';

\connect search_probe crm_app
\set QUIET off
\pset pager off
\echo ==== role
SELECT current_user, session_user,
       (SELECT rolsuper FROM pg_roles WHERE rolname = current_user) AS rolsuper,
       (SELECT rolbypassrls FROM pg_roles WHERE rolname = current_user) AS rolbypassrls;
DO $$
BEGIN
  IF session_user <> 'crm_app' OR (SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = session_user) THEN
    RAISE EXCEPTION 'phép thử phải chạy bằng crm_app không superuser, không BYPASSRLS';
  END IF;
END $$;

-- Ngữ cảnh tenant như ứng dụng đặt (tenancy.WithTenant dùng set_config(..., true) trong transaction; ở đây đặt cho phiên).
SELECT set_config('app.tenant_id', '00000000-0000-0000-0000-00000000000a', false) \gset

\echo ==== (a) LIKE thang tren bang, bang crm_app trong ngu canh tenant
\warn ==== (a) LIKE thang tren bang, bang crm_app trong ngu canh tenant
EXPLAIN (ANALYZE, COSTS OFF, BUFFERS OFF)
SELECT id, name_search FROM probe.names WHERE name_search LIKE '%tuyet 1234%' ORDER BY id LIMIT 100;

\echo ==== (b) qua ham probe.search, bang crm_app trong ngu canh tenant
\warn ==== (b) qua ham probe.search, bang crm_app trong ngu canh tenant
SELECT count(*) AS rows_via_function FROM probe.search('tuyet 1234', 100);

\echo ==== kiem ket qua
\warn ==== kiem ket qua
DO $$
DECLARE
  direct int;
  via_fn int;
  no_ctx int;
  other int;
BEGIN
  SELECT count(*) INTO direct FROM (SELECT 1 FROM probe.names WHERE name_search LIKE '%tuyet 1234%' LIMIT 100) s;
  SELECT count(*) INTO via_fn FROM probe.search('tuyet 1234', 100);
  IF direct = 0 OR direct <> via_fn THEN
    RAISE EXCEPTION 'hàm trả % dòng, câu thẳng trả % dòng: phải bằng nhau và khác 0', via_fn, direct;
  END IF;
  -- Ký tự đại diện trong q không được thành "khớp mọi thứ".
  SELECT count(*) INTO other FROM probe.search('%', 100);
  IF other <> 0 THEN RAISE EXCEPTION 'q = ''%%'' trả % dòng, cần 0', other; END IF;
  PERFORM set_config('app.tenant_id', '', false);
  SELECT count(*) INTO no_ctx FROM probe.search('tuyet 1234', 100);
  IF no_ctx <> 0 THEN RAISE EXCEPTION 'thiếu ngữ cảnh tenant mà hàm trả % dòng', no_ctx; END IF;
  SELECT count(*) INTO no_ctx FROM probe.names;
  IF no_ctx <> 0 THEN RAISE EXCEPTION 'thiếu ngữ cảnh tenant mà bảng trả % dòng', no_ctx; END IF;
  RAISE NOTICE 'KIEM KET QUA OK rows=% no_context_rows=0', via_fn;
END $$;
