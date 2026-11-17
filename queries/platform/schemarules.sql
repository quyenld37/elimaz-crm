-- Kiểm quy tắc schema (kế hoạch M0, Task 5; Global Constraints "Bảng nghiệp vụ"), đọc pg_catalog bằng chủ bảng.
-- Mỗi dòng trả về là một vi phạm "<schema>.<bảng>: <vấn đề>"; chỉ nêu tên bảng, ràng buộc, policy, không giá trị dòng.
-- Bảng nghiệp vụ = bảng thường hay bảng partition có cột tenant_id, ngoài schema hệ thống, không thuộc extension, không
-- nằm trong @platform_tables (tên "schema.bảng").

-- name: SchemaRuleViolations :many
WITH tenant_tables AS (
    SELECT c.oid,
           format('%I.%I', n.nspname, c.relname) AS tbl,
           c.relrowsecurity,
           c.relforcerowsecurity,
           a.attnum AS tenant_att
    FROM pg_catalog.pg_class AS c
    JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_attribute AS a ON a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped
    WHERE c.relkind IN ('r', 'p')
      AND n.nspname NOT IN ('pg_catalog', 'information_schema')
      AND n.nspname NOT LIKE 'pg\_%'
      AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend AS d
                      WHERE d.classid = 'pg_catalog.pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e')
      AND NOT (format('%I.%I', n.nspname, c.relname) = ANY (@platform_tables::text[]))
),
problems AS (
    -- (a) RLS bật, ép, có policy, mọi policy đúng biểu thức chuẩn.
    SELECT t.tbl, 'RLS chưa bật (gọi platform.enable_tenant_rls)' AS problem
    FROM tenant_tables AS t
    WHERE NOT t.relrowsecurity
    UNION ALL
    SELECT t.tbl, 'RLS chưa FORCE (chủ bảng không bị lọc)'
    FROM tenant_tables AS t
    WHERE t.relrowsecurity AND NOT t.relforcerowsecurity
    UNION ALL
    SELECT t.tbl, 'chưa có policy nào'
    FROM tenant_tables AS t
    WHERE t.relrowsecurity
      AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_policy AS p WHERE p.polrelid = t.oid)
    UNION ALL
    SELECT t.tbl, format('policy %I không đúng biểu thức tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid', p.polname)
    FROM tenant_tables AS t
    JOIN pg_catalog.pg_policy AS p ON p.polrelid = t.oid
    WHERE (p.polqual IS NOT NULL
           AND pg_catalog.pg_get_expr(p.polqual, p.polrelid) <> '(tenant_id = (NULLIF(current_setting(''app.tenant_id''::text, true), ''''::text))::uuid)')
       OR (p.polwithcheck IS NOT NULL
           AND pg_catalog.pg_get_expr(p.polwithcheck, p.polrelid) <> '(tenant_id = (NULLIF(current_setting(''app.tenant_id''::text, true), ''''::text))::uuid)')
    UNION ALL
    -- (b) có cột id thì có UNIQUE (tenant_id, id) (index unique đủ, không điều kiện, không biểu thức).
    SELECT t.tbl, 'thiếu UNIQUE (tenant_id, id)'
    FROM tenant_tables AS t
    JOIN pg_catalog.pg_attribute AS ida ON ida.attrelid = t.oid AND ida.attname = 'id' AND NOT ida.attisdropped
    WHERE NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_index AS i
        WHERE i.indrelid = t.oid AND i.indisunique AND i.indnkeyatts = 2 AND i.indpred IS NULL
          AND i.indexprs IS NULL AND i.indkey[0] = t.tenant_att AND i.indkey[1] = ida.attnum)
    UNION ALL
    -- (c) mọi index unique khác khóa chính có tenant_id là cột đầu.
    SELECT t.tbl, format('unique %I không bắt đầu bằng tenant_id', ic.relname)
    FROM tenant_tables AS t
    JOIN pg_catalog.pg_index AS i ON i.indrelid = t.oid AND i.indisunique AND NOT i.indisprimary
    JOIN pg_catalog.pg_class AS ic ON ic.oid = i.indexrelid
    WHERE i.indkey[0] <> t.tenant_att
    UNION ALL
    -- (d) khóa ngoại tới bảng có tenant_id phải nối tenant_id với tenant_id.
    SELECT t.tbl, format('khóa ngoại %I không nối tenant_id với tenant_id của bảng được trỏ', con.conname)
    FROM tenant_tables AS t
    JOIN pg_catalog.pg_constraint AS con ON con.conrelid = t.oid AND con.contype = 'f'
    JOIN pg_catalog.pg_attribute AS pa
      ON pa.attrelid = con.confrelid AND pa.attname = 'tenant_id' AND NOT pa.attisdropped
    WHERE NOT EXISTS (
        SELECT 1 FROM generate_subscripts(con.conkey, 1) AS k(i)
        WHERE con.conkey[k.i] = t.tenant_att AND con.confkey[k.i] = pa.attnum)
    UNION ALL
    -- (e) không bảng nào trong public (bảng của extension không tính).
    SELECT format('%I.%I', n.nspname, c.relname), 'bảng nằm trong schema public'
    FROM pg_catalog.pg_class AS c
    JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
      AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend AS d
                      WHERE d.classid = 'pg_catalog.pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e')
)
SELECT (tbl || ': ' || problem)::text AS violation
FROM problems
ORDER BY tbl, problem;
