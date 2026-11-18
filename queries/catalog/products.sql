-- Mẫu sản phẩm (kế hoạch M1, Task M1-27). Chạy trong tenancy.WithTenant. Mọi câu đọc, khóa, sửa có seller_id (ADR-29;
-- tests/arch/sellerfilter_test.go, Task M1-37).

-- name: InsertProduct :one
INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, description, article, status)
VALUES (@tenant_id, @seller_id, @code, @name, @name_search, @description, @article, @status)
RETURNING id, version;

-- name: GetProduct :one
SELECT id, seller_id, code, name, description, article, status, version
FROM catalog.products
WHERE seller_id = @seller_id AND id = @id AND deleted_at IS NULL;

-- name: LockProduct :one
SELECT id, seller_id, code, name, description, article, status, version
FROM catalog.products
WHERE seller_id = @seller_id AND id = @id AND deleted_at IS NULL
FOR UPDATE;

-- name: UpdateProduct :one
UPDATE catalog.products
SET name = @name, name_search = @name_search, description = @description, article = @article, status = @status,
    version = version + 1, updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;

-- name: BumpProductVersion :one
-- Mẫu đổi vì biến thể của nó đổi: version tăng (Seq của product.updated).
UPDATE catalog.products
SET version = version + 1, updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;

-- name: SetProductCategory :one
-- Phân loại mẫu (Task M1-34): đổi danh mục, version tăng (Seq của product.updated).
UPDATE catalog.products
SET category_id = sqlc.narg(category_id), version = version + 1, updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;

-- name: GetProductCategory :one
SELECT category_id, version
FROM catalog.products
WHERE seller_id = @seller_id AND id = @id AND deleted_at IS NULL;
