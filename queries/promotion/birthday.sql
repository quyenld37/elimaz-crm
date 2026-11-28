-- Quà sinh nhật (kế hoạch M5, Task 468 / M5-56; QĐ40). Mọi câu chạy trong tx tenant của bên gọi (RLS lọc tenant). Hồ
-- sơ khách đọc qua view customer.birthday_profiles_v (docs/views.md). Thứ tự khóa ADR-46 nhóm 10: coupon_batches →
-- coupons → birthday_settings → birthday_runs → birthday_gifts; lượt phát lấy khóa tư vấn trước mọi khóa dòng.

-- name: GetBirthdaySettings :one
-- Cài đặt của tenant, không khóa; chưa có dòng → pgx.ErrNoRows (bên gọi dùng mặc định).
SELECT enabled, amount, min_order, days_before, valid_days, min_lead_days, version, updated_by, updated_at
FROM promotion.birthday_settings;

-- name: InsertBirthdaySettings :exec
-- Lần lưu đầu: chèn dòng mặc định (version 1) rồi khóa như dòng có sẵn; đã có → không đổi gì.
INSERT INTO promotion.birthday_settings (tenant_id, enabled, amount, min_order, days_before, valid_days, min_lead_days)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @enabled, @amount, @min_order, @days_before,
        @valid_days, @min_lead_days)
ON CONFLICT (tenant_id) DO NOTHING;

-- name: LockBirthdaySettings :one
SELECT enabled, amount, min_order, days_before, valid_days, min_lead_days, version, updated_by, updated_at
FROM promotion.birthday_settings
FOR UPDATE;

-- name: UpdateBirthdaySettings :one
UPDATE promotion.birthday_settings
SET enabled = @enabled, amount = @amount, min_order = @min_order, days_before = @days_before,
    valid_days = @valid_days, min_lead_days = @min_lead_days, version = version + 1, updated_by = @updated_by,
    updated_at = @now
WHERE version = @version
RETURNING enabled, amount, min_order, days_before, valid_days, min_lead_days, version, updated_by, updated_at;

-- name: TryBirthdayRunLock :one
-- Khóa tư vấn của lượt phát theo tenant (docs/adr/0046-thu-tu-khoa.md mục "Khóa tư vấn"), nhả khi transaction kết thúc.
SELECT pg_try_advisory_xact_lock(hashtextextended('promotion.birthday_run:' || current_setting('app.tenant_id'), 0));

-- name: InsertBirthdayRun :one
INSERT INTO promotion.birthday_runs (tenant_id, trigger, started_by, started_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @trigger, sqlc.narg(started_by)::uuid, @started_at)
RETURNING id;

-- name: FinishBirthdayRun :one
UPDATE promotion.birthday_runs
SET given = @given, failed = @failed, finished_at = clock_timestamp()
WHERE id = @id
RETURNING id, trigger, started_by, started_at, finished_at, given, failed;

-- name: LastBirthdayRun :one
SELECT id, trigger, started_by, started_at, finished_at, given, failed
FROM promotion.birthday_runs
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: BirthdayCandidates :many
-- Khách có thể đến hạn quà: hồ sơ chưa dừng, sinh tháng trong months, đã có đơn giao (của gốc) hoặc cờ đơn cũ; kèm
-- các năm đã có quà.
SELECT p.customer_id, p.phone, p.day, p.month, p.created_at,
       coalesce((SELECT array_agg(g.gift_year ORDER BY g.gift_year) FROM promotion.birthday_gifts AS g
                 WHERE g.customer_id = p.customer_id), '{}')::int[] AS gift_years
FROM customer.birthday_profiles_v AS p
WHERE p.stopped_at IS NULL
  AND p.month = ANY(sqlc.arg(months)::int[])
  AND (p.delivered_count >= 1 OR p.legacy_delivered)
ORDER BY p.customer_id;

-- name: BatchCouponIDs :many
-- Mã của một lô kèm SĐT gắn mã.
SELECT id, coalesce(bound_phone, '')::text AS bound_phone
FROM promotion.coupons
WHERE batch_id = @batch_id;

-- name: InsertBirthdayGifts :exec
-- Quà của lượt phát; (khách, năm) đã có → lỗi 23505 (bên gọi giữ khóa tư vấn nên không xảy ra).
INSERT INTO promotion.birthday_gifts (tenant_id, customer_id, gift_year, coupon_id, run_id, created_at)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, (sqlc.arg(customer_ids)::uuid[])[i],
       (sqlc.arg(years)::int[])[i], (sqlc.arg(coupon_ids)::uuid[])[i], @run_id, @now
FROM generate_series(1, cardinality(sqlc.arg(customer_ids)::uuid[])) AS i;

-- name: BirthdayProfiles :many
-- Mọi hồ sơ sinh nhật (màn Tất cả khách, xuất CSV).
SELECT customer_id, full_name, phone, coalesce(email, '')::text AS email, day, month, consent_zalo, consent_mail,
       stopped_at, created_at, (delivered_count >= 1 OR legacy_delivered)::bool AS delivered
FROM customer.birthday_profiles_v
ORDER BY customer_id;

-- name: BirthdayGiftsSince :many
-- Quà từ năm from_year kèm trạng thái mã.
SELECT g.customer_id, g.gift_year, g.coupon_id, c.code::text AS code, c.value, c.min_subtotal, c.valid_from, c.valid_to,
       c.given_at, c.used_count, g.created_at
FROM promotion.birthday_gifts AS g
JOIN promotion.coupons AS c ON c.tenant_id = g.tenant_id AND c.id = g.coupon_id
WHERE g.gift_year >= @from_year
ORDER BY g.customer_id, g.gift_year;

-- name: PendingBirthdayGifts :many
-- Quà chưa trao (chưa nhắn tay, website chưa gửi), chưa dùng, còn hạn lúc now, của khách chưa dừng nhận tin và gốc không
-- có cờ do_not_contact (gửi quà là liên hệ; Task 521).
-- outbox (của website): chỉ khách đồng ý Zalo hay email, bỏ quà đang chờ thử lại (retry_at > now) hay đã hết lượt tự
-- gửi (send_failures ≥ max_failures), sau con trỏ (after_created, after_code) nếu có; thứ tự (lúc phát, mã) (Task 491).
SELECT g.customer_id, g.gift_year, c.code::text AS code, c.value, c.min_subtotal, c.valid_from, c.valid_to,
       p.full_name, p.phone, coalesce(p.email, '')::text AS email, p.day, p.month, p.consent_zalo,
       (p.consent_mail AND p.email IS NOT NULL)::bool AS consent_mail, p.stop_token, g.created_at, g.send_failures
FROM promotion.birthday_gifts AS g
JOIN promotion.coupons AS c ON c.tenant_id = g.tenant_id AND c.id = g.coupon_id
JOIN customer.birthday_profiles_v AS p ON p.tenant_id = g.tenant_id AND p.customer_id = g.customer_id
JOIN customer.customers_v AS root ON root.tenant_id = p.tenant_id AND root.id = p.root_id
WHERE c.given_at IS NULL AND c.used_count = 0 AND c.valid_to > @now AND p.stopped_at IS NULL
  AND NOT root.do_not_contact
  AND (NOT sqlc.arg(outbox)::bool OR (
        (p.consent_zalo OR (p.consent_mail AND p.email IS NOT NULL))
        AND g.send_failures < sqlc.arg(max_failures)::int AND (g.retry_at IS NULL OR g.retry_at <= @now)))
  AND (sqlc.narg(after_created)::timestamptz IS NULL
       OR (g.created_at, c.code) > (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_code)::text::citext))
ORDER BY g.created_at, c.code
LIMIT sqlc.arg(row_limit)::int;

-- name: LockBirthdayGiftByCode :one
-- Quà sinh nhật theo mã, khóa dòng mã (ADR-46 nhóm 10: coupons trước birthday_gifts) trước khi ghi lỗi gửi.
SELECT c.id, c.code::text AS code, c.given_at, c.used_count, g.send_failures, g.retry_at
FROM promotion.coupons AS c
JOIN promotion.birthday_gifts AS g ON g.tenant_id = c.tenant_id AND g.coupon_id = c.id
WHERE c.code = sqlc.arg(code)::text::citext
FOR UPDATE OF c;

-- name: MarkBirthdayGiftFailed :one
-- Ghi một lần website gửi lỗi: tăng số lỗi, đặt lúc thử lại (NULL khi hết lượt).
UPDATE promotion.birthday_gifts
SET send_failures = send_failures + 1, retry_at = sqlc.narg(retry_at)::timestamptz
WHERE coupon_id = @coupon_id
RETURNING send_failures, retry_at;

-- name: LockBirthdayCoupons :many
-- Mã quà sinh nhật theo chữ, khóa theo id tăng dần (ADR-46 nhóm 10) trước khi ghi lúc trao.
SELECT c.id
FROM promotion.coupons AS c
JOIN promotion.birthday_gifts AS g ON g.tenant_id = c.tenant_id AND g.coupon_id = c.id
WHERE c.code = ANY(sqlc.arg(codes)::text[]::citext[])
ORDER BY c.id
FOR UPDATE OF c;

-- name: MarkBirthdayCouponsGiven :many
-- Ghi lúc trao cho các mã đã khóa chưa trao; trả mã vừa ghi.
UPDATE promotion.coupons
SET given_at = @at
WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND given_at IS NULL
RETURNING code::text AS code;
