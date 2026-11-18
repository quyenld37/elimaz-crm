-- Đơn tối thiểu (kế hoạch M1, Task M1-30; 05 mục 2). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 7.
-- Gắn vận đơn, khóa đơn, đổi trạng thái: Task M1-30b (333).

-- name: InsertOrder :one
INSERT INTO ord.orders
    (id, tenant_id, seller_id, code, customer_id, channel, idempotency_key, status, ship_address, subtotal,
     discount_total, shipping_fee_charged, cod_amount, grand_total, created_by, version)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @code, @customer_id, @channel, @idempotency_key, @status, @ship_address,
        @subtotal, @discount_total, @shipping_fee_charged, @cod_amount, @grand_total, sqlc.narg(created_by)::uuid,
        @version)
RETURNING created_at;

-- name: GetOrder :one
-- Đơn của người bán seller_id (ADR-29: điều kiện seller_id ở tầng dưới cùng).
SELECT id, tenant_id, seller_id, code, customer_id, channel, status, current_shipment_id, ship_address, subtotal,
       discount_total, shipping_fee_charged, cod_amount, grand_total, version, created_at
FROM ord.orders
WHERE seller_id = @seller_id AND id = @id;
