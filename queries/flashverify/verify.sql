-- Kiểm một đợt flash sale sau thử tải (kế hoạch M5, Task 219 / M5-36; lệnh `crm verify-flash`). Chỉ đọc view _v đã
-- công bố (docs/views.md); chạy trong tenancy.WithTenant, transaction REPEATABLE READ (mọi câu cùng một ảnh chụp).
-- "Đơn còn hiệu lực": đơn chưa hủy (status <> 'cancelled'). Đường hủy (order/flashrelease.go) trả đơn vị của đơn hủy:
-- đợt còn mở → available; đợt đã đóng → held thành released, sold giữ nguyên dòng đơn (05 mục 7.4).

-- name: SaleStatus :one
SELECT id, status FROM promotion.flash_sales_v WHERE id = @sale_id;

-- name: ItemCounts :many
-- Mỗi món của đợt: số suất, giới hạn mỗi khách, kho, biến thể; số đơn vị theo trạng thái; tổng đã lấy của khách;
-- tổng số lượng dòng flash của đơn còn hiệu lực; số đơn vị held/sold của đơn còn hiệu lực, của đơn đã hủy.
WITH i AS (
    SELECT id, variant_id, warehouse_id, quantity, per_customer_limit
    FROM promotion.flash_sale_items_v
    WHERE flash_sale_id = @sale_id
),
u AS (
    SELECT u.flash_sale_item_id,
           count(*) FILTER (WHERE u.status = 'available') AS available,
           count(*) FILTER (WHERE u.status = 'held') AS held,
           count(*) FILTER (WHERE u.status = 'sold') AS sold,
           count(*) FILTER (WHERE u.status = 'released') AS released,
           count(*) FILTER (WHERE u.status IN ('held', 'sold') AND o.status <> 'cancelled') AS active_units,
           count(*) FILTER (WHERE u.status = 'held' AND o.status = 'cancelled') AS cancelled_held,
           count(*) FILTER (WHERE u.status = 'sold' AND o.status = 'cancelled') AS cancelled_sold
    FROM inventory.flash_sale_units_v AS u
    LEFT JOIN ord.order_flash_lines_v AS l ON l.id = u.order_item_id
    LEFT JOIN ord.orders_v AS o ON o.id = l.order_id
    WHERE u.flash_sale_item_id IN (SELECT i.id FROM i)
    GROUP BY u.flash_sale_item_id
),
c AS (
    SELECT flash_sale_item_id, sum(quantity) AS claimed, min(quantity) AS min_claim
    FROM promotion.flash_sale_claims_v
    WHERE flash_sale_item_id IN (SELECT i.id FROM i)
    GROUP BY flash_sale_item_id
),
l AS (
    SELECT l.flash_sale_item_id, sum(l.quantity) AS active_qty
    FROM ord.order_flash_lines_v AS l
    JOIN ord.orders_v AS o ON o.id = l.order_id
    WHERE l.flash_sale_item_id IN (SELECT i.id FROM i) AND o.status <> 'cancelled'
    GROUP BY l.flash_sale_item_id
)
SELECT i.id AS item_id, i.variant_id, i.warehouse_id, i.quantity::bigint AS quantity,
       i.per_customer_limit::bigint AS per_customer_limit,
       coalesce(u.available, 0)::bigint AS available, coalesce(u.held, 0)::bigint AS held,
       coalesce(u.sold, 0)::bigint AS sold, coalesce(u.released, 0)::bigint AS released,
       coalesce(u.active_units, 0)::bigint AS active_units, coalesce(u.cancelled_held, 0)::bigint AS cancelled_held,
       coalesce(u.cancelled_sold, 0)::bigint AS cancelled_sold,
       coalesce(c.claimed, 0)::bigint AS claimed, coalesce(c.min_claim, 0)::bigint AS min_claim,
       coalesce(l.active_qty, 0)::bigint AS active_qty
FROM i
LEFT JOIN u ON u.flash_sale_item_id = i.id
LEFT JOIN c ON c.flash_sale_item_id = i.id
LEFT JOIN l ON l.flash_sale_item_id = i.id
ORDER BY i.id;

-- name: LineUnitsOver :many
-- (b) Dòng đơn giữ (held/sold) nhiều đơn vị hơn số lượng của dòng; dòng không phải dòng flash của đúng món đó tính
-- số lượng 0. Index flash_sale_units_order_item.
SELECT u.flash_sale_item_id, u.order_item_id::uuid AS order_item_id,
       coalesce(max(l.quantity), 0)::bigint AS quantity, count(*)::bigint AS units
FROM inventory.flash_sale_units_v AS u
JOIN promotion.flash_sale_items_v AS i ON i.id = u.flash_sale_item_id
LEFT JOIN ord.order_flash_lines_v AS l ON l.id = u.order_item_id AND l.flash_sale_item_id = u.flash_sale_item_id
WHERE i.flash_sale_id = @sale_id AND u.status IN ('held', 'sold')
GROUP BY u.flash_sale_item_id, u.order_item_id
HAVING count(*) > coalesce(max(l.quantity), 0)
ORDER BY u.flash_sale_item_id, u.order_item_id;

-- name: CustomersOverLimit :many
-- (d) Khách vượt giới hạn mỗi khách của món: theo số đã lấy (claims) hay theo tổng dòng flash của đơn còn hiệu lực.
WITH i AS (
    SELECT id, per_customer_limit FROM promotion.flash_sale_items_v WHERE flash_sale_id = @sale_id
),
q AS (
    SELECT c.flash_sale_item_id, c.customer_id, c.quantity::bigint AS qty
    FROM promotion.flash_sale_claims_v AS c
    WHERE c.flash_sale_item_id IN (SELECT i.id FROM i)
    UNION ALL
    SELECT l.flash_sale_item_id, o.customer_id, sum(l.quantity)::bigint AS qty
    FROM ord.order_flash_lines_v AS l
    JOIN ord.orders_v AS o ON o.id = l.order_id
    WHERE l.flash_sale_item_id IN (SELECT i.id FROM i) AND o.status <> 'cancelled'
    GROUP BY l.flash_sale_item_id, o.customer_id
)
SELECT q.flash_sale_item_id, q.customer_id, max(q.qty)::bigint AS qty, i.per_customer_limit::bigint AS per_customer_limit
FROM q
JOIN i ON i.id = q.flash_sale_item_id
GROUP BY q.flash_sale_item_id, q.customer_id, i.per_customer_limit
HAVING max(q.qty) > i.per_customer_limit
ORDER BY q.flash_sale_item_id, q.customer_id;

-- name: ItemBalances :many
-- (e) Số dư của biến thể, kho của mỗi món (on_hand, held, available; món chưa có dòng số dư thì không có dòng).
SELECT i.id AS item_id, b.on_hand::bigint AS on_hand, b.held::bigint AS held, b.available::bigint AS available
FROM promotion.flash_sale_items_v AS i
JOIN inventory.stock_available_v AS b ON b.variant_id = i.variant_id AND b.warehouse_id = i.warehouse_id
WHERE i.flash_sale_id = @sale_id
ORDER BY i.id;

-- name: DuplicateExternalRefs :many
-- (h) Mã đơn website (external_ref) có từ hai đơn trở lên trong các đơn có dòng flash của đợt (mọi trạng thái).
SELECT o.external_ref::text AS external_ref, count(DISTINCT o.id)::bigint AS orders
FROM ord.order_flash_lines_v AS l
JOIN promotion.flash_sale_items_v AS i ON i.id = l.flash_sale_item_id
JOIN ord.orders_v AS o ON o.id = l.order_id
WHERE i.flash_sale_id = @sale_id AND o.external_ref IS NOT NULL
GROUP BY o.external_ref
HAVING count(DISTINCT o.id) > 1
ORDER BY o.external_ref;
