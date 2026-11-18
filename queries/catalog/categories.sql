-- Danh mục dạng cây tối đa 3 cấp (kế hoạch M1, Task M1-34). Chạy trong tenancy.WithTenant. Danh mục dùng chung cả
-- cửa hàng (không có seller_id, spec 05). Khóa theo ADR-46 nhóm 4: danh mục trước mẫu.

-- name: InsertCategory :one
INSERT INTO catalog.categories (tenant_id, parent_id, name, slug, depth, position)
VALUES (@tenant_id, sqlc.narg(parent_id), @name, @slug, @depth, @position)
RETURNING id, parent_id, name, slug, depth, position, status, version;

-- name: ShareLockCategory :one
-- Khóa chia sẻ danh mục id (cha khi thêm con, danh mục khi gán cho mẫu): đổi cha hay lưu trữ phải chờ.
SELECT id, depth, status FROM catalog.categories WHERE id = @id FOR SHARE;

-- name: LockCategory :one
SELECT id, parent_id, name, slug, depth, position, status, version
FROM catalog.categories
WHERE id = @id
FOR UPDATE;

-- name: LockAllCategories :many
-- Đổi cha: khóa mọi danh mục của shop theo id tăng dần rồi mới kiểm vòng và độ sâu (cây vài chục dòng).
SELECT id FROM catalog.categories ORDER BY id FOR UPDATE;

-- name: CategorySubtree :one
-- Độ sâu lớn nhất trong cây con của id (kể cả id) và cây con có chứa candidate không (đổi cha: candidate là cha mới).
WITH RECURSIVE sub AS (
    SELECT c.id, c.depth FROM catalog.categories AS c WHERE c.id = @id
    UNION ALL
    SELECT c.id, c.depth FROM catalog.categories AS c JOIN sub ON c.parent_id = sub.id
)
SELECT coalesce(max(sub.depth), 0)::smallint AS max_depth,
       coalesce(bool_or(sub.id = @candidate::uuid), false)::bool AS contains_candidate
FROM sub;

-- name: ShiftDescendantsDepth :exec
-- Cộng delta vào depth của mọi con cháu của id (không kể id).
WITH RECURSIVE sub AS (
    SELECT c.id FROM catalog.categories AS c WHERE c.parent_id = @id::uuid
    UNION ALL
    SELECT c.id FROM catalog.categories AS c JOIN sub ON c.parent_id = sub.id
)
UPDATE catalog.categories
SET depth = depth + @delta::smallint, updated_at = now()
WHERE id IN (SELECT sub.id FROM sub);

-- name: UpdateCategory :one
UPDATE catalog.categories
SET parent_id = sqlc.narg(parent_id), name = @name, slug = @slug, depth = @depth, position = @position,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING id, parent_id, name, slug, depth, position, status, version;

-- name: CategoryInUse :one
-- Danh mục còn con active hoặc còn mẫu active chưa xóa.
SELECT (EXISTS (SELECT 1 FROM catalog.categories AS c WHERE c.parent_id = @id::uuid AND c.status = 'active')
    OR EXISTS (SELECT 1 FROM catalog.products AS p
               WHERE p.category_id = @id::uuid AND p.status = 'active' AND p.deleted_at IS NULL))::bool AS in_use;

-- name: ArchiveCategory :one
UPDATE catalog.categories
SET status = 'archived', version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING id, parent_id, name, slug, depth, position, status, version;

-- name: ListCategories :many
SELECT id, parent_id, name, slug, depth, position, status, version
FROM catalog.categories
ORDER BY depth, position, name, id;
