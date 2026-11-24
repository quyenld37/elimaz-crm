-- name: Table :many
SELECT id FROM ord.orders;

-- name: OtherSchema :many
SELECT variant_id FROM catalog.variants_v;
