-- Danh bạ nhà cung cấp (kế hoạch M1, Task M1-29; Q33). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant.

-- name: InsertSupplier :one
INSERT INTO inventory.suppliers (tenant_id, name, name_search, phone, note)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @name, @name_search, @phone, @note)
RETURNING id, name, phone, note, status, version;

-- name: LockSupplier :one
-- Khóa dòng nhà cung cấp trước khi so version (ADR-46 nhóm 11: suppliers trước stock_documents).
SELECT id, name, phone, note, status, version
FROM inventory.suppliers
WHERE id = @id
FOR UPDATE;

-- name: ShareSupplier :one
-- Khóa chia sẻ khi tạo phiếu nhập: không ai lưu trữ nhà cung cấp tới hết transaction.
SELECT status
FROM inventory.suppliers
WHERE id = @id
FOR SHARE;

-- name: UpdateSupplier :one
UPDATE inventory.suppliers
SET name = @name, name_search = @name_search, phone = @phone, note = @note, status = @status,
    version = version + 1, updated_at = now()
WHERE id = @id AND version = @version
RETURNING id, name, phone, note, status, version;

-- name: ListSuppliers :many
-- q là chuỗi đã bỏ dấu, viết thường, đã thoát % và _ (rỗng: mọi dòng). Tối đa 200 dòng theo tên.
SELECT id, name, phone, note, status, version
FROM inventory.suppliers
WHERE (@q::text = '' OR name_search LIKE '%' || @q::text || '%' ESCAPE '\')
  AND (@include_archived::bool OR status = 'active')
ORDER BY name, id
LIMIT 200;
