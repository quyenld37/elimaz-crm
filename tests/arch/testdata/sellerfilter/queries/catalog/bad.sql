-- Mẫu sai luật người bán (sellerfilter_test).

-- name: BySKU :one
SELECT id FROM catalog.variants WHERE sku = $1;

-- name: JoinMissing :many
-- seller_id = trong chú thích không tính.
SELECT v.variant_id
FROM catalog.variants_v AS v
JOIN catalog.products_v AS p ON p.product_id = v.product_id
WHERE v.seller_id = @seller_id AND 'p.seller_id = x' <> '';

-- name: InsertNoSeller :exec
INSERT INTO catalog.products (tenant_id, code) VALUES (@tenant_id, @code);
