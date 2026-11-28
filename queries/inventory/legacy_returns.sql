-- Hàng hoàn của đơn cũ về kho (kế hoạch M7, Task 264 = M7-19). Chạy trong tenancy.WithTenant: RLS giới hạn trong
-- tenant. Thứ tự khóa (ADR-46 nhóm 11): stock_documents → stock_document_lines → legacy_returns → stock_balances.

-- name: FindLegacyReturnByNorm :one
-- Kiểm trước, không khóa (trùng thường gặp không tốn số phiếu); chống đua là InsertLegacyReturn.
SELECT id FROM inventory.legacy_returns WHERE reference_norm = @reference_norm;

-- name: InsertLegacyReturn :one
-- Tenant lấy từ kho (RLS). Trùng reference_norm (kể cả transaction khác vừa commit) → 0 dòng.
INSERT INTO inventory.legacy_returns
    (tenant_id, warehouse_id, reference, reference_norm, stock_document_id, damage_document_id, created_by)
SELECT w.tenant_id, w.id, @reference, @reference_norm, @stock_document_id, sqlc.narg(damage_document_id)::uuid,
       @created_by
FROM inventory.warehouses AS w
WHERE w.id = @warehouse_id
ON CONFLICT (tenant_id, reference_norm) DO NOTHING
RETURNING id, created_at;

-- name: GetLegacyReturn :one
SELECT id, warehouse_id, reference, stock_document_id, damage_document_id, created_by, created_at
FROM inventory.legacy_returns
WHERE id = @id;

-- name: ListLegacyReturns :many
-- Mới nhất trước (id uuidv7 giảm dần), trang sau id con trỏ; khoảng giờ [from, to). all_warehouses = false → chỉ
-- các kho warehouse_ids.
SELECT id, warehouse_id, reference, stock_document_id, damage_document_id, created_by, created_at
FROM inventory.legacy_returns
WHERE (@all_warehouses::bool OR warehouse_id = ANY (@warehouse_ids::uuid[]))
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR created_at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR created_at < sqlc.narg(to_at)::timestamptz)
  AND (sqlc.narg(cursor)::uuid IS NULL OR id < sqlc.narg(cursor)::uuid)
ORDER BY id DESC
LIMIT @lim::int;

-- name: LegacyReturnDocumentLines :many
-- Dòng của các phiếu nhập, phiếu hỏng của hàng hoàn (dựng lại dòng tốt/hỏng ở Go).
SELECT document_id, variant_id, quantity
FROM inventory.stock_document_lines
WHERE document_id = ANY (@document_ids::uuid[])
ORDER BY document_id, variant_id;
