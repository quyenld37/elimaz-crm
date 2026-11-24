-- Quà sinh nhật trong CRM (kế hoạch M5, Task 468 / M5-56; QĐ40). Pha mở rộng: chỉ CREATE TABLE, RLS, INDEX, GRANT.
-- - birthday_settings: một dòng mỗi tenant, chưa có dòng = mặc định (tắt) version 1; lần lưu đầu chèn dòng rồi khóa
--   (như ord.web_intake). Giới hạn như màn "Sinh nhật khách" của admin: mệnh giá 10.000–1.000.000 bước 1.000, đơn tối
--   thiểu 0–100.000.000, phát trước 0–30 ngày, hạn 1–60 ngày, đăng ký trước tối thiểu 0–120 ngày.
-- - birthday_runs: mỗi lượt phát (lịch mỗi giờ hay nhân viên bấm "Tạo mã ngay"); một lượt mỗi lúc theo khóa tư vấn
--   promotion.birthday_run:<tenant> (docs/adr/0046-thu-tu-khoa.md). Lượt ghi dòng lúc bắt đầu, cập nhật số đếm và
--   finished_at trước khi commit (cùng transaction).
-- - birthday_gifts: quà của một khách cho một năm sinh nhật (khóa tự nhiên: không phát hai mã cùng năm), trỏ mã trong
--   coupons (lô purpose birthday). "Đã nhắn"/"website đã gửi" là coupons.given_at, không thêm cột ở đây. Chỉ chèn.
-- - Thứ tự khóa: nhóm 10 ADR-46 (… → web_campaign_targets → birthday_settings → birthday_runs → birthday_gifts → …).

-- +goose Up
CREATE TABLE promotion.birthday_settings (
    tenant_id     uuid PRIMARY KEY REFERENCES platform.tenants (id),
    enabled       boolean NOT NULL DEFAULT false,
    amount        bigint NOT NULL CHECK (amount BETWEEN 10000 AND 1000000 AND amount % 1000 = 0),
    min_order     bigint NOT NULL CHECK (min_order BETWEEN 0 AND 100000000),
    days_before   int NOT NULL CHECK (days_before BETWEEN 0 AND 30),
    valid_days    int NOT NULL CHECK (valid_days BETWEEN 1 AND 60),
    min_lead_days int NOT NULL CHECK (min_lead_days BETWEEN 0 AND 120),
    version       int NOT NULL DEFAULT 1,
    updated_by    uuid,
    updated_at    timestamptz
);
SELECT platform.enable_tenant_rls('promotion.birthday_settings');

CREATE TABLE promotion.birthday_runs (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    trigger     text NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    started_by  uuid,
    started_at  timestamptz NOT NULL,
    finished_at timestamptz,
    given       int NOT NULL DEFAULT 0 CHECK (given >= 0),
    failed      int NOT NULL DEFAULT 0 CHECK (failed >= 0),
    UNIQUE (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.birthday_runs');
CREATE INDEX birthday_runs_latest ON promotion.birthday_runs (tenant_id, started_at DESC, id DESC);

CREATE TABLE promotion.birthday_gifts (
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    customer_id uuid NOT NULL,
    gift_year   int NOT NULL CHECK (gift_year BETWEEN 2000 AND 9999),
    coupon_id   uuid NOT NULL,
    run_id      uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, customer_id, gift_year),
    CONSTRAINT birthday_gifts_coupon_key UNIQUE (tenant_id, coupon_id),
    FOREIGN KEY (tenant_id, coupon_id) REFERENCES promotion.coupons (tenant_id, id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES promotion.birthday_runs (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.birthday_gifts');

GRANT SELECT, INSERT ON promotion.birthday_settings TO crm_app;
GRANT UPDATE (enabled, amount, min_order, days_before, valid_days, min_lead_days, version, updated_by, updated_at)
    ON promotion.birthday_settings TO crm_app;
GRANT SELECT, INSERT ON promotion.birthday_runs TO crm_app;
GRANT UPDATE (finished_at, given, failed) ON promotion.birthday_runs TO crm_app;
GRANT SELECT, INSERT ON promotion.birthday_gifts TO crm_app;
