-- Lượt dùng mã (kế hoạch M2, Task M2-06). Câu ghi có điều kiện: 0 dòng nghĩa là hết lượt hay đã đổi rồi.
-- Thứ tự khóa (ADR-46 nhóm 10): coupons → coupon_customer_uses → coupon_redemptions; bên gọi đã khóa dòng mã.

-- name: IncrementUsed :execrows
UPDATE promotion.coupons
SET used_count = used_count + 1
WHERE id = @id AND (max_uses IS NULL OR used_count < max_uses);

-- name: DecrementUsed :execrows
UPDATE promotion.coupons
SET used_count = used_count - 1
WHERE id = @id AND used_count > 0;

-- name: IncrementCustomerUse :execrows
-- Một lượt nữa của khách với mã; per_customer_limit rỗng là không giới hạn (vẫn đếm).
INSERT INTO promotion.coupon_customer_uses (tenant_id, coupon_id, customer_id, used)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @coupon_id, @customer_id, 1)
ON CONFLICT (tenant_id, coupon_id, customer_id) DO UPDATE
SET used = promotion.coupon_customer_uses.used + 1
WHERE sqlc.narg(per_customer_limit)::int IS NULL
   OR promotion.coupon_customer_uses.used < sqlc.narg(per_customer_limit)::int;

-- name: DecrementCustomerUse :execrows
UPDATE promotion.coupon_customer_uses
SET used = used - 1
WHERE coupon_id = @coupon_id AND customer_id = @customer_id AND used > 0;

-- name: CustomerUsed :one
-- Số lượt đang hiệu lực của khách với mã (Check); chưa có dòng → 0.
SELECT coalesce(max(used), 0)::int AS used
FROM promotion.coupon_customer_uses
WHERE coupon_id = @coupon_id AND customer_id = @customer_id;

-- name: ActiveRedemptionByOrder :one
SELECT id, coupon_id, customer_id, discount
FROM promotion.coupon_redemptions
WHERE order_id = @order_id AND status = 'active';

-- name: OrderCouponCode :one
-- Mã (đúng chữ đã lưu) mà đơn đang dùng (dấu vân tay lần gửi lại tạo đơn): lượt đang hiệu lực; không có thì lượt đã
-- trả gần nhất trừ lượt bị bỏ khi sửa đơn (detach_reason): đơn đã hủy vẫn được nhận ra là cùng đơn (Task M2-21).
SELECT c.code::text AS code
FROM promotion.coupon_redemptions r
JOIN promotion.coupons c ON c.id = r.coupon_id
WHERE r.order_id = @order_id AND (r.status = 'active' OR r.release_reason <> @detach_reason::text)
ORDER BY r.status = 'active' DESC, r.released_at DESC, r.id DESC
LIMIT 1;

-- name: UpsertRedemption :execrows
-- Lượt mới của đơn, hoặc đổi dòng released cũ của cùng (mã, đơn) về active; dòng còn active → 0 dòng.
INSERT INTO promotion.coupon_redemptions (tenant_id, coupon_id, order_id, customer_id, discount, status)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @coupon_id, @order_id, @customer_id, @discount,
        'active')
ON CONFLICT (tenant_id, coupon_id, order_id) DO UPDATE
SET status = 'active', customer_id = EXCLUDED.customer_id, discount = EXCLUDED.discount, released_at = NULL,
    release_reason = NULL
WHERE promotion.coupon_redemptions.status = 'released';

-- name: ReleaseRedemption :execrows
UPDATE promotion.coupon_redemptions
SET status = 'released', released_at = now(), release_reason = sqlc.arg(release_reason)::text
WHERE id = @id AND status = 'active';

-- name: SetRedemptionDiscount :execrows
UPDATE promotion.coupon_redemptions
SET discount = @discount
WHERE id = @id AND status = 'active';
