-- name: Table :many
SELECT id FROM catalog.variants;

-- name: Platform :many
SELECT key FROM platform.file_variants_v;

-- name: Unlisted :many
SELECT x FROM catalog.secret_v;
