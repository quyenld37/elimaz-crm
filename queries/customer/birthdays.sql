-- Hồ sơ sinh nhật của khách (kế hoạch M5, Task 468). Chạy trong tx tenant của bên gọi (RLS lọc tenant). Thứ tự khóa
-- ADR-46 nhóm 5: customers (khi tạo khách) → birthday_profiles.

-- name: InsertBirthdayProfile :many
-- Đăng ký lần đầu thắng: khách đã có hồ sơ → không chèn (0 dòng), bên gọi đọc hồ sơ có sẵn.
INSERT INTO customer.birthday_profiles (tenant_id, customer_id, phone, day, month, consent_zalo, consent_mail, email,
                                        stop_token, source, created_at, updated_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @customer_id, @phone, @day, @month, @consent_zalo,
        @consent_mail, sqlc.narg(email)::text, @stop_token, @source, @now, @now)
ON CONFLICT (tenant_id, customer_id) DO NOTHING
RETURNING tenant_id, customer_id, phone, day, month, consent_zalo, consent_mail, coalesce(email, '')::text AS email,
          stopped_at, created_at;

-- name: GetBirthdayProfile :one
SELECT customer_id, phone, day, month, consent_zalo, consent_mail, coalesce(email, '')::text AS email, stopped_at,
       created_at
FROM customer.birthday_profiles
WHERE customer_id = @customer_id;

-- name: StopBirthdayProfile :many
-- Dừng nhận tin theo mã dừng; đã dừng → giữ lúc dừng đầu tiên (0 dòng, bên gọi đọc lại).
UPDATE customer.birthday_profiles
SET stopped_at = @now, updated_at = @now
WHERE stop_token = @stop_token AND stopped_at IS NULL
RETURNING tenant_id, customer_id, stopped_at;

-- name: BirthdayStoppedAt :one
SELECT customer_id, stopped_at
FROM customer.birthday_profiles
WHERE stop_token = @stop_token;

-- name: ImportBirthdayProfile :many
-- Hồ sơ sinh nhật cũ của website (Task 491): giữ mã dừng, lúc đăng ký, lúc dừng, cờ đơn cũ. Khách đã có hồ sơ hay mã
-- dừng đã thuộc hồ sơ khác → không chèn (0 dòng), bên gọi đọc lại để phân loại.
INSERT INTO customer.birthday_profiles (tenant_id, customer_id, phone, day, month, consent_zalo, consent_mail, email,
                                        stop_token, stopped_at, legacy_delivered, source, created_at, updated_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @customer_id, @phone, @day, @month, @consent_zalo,
        @consent_mail, sqlc.narg(email)::text, @stop_token, sqlc.narg(stopped_at)::timestamptz, @legacy_delivered,
        'import', @saved_at, @now)
ON CONFLICT DO NOTHING
RETURNING tenant_id, customer_id;

-- name: BirthdayProfileToken :one
-- Mã dừng và lúc dừng của hồ sơ của khách (phân loại mục nhập không chèn được).
SELECT stop_token, stopped_at
FROM customer.birthday_profiles
WHERE customer_id = @customer_id;

-- name: StopBirthdayProfileOf :many
-- Dừng nhận tin hồ sơ của khách lúc at (website ghi khách đã dừng); đã dừng → 0 dòng.
UPDATE customer.birthday_profiles
SET stopped_at = @at, updated_at = @now
WHERE customer_id = @customer_id AND stopped_at IS NULL
RETURNING tenant_id, customer_id;
