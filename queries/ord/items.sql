-- Dòng đơn (kế hoạch M1, Task M1-30). Chạy trong tenancy.WithTenant.

-- name: InsertOrderItem :exec
INSERT INTO ord.order_items
    (id, tenant_id, seller_id, order_id, position, variant_id, sku_snapshot, name_snapshot, quantity, unit_price,
     line_discount, allocated_order_discount, net_amount)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @order_id, @position, @variant_id, @sku_snapshot, @name_snapshot, @quantity,
        @unit_price, @line_discount, @allocated_order_discount, @net_amount);

-- name: ListOrderItems :many
SELECT id, position, variant_id, sku_snapshot, name_snapshot, quantity, unit_price, line_discount,
       allocated_order_discount, net_amount
FROM ord.order_items
WHERE seller_id = @seller_id AND order_id = @order_id
ORDER BY position;

-- name: OrderItemIDs :many
-- Mã các dòng đơn của đơn (đơn đã khóa bằng LockOrder).
SELECT id
FROM ord.order_items
WHERE order_id = @order_id
ORDER BY position;
