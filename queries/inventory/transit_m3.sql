-- Giao một phần và đảo phiếu xuất (kế hoạch M3, Task M3-05; inventory.Writer.ConsumePartial, ReverseConsumption).
-- Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 11: số dư (LockBalancesOfReservations) trước giữ hàng
-- (LockReservations), rồi mới đổi.

-- name: InTransitReservationsByItems :many
-- Dòng giữ hàng in_transit của các dòng đơn (không khóa: bên gọi khóa theo thứ tự ADR-46 rồi kiểm lại trạng thái).
SELECT id, order_item_id
FROM inventory.stock_reservations
WHERE order_item_id = ANY (@order_item_ids::uuid[]) AND state = 'in_transit'
ORDER BY id;

-- name: ConsumeReservationPart :execrows
-- Giao một phần dòng giữ hàng đã khóa: bớt qty, phần còn lại vẫn in_transit (CHECK quantity > 0 giữ bởi quantity > qty).
UPDATE inventory.stock_reservations
SET quantity = quantity - @qty::int
WHERE id = @id AND state = 'in_transit' AND quantity > @qty::int;

-- name: ReservationsWithStep :many
-- Dòng giữ hàng (trong ids) đã có dòng sổ doc_type order ở một bước trong steps: giao một phần, giao đủ không ghi lần
-- hai (khóa unique của sổ).
SELECT DISTINCT doc_id, step
FROM inventory.stock_ledger
WHERE doc_type = 'order' AND doc_id = ANY (@ids::uuid[]) AND step = ANY (@steps::text[]);
