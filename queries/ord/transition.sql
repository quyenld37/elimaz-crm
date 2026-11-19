-- Đổi trạng thái đơn và lịch sử trạng thái (kế hoạch M2, Task M2-16: chỗ duy nhất có câu đổi trạng thái đơn; dời từ
-- orders.sql, history.sql của M1 Task M1-30, M1-30b). Chạy trong tenancy.WithTenant. Lịch sử chỉ ghi thêm.

-- name: UpdateOrderStatus :one
-- Đổi trạng thái khi version còn khớp (đơn đã khóa hoặc so version, ADR-46 nhóm 7); trả version mới (không dòng nào →
-- pgx.ErrNoRows). Rời draft thì bỏ hạn nháp; tới confirmed lần đầu ghi confirmed_at, confirmed_by; tới cancelled ghi
-- cancelled_at.
UPDATE ord.orders
SET status           = @to_status::text,
    pending_reason   = @pending_reason::text,
    draft_expires_at = CASE WHEN @to_status::text = 'draft' THEN draft_expires_at END,
    confirmed_at     = CASE WHEN @to_status::text = 'confirmed' AND confirmed_at IS NULL THEN @now::timestamptz
                            ELSE confirmed_at END,
    confirmed_by     = CASE WHEN @to_status::text = 'confirmed' AND confirmed_at IS NULL THEN sqlc.narg(actor_id)::uuid
                            ELSE confirmed_by END,
    cancelled_at     = CASE WHEN @to_status::text = 'cancelled' THEN @now::timestamptz ELSE cancelled_at END,
    version          = version + 1,
    updated_at       = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;

-- name: InsertStatusHistory :exec
INSERT INTO ord.order_status_history
    (tenant_id, order_id, from_status, to_status, from_pending, to_pending, actor_type, actor_id, reason, occurred_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id, sqlc.narg(from_status)::text, @to_status,
        @from_pending, @to_pending, @actor_type, sqlc.narg(actor_id)::uuid, sqlc.narg(reason)::text, @occurred_at);

-- name: RecordDiscountApproval :execrows
-- Ghi người duyệt giảm giá của đơn đã khóa (Task M2-19), không đổi status, version: chạy ngay sau applyTransition bỏ
-- lý do discount_approval, cùng transaction (version là version mới vừa trả).
UPDATE ord.orders
SET discount_approved_by = sqlc.narg(actor_id)::uuid,
    discount_approved_at = @now::timestamptz
WHERE seller_id = @seller_id AND id = @id AND version = @version;
