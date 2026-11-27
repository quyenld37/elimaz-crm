-- Nhả, bán đơn vị suất flash sale theo chuyển trạng thái của đơn (kế hoạch M5, Task 202 / M5-19; 05 mục 7.4). Chạy
-- trong tx tenant của bên gọi (order đã khóa đơn, ADR-46 nhóm 7). Thứ tự nhóm 11: stock_balances (chỉ đường đợt đã
-- đóng, qua ReleaseHeldByItems) → stock_reservations → flash_sale_units. Câu ở đây không đổi số dư.

-- name: LockHeldReservationsOfItem :many
-- Khóa các dòng giữ hàng held của dòng đơn theo id tăng dần (đợt còn mở: nhả không đổi số dư).
SELECT id
FROM inventory.stock_reservations
WHERE order_item_id = @order_item_id::uuid AND state = 'held'
ORDER BY id
FOR UPDATE;

-- name: ReleaseReservationsKeepBalance :execrows
-- Dòng giữ hàng đã khóa held → released với lý do (suất về phần giữ của đợt: held của số dư giữ nguyên).
UPDATE inventory.stock_reservations
SET state = 'released', release_reason = @reason::text, state_changed_at = now()
WHERE id = ANY (@ids::uuid[]) AND state = 'held';

-- name: ReopenFlashUnits :execrows
-- Đợt còn mở: đơn vị held/sold của dòng đơn → available, bỏ dòng đơn.
UPDATE inventory.flash_sale_units
SET status = 'available', order_item_id = NULL, updated_at = now()
WHERE order_item_id = @order_item_id::uuid AND status IN ('held', 'sold');

-- name: ReleaseHeldFlashUnits :execrows
-- Đợt đã đóng: đơn vị held của dòng đơn → released, bỏ dòng đơn (đơn vị sold giữ nguyên, 05 mục 7.4).
UPDATE inventory.flash_sale_units
SET status = 'released', order_item_id = NULL, updated_at = now()
WHERE order_item_id = @order_item_id::uuid AND status = 'held';

-- name: MarkFlashUnitsSold :execrows
-- Đơn nháp hoàn tất: đơn vị held của dòng đơn → sold.
UPDATE inventory.flash_sale_units
SET status = 'sold', updated_at = now()
WHERE order_item_id = @order_item_id::uuid AND status = 'held';
