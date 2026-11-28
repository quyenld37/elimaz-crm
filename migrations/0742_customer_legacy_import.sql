-- Khách chuyển từ hệ cũ (kế hoạch M7, Task 542 / M7-42; ADR-48). Pha mở rộng: chỉ CREATE TABLE, RLS, GRANT.
-- - legacy_customers: mã khách của hệ cũ (legacy_id) → khách đã tạo trong CRM. Khóa chính (tenant, legacy_id) làm lần
--   chuyển chạy lại không tạo trùng: khách đã có mã thì bỏ qua.
-- - legacy_review: danh sách "cần xem" — khách của hệ cũ mà CRM không tự quyết (không tự gộp, ADR-23 điểm 6): các SĐT
--   thuộc hai khách CRM khác nhau, SĐT đã thuộc khách đến từ một khách cũ khác, hoặc SĐT khác chưa được thêm cho khách đã
--   có. Chỉ lưu mã cũ, lý do và mã khách CRM liên quan: không tên, không SĐT (dữ liệu cá nhân ở lại hệ cũ và hồ sơ khách).
-- - Thứ tự khóa: nhóm 5 ADR-46, hai bảng cuối nhóm (… → birthday_profiles → legacy_customers → legacy_review).

-- +goose Up
CREATE TABLE customer.legacy_customers (
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    legacy_id   bigint NOT NULL CHECK (legacy_id > 0),
    customer_id uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, legacy_id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.legacy_customers');
CREATE INDEX legacy_customers_customer ON customer.legacy_customers (tenant_id, customer_id);

CREATE TABLE customer.legacy_review (
    tenant_id            uuid NOT NULL REFERENCES platform.tenants (id),
    legacy_id            bigint NOT NULL CHECK (legacy_id > 0),
    reason               text NOT NULL CHECK (reason IN ('phones_split', 'duplicate_in_source', 'phone_not_added')),
    related_customer_ids uuid[] NOT NULL DEFAULT '{}',
    created_at           timestamptz NOT NULL DEFAULT now(),
    resolved_at          timestamptz,
    PRIMARY KEY (tenant_id, legacy_id, reason)
);
SELECT platform.enable_tenant_rls('customer.legacy_review');

GRANT SELECT, INSERT ON customer.legacy_customers, customer.legacy_review TO crm_app;
GRANT UPDATE (resolved_at) ON customer.legacy_review TO crm_app;
