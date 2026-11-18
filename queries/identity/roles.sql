-- Vai trò tự định nghĩa (kế hoạch M1, Task M1-56). Mọi câu chạy trong tenancy.WithTenant (RLS).
-- Thứ tự khóa ADR-46 nhóm 2: users → roles → role_permissions → user_role_grants. Sửa, lưu trữ vai trò chỉ khóa dòng
-- roles (FOR UPDATE) rồi đổi role_permissions; đọc user_role_grants không khóa.

-- name: LockRoleNames :exec
-- Khóa tư vấn theo tenant cho tạo vai trò và đổi tên vai trò (database không có ràng buộc tên duy nhất; tên so không
-- phân biệt hoa thường trong mã Go). Lấy trước mọi khóa dòng; chỉ hai đường này lấy, nên không khóa chéo.
SELECT pg_advisory_xact_lock(hashtextextended('identity.roles.name:' || current_setting('app.tenant_id'), 0));

-- name: InsertCustomRole :one
INSERT INTO identity.roles (tenant_id, key, name, customized)
VALUES (@tenant_id, @key, @name, true)
RETURNING id;

-- name: LockRole :one
SELECT id, key, name, status, customized, version
FROM identity.roles
WHERE id = @id
FOR UPDATE;

-- name: UpdateRoleRow :one
UPDATE identity.roles SET name = @name, customized = true, version = version + 1
WHERE id = @id
RETURNING version;

-- name: ArchiveRoleRow :one
UPDATE identity.roles SET status = 'archived', version = version + 1
WHERE id = @id
RETURNING version;

-- name: DeleteRolePermission :exec
DELETE FROM identity.role_permissions
WHERE role_id = @role_id AND permission = @permission;

-- name: CountActiveHolders :one
-- Số nhân viên active có ít nhất một grant của vai trò (luật lưu trữ: role_in_use). Không khóa users (ADR-46: không
-- khóa nhóm trước sau khi đã khóa roles); gán chạy cùng lúc thì khóa dòng vai trò FOR SHARE nên chờ hoặc đã commit.
SELECT count(DISTINCT g.user_id)::bigint
FROM identity.user_role_grants AS g
JOIN identity.users AS u ON u.tenant_id = g.tenant_id AND u.id = g.user_id
WHERE g.role_id = @role_id AND u.status = 'active';

-- name: ShareLockRoles :many
-- ReplaceGrants: khóa FOR SHARE (theo id tăng dần) các vai trò được thêm, sau khi đã khóa users, rồi đọc trạng thái.
SELECT id, status
FROM identity.roles
WHERE id = ANY(@ids::uuid[])
ORDER BY id
FOR SHARE;
