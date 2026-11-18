-- Giữ hàng theo dòng đơn (kế hoạch M1, Task M1-28; 05 mục 2 B2). Chạy trong tenancy.WithTenant.

-- name: InsertReservation :exec
INSERT INTO inventory.stock_reservations
    (id, tenant_id, order_item_id, variant_id, warehouse_id, quantity, expires_at)
VALUES (@id, @tenant_id, @order_item_id, @variant_id, @warehouse_id, @quantity, sqlc.narg(expires_at)::timestamptz);

-- name: LockReservations :many
-- Khóa theo id tăng dần (ADR-46), sau khi đã khóa số dư (LockBalancesOfReservations).
SELECT id, variant_id, warehouse_id, quantity, state
FROM inventory.stock_reservations
WHERE id = ANY (@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: SetReservationState :many
-- Đổi trạng thái chỉ khi đang ở trạng thái cũ; trả các dòng đã đổi (ít hơn số mã → sai trạng thái).
UPDATE inventory.stock_reservations
SET state = @to_state, release_reason = sqlc.narg(release_reason)::text, state_changed_at = now()
WHERE id = ANY (@ids::uuid[]) AND state = @from_state
RETURNING id;

-- name: ReservationIDsByItems :many
-- Dòng giữ hàng của các dòng đơn đang ở trạng thái state (không khóa: lõi khóa theo thứ tự ADR-46).
SELECT id
FROM inventory.stock_reservations
WHERE order_item_id = ANY (@order_item_ids::uuid[]) AND state = @state
ORDER BY id;

-- name: CountReservationsByItems :many
-- Số dòng giữ hàng của các dòng đơn theo trạng thái (không khóa; bên gọi đã khóa đơn, ADR-46 nhóm 7).
SELECT state, count(*)::int AS n
FROM inventory.stock_reservations
WHERE order_item_id = ANY (@order_item_ids::uuid[])
GROUP BY state;
