-- Người dùng và quyền theo phạm vi (kế hoạch M0, Task 7). Mọi câu chạy trong tenancy.WithTenant (RLS).

-- name: GetUserByEmail :one
SELECT id, email::text AS email, status
FROM identity.users
WHERE email = @email::citext;

-- name: ListUserGrants :many
-- Cặp (quyền, phạm vi) của user qua mọi vai trò được gán còn active (vai trò đã lưu trữ không cho quyền, Task M1-56);
-- trùng lặp được bỏ.
SELECT DISTINCT rp.permission, g.scope_type, g.scope_id
FROM identity.user_role_grants AS g
JOIN identity.roles AS r ON r.tenant_id = g.tenant_id AND r.id = g.role_id AND r.status = 'active'
JOIN identity.role_permissions AS rp ON rp.tenant_id = g.tenant_id AND rp.role_id = g.role_id
WHERE g.user_id = @user_id
ORDER BY rp.permission, g.scope_type, g.scope_id;

-- name: AddRolePermission :exec
INSERT INTO identity.role_permissions (tenant_id, role_id, permission)
VALUES (@tenant_id, @role_id, @permission)
ON CONFLICT DO NOTHING;

-- name: UpsertUser :one
-- Seed (chủ bảng): user theo email; chạy lại thì giữ nguyên trạng thái, trả mã cũ.
INSERT INTO identity.users (tenant_id, email, display_name)
VALUES (@tenant_id, @email::citext, @display_name)
ON CONFLICT (tenant_id, email) DO UPDATE SET display_name = EXCLUDED.display_name
RETURNING id;

-- name: AddUserRoleGrant :exec
INSERT INTO identity.user_role_grants (tenant_id, user_id, role_id, scope_type, scope_id)
VALUES (@tenant_id, @user_id, @role_id, @scope_type, @scope_id)
ON CONFLICT DO NOTHING;

-- name: GetUserByID :one
-- Nhân viên theo mã (worker nạp lại người tạo một lần chạy nền, kế hoạch M6 Task 235): trạng thái và email đọc lúc chạy.
SELECT id, tenant_id, email::text AS email, status
FROM identity.users
WHERE id = @id;
