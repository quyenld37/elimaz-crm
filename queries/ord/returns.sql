-- Phiếu trả, đổi, phiếu hoàn của hãng (kế hoạch M2, Task M2-22; 05 mục 2). Chạy trong tenancy.WithTenant, trong tx của
-- bên gọi. Thứ tự khóa ADR-46 nhóm 7: orders (LockOrder, orders.sql) → order_items → returns → return_items. Phiếu không
-- có cột người bán: mọi câu đọc phiếu theo mã đi qua đơn của người bán (ADR-29). Mã phiếu từ NextReturnCode (code.sql).

-- name: LockOrderItemsForReturn :many
-- Khóa dòng đơn của đơn đã khóa, theo id tăng dần (ADR-46: cùng bảng theo khóa chính).
SELECT id, variant_id, quantity, net_amount
FROM ord.order_items
WHERE seller_id = @seller_id AND order_id = @order_id
ORDER BY id
FOR UPDATE;

-- name: ReturnedLinesOfOrder :many
-- Dòng của mọi phiếu chưa hủy của đơn (đơn đã khóa: phiếu của đơn chỉ đổi khi giữ khóa đơn). missing_qty > 0: dòng đã
-- nhận có món thiếu (M3-24).
SELECT ri.order_item_id, ri.quantity, ri.condition, ri.refund_amount, ri.missing_qty, r.status
FROM ord.return_items AS ri
JOIN ord.returns AS r ON r.id = ri.return_id
WHERE r.order_id = @order_id AND r.status <> 'cancelled';

-- name: InsertReturn :exec
INSERT INTO ord.returns
    (id, tenant_id, code, order_id, shipment_id, kind, reason, refund_amount, replacement_order_id, created_by,
     created_at, updated_at)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @code, @order_id, sqlc.narg(shipment_id)::uuid,
        @kind, @reason, @refund_amount, sqlc.narg(replacement_order_id)::uuid, sqlc.narg(created_by)::uuid,
        @now::timestamptz, @now::timestamptz);

-- name: InsertCarrierReturn :execrows
-- Phiếu hoàn của hãng: một phiếu chưa hủy mỗi (đơn, vận đơn) — trùng thì không chèn (0 dòng).
INSERT INTO ord.returns
    (id, tenant_id, code, order_id, shipment_id, kind, created_by, created_at, updated_at)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @code, @order_id, @shipment_id::uuid,
        'carrier_return', sqlc.narg(created_by)::uuid, @now::timestamptz, @now::timestamptz)
ON CONFLICT (tenant_id, order_id, shipment_id) WHERE kind = 'carrier_return' AND status <> 'cancelled' DO NOTHING;

-- name: InsertReturnItem :exec
INSERT INTO ord.return_items (tenant_id, return_id, order_item_id, quantity, condition, refund_amount)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @return_id, @order_item_id, @quantity, 'sellable',
        @refund_amount);

-- name: ReturnOrderRef :one
-- Đơn của phiếu (không khóa): bên gọi khóa đơn trước rồi mới khóa phiếu.
SELECT r.order_id, r.replacement_order_id
FROM ord.returns AS r
JOIN ord.orders AS o ON o.id = r.order_id
WHERE r.id = @id AND o.seller_id = @seller_id;

-- name: LockReturn :one
-- Khóa phiếu của đơn đã khóa.
SELECT id, code, order_id, shipment_id, kind, status, replacement_order_id, version
FROM ord.returns
WHERE id = @id AND order_id = @order_id
FOR UPDATE;

-- name: LockReturnItems :many
SELECT id, order_item_id, quantity, refund_amount
FROM ord.return_items
WHERE return_id = @return_id
ORDER BY id
FOR UPDATE;

-- name: MarkReturnReceived :one
-- requested → restocked (kho nhận và nhập kho cùng lúc) khi version còn khớp; trả version mới.
UPDATE ord.returns
SET status = 'restocked', refund_amount = @refund_amount, received_by = sqlc.narg(received_by)::uuid,
    received_at = @now::timestamptz, updated_at = @now::timestamptz, version = version + 1
WHERE id = @id AND version = @version AND status = 'requested'
RETURNING version;

-- name: SetReturnStatus :one
-- Đổi trạng thái phiếu from → to khi version còn khớp; trả version mới.
UPDATE ord.returns
SET status = @to_status::text, updated_at = @now::timestamptz, version = version + 1
WHERE id = @id AND version = @version AND status = @from_status::text
RETURNING version;

-- name: BumpOrderVersion :one
-- Tăng version của đơn đã khóa (nhận phiếu khách trả: đơn không đổi trường nào, sự kiện cần seq mới); trả version mới.
UPDATE ord.orders
SET version = version + 1, updated_at = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;

-- name: GetReturnRow :one
SELECT r.id, r.code, r.order_id, r.shipment_id, r.kind, r.status, r.reason, r.refund_amount, r.replacement_order_id,
       r.received_at, r.created_at, r.version
FROM ord.returns AS r
JOIN ord.orders AS o ON o.id = r.order_id
WHERE r.id = @id AND o.seller_id = @seller_id;

-- name: CarrierReturnOfShipment :one
SELECT r.id
FROM ord.returns AS r
JOIN ord.orders AS o ON o.id = r.order_id
WHERE r.shipment_id = @shipment_id::uuid AND r.kind = 'carrier_return' AND r.status <> 'cancelled'
  AND o.seller_id = @seller_id;

-- name: ListReturnItemsOf :many
-- Dòng của các phiếu ids (bên gọi đã đọc phiếu qua đơn của người bán).
SELECT id, return_id, order_item_id, quantity, condition, refund_amount
FROM ord.return_items
WHERE return_id = ANY (@return_ids::uuid[])
ORDER BY return_id, id;

-- name: ListReturnRows :many
-- Phiếu của người bán, mới nhất trước (id uuidv7 giảm dần); lọc trạng thái, loại; trang sau id < after_id.
SELECT r.id, r.code, r.order_id, r.shipment_id, r.kind, r.status, r.reason, r.refund_amount, r.replacement_order_id,
       r.received_at, r.created_at, r.version
FROM ord.returns AS r
JOIN ord.orders AS o ON o.id = r.order_id
WHERE o.seller_id = @seller_id
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
  AND (sqlc.narg(kind)::text IS NULL OR r.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(after_id)::uuid IS NULL OR r.id < sqlc.narg(after_id)::uuid)
ORDER BY r.id DESC
LIMIT @lim;
