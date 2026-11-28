-- Nghĩa vụ hoàn tiền cho khách (kế hoạch M3, Task M3-30; Q-2, TI09). Chạy trong tenancy.WithTenant, trong tx của bên
-- gọi. Chi, miễn khóa đơn trước (ADR-46 nhóm 7) rồi nghĩa vụ (nhóm 9, FOR UPDATE).

-- name: InsertRefundObligation :execrows
-- Mở nghĩa vụ hoàn; đã có nghĩa vụ cùng (đơn, nguồn, phiếu trả) → 0 dòng (sự kiện giao lại không mở thêm).
INSERT INTO cash.refund_obligations (tenant_id, order_id, return_id, source, amount)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id, sqlc.narg(return_id)::uuid, @source,
        @amount)
ON CONFLICT ON CONSTRAINT refund_obligations_source_key DO NOTHING;

-- name: RefundObligationOrder :one
-- Đơn và nguồn của nghĩa vụ (không khóa: bên gọi khóa đơn trước rồi mới khóa nghĩa vụ).
SELECT order_id, source
FROM cash.refund_obligations
WHERE id = @id;

-- name: LockRefundObligation :one
-- Khóa nghĩa vụ (nhóm 9) sau khi đã khóa đơn của nó.
SELECT *
FROM cash.refund_obligations
WHERE id = @id
FOR UPDATE;

-- name: PayRefundObligation :one
-- Cộng amount vào số đã chi khi còn mở và không vượt số phải hoàn; chi đủ → paid. Không dòng → bên gọi trả lỗi.
UPDATE cash.refund_obligations
SET paid_amount = paid_amount + @pay::bigint,
    status      = CASE WHEN paid_amount + @pay::bigint = amount THEN 'paid' ELSE 'open' END,
    updated_at  = @now::timestamptz,
    version     = version + 1
WHERE id = @id
  AND status = 'open'
  AND paid_amount + @pay::bigint <= amount
RETURNING *;

-- name: WaiveRefundObligation :one
-- Đóng nghĩa vụ còn mở là "không hoàn" kèm lý do. Không dòng → nghĩa vụ không còn mở.
UPDATE cash.refund_obligations
SET status       = 'waived',
    waive_reason = @reason::text,
    updated_at   = @now::timestamptz,
    version      = version + 1
WHERE id = @id
  AND status = 'open'
RETURNING *;

-- name: GetRefundObligation :one
-- Nghĩa vụ kèm mã đơn (view công bố của ord).
SELECT r.*, coalesce(o.code, '')::text AS order_code
FROM cash.refund_obligations r
LEFT JOIN ord.orders_v o ON o.id = r.order_id
WHERE r.id = @id;

-- name: ListRefundObligations :many
-- Nghĩa vụ mới nhất trước (mã uuidv7 giảm dần), lọc theo trạng thái (NULL: mọi trạng thái); trang sau bắt đầu sau mã
-- before. Chỉ nghĩa vụ của đơn của người bán @seller_id (ADR-29; câu con theo khóa chính của đơn).
SELECT r.*, coalesce(o.code, '')::text AS order_code
FROM cash.refund_obligations r
LEFT JOIN ord.orders_v o ON o.id = r.order_id
WHERE (SELECT so.seller_id FROM ord.orders_v so WHERE so.id = r.order_id) = @seller_id
  AND (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
  AND (sqlc.narg(before)::uuid IS NULL OR r.id < sqlc.narg(before)::uuid)
ORDER BY r.id DESC
LIMIT @lim;
