-- Phiếu thu, cọc, phiếu chi hoàn của đơn (kế hoạch M3, Task M3-30; Q-17). Chạy trong tenancy.WithTenant, trong tx của
-- bên gọi. Đường ghi khóa đơn trước (order.LockSnapshot, ADR-46 nhóm 7) rồi mới chạm cash.payments (nhóm 9).

-- name: OrderForPayment :one
-- Người bán, trạng thái, loại đơn, tổng đã thanh toán của đơn qua view công bố của ord (không khóa; RLS: tenant khác
-- như không có).
SELECT seller_id, status, order_type, prepaid_total
FROM ord.orders_v
WHERE id = @id;

-- name: InsertPayment :one
-- Chèn một phiếu (thu: amount > 0; chi hoàn: method refund, amount < 0, refund_obligation_id có). Chỉ ghi thêm.
INSERT INTO cash.payments (tenant_id, order_id, method, amount, reference, received_at, refund_obligation_id, actor_id)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id, @method, @amount, @reference,
        @received_at::timestamptz, sqlc.narg(refund_obligation_id)::uuid, @actor_id)
RETURNING *;

-- name: PaymentsTotal :one
-- Tổng mọi phiếu của đơn (thu dương, chi hoàn âm); đơn chưa có phiếu → 0.
SELECT coalesce(sum(amount), 0)::bigint AS total
FROM cash.payments
WHERE order_id = @order_id;

-- name: PaidBeforeCancel :one
-- Tổng tiền khách đã trả cho đơn tính vào nghĩa vụ hoàn khi hủy: mọi phiếu trừ bù đổi hàng (exchange_credit là tiền
-- của đơn gốc chuyển sang, không phải tiền khách trả cho đơn này); chưa có phiếu → 0.
SELECT coalesce(sum(amount), 0)::bigint AS total
FROM cash.payments
WHERE order_id = @order_id
  AND method <> 'exchange_credit';

-- name: ListOrderPayments :many
-- Phiếu của đơn mới nhất trước (mã uuidv7 giảm dần); trang sau bắt đầu sau mã before.
SELECT *
FROM cash.payments
WHERE order_id = @order_id
  AND (sqlc.narg(before)::uuid IS NULL OR id < sqlc.narg(before)::uuid)
ORDER BY id DESC
LIMIT @lim;
