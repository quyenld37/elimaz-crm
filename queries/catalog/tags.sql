-- Nhãn kệ hàng (kế hoạch M1, Task M1-34). Chạy trong tenancy.WithTenant; dùng chung cả cửa hàng.

-- name: InsertTag :one
INSERT INTO catalog.tags (tenant_id, code, name)
VALUES (@tenant_id, @code, @name)
RETURNING id, code, name;

-- name: ListTags :many
SELECT id, code, name FROM catalog.tags ORDER BY code, id;

-- name: DeleteProductTags :exec
DELETE FROM catalog.product_tags WHERE product_id = @product_id;

-- name: InsertProductTags :exec
INSERT INTO catalog.product_tags (tenant_id, product_id, tag_id)
SELECT @tenant_id, @product_id, unnest(@tag_ids::uuid[]);

-- name: ListProductTagIDs :many
SELECT tag_id FROM catalog.product_tags WHERE product_id = @product_id ORDER BY tag_id;
