-- Mẫu sản phẩm (kế hoạch M1, Task M1-27). Chạy trong tenancy.WithTenant. Mọi câu đọc, khóa, sửa có seller_id (ADR-29;
-- tests/arch/sellerfilter_test.go, Task M1-37).

-- name: InsertProduct :one
-- id rỗng (uuid.Nil) → uuidv7() như mặc định của cột (id đặt trước: lệnh seed-web-fixture, Task 218); created_at NULL
-- → now() như mặc định của cột (đặt trước: seed-web-fixture, Task 511). Không nhắc cột số đo (Task 516: SetProductMeasure),
-- để test dựng dữ liệu trên schema trước 0535 (catalogtest.Product) vẫn chạy.
INSERT INTO catalog.products (id, tenant_id, seller_id, code, name, name_search, description, article, status, created_at)
VALUES (coalesce(nullif(@id::uuid, '00000000-0000-0000-0000-000000000000'), uuidv7()), @tenant_id, @seller_id, @code, @name, @name_search, @description, @article, @status,
        coalesce(sqlc.narg(created_at)::timestamptz, now()))
RETURNING id, version;

-- name: SetProductMeasure :exec
-- Số đo của mẫu vừa tạo trong cùng transaction (Task 516; NULL là chưa nhập). Không tăng version: là một phần của lần
-- tạo; sửa số đo sau đó đi qua UpdateProduct.
UPDATE catalog.products
SET length_cm = sqlc.narg(length_cm)::int, width_cm = sqlc.narg(width_cm)::int, height_cm = sqlc.narg(height_cm)::int
WHERE seller_id = @seller_id AND id = @id;

-- name: GetProduct :one
SELECT id, seller_id, code, name, description, article, status, version, length_cm, width_cm, height_cm
FROM catalog.products
WHERE seller_id = @seller_id AND id = @id AND deleted_at IS NULL;

-- name: LockProduct :one
SELECT id, seller_id, code, name, description, article, status, version, length_cm, width_cm, height_cm
FROM catalog.products
WHERE seller_id = @seller_id AND id = @id AND deleted_at IS NULL
FOR UPDATE;

-- name: UpdateProduct :one
UPDATE catalog.products
SET name = @name, name_search = @name_search, description = @description, article = @article, status = @status,
    length_cm = sqlc.narg(length_cm)::int, width_cm = sqlc.narg(width_cm)::int, height_cm = sqlc.narg(height_cm)::int,
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
