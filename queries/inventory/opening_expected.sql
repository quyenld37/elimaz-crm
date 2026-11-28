-- Tồn dự kiến của phiên kiểm kho mở sổ (kế hoạch M7, Task 541 = M7-41; ADR-48). Chạy trong tenancy.WithTenant.
-- opening_expected là bảng lá (không khóa bảng khác); chỉ là số để so, không đổi tồn.

-- name: UpsertOpeningExpected :exec
INSERT INTO inventory.opening_expected (tenant_id, warehouse_id, variant_id, seller_id, expected, source, taken_at)
VALUES (@tenant_id, @warehouse_id, @variant_id, @seller_id, @expected, @source, @taken_at)
ON CONFLICT (tenant_id, warehouse_id, variant_id)
DO UPDATE SET expected = EXCLUDED.expected, source = EXCLUDED.source, taken_at = EXCLUDED.taken_at, updated_at = now();

-- name: ListOpeningDiff :many
-- Mỗi biến thể có số dự kiến của kho: số dự kiến, số đã đếm của phiên (NULL: chưa đếm), SKU. Lệch tính ở Go.
SELECT e.variant_id, v.sku, e.expected, l.counted
FROM inventory.opening_expected AS e
JOIN catalog.variants AS v ON v.seller_id = e.seller_id AND v.id = e.variant_id
LEFT JOIN inventory.stocktake_lines AS l
       ON l.tenant_id = e.tenant_id AND l.stocktake_id = @stocktake_id AND l.variant_id = e.variant_id
WHERE e.warehouse_id = @warehouse_id
ORDER BY v.sku, e.variant_id;
