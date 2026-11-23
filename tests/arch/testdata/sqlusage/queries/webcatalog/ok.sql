-- name: Ok :many
SELECT v.variant_id, s.available, o.id
FROM catalog.variants_v AS v
JOIN inventory.stock_available_v AS s ON s.variant_id = v.variant_id
JOIN ord.orders_v AS o ON o.id = v.variant_id;
