-- Góp ý của khách (kế hoạch M5, Task 455 / M5-43; Task 450: góp ý thuộc customer). Pha mở rộng: CREATE TABLE, INDEX,
-- RLS, GRANT.
-- - Website gửi form /gop-y qua khóa dịch vụ (web.feedback.write); nhân viên xem, đổi trạng thái, ghi chú nội bộ.
-- - customer_id: hồ sơ giữ danh tính phone khớp contact lúc gửi (không khớp → NULL, không tạo khách). Như ghi chú khách:
--   gộp, hoàn tác gộp không dời; đọc trả gốc hiện tại và lọc theo cả nhóm (customers.root_id).
-- - body, name, contact, note là dữ liệu cá nhân: không vào log, audit chỉ ghi tên trường.
-- - client_hash: băm địa chỉ của khách do website tính; cùng created_at là khóa tự nhiên khi nhập dữ liệu cũ (M5-55).
-- - Thứ tự khóa: nhóm 5 ADR-46 (… → customer_imports → feedback → birthday_profiles).

-- +goose Up
CREATE TABLE customer.feedback (
    id          uuid NOT NULL DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    topic       text NOT NULL CHECK (topic IN ('product', 'delivery', 'website', 'service', 'other')),
    mood        int CHECK (mood BETWEEN 1 AND 5),
    body        text NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
    name        text CHECK (length(name) BETWEEN 1 AND 80),
    contact     text CHECK (length(contact) BETWEEN 1 AND 120),
    order_ref   text CHECK (length(order_ref) BETWEEN 1 AND 60),
    page        text CHECK (length(page) BETWEEN 1 AND 300),
    device      text CHECK (length(device) BETWEEN 1 AND 120),
    client_hash text CHECK (length(client_hash) BETWEEN 1 AND 128),
    customer_id uuid,
    state       text NOT NULL DEFAULT 'new' CHECK (state IN ('new', 'seen', 'answered')),
    note        text CHECK (length(note) BETWEEN 1 AND 1000),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid,
    version     int NOT NULL DEFAULT 1,
    PRIMARY KEY (id),
    CONSTRAINT feedback_tenant_id_key UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.feedback');
CREATE INDEX feedback_state_created ON customer.feedback (tenant_id, state, created_at DESC, id DESC);
CREATE INDEX feedback_created ON customer.feedback (tenant_id, created_at DESC, id DESC);
CREATE INDEX feedback_customer ON customer.feedback (tenant_id, customer_id) WHERE customer_id IS NOT NULL;
CREATE INDEX feedback_client_hash ON customer.feedback (tenant_id, created_at, client_hash) WHERE client_hash IS NOT NULL;

GRANT SELECT, INSERT ON customer.feedback TO crm_app;
GRANT UPDATE (state, note, updated_at, updated_by, version) ON customer.feedback TO crm_app;
