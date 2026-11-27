-- Đơn tối thiểu (kế hoạch M1, Task M1-30; 05 mục 2). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 7.
-- Khóa đơn: Task M1-30b (333); gắn, gỡ vận đơn và tổng thanh toán ở shiphooks.sql (Task M2-18). Chèn đơn ở
-- create.sql, đổi trạng thái ở transition.sql (Task M2-16).

-- name: LockOrder :one
-- Khóa đơn trước mọi dòng nhóm sau (ADR-46 nhóm 7, dòng "khóa đơn trước"). Bên gọi (fulfillment) truyền người bán
-- của tài khoản hãng (ADR-29, Task M1-37).
SELECT id, tenant_id, seller_id, customer_id, status, pending_reason, current_shipment_id, address_complete, version,
       grand_total, prepaid_total, cod_amount, payment_status, packing_status, order_type, warehouse_id, channel
FROM ord.orders
WHERE seller_id = @seller_id AND id = @id
FOR UPDATE;
