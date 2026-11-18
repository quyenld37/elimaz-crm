-- Biến thể màu/size (kế hoạch M1, Task M1-27). Chạy trong tenancy.WithTenant. Mọi câu đọc có seller_id (ADR-29).

-- name: InsertVariant :one
INSERT INTO catalog.variants
    (tenant_id, seller_id, product_id, sku, barcode, color, color_hex, size, price, cost_price)
VALUES (@tenant_id, @seller_id, @product_id, @sku, @barcode, @color, sqlc.narg(color_hex), @size, @price,
        sqlc.narg(cost_price))
RETURNING id, status, version;

-- name: ListProductVariants :many
SELECT id, seller_id, product_id, sku, barcode, color, color_hex, size, price, cost_price, status, version
FROM catalog.variants
WHERE seller_id = @seller_id AND product_id = @product_id
ORDER BY id;

-- name: GetVariantProductID :one
-- Mã mẫu của biến thể (không khóa): sửa biến thể khóa mẫu trước rồi biến thể (ADR-46 nhóm 4).
SELECT product_id FROM catalog.variants WHERE id = @id;

-- name: LockVariant :one
SELECT id, seller_id, product_id, sku, barcode, color, color_hex, size, price, cost_price, status, version
FROM catalog.variants
WHERE id = @id
FOR UPDATE;

-- name: UpdateVariant :one
UPDATE catalog.variants
SET sku = @sku, barcode = @barcode, color = @color, color_hex = sqlc.narg(color_hex), size = @size, price = @price,
    cost_price = sqlc.narg(cost_price), status = @status, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING version;

-- name: BarcodeExists :one
SELECT EXISTS (SELECT 1 FROM catalog.variants WHERE seller_id = @seller_id AND barcode = @barcode);

-- name: GetSaleVariant :one
SELECT variant_id, seller_id, product_id, sku, product_name, color, size, price, sellable
FROM catalog.variants_v
WHERE seller_id = @seller_id AND variant_id = @variant_id;

-- name: ListSaleVariants :many
SELECT variant_id, seller_id, product_id, sku, product_name, color, size, price
FROM catalog.variants_v
WHERE seller_id = @seller_id AND variant_id = ANY(@variant_ids::uuid[]) AND sellable;
