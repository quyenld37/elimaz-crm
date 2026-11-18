-- Đơn tối thiểu (kế hoạch M1, Task M1-30; 05 mục 2). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 7.
-- Gắn vận đơn, khóa đơn: Task M1-30b (333). Chèn đơn ở create.sql, đổi trạng thái ở transition.sql (Task M2-16).

-- name: LockOrder :one
-- Khóa đơn trước mọi dòng nhóm sau (ADR-46 nhóm 7, dòng "khóa đơn trước"). Bên gọi (fulfillment) truyền người bán
-- của tài khoản hãng (ADR-29, Task M1-37).
SELECT id, tenant_id, seller_id, customer_id, status, pending_reason, current_shipment_id, address_complete, version
FROM ord.orders
WHERE seller_id = @seller_id AND id = @id
FOR UPDATE;

-- name: SetCurrentShipment :execrows
-- Gắn vận đơn hiện hành; chỉ khi đơn đang confirmed và chưa có vận đơn (đã khóa bằng LockOrder).
UPDATE ord.orders
SET current_shipment_id = @shipment_id, version = version + 1, updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND status = 'confirmed' AND current_shipment_id IS NULL;

-- name: ClearCurrentShipment :execrows
-- Gỡ vận đơn hiện hành; chỉ khi đúng vận đơn đó (đã khóa bằng LockOrder). Lệnh tạo vận đơn kết thúc hỏng (D-03).
UPDATE ord.orders
SET current_shipment_id = NULL, version = version + 1, updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND current_shipment_id = @shipment_id;

