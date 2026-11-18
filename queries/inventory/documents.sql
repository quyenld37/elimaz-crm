-- Chứng từ kho (kế hoạch M1, Task M1-29). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant. Thứ tự khóa
-- (ADR-46 nhóm 11): suppliers → stock_documents → stock_document_lines → stock_balances.

-- name: NextDocumentNumber :one
SELECT nextval('inventory.stock_document_code_seq')::bigint;

-- name: InsertDocument :one
-- Tenant lấy từ kho (RLS: kho của tenant khác không thấy → 0 dòng → không có kho).
INSERT INTO inventory.stock_documents (tenant_id, warehouse_id, code, kind, supplier_id, note, created_by)
SELECT w.tenant_id, w.id, @code, @kind, sqlc.narg(supplier_id)::uuid, @note, sqlc.narg(created_by)::uuid
FROM inventory.warehouses AS w
WHERE w.id = @warehouse_id
RETURNING id;

-- name: LockDocument :one
SELECT id, kind, status, version
FROM inventory.stock_documents
WHERE id = @id
FOR UPDATE;

-- name: GetDocument :one
SELECT d.id, d.warehouse_id, d.code, d.kind, d.status, d.supplier_id, coalesce(s.name, '')::text AS supplier_name,
       d.note, d.created_by, d.posted_by, d.posted_at, d.created_at, d.version
FROM inventory.stock_documents AS d
LEFT JOIN inventory.suppliers AS s ON s.tenant_id = d.tenant_id AND s.id = d.supplier_id
WHERE d.id = @id;

-- name: ListDocumentLines :many
-- Theo variant_id tăng dần: đúng thứ tự khóa số dư khi ghi sổ (một kho mỗi phiếu).
SELECT variant_id, quantity, unit_cost
FROM inventory.stock_document_lines
WHERE document_id = @document_id
ORDER BY variant_id;

-- name: DeleteDocumentLines :exec
DELETE FROM inventory.stock_document_lines WHERE document_id = @document_id;

-- name: InsertDocumentLine :execrows
-- Người bán lấy từ biến thể (view công bố của catalog); biến thể không có → 0 dòng.
INSERT INTO inventory.stock_document_lines (tenant_id, document_id, variant_id, seller_id, quantity, unit_cost)
SELECT d.tenant_id, d.id, v.variant_id, v.seller_id, @quantity, sqlc.narg(unit_cost)::bigint
FROM inventory.stock_documents AS d
JOIN catalog.variants_v AS v ON v.variant_id = @variant_id::uuid
WHERE d.id = @document_id;

-- name: TouchDocument :one
-- Sửa nháp (thay dòng): version + 1 khi còn nháp và version khớp (đã khóa dòng bằng LockDocument).
UPDATE inventory.stock_documents
SET version = version + 1, updated_at = now()
WHERE id = @id AND status = 'draft' AND version = @version
RETURNING version;

-- name: PostDocument :one
-- Ghi sổ: chỉ phiếu nháp đúng version; hai yêu cầu song song → một dòng đổi, bên kia 0 dòng (đọc lại sau khóa).
UPDATE inventory.stock_documents
SET status = 'posted', posted_by = sqlc.narg(posted_by)::uuid, posted_at = now(), version = version + 1,
    updated_at = now()
WHERE id = @id AND status = 'draft' AND version = @version
RETURNING kind, warehouse_id;

-- name: CancelDocument :one
UPDATE inventory.stock_documents
SET status = 'cancelled', cancelled_by = sqlc.narg(cancelled_by)::uuid, cancelled_at = now(), version = version + 1,
    updated_at = now()
WHERE id = @id AND status = 'draft' AND version = @version
RETURNING version;

-- name: ListDocuments :many
-- Mới nhất trước (id uuidv7 giảm dần), trang sau id con trỏ. all_warehouses = false → chỉ các kho warehouse_ids.
SELECT d.id, d.warehouse_id, d.code, d.kind, d.status, d.supplier_id, coalesce(s.name, '')::text AS supplier_name,
       d.note, d.created_by, d.posted_by, d.posted_at, d.created_at, d.version
FROM inventory.stock_documents AS d
LEFT JOIN inventory.suppliers AS s ON s.tenant_id = d.tenant_id AND s.id = d.supplier_id
WHERE (@all_warehouses::bool OR d.warehouse_id = ANY (@warehouse_ids::uuid[]))
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status)::text)
  AND (sqlc.narg(kind)::text IS NULL OR d.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(cursor)::uuid IS NULL OR d.id < sqlc.narg(cursor)::uuid)
ORDER BY d.id DESC
LIMIT @lim::int;
