-- Quản lý mã giảm giá và đợt mã (kế hoạch M2, Task M2-14). Mọi câu chạy trong tx tenant của bên gọi (RLS lọc theo
-- tenant). Câu chèn mã dùng ON CONFLICT DO NOTHING: mã trùng là 0 dòng, không làm hỏng transaction.

-- name: InsertCoupon :one
INSERT INTO promotion.coupons (tenant_id, code, name, discount_kind, value, max_discount, min_subtotal, channels,
                               bound_customer_id, bound_phone, max_uses, per_customer_limit, release_on_cancel,
                               valid_from, valid_to, created_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, sqlc.arg(code)::text::citext, @name, @discount_kind,
        @value, sqlc.narg(max_discount), @min_subtotal, sqlc.arg(channels)::text[], sqlc.narg(bound_customer_id),
        sqlc.narg(bound_phone), sqlc.narg(max_uses), sqlc.narg(per_customer_limit), @release_on_cancel, @valid_from,
        @valid_to, @created_by)
ON CONFLICT (tenant_id, code) DO NOTHING
RETURNING id, code::text AS code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id,
          bound_customer_id, bound_phone, max_uses, used_count, per_customer_limit, release_on_cancel, valid_from,
          valid_to, status, created_by, created_at, updated_at, version;

-- name: InsertCouponsBulk :many
-- Một lô mã của đợt (tối đa 1.000 dòng mỗi câu); phones[i] rỗng = mã không gắn SĐT. Trả các mã đã chèn (mã trùng bị
-- bỏ qua, bên gọi sinh lại).
INSERT INTO promotion.coupons (tenant_id, code, batch_id, name, discount_kind, value, max_discount, min_subtotal,
                               channels, bound_phone, max_uses, per_customer_limit, valid_from, valid_to, created_by)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, (sqlc.arg(codes)::text[])[i]::citext, @batch_id::uuid, @name::text,
       @discount_kind::text, @value::bigint, sqlc.narg(max_discount)::bigint, @min_subtotal::bigint,
       sqlc.arg(channels)::text[], NULLIF((sqlc.arg(phones)::text[])[i], ''), 1, sqlc.narg(per_customer_limit)::int, @valid_from::timestamptz,
       @valid_to::timestamptz, @created_by::uuid
FROM generate_series(1, cardinality(sqlc.arg(codes)::text[])) AS i
ON CONFLICT (tenant_id, code) DO NOTHING
RETURNING code::text AS code;

-- name: InsertBatch :one
INSERT INTO promotion.coupon_batches (tenant_id, name, code_prefix, quantity, template, created_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @name, @code_prefix, @quantity, @template,
        @created_by)
RETURNING id, name, code_prefix, quantity, template, created_by, created_at;

-- name: GetBatch :one
SELECT id, name, code_prefix, quantity, template, created_by, created_at
FROM promotion.coupon_batches
WHERE id = @id;

-- name: CouponByID :one
SELECT id, code::text AS code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id,
       bound_customer_id, bound_phone, max_uses, used_count, per_customer_limit, release_on_cancel, valid_from,
       valid_to, status, created_by, created_at, updated_at, version
FROM promotion.coupons
WHERE id = @id;

-- name: HasRedemptions :one
-- Mã đã có lượt dùng nào (active hay released): khóa điều kiện của mã (bên gọi đã khóa dòng mã).
SELECT EXISTS (SELECT 1 FROM promotion.coupon_redemptions WHERE coupon_id = @coupon_id) AS used;

-- name: UpdateCoupon :one
-- Sửa điều khoản khi version khớp (bên gọi đã khóa dòng mã và so version); 0 dòng → version đã đổi.
UPDATE promotion.coupons
SET name = @name, discount_kind = @discount_kind, value = @value, max_discount = sqlc.narg(max_discount),
    min_subtotal = @min_subtotal, channels = sqlc.arg(channels)::text[], max_uses = sqlc.narg(max_uses),
    per_customer_limit = sqlc.narg(per_customer_limit), release_on_cancel = @release_on_cancel,
    valid_from = @valid_from, valid_to = @valid_to, status = @status, updated_at = now(), version = version + 1
WHERE id = @id AND version = @version
RETURNING id, code::text AS code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id,
          bound_customer_id, bound_phone, max_uses, used_count, per_customer_limit, release_on_cancel, valid_from,
          valid_to, status, created_by, created_at, updated_at, version;

-- name: ListCoupons :many
-- Mới nhất trước (id uuidv7 giảm dần), sau con trỏ after; q tìm trong mã hoặc tên, không phân biệt hoa thường, không
-- phải mẫu LIKE.
SELECT id, code::text AS code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id,
       bound_customer_id, bound_phone, max_uses, used_count, per_customer_limit, release_on_cancel, valid_from,
       valid_to, status, created_by, created_at, updated_at, version
FROM promotion.coupons
WHERE (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after)::uuid)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(batch_id)::uuid IS NULL OR batch_id = sqlc.narg(batch_id)::uuid)
  AND (sqlc.narg(q)::text IS NULL
       OR strpos(lower(code::text), lower(sqlc.narg(q)::text)) > 0
       OR strpos(lower(name), lower(sqlc.narg(q)::text)) > 0)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: ListRedemptions :many
-- Lượt dùng của một mã, mới nhất trước. Không có SĐT, khách; mã đơn do lớp HTTP ghép (promotion không gọi order).
SELECT id, order_id, discount, status, created_at, released_at
FROM promotion.coupon_redemptions
WHERE coupon_id = @coupon_id
  AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: CustomerRoot :one
-- Hồ sơ gốc của khách (sau gộp), đọc qua view customer.customers_v.
SELECT root_id FROM customer.customers_v WHERE id = @id;
