-- Bản nhập sản phẩm bằng Excel (kế hoạch M1, Task M1-47). Chạy trong tenancy.WithTenant. Khóa theo ADR-46 nhóm 4:
-- catalog.imports trước mẫu, biến thể.

-- name: InsertImport :one
INSERT INTO catalog.imports (tenant_id, file_id, status, seller_id, created_by)
VALUES (@tenant_id, @file_id, 'validating', @seller_id, @created_by)
RETURNING id, file_id, seller_id, status, summary, errors, plan, duplicate_of, created_by, version, created_at, updated_at;

-- name: GetImport :one
SELECT id, file_id, seller_id, status, summary, errors, plan, duplicate_of, created_by, version, created_at, updated_at
FROM catalog.imports
WHERE seller_id = @seller_id AND id = @id;

-- name: GetImportForValidation :one
-- Lệnh kiểm (ngữ cảnh tenant của lệnh, không biết người bán trước): tệp, người bán, trạng thái.
SELECT file_id, seller_id, status FROM catalog.imports WHERE id = @id;

-- name: LockImport :one
SELECT id, status FROM catalog.imports WHERE id = @id FOR UPDATE;

-- name: FindAppliedImportBySHA :one
SELECT id FROM catalog.imports WHERE seller_id = @seller_id AND file_sha256 = @file_sha256 AND status = 'applied';

-- name: FinishValidation :execrows
-- Ghi kết quả kiểm khi bản nhập còn validating (chạy lại lệnh không ghi đè kết quả đã có).
UPDATE catalog.imports
SET status = @status, file_sha256 = @file_sha256, summary = @summary, errors = @errors, plan = sqlc.narg(plan),
    duplicate_of = sqlc.narg(duplicate_of), version = version + 1, updated_at = now()
WHERE id = @id AND status = 'validating';

-- name: FailValidation :execrows
UPDATE catalog.imports
SET status = 'failed', version = version + 1, updated_at = now()
WHERE id = @id AND status = 'validating';

-- name: ListVariantsBySKUs :many
-- Biến thể có sẵn của người bán theo SKU (chỉ SKU có trong tệp nhập), kèm mã mẫu.
SELECT v.id, v.product_id, p.code AS product_code, v.version, v.sku, v.barcode, v.color, v.color_hex, v.size, v.price,
       v.cost_price
FROM catalog.variants AS v
JOIN catalog.products AS p ON p.id = v.product_id AND p.seller_id = v.seller_id
WHERE v.seller_id = @seller_id AND v.sku = ANY(@skus::text[]);

-- name: ListVariantSKUsByBarcodes :many
-- SKU của các biến thể có sẵn của người bán mang một trong các barcode gõ tay có trong tệp nhập (bước kiểm bắt barcode
-- đã thuộc biến thể có SKU không nằm trong tệp; unique (tenant_id, seller_id, barcode)).
SELECT v.barcode, v.sku
FROM catalog.variants AS v
WHERE v.seller_id = @seller_id AND v.barcode = ANY(@barcodes::text[]);

-- name: ListVariantsByProductCodes :many
-- Màu, size, SKU của mọi biến thể thuộc các mẫu chưa xóa có mã trong codes (mã mẫu có trong tệp nhập); mẫu chưa có
-- biến thể vẫn có một dòng (cột biến thể rỗng).
SELECT p.id AS product_id, p.code, v.sku, v.color, v.size
FROM catalog.products AS p
LEFT JOIN catalog.variants AS v ON v.product_id = p.id AND v.seller_id = p.seller_id
WHERE p.seller_id = @seller_id AND p.code = ANY(@codes::text[]) AND p.deleted_at IS NULL;

-- Áp dụng bản nhập (Task M1-47b). Thứ tự khóa (ADR-46): khóa tư vấn theo người bán → catalog.imports → danh mục
-- (chia sẻ) → mẫu có sẵn theo id tăng dần → biến thể → platform.jobs.

-- name: LockImportForApply :one
SELECT status, version FROM catalog.imports WHERE seller_id = @seller_id AND id = @id FOR UPDATE;

-- name: StartApply :execrows
UPDATE catalog.imports
SET status = 'applying', version = version + 1, updated_at = now()
WHERE id = @id AND status = 'ready';

-- name: LockImportApplySeller :exec
-- Khóa tư vấn theo (tenant, người bán): hai lần áp của cùng người bán chạy lần lượt, lần sau thấy đúng dữ liệu lần
-- trước vừa ghi. Chỉ việc catalog.import.apply lấy, trước mọi khóa dòng.
SELECT pg_advisory_xact_lock(hashtextextended(
    'catalog.import.apply:' || current_setting('app.tenant_id') || ':' || sqlc.arg(seller_id)::uuid::text, 0));

-- name: GetImportPlan :one
SELECT file_sha256, summary, plan FROM catalog.imports WHERE seller_id = @seller_id AND id = @id;

-- name: FinishApply :execrows
UPDATE catalog.imports
SET status = 'applied', applied_by = @applied_by, applied_at = now(), version = version + 1, updated_at = now()
WHERE id = @id AND status = 'applying';

-- name: FailApply :execrows
-- Áp thất bại: lỗi dòng và tóm tắt mới (nil: giữ nguyên, khi việc hết lượt). Plan giữ lại để xem.
UPDATE catalog.imports
SET status = 'failed', errors = coalesce(sqlc.narg(errors), errors), summary = coalesce(sqlc.narg(summary), summary),
    version = version + 1, updated_at = now()
WHERE id = @id AND status = 'applying';

-- name: MarkApplyDuplicate :execrows
-- Tệp cùng nội dung vừa được áp cho người bán (bởi bản nhập khác) → duplicate, bỏ plan.
UPDATE catalog.imports
SET status = 'duplicate', duplicate_of = @duplicate_of, plan = NULL, version = version + 1, updated_at = now()
WHERE id = @id AND status = 'applying';

-- name: LockProductsByIDs :many
-- Khóa trước các mẫu có sẵn của plan theo id tăng dần (ADR-46: sửa nhiều dòng).
SELECT id FROM catalog.products WHERE seller_id = @seller_id AND id = ANY(@ids::uuid[]) ORDER BY id FOR UPDATE;
