-- Giữ hàng quá hạn (kế hoạch M3, Task M3-22; KH09, AT53): chỉ đọc. Chạy trong tenancy.WithTenant. Chỉ mục
-- stock_reservations_state_age (tenant_id, state, state_changed_at) của migration 0307: hai lần quét khoảng theo trạng
-- thái (BitmapOr), không quét cả bảng.

-- name: AgedReservations :many
-- Dòng held từ held_before trở về trước và dòng in_transit từ transit_before trở về trước (bên gọi đặt mốc rất xa
-- trong quá khứ để bỏ một trạng thái), ở các kho warehouse_ids (all_warehouses: mọi kho), sau con trỏ
-- (after_at, after_id), cũ nhất trước. Chỉ hàng của người bán @seller_id (ADR-29, Task 531): người bán của số dư mà dòng
-- giữ (khóa chính của stock_balances, câu con vô hướng: không đổi kế hoạch quét khoảng theo trạng thái).
SELECT r.id, r.order_item_id, r.variant_id, r.warehouse_id, r.state, r.quantity, r.state_changed_at
FROM inventory.stock_reservations r
WHERE (SELECT b.seller_id
       FROM inventory.stock_balances b
       WHERE b.tenant_id = r.tenant_id AND b.variant_id = r.variant_id AND b.warehouse_id = r.warehouse_id) = @seller_id
  AND ((r.state = 'held' AND r.state_changed_at <= @held_before::timestamptz)
       OR (r.state = 'in_transit' AND r.state_changed_at <= @transit_before::timestamptz))
  AND (@all_warehouses::bool OR r.warehouse_id = ANY (@warehouse_ids::uuid[]))
  AND (r.state_changed_at, r.id) > (@after_at::timestamptz, @after_id::uuid)
ORDER BY r.state_changed_at, r.id
LIMIT @lim::int;
