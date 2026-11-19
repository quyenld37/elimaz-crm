-- Nhập khách từ tệp Excel (kế hoạch M2, Task M2-13). Chạy trong tenancy.WithTenant (RLS lọc tenant). Thứ tự khóa
-- ADR-46 nhóm 5: lô chèn khách → danh tính → địa chỉ → liên kết nhãn trước, khóa customer_imports cuối cùng.

-- name: InsertImport :one
INSERT INTO customer.customer_imports (tenant_id, file_id, status, created_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @file_id, 'pending', @created_by)
RETURNING id, tenant_id, file_id, status, total_rows, ok_rows, skipped_rows, error_rows, errors, created_by,
    created_at, finished_at;

-- name: GetImport :one
SELECT id, tenant_id, file_id, status, total_rows, ok_rows, skipped_rows, error_rows, errors, created_by, created_at,
    finished_at
FROM customer.customer_imports
WHERE id = @id;

-- name: LockImport :one
SELECT id, tenant_id, file_id, status, total_rows, ok_rows, skipped_rows, error_rows, errors, created_by, created_at,
    finished_at
FROM customer.customer_imports
WHERE id = @id
FOR UPDATE;

-- name: StartImportRun :exec
-- pending → running với tổng số dòng của tệp; số đếm về 0 (lượt đầu).
UPDATE customer.customer_imports
SET status       = 'running',
    total_rows   = @total_rows,
    ok_rows      = 0,
    skipped_rows = 0,
    error_rows   = 0,
    errors       = '[]'
WHERE id = @id
  AND status = 'pending';

-- name: SaveImportProgress :exec
-- Cộng số đếm của một lô và nối lỗi của lô (đã cắt theo trần ở Go); chỉ khi còn running.
UPDATE customer.customer_imports
SET ok_rows      = ok_rows + @ok::int,
    skipped_rows = skipped_rows + @skipped::int,
    error_rows   = error_rows + @failed::int,
    errors       = errors || @new_errors::jsonb
WHERE id = @id
  AND status = 'running';

-- name: FinishImport :exec
-- running → done hoặc pending/running → failed (kèm lỗi cả tệp khi có).
UPDATE customer.customer_imports
SET status      = @status,
    errors      = errors || @new_errors::jsonb,
    finished_at = @now::timestamptz
WHERE id = @id
  AND status IN ('pending', 'running');

-- name: ExistingPhones :many
-- SĐT (đã chuẩn hóa) đã là danh tính phone của một hồ sơ trong tenant.
SELECT value
FROM customer.customer_identities
WHERE kind = 'phone'
  AND scope = ''
  AND value = ANY (@phones::text[]);

-- name: AllAdminUnits :many
-- Cả danh mục hai cấp (vài nghìn dòng) để đổi tên tỉnh, xã trong tệp sang mã.
SELECT code, level, parent_code, name_search
FROM customer.admin_units;

-- name: InsertTagIfAbsent :exec
INSERT INTO customer.customer_tags (tenant_id, name, color)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @name, 'gray')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- name: TagIDsByName :many
SELECT id, name
FROM customer.customer_tags
WHERE name = ANY (@names::text[]);
