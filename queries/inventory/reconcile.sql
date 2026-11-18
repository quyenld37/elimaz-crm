-- Đối chiếu tồn (kế hoạch M1, Task M1-36; spec 05 mục 2). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant.

-- name: ReconcileBatch :many
-- Một lô số dư sau (after_variant, after_warehouse) theo khóa chính, kèm tổng sổ theo ngăn và tổng giữ hàng theo
-- trạng thái. Một câu cho cả lô: số dư, sổ và giữ hàng của mỗi dòng cùng một snapshot (mọi đường đổi tồn ghi cả ba
-- trong một transaction) nên không báo lệch giả khi có đơn đang chạy. Chỉ đọc, không khóa.
WITH b AS (
    SELECT variant_id, warehouse_id, on_hand, held, in_transit, damaged
    FROM inventory.stock_balances
    WHERE (variant_id, warehouse_id) > (@after_variant::uuid, @after_warehouse::uuid)
    ORDER BY variant_id, warehouse_id
    LIMIT @batch_size::int
)
SELECT b.variant_id, b.warehouse_id, b.on_hand, b.held, b.in_transit, b.damaged,
       l.on_hand::bigint AS ledger_on_hand, l.held::bigint AS ledger_held,
       l.in_transit::bigint AS ledger_in_transit, l.damaged::bigint AS ledger_damaged,
       r.held::bigint AS reserved_held, r.in_transit::bigint AS reserved_in_transit
FROM b
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
