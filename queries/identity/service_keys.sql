-- Khóa dịch vụ của website (kế hoạch M5, Task 189). Mọi câu chạy trong tenancy.WithTenant (RLS). Không câu nào trả
-- key_hash ra ngoài đường kiểm khóa (GetServiceKeyByPrefix).

-- name: InsertServiceKey :one
INSERT INTO identity.service_keys (tenant_id, name, key_prefix, key_hash, permissions, allowed_cidrs, created_by)
VALUES (@tenant_id, @name, @key_prefix, @key_hash, @permissions::text[], CAST(sqlc.arg(allowed_cidrs)::text[] AS cidr[]), @created_by)
RETURNING id, created_at, version;

-- name: ListServiceKeys :many
SELECT id, name, key_prefix, permissions, allowed_cidrs::text[] AS allowed_cidrs, created_at, revoked_at, last_used_at,
    version
FROM identity.service_keys
ORDER BY created_at, id;

-- name: GetServiceKeyByPrefix :one
SELECT id, tenant_id, key_hash, permissions, allowed_cidrs::text[] AS allowed_cidrs, revoked_at
FROM identity.service_keys
WHERE key_prefix = @key_prefix;

-- name: RevokeServiceKey :one
-- Thu hồi có điều kiện: đúng version và chưa thu hồi (0 dòng → bên gọi phân biệt không có / lệch version).
UPDATE identity.service_keys
SET revoked_at = now(), revoked_by = sqlc.narg(revoked_by), version = version + 1
WHERE id = @id AND version = @version AND revoked_at IS NULL
RETURNING id, name, key_prefix, permissions, allowed_cidrs::text[] AS allowed_cidrs, created_at, revoked_at, last_used_at,
    version;

-- name: ServiceKeyExists :one
SELECT EXISTS (SELECT 1 FROM identity.service_keys WHERE id = @id);

-- name: TouchServiceKeys :exec
-- Mốc dùng gần nhất chỉ tiến lên (hai tiến trình ghi lệch thứ tự không lùi mốc); không đổi version.
UPDATE identity.service_keys
SET last_used_at = @at
WHERE id = ANY(@ids::uuid[]) AND (last_used_at IS NULL OR last_used_at < @at);
