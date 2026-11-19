-- Giữ hàng theo dòng đơn: đổi số lượng giữ khi sửa đơn (kế hoạch M2, Task M2-20; inventory.Writer.ResizeHeld). Chạy
-- trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 11: số dư (LockBalancesOfReservations) trước giữ hàng
-- (LockReservations), rồi mới đổi.

-- name: HeldReservationByItem :one
-- Dòng giữ hàng held của dòng đơn (unique (tenant_id, order_item_id, variant_id, warehouse_id): mỗi dòng đơn một dòng
-- giữ). Không khóa: bên gọi khóa số dư rồi giữ hàng theo thứ tự ADR-46 và kiểm lại trạng thái dưới khóa.
SELECT id
FROM inventory.stock_reservations
WHERE order_item_id = @order_item_id::uuid AND state = 'held'
ORDER BY id
LIMIT 1;

-- name: ResizeReservation :execrows
-- Số lượng mới của dòng giữ hàng đã khóa, chỉ khi còn held (CHECK quantity > 0).
UPDATE inventory.stock_reservations
SET quantity = @quantity
WHERE id = @id AND state = 'held';
