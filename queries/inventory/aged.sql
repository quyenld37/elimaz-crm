-- Giữ hàng quá hạn (kế hoạch M3, Task M3-22; KH09, AT53): chỉ đọc. Chạy trong tenancy.WithTenant. Chỉ mục
-- stock_reservations_state_age (tenant_id, state, state_changed_at) của migration 0307: hai lần quét khoảng theo trạng
-- thái (BitmapOr), không quét cả bảng.

-- name: AgedReservations :many
-- Dòng held từ held_before trở về trước và dòng in_transit từ transit_before trở về trước (bên gọi đặt mốc rất xa
-- trong quá khứ để bỏ một trạng thái), ở các kho warehouse_ids (all_warehouses: mọi kho), sau con trỏ
-- (after_at, after_id), cũ nhất trước.
SELECT id, order_item_id, variant_id, warehouse_id, state, quantity, state_changed_at
FROM inventory.stock_reservations
WHERE ((state = 'held' AND state_changed_at <= @held_before::timestamptz)
       OR (state = 'in_transit' AND state_changed_at <= @transit_before::timestamptz))
  AND (@all_warehouses::bool OR warehouse_id = ANY (@warehouse_ids::uuid[]))
  AND (state_changed_at, id) > (@after_at::timestamptz, @after_id::uuid)
ORDER BY state_changed_at, id
LIMIT @lim::int;
