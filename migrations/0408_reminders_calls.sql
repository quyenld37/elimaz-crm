-- Nhắc việc trên hội thoại và kết quả cuộc gọi khách (kế hoạch M4, Task 169 / M4-29; Q14, CH17). Pha mở rộng: CREATE
-- TABLE, INDEX, VIEW, FUNCTION, TRIGGER, GRANT; không thu hẹp gì.
-- - note của hai bảng là dữ liệu cá nhân: không vào log, audit, báo hiệu; view call_logs_v không có note.
-- - call_logs chỉ ghi thêm (trigger theo câu lệnh như conversation_assignments, 0402).
-- - Thứ tự khóa: nhóm 6 ADR-46 (… conversations → … → reminders → … → messages → … → call_logs …).

-- +goose Up
CREATE TABLE conversation.reminders (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id       uuid NOT NULL REFERENCES platform.tenants (id),
    conversation_id uuid NOT NULL,
    user_id         uuid NOT NULL,
    remind_at       timestamptz NOT NULL,
    note            text CHECK (length(note) <= 500),
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'fired', 'done', 'cancelled')),
    source          text NOT NULL CHECK (source IN ('manual', 'call_callback')),
    created_by      uuid NOT NULL,
    fired_at        timestamptz,
    version         int NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT reminders_fired_at CHECK (status <> 'fired' OR fired_at IS NOT NULL),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES conversation.conversations (tenant_id, id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES identity.users (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES identity.users (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.reminders');
-- Lịch conversation.reminders tìm nhắc tới hạn; /me/reminders đọc theo người và trạng thái.
CREATE INDEX reminders_due ON conversation.reminders (tenant_id, remind_at) WHERE status = 'pending';
CREATE INDEX reminders_user_status ON conversation.reminders (tenant_id, user_id, status, remind_at);
CREATE INDEX reminders_conversation ON conversation.reminders (tenant_id, conversation_id);

-- Kết quả cuộc gọi khách: gắn hồ sơ khách (customer_id), tùy chọn hội thoại và đơn (order_id tham chiếu mềm, như
-- ord.orders.conversation_id, Q31). "Hẹn gọi lại" có giờ gọi lại, kết quả khác không có.
CREATE TABLE conversation.call_logs (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id       uuid NOT NULL REFERENCES platform.tenants (id),
    customer_id     uuid NOT NULL,
    conversation_id uuid,
    order_id        uuid,
    outcome         text NOT NULL CHECK (outcome IN ('closed', 'no_answer', 'callback', 'wrong_number', 'declined',
                                                     'other')),
    note            text CHECK (length(note) <= 1000),
    callback_at     timestamptz,
    created_by      uuid NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT call_logs_callback_at CHECK ((outcome = 'callback') = (callback_at IS NOT NULL)),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES conversation.conversations (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES identity.users (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.call_logs');
CREATE INDEX call_logs_customer ON conversation.call_logs (tenant_id, customer_id, created_at DESC, id DESC);
CREATE INDEX call_logs_conversation ON conversation.call_logs (tenant_id, conversation_id) WHERE conversation_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION conversation.call_logs_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'call_logs is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER call_logs_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON conversation.call_logs
    FOR EACH STATEMENT EXECUTE FUNCTION conversation.call_logs_append_only();

-- View công bố (docs/views.md): hồ sơ khách, dòng thời gian khách (C7) đọc cuộc gọi; không có note (dữ liệu cá nhân).
CREATE VIEW conversation.call_logs_v WITH (security_invoker = true) AS
SELECT tenant_id, id, customer_id, conversation_id, order_id, outcome, callback_at, created_by, created_at
FROM conversation.call_logs;

GRANT SELECT, INSERT, UPDATE ON conversation.reminders TO crm_app;
GRANT SELECT, INSERT ON conversation.call_logs TO crm_app;
GRANT SELECT ON conversation.call_logs_v TO crm_app;
