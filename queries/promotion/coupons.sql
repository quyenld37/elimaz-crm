-- Mã giảm giá (kế hoạch M2, Task M2-06). Mọi câu chạy trong tx tenant của bên gọi (RLS lọc theo tenant).

-- name: CouponByCode :one
-- Kiểm mã (Check): không khóa.
SELECT id, code::text AS code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id,
       bound_customer_id, bound_phone, max_uses, used_count, per_customer_limit, release_on_cancel, valid_from,
       valid_to, status, created_by, created_at, updated_at, version
FROM promotion.coupons
WHERE code = sqlc.arg(code)::text::citext;

-- name: CouponByCodeForUpdate :one
-- Dùng mã (Redeem): khóa dòng mã trước mọi bảng lượt (ADR-46 nhóm 10).
SELECT id, code::text AS code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id,
       bound_customer_id, bound_phone, max_uses, used_count, per_customer_limit, release_on_cancel, valid_from,
       valid_to, status, created_by, created_at, updated_at, version
FROM promotion.coupons
WHERE code = sqlc.arg(code)::text::citext
FOR UPDATE;

-- name: CouponByIDForUpdate :one
-- Trả lượt, tính lại (Release, Detach, Reprice): khóa dòng mã trước mọi bảng lượt (ADR-46 nhóm 10).
SELECT id, code::text AS code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id,
       bound_customer_id, bound_phone, max_uses, used_count, per_customer_limit, release_on_cancel, valid_from,
       valid_to, status, created_by, created_at, updated_at, version
FROM promotion.coupons
WHERE id = @id
FOR UPDATE;
