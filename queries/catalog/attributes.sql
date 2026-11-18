-- Thuộc tính và giá trị (kế hoạch M1, Task M1-34). Chạy trong tenancy.WithTenant; dùng chung cả cửa hàng.

-- name: InsertAttribute :one
INSERT INTO catalog.attributes (tenant_id, code, name, position)
VALUES (@tenant_id, @code, @name, @position)
RETURNING id, code, name, position;

-- name: InsertAttributeValue :one
INSERT INTO catalog.attribute_values (tenant_id, attribute_id, value, position)
VALUES (@tenant_id, @attribute_id, @value, @position)
RETURNING id, attribute_id, value, position;

-- name: ListAttributes :many
SELECT id, code, name, position FROM catalog.attributes ORDER BY position, code, id;

-- name: ListAttributeValues :many
SELECT id, attribute_id, value, position FROM catalog.attribute_values ORDER BY attribute_id, position, value, id;

-- name: DeleteProductAttributeValues :exec
DELETE FROM catalog.product_attribute_values WHERE product_id = @product_id;

-- name: InsertProductAttributeValues :exec
INSERT INTO catalog.product_attribute_values (tenant_id, product_id, attribute_value_id)
SELECT @tenant_id, @product_id, unnest(@attribute_value_ids::uuid[]);

-- name: ListProductAttributeValueIDs :many
SELECT attribute_value_id FROM catalog.product_attribute_values WHERE product_id = @product_id
ORDER BY attribute_value_id;
