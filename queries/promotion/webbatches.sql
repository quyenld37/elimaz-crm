-- Đợt mã của website và mã nhập từ website (kế hoạch M5, Task 197 / M5-14). Mọi câu chạy trong tx tenant của bên gọi
-- (RLS lọc theo tenant). Thứ tự khóa ADR-46 nhóm 10: coupon_batches → coupons.

-- name: BatchByPurposeName :one
-- Lô của website theo (purpose, tên) — unique một phần coupon_batches_web_name_key. Không khóa.
SELECT id, name, code_prefix, quantity, template, created_by, created_at, purpose
FROM promotion.coupon_batches
WHERE purpose = @purpose AND name = @name AND purpose <> 'manual';

-- name: BatchCodes :many
-- Mã của một lô kèm SĐT gắn mã (rỗng: không gắn), theo mã.
SELECT code::text AS code, coalesce(bound_phone, '')::text AS bound_phone
FROM promotion.coupons
WHERE batch_id = @batch_id
ORDER BY code;

-- name: SetBatchPurpose :exec
-- Đặt purpose cho lô vừa ghi trong cùng tx (CreateBatch của M2 ghi manual); trùng (purpose, tên) với lô đã commit →
-- 23505 coupon_batches_web_name_key.
UPDATE promotion.coupon_batches SET purpose = @purpose WHERE id = @id;

-- name: InsertImportBatch :exec
-- Lô của mã nhập; đã có (purpose, tên) → không đổi gì.
INSERT INTO promotion.coupon_batches (tenant_id, name, quantity, template, created_by, purpose)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @name, @quantity, @template, @created_by, 'imported')
ON CONFLICT (tenant_id, purpose, name) WHERE purpose <> 'manual' DO NOTHING;

-- name: LockImportBatches :many
-- Khóa các lô nhập theo tên, id tăng dần (ADR-46 nhóm 10, trước coupons).
SELECT id, name
FROM promotion.coupon_batches
WHERE purpose = 'imported' AND name = ANY(sqlc.arg(names)::text[])
ORDER BY id
FOR UPDATE;

-- name: InsertImportedCoupons :many
-- Mã nhập từ website: mỗi mã dùng một lần (max_uses 1), used[i] 1 = đã dùng trên website; max_discounts[i] 0 = không
-- trần; given_ats[i] rỗng = chưa trao. Mã đã có → bỏ qua (bên gọi so điều khoản). Trả các mã đã chèn.
INSERT INTO promotion.coupons (tenant_id, code, batch_id, name, discount_kind, value, max_discount, min_subtotal,
                               channels, max_uses, used_count, valid_from, valid_to, given_at, origin, created_by)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, (sqlc.arg(codes)::text[])[i]::citext,
       (sqlc.arg(batch_ids)::uuid[])[i], (sqlc.arg(names)::text[])[i], (sqlc.arg(discount_kinds)::text[])[i],
       (sqlc.arg(vals)::bigint[])[i], NULLIF((sqlc.arg(max_discounts)::bigint[])[i], 0),
       (sqlc.arg(min_subtotals)::bigint[])[i], sqlc.arg(channels)::text[], 1, (sqlc.arg(used)::int[])[i],
       (sqlc.arg(valid_froms)::timestamptz[])[i], (sqlc.arg(valid_tos)::timestamptz[])[i],
       NULLIF((sqlc.arg(given_ats)::text[])[i], '')::timestamptz, 'web_import', @created_by::uuid
FROM generate_series(1, cardinality(sqlc.arg(codes)::text[])) AS i
ON CONFLICT (tenant_id, code) DO NOTHING
RETURNING code::text AS code;

-- name: CouponTermsByCodes :many
-- Điều khoản của các mã có sẵn (so với mã nhập trùng).
SELECT code::text AS code, discount_kind, value, coalesce(max_discount, 0)::bigint AS max_discount, min_subtotal,
       valid_from, valid_to
FROM promotion.coupons
WHERE code = ANY(sqlc.arg(codes)::text[]::citext[]);

-- name: RecountImportBatches :exec
-- Số mã thật của lô nhập (bên gọi đã khóa các lô).
UPDATE promotion.coupon_batches AS b
SET quantity = greatest(1, (SELECT count(*) FROM promotion.coupons AS c WHERE c.batch_id = b.id))
WHERE b.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: MarkCouponGiven :many
-- Ghi lúc trao mã lần đầu; mã của lô manual (nhân viên tạo) không thuộc website → không đổi. 0 dòng: đã trao hay không có.
UPDATE promotion.coupons AS c
SET given_at = @at
FROM promotion.coupon_batches AS b
WHERE c.code = sqlc.arg(code)::text::citext AND c.given_at IS NULL
  AND b.tenant_id = c.tenant_id AND b.id = c.batch_id AND b.purpose <> 'manual'
RETURNING c.id, c.given_at;

-- name: WebCouponGiven :one
-- Mã thuộc lô của website và lúc đã trao (rỗng: chưa trao).
SELECT c.id, c.given_at
FROM promotion.coupons AS c
JOIN promotion.coupon_batches AS b ON b.tenant_id = c.tenant_id AND b.id = c.batch_id
WHERE c.code = sqlc.arg(code)::text::citext AND b.purpose <> 'manual';

-- name: ListBatches :many
-- Đợt mã mới nhất trước (id uuidv7 giảm dần) sau con trỏ after; purpose rỗng = mọi loại.
SELECT id, name, code_prefix, quantity, template, created_by, created_at, purpose
FROM promotion.coupon_batches
WHERE (sqlc.narg(purpose)::text IS NULL OR purpose = sqlc.narg(purpose)::text)
  AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after)::uuid)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit)::int;

-- name: BatchStats :one
-- Số mã đã trao, đã dùng, hết hạn chưa dùng (valid_to ≤ now) của một lô.
SELECT count(*) FILTER (WHERE given_at IS NOT NULL)::int AS given,
       count(*) FILTER (WHERE used_count > 0)::int AS used,
       count(*) FILTER (WHERE used_count = 0 AND valid_to <= @now)::int AS expired
FROM promotion.coupons
WHERE batch_id = @batch_id;
