-- Đơn tối thiểu (kế hoạch M1, Task M1-30; 05 mục 2). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 7.
-- Gắn vận đơn, khóa đơn, đổi trạng thái: Task M1-30b (333), cuối tệp.

-- name: InsertOrder :one
-- warehouse_id: kho giữ hàng của đơn (M2-05); ship_phone đã chuẩn hóa ('' khi không chuẩn hóa được); đơn tạo thẳng
-- confirmed ghi confirmed_at, confirmed_by lúc tạo.
INSERT INTO ord.orders
    (id, tenant_id, seller_id, code, customer_id, channel, idempotency_key, status, ship_address, subtotal,
     discount_total, shipping_fee_charged, cod_amount, grand_total, created_by, version, warehouse_id, ship_phone,
     ship_phone_reversed, confirmed_at, confirmed_by)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @code, @customer_id, @channel, @idempotency_key, @status, @ship_address,
        @subtotal, @discount_total, @shipping_fee_charged, @cod_amount, @grand_total, sqlc.narg(created_by)::uuid,
        @version, @warehouse_id, @ship_phone, @ship_phone_reversed,
        CASE WHEN @status::text = 'confirmed' THEN now() END,
        CASE WHEN @status::text = 'confirmed' THEN sqlc.narg(created_by)::uuid END)
RETURNING created_at;

-- name: LockOrder :one
-- Khóa đơn trước mọi dòng nhóm sau (ADR-46 nhóm 7, dòng "khóa đơn trước"). Bên gọi (fulfillment) truyền người bán
-- của tài khoản hãng (ADR-29, Task M1-37).
SELECT id, tenant_id, seller_id, status, current_shipment_id, version
FROM ord.orders
WHERE seller_id = @seller_id AND id = @id
FOR UPDATE;

-- name: SetCurrentShipment :execrows
-- Gắn vận đơn hiện hành; chỉ khi đơn đang confirmed và chưa có vận đơn (đã khóa bằng LockOrder).
UPDATE ord.orders
SET current_shipment_id = @shipment_id, version = version + 1, updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND status = 'confirmed' AND current_shipment_id IS NULL;

-- name: SetOrderStatus :one
-- Đổi trạng thái chỉ khi đang ở trạng thái cũ; trả version mới (không dòng nào → pgx.ErrNoRows).
UPDATE ord.orders
SET status = @to_status, version = version + 1, updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND status = @from_status
RETURNING version;
