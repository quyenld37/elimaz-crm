-- Đếm cộng dồn nhiều người và nhập số đếm bằng Excel (kế hoạch M7, Task 263 = M7-18). Chạy trong tenancy.WithTenant:
-- RLS giới hạn trong tenant. Thứ tự khóa (ADR-46 nhóm 11): stocktakes (FOR SHARE) → stocktake_lines (theo variant_id
-- tăng dần) → count_imports.

-- name: ShareStocktake :one
-- Khóa chia sẻ phiên: nhiều lần cộng chạy song song, PostOpening/ApplyStocktake (FOR UPDATE) chờ chúng xong và ngược
-- lại, nên không dòng nào đổi sau khi phiên đã đăng.
SELECT id, warehouse_id, status, kind
FROM inventory.stocktakes
WHERE id = @id
FOR SHARE;

-- name: VariantsByCodes :many
-- Biến thể (mọi người bán của tenant, mẫu chưa xóa) có barcode đúng nguyên chuỗi hoặc SKU đúng sau khi viết hoa (SKU lưu
-- viết hoa) với một mã trong codes. Một mã khớp nhiều biến thể → bên gọi báo trùng.
SELECT c.code::text AS code, v.variant_id, v.seller_id, v.sku
FROM unnest(@codes::text[]) AS c(code)
JOIN catalog.variants_v AS v ON v.barcode = c.code
JOIN catalog.products_v AS p ON p.seller_id = v.seller_id AND p.product_id = v.product_id AND p.deleted_at IS NULL
UNION
SELECT c.code::text AS code, v.variant_id, v.seller_id, v.sku
FROM unnest(@codes::text[]) AS c(code)
JOIN catalog.variants_v AS v ON v.sku = upper(c.code)
JOIN catalog.products_v AS p ON p.seller_id = v.seller_id AND p.product_id = v.product_id AND p.deleted_at IS NULL
ORDER BY code, variant_id;

-- name: AddToStocktakeLine :one
-- Cộng by (> 0) vào dòng đếm của biến thể (chưa có dòng thì chèn với counted = by), chụp lại expected_on_shelf. Vượt
-- 1 000 000 → không dòng nào (bên gọi trả lỗi). Trigger stocktake_lines_open_only của M1 là lớp sau.
INSERT INTO inventory.stocktake_lines
    (tenant_id, stocktake_id, variant_id, seller_id, expected_on_shelf, counted, counted_at, counted_by)
SELECT s.tenant_id, s.id, @variant_id::uuid, @seller_id::uuid, @expected_on_shelf, @by::int, @now, sqlc.narg(counted_by)::uuid
FROM inventory.stocktakes AS s
WHERE s.id = @stocktake_id
ON CONFLICT ON CONSTRAINT stocktake_lines_variant_key DO UPDATE
SET counted = stocktake_lines.counted + EXCLUDED.counted, expected_on_shelf = EXCLUDED.expected_on_shelf,
    counted_at = EXCLUDED.counted_at, counted_by = EXCLUDED.counted_by, version = stocktake_lines.version + 1
WHERE stocktake_lines.counted + EXCLUDED.counted <= 1000000
RETURNING counted;

-- name: SubtractFromStocktakeLine :one
-- Trừ |by| (by < 0) khỏi dòng đếm đã có; dòng chưa có hoặc số đếm sẽ âm → không dòng nào.
UPDATE inventory.stocktake_lines
SET counted = counted + @by::int, expected_on_shelf = @expected_on_shelf, counted_at = @now,
    counted_by = sqlc.narg(counted_by)::uuid, version = version + 1
WHERE stocktake_id = @stocktake_id AND variant_id = @variant_id AND counted + @by::int >= 0
RETURNING counted;

-- name: UpsertStocktakeLines :many
-- Áp một bản nhập: mỗi biến thể một dòng (mảng cùng độ dài), chèn hoặc ghi theo variant_id tăng dần. add_mode: cộng
-- totals vào số đếm đã có; không thì đặt bằng totals. Cộng vượt 1 000 000 → dòng đó không đổi, không trả về.
INSERT INTO inventory.stocktake_lines
    (tenant_id, stocktake_id, variant_id, seller_id, expected_on_shelf, counted, counted_at, counted_by)
SELECT s.tenant_id, s.id, x.variant_id, x.seller_id, x.expected, x.total, @now, sqlc.narg(counted_by)::uuid
FROM inventory.stocktakes AS s
CROSS JOIN (SELECT unnest(@variant_ids::uuid[]) AS variant_id, unnest(@seller_ids::uuid[]) AS seller_id,
                   unnest(@expected::int[]) AS expected, unnest(@totals::int[]) AS total) AS x
WHERE s.id = @stocktake_id
ORDER BY x.variant_id
ON CONFLICT ON CONSTRAINT stocktake_lines_variant_key DO UPDATE
SET counted = CASE WHEN @add_mode::bool THEN stocktake_lines.counted + EXCLUDED.counted ELSE EXCLUDED.counted END,
    expected_on_shelf = EXCLUDED.expected_on_shelf, counted_at = EXCLUDED.counted_at,
    counted_by = EXCLUDED.counted_by, version = stocktake_lines.version + 1
WHERE NOT @add_mode::bool OR stocktake_lines.counted + EXCLUDED.counted <= 1000000
RETURNING variant_id;

-- name: InsertCountImport :one
-- Cùng phiên, cùng băm tệp, cùng mode đã có → không dòng nào (bên gọi đọc bản đã có).
INSERT INTO inventory.count_imports
    (tenant_id, stocktake_id, file_id, file_sha256, mode, pending_rows, error_rows, created_by)
SELECT s.tenant_id, s.id, @file_id, @file_sha256, @mode, @pending_rows, @error_rows, @created_by
FROM inventory.stocktakes AS s
WHERE s.id = @stocktake_id
ON CONFLICT ON CONSTRAINT count_imports_file_key DO NOTHING
RETURNING id, tenant_id;

-- name: CountImportByFile :one
SELECT id FROM inventory.count_imports
WHERE stocktake_id = @stocktake_id AND file_sha256 = @file_sha256 AND mode = @mode;

-- name: GetCountImport :one
SELECT id, stocktake_id, file_id, mode, status, pending_rows, applied_rows, error_rows, created_by, created_at,
       finished_at
FROM inventory.count_imports
WHERE id = @id;

-- name: FinishCountImport :execrows
-- Kết thúc bản nhập còn chờ: done (kèm số dòng đã áp, lỗi từng dòng) hoặc failed; xóa các dòng chờ. Đã kết thúc → 0 dòng.
UPDATE inventory.count_imports
SET status = @status, applied_rows = @applied_rows, error_rows = @error_rows, pending_rows = '[]',
    finished_at = @now
WHERE id = @id AND status IN ('queued', 'running');
