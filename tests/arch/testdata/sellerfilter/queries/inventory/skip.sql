-- Ngoài phạm vi quét (chỉ queries/catalog, queries/ord).
-- name: Skip :one
SELECT variant_id FROM catalog.variants_v WHERE variant_id = @id;
