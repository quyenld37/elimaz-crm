-- Mẫu đúng luật người bán (sellerfilter_test): không được báo.

-- name: ByID :one
SELECT id FROM catalog.products WHERE seller_id = @seller_id AND id = @id;

-- name: Joined :many
SELECT v.variant_id
FROM catalog.variants_v v
JOIN catalog.products_v AS p ON p.seller_id = v.seller_id AND p.product_id = v.product_id
WHERE v.seller_id = ANY(@sellers::uuid[]);

-- name: Lock :one
SELECT id FROM catalog.variants WHERE variants.seller_id = @seller_id AND id = @id FOR UPDATE;

-- name: Insert :exec
INSERT INTO catalog.variants (tenant_id, seller_id, sku) VALUES (@tenant_id, @seller_id, @sku);

-- name: Bump :one
UPDATE catalog.products SET version = version + 1 WHERE seller_id = @seller_id AND id = @id RETURNING version;

-- name: Other :many
SELECT id FROM catalog.product_images WHERE product_id = @product_id;
