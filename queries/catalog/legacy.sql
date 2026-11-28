-- Mã cũ (Nhanh) của thực thể CRM và tra biến thể theo SKU (kế hoạch M7, Task 541 = M7-41; ADR-48). Chạy trong
-- tenancy.WithTenant. Bảng legacy_refs là bảng lá: chỉ chèn, không khóa bảng khác.

-- name: InsertLegacyRef :execrows
-- Chạy lại không trùng: cùng (nguồn, loại, mã cũ) đã có thì không ghi (0 dòng).
INSERT INTO catalog.legacy_refs (tenant_id, source, kind, legacy_id, entity_id)
VALUES (@tenant_id, @source, @kind, @legacy_id, @entity_id)
ON CONFLICT (tenant_id, source, kind, legacy_id) DO NOTHING;

-- name: ListLegacyRefs :many
SELECT legacy_id, entity_id
FROM catalog.legacy_refs
WHERE source = @source AND kind = @kind AND legacy_id = ANY(@legacy_ids::text[]);

-- name: ListVariantsBySKU :many
-- Biến thể của người bán theo SKU (viết hoa) kèm mã và phiên bản mẫu cha (chưa xóa).
SELECT v.id, v.sku, v.product_id, v.color, p.version AS product_version
FROM catalog.variants AS v
JOIN catalog.products AS p ON p.seller_id = v.seller_id AND p.id = v.product_id
WHERE v.seller_id = @seller_id AND upper(v.sku) = ANY(@skus::text[]) AND p.deleted_at IS NULL;
