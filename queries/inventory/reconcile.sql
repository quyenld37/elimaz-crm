-- Đối chiếu tồn (kế hoạch M1, Task M1-36; spec 05 mục 2). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant.

-- name: ReconcileBatch :many
-- Một lô số dư sau (after_variant, after_warehouse) theo khóa chính, kèm tổng sổ theo ngăn và tổng giữ hàng theo
-- trạng thái. Một câu cho cả lô: số dư, sổ và giữ hàng của mỗi dòng cùng một snapshot (mọi đường đổi tồn ghi cả ba
-- trong một transaction) nên không báo lệch giả khi có đơn đang chạy. Chỉ đọc, không khóa. flash_available (M5 Task
-- 202, 05 mục 7.4): số đơn vị suất còn trống của các món thuộc đợt scheduled/open của biến thể ở kho đó — phần held
-- đã giữ cho đợt mà chưa có dòng giữ hàng; held đúng = giữ hàng held + flash_available.
WITH b AS (
    SELECT variant_id, warehouse_id, on_hand, held, in_transit, damaged
    FROM inventory.stock_balances
    WHERE (variant_id, warehouse_id) > (@after_variant::uuid, @after_warehouse::uuid)
    ORDER BY variant_id, warehouse_id
    LIMIT @batch_size::int
),
f AS (
    SELECT i.variant_id, i.warehouse_id, count(*) AS n
    FROM inventory.flash_sale_units AS u
    JOIN promotion.flash_sale_items_v AS i ON i.id = u.flash_sale_item_id
    JOIN promotion.flash_sales_v AS s ON s.id = i.flash_sale_id
    WHERE u.status = 'available' AND s.status IN ('scheduled', 'open')
      AND (i.variant_id, i.warehouse_id) IN (SELECT b.variant_id, b.warehouse_id FROM b)
    GROUP BY i.variant_id, i.warehouse_id
)
SELECT b.variant_id, b.warehouse_id, b.on_hand, b.held, b.in_transit, b.damaged,
       l.on_hand::bigint AS ledger_on_hand, l.held::bigint AS ledger_held,
       l.in_transit::bigint AS ledger_in_transit, l.damaged::bigint AS ledger_damaged,
       r.held::bigint AS reserved_held, r.in_transit::bigint AS reserved_in_transit,
       coalesce(f.n, 0)::bigint AS flash_available
FROM b
LEFT JOIN f ON f.variant_id = b.variant_id AND f.warehouse_id = b.warehouse_id
CROSS JOIN LATERAL (
    SELECT coalesce(sum(delta) FILTER (WHERE bucket = 'on_hand'), 0) AS on_hand,
           coalesce(sum(delta) FILTER (WHERE bucket = 'held'), 0) AS held,
           coalesce(sum(delta) FILTER (WHERE bucket = 'in_transit'), 0) AS in_transit,
           coalesce(sum(delta) FILTER (WHERE bucket = 'damaged'), 0) AS damaged
    FROM inventory.stock_ledger AS sl
    WHERE sl.variant_id = b.variant_id AND sl.warehouse_id = b.warehouse_id
) AS l
CROSS JOIN LATERAL (
    SELECT coalesce(sum(quantity) FILTER (WHERE state = 'held'), 0) AS held,
           coalesce(sum(quantity) FILTER (WHERE state = 'in_transit'), 0) AS in_transit
    FROM inventory.stock_reservations AS sr
    WHERE sr.variant_id = b.variant_id AND sr.warehouse_id = b.warehouse_id
) AS r
ORDER BY b.variant_id, b.warehouse_id;

-- name: InsertReconcileRun :one
-- Ghi kết quả một lần chạy cho tenant của ngữ cảnh (app.tenant_id).
INSERT INTO inventory.reconcile_runs (tenant_id, ran_at, ok, checked, mismatches)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @ran_at, @ok, @checked, @mismatches)
RETURNING id;

-- name: FlashAvailableInClosedSales :many
-- Đối chiếu đơn vị suất (M5 Task 202): món của đợt đã đóng còn đơn vị available (đóng đợt phải trả hết) và số đó.
SELECT u.flash_sale_item_id, count(*)::bigint AS units
FROM inventory.flash_sale_units AS u
JOIN promotion.flash_sale_items_v AS i ON i.id = u.flash_sale_item_id
JOIN promotion.flash_sales_v AS s ON s.id = i.flash_sale_id
WHERE u.status = 'available' AND s.status = 'closed'
GROUP BY u.flash_sale_item_id
ORDER BY u.flash_sale_item_id;

-- name: FlashUnitsVsOrderLines :many
-- Đối chiếu đơn vị suất (M5 Task 202): dòng đơn có đơn vị held/sold mà số đơn vị khác số lượng của dòng (mã món, số
-- lượng dòng, số đơn vị). Index flash_sale_units_order_item.
SELECT u.flash_sale_item_id, oi.quantity::bigint AS quantity, count(*)::bigint AS units
FROM inventory.flash_sale_units AS u
JOIN ord.order_items_v AS oi ON oi.id = u.order_item_id
WHERE u.status IN ('held', 'sold')
GROUP BY u.flash_sale_item_id, u.order_item_id, oi.quantity
HAVING count(*) <> oi.quantity
ORDER BY u.flash_sale_item_id, u.order_item_id;
