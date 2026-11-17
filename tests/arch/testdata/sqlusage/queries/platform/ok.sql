-- Tệp mẫu đúng luật (không bị báo): bảng có schema, hàm trong FROM, CTE, DISTINCT FROM, EXTRACT.
-- name: Ok :many
WITH recent AS (SELECT id, created_at FROM platform.tenants)
SELECT r.id, extract(epoch FROM r.created_at)::bigint AS ts
FROM recent r
JOIN platform.tenant_routes tr ON tr.tenant_id = r.id
CROSS JOIN unnest(@names::text[]) AS n(name)
WHERE tr.kind IS DISTINCT FROM n.name;
-- name: OkInsert :exec
INSERT INTO platform.tenants (name) VALUES (@name);
-- name: OkFunctionsAndUsing :many
SELECT trim(leading 'x' FROM t.name), substring(t.name FROM 2)
FROM platform.tenants t
JOIN platform.tenant_routes r USING (id), platform.tenant_routes r2
WHERE t.id IN (SELECT tenant_id FROM platform.tenant_routes);
-- name: OkUpdateFrom :exec
UPDATE platform.tenants t SET name = r.kind FROM platform.tenant_routes r, platform.tenants t2 WHERE r.tenant_id = t.id;
-- name: OkLockSkip :many
SELECT id FROM platform.tenants ORDER BY id FOR UPDATE SKIP LOCKED;
-- name: OkLockOf :many
SELECT t.id FROM platform.tenants t FOR NO KEY UPDATE OF t NOWAIT;
