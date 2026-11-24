-- Hồ sơ sinh nhật của khách (kế hoạch M5, Task 468 / M5-56; QĐ40). Pha mở rộng: chỉ CREATE TABLE, RLS, VIEW, GRANT.
-- - Một hồ sơ mỗi khách (customer_id): ngày, tháng sinh (website không hỏi năm; không ghi đè customers.birthday), đồng ý
--   nhận tin qua Zalo, email, SĐT đã đăng ký (mã quà gắn đúng số này), mã dừng nhận tin (link trong tin quà do website
--   gửi; lưu nguyên vì website soạn tin từ outbox của CRM), lúc dừng. legacy_delivered: khách đã có đơn web giao thành
--   công trên website cũ (đơn Nhanh), nhập ở Task 466/467.
-- - Đăng ký lần đầu thắng (như website): chỉ chèn; đổi sau đó chỉ stopped_at.
-- - View công bố customer.birthday_profiles_v (docs/views.md): hồ sơ kèm tên, gốc và delivered_count của gốc (chỉ số chỉ
--   đúng ở gốc). Module promotion đọc ở đây để chọn khách nhận quà.
-- - Thứ tự khóa: nhóm 5 ADR-46, bảng cuối nhóm (… → feedback → birthday_profiles).

-- +goose Up
CREATE TABLE customer.birthday_profiles (
    tenant_id        uuid NOT NULL REFERENCES platform.tenants (id),
    customer_id      uuid NOT NULL,
    phone            text NOT NULL CHECK (phone ~ '^\+84[1-9][0-9]{8,9}$'),
    day              int NOT NULL CHECK (day BETWEEN 1 AND 31),
    month            int NOT NULL CHECK (month BETWEEN 1 AND 12),
    consent_zalo     boolean NOT NULL DEFAULT false,
    consent_mail     boolean NOT NULL DEFAULT false,
    email            text CHECK (length(email) BETWEEN 3 AND 200),
    stop_token       text NOT NULL CHECK (length(stop_token) BETWEEN 16 AND 100),
    stopped_at       timestamptz,
    legacy_delivered boolean NOT NULL DEFAULT false,
    source           text NOT NULL CHECK (source IN ('web', 'import', 'staff')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, customer_id),
    CONSTRAINT birthday_profiles_stop_token_key UNIQUE (tenant_id, stop_token),
    CONSTRAINT birthday_profiles_day_in_month CHECK (
        day <= CASE WHEN month = 2 THEN 29 WHEN month IN (4, 6, 9, 11) THEN 30 ELSE 31 END),
    CONSTRAINT birthday_profiles_mail_needs_email CHECK (NOT consent_mail OR email IS NOT NULL),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.birthday_profiles');
CREATE INDEX birthday_profiles_month ON customer.birthday_profiles (tenant_id, month, day);

CREATE VIEW customer.birthday_profiles_v WITH (security_invoker = true) AS
SELECT p.tenant_id, p.customer_id, c.root_id, c.full_name, p.phone, p.email, p.day, p.month, p.consent_zalo,
       p.consent_mail, p.stop_token, p.stopped_at, p.legacy_delivered, p.source, p.created_at, r.delivered_count
FROM customer.birthday_profiles AS p
JOIN customer.customers AS c ON c.tenant_id = p.tenant_id AND c.id = p.customer_id
JOIN customer.customers AS r ON r.tenant_id = c.tenant_id AND r.id = c.root_id;

GRANT SELECT, INSERT ON customer.birthday_profiles TO crm_app;
GRANT UPDATE (stopped_at, updated_at) ON customer.birthday_profiles TO crm_app;
GRANT SELECT ON customer.birthday_profiles_v TO crm_app;
