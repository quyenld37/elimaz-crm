-- Kiểm kho (kế hoạch M1, Task M1-35). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant. Thứ tự khóa
-- (ADR-46 nhóm 11): stocktakes → stocktake_lines → stock_balances (theo variant_id tăng dần, một kho mỗi phiên).

-- name: NextStocktakeNumber :one
SELECT nextval('inventory.stocktake_code_seq')::bigint;

-- name: GetWarehouseStatus :one
SELECT status FROM inventory.warehouses WHERE id = @id;

-- name: InsertStocktake :one
-- Tenant lấy từ kho. Một phiên mở mỗi kho: vi phạm stocktakes_one_open → bên gọi trả ErrStocktakeOpen.
INSERT INTO inventory.stocktakes (tenant_id, warehouse_id, code, started_by)
SELECT w.tenant_id, w.id, @code, sqlc.narg(started_by)::uuid
FROM inventory.warehouses AS w
WHERE w.id = @warehouse_id
RETURNING id;

-- name: LockStocktake :one
SELECT id, warehouse_id, status, version
FROM inventory.stocktakes
WHERE id = @id
FOR UPDATE;

-- name: TouchStocktake :exec
-- Ghi dòng đếm: version phiên + 1 (đã khóa bằng LockStocktake, còn mở) để áp dụng đúng bản đã xem trước.
UPDATE inventory.stocktakes
SET version = version + 1, updated_at = now()
WHERE id = @id AND status = 'open';

-- name: GetStocktake :one
SELECT id, warehouse_id, code, status, started_by, applied_by, applied_at, created_at, version
FROM inventory.stocktakes
WHERE id = @id;

-- name: GetStocktakeLine :one
SELECT version FROM inventory.stocktake_lines WHERE stocktake_id = @stocktake_id AND variant_id = @variant_id;

-- name: InsertStocktakeLine :execrows
-- Người bán lấy từ biến thể (view công bố của catalog); biến thể không có → 0 dòng. Bên gọi đã khóa phiên và kiểm
-- dòng chưa có (GetStocktakeLine), nên stocktake_lines_variant_key chỉ là lớp sau.
INSERT INTO inventory.stocktake_lines
    (tenant_id, stocktake_id, variant_id, seller_id, expected_on_shelf, counted, counted_at, counted_by)
SELECT s.tenant_id, s.id, v.variant_id, v.seller_id, @expected_on_shelf, @counted, now(), sqlc.narg(counted_by)::uuid
FROM inventory.stocktakes AS s
JOIN catalog.variants_v AS v ON v.variant_id = @variant_id::uuid
WHERE s.id = @stocktake_id;

-- name: UpdateStocktakeLine :execrows
-- Ghi lại số đếm khi version dòng còn khớp; chụp lại expected_on_shelf.
UPDATE inventory.stocktake_lines
SET expected_on_shelf = @expected_on_shelf, counted = @counted, counted_at = now(),
    counted_by = sqlc.narg(counted_by)::uuid, version = version + 1
WHERE stocktake_id = @stocktake_id AND variant_id = @variant_id AND version = @version;

-- name: ListStocktakeLines :many
-- Theo variant_id tăng dần (thứ tự khóa số dư khi áp dụng); kèm số dư hiện tại để xem trước (0 khi chưa có dòng số dư).
-- variant_id NULL: mọi dòng; khác NULL: chỉ dòng của biến thể đó.
SELECT l.variant_id, l.expected_on_shelf, l.counted, (l.counted - l.expected_on_shelf)::int AS delta, l.counted_at, l.counted_by, l.version,
       coalesce(b.on_hand, 0)::int AS on_hand, coalesce(b.held, 0)::int AS held,
       coalesce(b.in_transit, 0)::int AS in_transit, coalesce(b.damaged, 0)::int AS damaged,
       coalesce(b.on_hand - b.damaged - b.held - b.in_transit, 0)::int AS available
FROM inventory.stocktake_lines AS l
JOIN inventory.stocktakes AS s ON s.tenant_id = l.tenant_id AND s.id = l.stocktake_id
LEFT JOIN inventory.stock_balances AS b
    ON b.tenant_id = l.tenant_id AND b.variant_id = l.variant_id AND b.warehouse_id = s.warehouse_id
WHERE l.stocktake_id = @stocktake_id AND (sqlc.narg(variant_id)::uuid IS NULL OR l.variant_id = sqlc.narg(variant_id)::uuid)
ORDER BY l.variant_id;

-- name: ApplyStocktake :one
-- Áp dụng: chỉ phiên mở đúng version; hai yêu cầu song song → một dòng đổi, bên kia 0 dòng (đọc lại sau khóa).
UPDATE inventory.stocktakes
SET status = 'applied', applied_by = sqlc.narg(applied_by)::uuid, applied_at = now(), version = version + 1,
    updated_at = now()
WHERE id = @id AND status = 'open' AND version = @version
RETURNING warehouse_id, code;

-- name: CancelStocktake :one
UPDATE inventory.stocktakes
SET status = 'cancelled', cancelled_by = sqlc.narg(cancelled_by)::uuid, cancelled_at = now(), version = version + 1,
    updated_at = now()
WHERE id = @id AND status = 'open' AND version = @version
RETURNING version;
