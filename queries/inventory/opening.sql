-- Kiểm kho mở sổ (kế hoạch M7, Task 262 = M7-17). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant. Thứ tự
-- khóa (ADR-46 nhóm 11): warehouses (chỉ lúc mở phiên) → stocktakes → stocktake_lines → stock_balances.

-- name: LockWarehouseForOpening :one
-- Hai lần mở phiên mở sổ cùng kho chạy lần lượt (kiểm phiên đã có rồi mới mở). FOR NO KEY UPDATE không chặn khóa
-- ngoại (FOR KEY SHARE) của các bảng trỏ tới kho.
SELECT status FROM inventory.warehouses WHERE id = @id FOR NO KEY UPDATE;

-- name: OpeningExists :one
-- Kho đã có phiên mở sổ chưa hủy (đang mở hoặc đã đăng).
SELECT EXISTS (
    SELECT 1 FROM inventory.stocktakes
    WHERE warehouse_id = @warehouse_id AND kind = 'opening' AND status <> 'cancelled'
)::bool;

-- name: WarehouseNotEmpty :one
-- Kho không còn trống: có dòng sổ, giữ hàng chưa nhả hay chưa giao xong, hoặc số dư khác 0. stocktake_id khác NULL:
-- bỏ qua sổ của chính phiên đó và so on_hand với tổng sổ của phiên (kiểm lại sau khi áp dụng).
SELECT (
    EXISTS (
        SELECT 1 FROM inventory.stock_ledger AS l
        WHERE l.warehouse_id = @warehouse_id
          AND NOT (l.doc_type = 'stocktake' AND l.doc_id IS NOT DISTINCT FROM sqlc.narg(stocktake_id)::uuid)
    )
    OR EXISTS (
        SELECT 1 FROM inventory.stock_reservations AS r
        WHERE r.warehouse_id = @warehouse_id AND r.state IN ('held', 'in_transit')
    )
    OR EXISTS (
        SELECT 1 FROM inventory.stock_balances AS b
        WHERE b.warehouse_id = @warehouse_id
          AND (b.held <> 0 OR b.in_transit <> 0 OR b.damaged <> 0 OR b.on_hand <> coalesce((
              SELECT sum(l.delta) FROM inventory.stock_ledger AS l
              WHERE l.tenant_id = b.tenant_id AND l.doc_type = 'stocktake'
                AND l.doc_id = sqlc.narg(stocktake_id)::uuid AND l.variant_id = b.variant_id
                AND l.warehouse_id = b.warehouse_id AND l.bucket = 'on_hand'), 0))
    )
)::bool AS not_empty;

-- name: SetStocktakeOpening :execrows
-- Đặt phiên vừa mở (cùng transaction với StartStocktake) làm phiên mở sổ; vi phạm stocktakes_one_opening →
-- ErrOpeningExists.
UPDATE inventory.stocktakes SET kind = 'opening' WHERE id = @id AND status = 'open';

-- name: CountOpeningUncounted :one
-- Biến thể phải đếm (active, của mẫu draft hoặc active chưa xóa; cùng điều kiện với opening_status_v) mà phiên chưa có
-- dòng đếm.
SELECT count(*)::int
FROM catalog.variants_v AS v
JOIN catalog.products_v AS p ON p.seller_id = v.seller_id AND p.product_id = v.product_id
WHERE v.variant_status = 'active' AND v.product_status IN ('draft', 'active') AND p.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM inventory.stocktake_lines AS l
      WHERE l.stocktake_id = @stocktake_id AND l.variant_id = v.variant_id
  );

-- name: OpeningHasStockedLine :one
-- Có dòng đếm chụp expected_on_shelf ≠ 0 (kho có hàng lúc đếm).
SELECT EXISTS (
    SELECT 1 FROM inventory.stocktake_lines WHERE stocktake_id = @stocktake_id AND expected_on_shelf <> 0
)::bool;

-- name: GetOpeningStatus :one
SELECT warehouse_id, stocktake_id, status, lines_expected, lines_counted, units_counted, applied_at
FROM inventory.opening_status_v
WHERE stocktake_id = @stocktake_id;

-- name: ListOpeningStatus :many
-- warehouse_id NULL: mọi kho. Mới nhất trước (uuidv7).
SELECT warehouse_id, stocktake_id, status, lines_expected, lines_counted, units_counted, applied_at
FROM inventory.opening_status_v
WHERE sqlc.narg(warehouse_id)::uuid IS NULL OR warehouse_id = sqlc.narg(warehouse_id)::uuid
ORDER BY stocktake_id DESC
LIMIT 200;

-- name: OpeningLedgerOtherDocs :one
-- Kho đã có chứng từ khác ngoài phiên mở sổ (sau giờ mở bán).
SELECT EXISTS (
    SELECT 1 FROM inventory.stock_ledger
    WHERE warehouse_id = @warehouse_id AND NOT (doc_type = 'stocktake' AND doc_id = @stocktake_id)
)::bool;

-- name: CompareOpening :many
-- Mỗi biến thể có dòng đếm hoặc có sổ của phiên: số đếm, tổng sổ on_hand của phiên, on_hand hiện tại, SKU.
WITH lin AS (
    SELECT variant_id, counted FROM inventory.stocktake_lines WHERE stocktake_id = @stocktake_id
), led AS (
    SELECT variant_id, sum(delta)::int AS total
    FROM inventory.stock_ledger
    WHERE doc_type = 'stocktake' AND doc_id = @stocktake_id AND warehouse_id = @warehouse_id AND bucket = 'on_hand'
    GROUP BY variant_id
), ids AS (
    SELECT coalesce(lin.variant_id, led.variant_id) AS variant_id, coalesce(lin.counted, 0)::int AS counted,
           coalesce(led.total, 0)::int AS ledger_opening
    FROM lin FULL JOIN led ON led.variant_id = lin.variant_id
)
SELECT ids.variant_id::uuid AS variant_id, ids.counted::int AS counted, ids.ledger_opening::int AS ledger_opening,
       coalesce(b.on_hand, 0)::int AS on_hand, coalesce(v.sku, '')::text AS sku
FROM ids
LEFT JOIN inventory.stock_balances AS b ON b.variant_id = ids.variant_id AND b.warehouse_id = @warehouse_id
LEFT JOIN catalog.variants_v AS v ON v.variant_id = ids.variant_id
ORDER BY ids.variant_id;
