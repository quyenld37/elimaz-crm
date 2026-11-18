-- Quản lý nhân viên và vai trò mẫu (kế hoạch M1, Task M1-26). Mọi câu chạy trong tenancy.WithTenant (RLS).
-- Thứ tự khóa ADR-46 nhóm 2: users → roles → role_permissions → user_role_grants.

-- name: ListStaffUsers :many
SELECT id, email::text AS email, display_name, status, version, created_at
FROM identity.users
ORDER BY created_at, id;

-- name: GetStaffUser :one
SELECT id, email::text AS email, display_name, status, version, created_at
FROM identity.users
WHERE id = @id;

-- name: ListStaffGrants :many
-- Grant (vai trò, phạm vi) của mọi nhân viên, hoặc của một nhân viên khi user_id khác NULL.
SELECT g.user_id, r.key AS role_key, g.scope_type, g.scope_id
FROM identity.user_role_grants AS g
JOIN identity.roles AS r ON r.tenant_id = g.tenant_id AND r.id = g.role_id
WHERE sqlc.narg(user_id)::uuid IS NULL OR g.user_id = sqlc.narg(user_id)::uuid
ORDER BY g.user_id, r.key, g.scope_type, g.scope_id;

-- name: InsertStaffUser :one
INSERT INTO identity.users (tenant_id, email, display_name)
VALUES (@tenant_id, @email::citext, @display_name)
RETURNING id, email::text AS email, display_name, status, version, created_at;

-- name: AddStaffRoute :exec
SELECT platform.add_staff_route(@email::citext);

-- name: LockStaffRows :many
-- Khóa (theo id tăng dần, một câu) các nhân viên trong ids cùng mọi Chủ còn hoạt động, trước khi đọc lại grant và
-- đếm Chủ (luật "Chủ cuối cùng"; hai Chủ thao tác cùng lúc thì bên sau chờ bên trước).
SELECT u.id, u.status, u.version
FROM identity.users AS u
WHERE u.id = ANY(@ids::uuid[])
   OR (u.status = 'active' AND EXISTS (
        SELECT 1 FROM identity.user_role_grants AS g
        JOIN identity.roles AS r ON r.tenant_id = g.tenant_id AND r.id = g.role_id
        WHERE g.user_id = u.id AND r.key = 'owner' AND g.scope_type = 'all'))
ORDER BY u.id
FOR UPDATE OF u;

-- name: CountActiveOwners :one
SELECT count(DISTINCT u.id)::bigint
FROM identity.users AS u
JOIN identity.user_role_grants AS g ON g.tenant_id = u.tenant_id AND g.user_id = u.id AND g.scope_type = 'all'
JOIN identity.roles AS r ON r.tenant_id = g.tenant_id AND r.id = g.role_id AND r.key = 'owner'
WHERE u.status = 'active';

-- name: SetStaffStatus :one
UPDATE identity.users SET status = @status, version = version + 1
WHERE id = @id
RETURNING version;

-- name: BumpStaffVersion :one
UPDATE identity.users SET version = version + 1
WHERE id = @id AND version = @version
RETURNING version;

-- name: DeleteUserRoleGrant :exec
DELETE FROM identity.user_role_grants AS g
USING identity.roles AS r
WHERE r.tenant_id = g.tenant_id AND r.id = g.role_id
  AND g.user_id = @user_id AND r.key = @role_key AND g.scope_type = @scope_type AND g.scope_id = @scope_id;

-- name: ListRolesWithPermissions :many
-- holder_count: số nhân viên active có ít nhất một grant của vai trò (màn Vai trò, Task M1-56).
SELECT r.id, r.key, r.name, r.status, r.customized, r.version,
       coalesce(array_agg(rp.permission ORDER BY rp.permission) FILTER (WHERE rp.permission IS NOT NULL), '{}')::text[] AS permissions,
       (SELECT count(DISTINCT g.user_id)
        FROM identity.user_role_grants AS g
        JOIN identity.users AS u ON u.tenant_id = g.tenant_id AND u.id = g.user_id
        WHERE g.role_id = r.id AND u.status = 'active')::bigint AS holder_count
FROM identity.roles AS r
LEFT JOIN identity.role_permissions AS rp ON rp.tenant_id = r.tenant_id AND rp.role_id = r.id
GROUP BY r.id
ORDER BY r.created_at, r.id;

-- name: InsertRoleIfMissing :exec
INSERT INTO identity.roles (tenant_id, key, name)
VALUES (@tenant_id, @key, @name)
ON CONFLICT (tenant_id, key) DO NOTHING;

-- name: GetRoleByKey :one
-- FOR SHARE: EnsureDefaultRoles đọc customized rồi mới thêm quyền; lần sửa vai trò chạy cùng lúc phải chờ (Task 315).
SELECT id, customized FROM identity.roles WHERE key = @key FOR SHARE;
