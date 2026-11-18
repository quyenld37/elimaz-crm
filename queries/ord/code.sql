-- Mã đơn (kế hoạch M1, Task M1-30; Q26): một chỗ sinh duy nhất, không khóa dòng.

-- name: NextOrderCode :one
SELECT ('DH' || lpad(nextval('ord.order_code_seq')::text, 7, '0'))::text AS code;

-- name: NextReturnCode :one
-- Mã phiếu trả (kế hoạch M2, Task M2-05): một chỗ sinh duy nhất (order.nextReturnCode), không khóa dòng.
SELECT ('TH' || lpad(nextval('ord.return_code_seq')::text, 7, '0'))::text AS code;

-- name: OrderCodes :many
-- Mã đơn theo id của một người bán (kế hoạch M2, Task M2-14: lượt dùng mã giảm giá hiện mã đơn; ADR-29).
SELECT id, code FROM ord.orders WHERE seller_id = @seller_id AND id = ANY(sqlc.arg(ids)::uuid[]);
