-- Danh tính của khách (kế hoạch M2, Task M2-09; 05 mục 1). Một (kind, scope, value) chỉ thuộc một hồ sơ
-- (customer_identities_value_key). Chạy trong tenancy.WithTenant.

-- name: InsertIdentity :one
-- Đã có chủ (kể cả bên kia vừa commit trong lúc chờ) → không chèn, pgx.ErrNoRows; bên gọi đọc chủ bằng GetIdentityOwner.
INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, scope, value, value_reversed, source,
                                          created_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @customer_id, @kind, @scope, @value, @value_reversed,
        @source, sqlc.narg(created_by)::uuid)
ON CONFLICT (tenant_id, kind, scope, value) DO NOTHING
RETURNING id, tenant_id, customer_id, kind, scope, value, verified_at, source, created_at;

-- name: GetIdentityOwner :one
SELECT i.customer_id, c.root_id
FROM customer.customer_identities i
JOIN customer.customers c ON c.tenant_id = i.tenant_id AND c.id = i.customer_id
WHERE i.kind = @kind
  AND i.scope = @scope
  AND i.value = @value;

-- name: GetIdentity :one
SELECT id, customer_id, kind
FROM customer.customer_identities
WHERE id = @id;

-- name: DeleteIdentity :execrows
DELETE FROM customer.customer_identities
WHERE id = @id;

-- name: ListGroupIdentities :many
SELECT id, customer_id, kind, scope, value, verified_at, source, created_at
FROM customer.customer_identities
WHERE customer_id = ANY (@customer_ids::uuid[])
ORDER BY created_at, id;
