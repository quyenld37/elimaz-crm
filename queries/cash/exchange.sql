-- Đổi hàng: tiền món trả cấn trừ vào đơn thay thế (kế hoạch M3, Task M3-25; TD06, AT14). Chạy trong tx của consumer
-- cash_refunds (tenant đã đặt), sau khi đã khóa đơn thay thế (order.LockSnapshot, ADR-46 nhóm 7).

-- name: ExchangeReturnOfReplacement :one
-- Phiếu đổi đã tạo đơn thay thế order_id (view công bố của ord; mỗi đơn thay thế đúng một phiếu).
SELECT id, order_id, status, refund_amount, replacement_order_id
FROM ord.returns_v
WHERE replacement_order_id = @order_id::uuid
  AND kind = 'exchange';

-- name: ExchangeReturn :one
-- Phiếu đổi id (view công bố của ord).
SELECT id, order_id, status, refund_amount, replacement_order_id
FROM ord.returns_v
WHERE id = @id
  AND kind = 'exchange';

-- name: ExchangeCredit :one
-- Tổng phiếu cấn trừ mã tham chiếu reference của đơn order_id; chưa có → 0.
SELECT coalesce(sum(amount), 0)::bigint AS total
FROM cash.payments
WHERE order_id = @order_id
  AND method = 'exchange_credit'
  AND reference = @reference;

-- name: ExchangeObligationExists :one
-- Đã có nghĩa vụ hoàn exchange của phiếu đổi return_id trên đơn gốc order_id (kho đã nhận phiếu, phần tiền đã chốt lúc
-- nhận; khóa unique refund_obligations_source_key).
SELECT EXISTS (SELECT 1
               FROM cash.refund_obligations
               WHERE order_id = @order_id
                 AND source = 'exchange'
                 AND return_id = @return_id::uuid) AS found;
