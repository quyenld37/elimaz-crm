-- Ảnh sản phẩm (kế hoạch M1, Task M1-44). Chạy trong tenancy.WithTenant. Đường ghi khóa dòng mẫu trước (LockProduct),
-- rồi dòng ảnh (ADR-46 nhóm 4). Trạng thái tệp đọc bằng files.Status, không đọc bảng của kho tệp ở đây.

-- name: ProductVersionOf :one
-- Mẫu chưa xóa của người bán (ADR-29): kiểm có trước khi đọc ảnh.
SELECT version
FROM catalog.products
WHERE seller_id = @seller_id AND id = @id AND deleted_at IS NULL;

-- name: ListProductImages :many
-- Ảnh đang hiện của mẫu theo thứ tự hiện.
SELECT id, file_id, color, position
FROM catalog.product_images
WHERE product_id = @product_id AND deleted_at IS NULL
ORDER BY position, id;

-- name: LockProductImage :one
SELECT id, file_id, color, position, deleted_at
FROM catalog.product_images
WHERE product_id = @product_id AND id = @id
FOR UPDATE;

-- name: LockProductImageByFile :one
SELECT id, file_id, color, position, deleted_at
FROM catalog.product_images
WHERE product_id = @product_id AND file_id = @file_id
FOR UPDATE;

-- name: NextImagePosition :one
-- Vị trí cuối + 1 và số ảnh đang hiện của mẫu (dòng mẫu đã khóa nên không đổi giữa câu này và câu ghi).
SELECT (coalesce(max(position), 0) + 1)::int AS next, count(*)::int AS active
FROM catalog.product_images
WHERE product_id = @product_id AND deleted_at IS NULL;

-- name: InsertProductImage :one
INSERT INTO catalog.product_images (tenant_id, product_id, file_id, color, position, created_by)
VALUES (@tenant_id, @product_id, @file_id, sqlc.narg(color), @position, sqlc.narg(created_by))
RETURNING id;

-- name: ReviveProductImage :exec
-- Gắn lại tệp đã xóa mềm: hồi dòng cũ ở vị trí mới với màu mới.
UPDATE catalog.product_images
SET color = sqlc.narg(color), position = @position, deleted_at = NULL, deleted_by = NULL
WHERE id = @id;

-- name: SetProductImagePosition :exec
UPDATE catalog.product_images
SET position = @position
WHERE id = @id;

-- name: SoftDeleteProductImage :exec
UPDATE catalog.product_images
SET deleted_at = now(), deleted_by = sqlc.narg(deleted_by)
WHERE id = @id AND deleted_at IS NULL;

-- name: RestoreProductImage :one
-- Khôi phục trong hạn: quá hạn thì không đổi dòng nào (bên gọi trả ErrRestoreExpired).
UPDATE catalog.product_images
SET deleted_at = NULL, deleted_by = NULL
WHERE id = @id AND deleted_at IS NOT NULL AND deleted_at > now() - make_interval(days => @keep_days::int)
RETURNING position;
