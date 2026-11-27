-- Lượt mua flash sale, phần của order (kế hoạch M5, Task 201 / M5-18; ADR-40). Chạy trong tx tenant của bên gọi. Đơn
-- nháp của lượt mua chèn bằng InsertOrder (create.sql); dòng đơn flash chèn ở đây (có flash_sale_item_id). Dòng mới,
-- không khóa dòng có sẵn (ADR-46: sau flash_sale_claims nhóm 10 và flash_sale_units nhóm 11).

-- name: FlashOrderByExternalRef :one
-- Đơn web của mã ngoài external_ref (unique orders_external_ref_key) kèm khách, hạn nháp và món flash của dòng đầu
-- (uuid 0: không phải đơn flash).
SELECT o.id, o.code, o.customer_id, o.draft_expires_at,
       coalesce((SELECT i.flash_sale_item_id FROM ord.order_items AS i
                 WHERE i.seller_id = o.seller_id AND i.order_id = o.id ORDER BY i.position LIMIT 1),
                '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS flash_sale_item_id
FROM ord.orders AS o
WHERE o.seller_id = @seller_id AND o.channel = 'web' AND o.external_ref = @external_ref;

-- name: InsertFlashOrderItem :exec
-- Dòng đơn của lượt mua flash: đơn giá là giá flash, không giảm dòng, không chia giảm cấp đơn.
INSERT INTO ord.order_items
    (id, tenant_id, seller_id, order_id, position, variant_id, sku_snapshot, name_snapshot, quantity, unit_price,
     line_discount, allocated_order_discount, net_amount, flash_sale_item_id)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @order_id, 1, @variant_id,
        @sku_snapshot, @name_snapshot, @quantity, @unit_price, 0, 0, @net_amount, @flash_sale_item_id::uuid);

-- name: FlashLinesOfOrder :many
-- Dòng flash sale của đơn (đơn đã khóa, đọc không khóa thêm): mã dòng, món flash, số lượng (M5 Task 202).
SELECT id, flash_sale_item_id::uuid AS flash_sale_item_id, quantity
FROM ord.order_items
WHERE seller_id = @seller_id AND order_id = @order_id AND flash_sale_item_id IS NOT NULL
ORDER BY id;

-- name: FlashItemOfOrder :one
-- Món flash sale của dòng flash đầu tiên của đơn (không lọc người bán: consumer webcache chỉ có mã đơn; RLS lọc tenant).
SELECT flash_sale_item_id::uuid AS flash_sale_item_id
FROM ord.order_items
WHERE order_id = @order_id AND flash_sale_item_id IS NOT NULL
ORDER BY position
LIMIT 1;
