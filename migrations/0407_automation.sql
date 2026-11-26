-- Quy tắc tự động và lượt chạy (kế hoạch M4, Task 165 / M4-25; 04 mục 5.3, 05 mục 3). Pha mở rộng: CREATE TABLE,
-- INDEX, VIEW, GRANT; không thu hẹp gì.
-- - automation_rules: một dòng mỗi (Page, loại); chưa có dòng = mặc định, tắt (API trả version 0). config là cấu hình
--   theo loại (oneOf của openapi.yaml), chữ shop soạn, không phải dữ liệu khách.
-- - automation_runs: mỗi lượt quy tắc gặp một sự kiện ghi đúng một dòng; UNIQUE (tenant_id, rule_id, external_ref)
--   chặn chạy hai lần kể cả khi sự kiện được xử lý lại (webhook, đọc comment, hỏi bù). Lượt bị bỏ qua cũng ghi dòng
--   (để trả lời "vì sao không gửi"). command_job_id không có khóa ngoại: platform.jobs được dọn theo hạn giữ.
-- - Thứ tự khóa: nhóm 6 ADR-46 (… messages → message_revisions → automation_rules → automation_runs …).

-- +goose Up
CREATE TABLE conversation.automation_rules (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    channel_account_id uuid NOT NULL,
    kind               text NOT NULL CHECK (kind IN ('auto_hide', 'auto_reply_comment', 'greeting', 'follow_up')),
    config             jsonb NOT NULL CHECK (jsonb_typeof(config) = 'object'),
    enabled            boolean NOT NULL DEFAULT false,
    version            int NOT NULL DEFAULT 1,
    updated_by         uuid,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, channel_account_id, kind),
    FOREIGN KEY (tenant_id, channel_account_id) REFERENCES channel.channel_accounts (tenant_id, id),
    FOREIGN KEY (tenant_id, updated_by) REFERENCES identity.users (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.automation_rules');

CREATE TABLE conversation.automation_runs (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid NOT NULL REFERENCES platform.tenants (id),
    rule_id        uuid NOT NULL,
    external_ref   text NOT NULL CHECK (length(external_ref) BETWEEN 1 AND 200),
    subject_ref    text,
    outcome        text NOT NULL CHECK (outcome IN ('enqueued', 'done', 'failed', 'dropped', 'skipped_stale',
                                                    'skipped_dnc', 'skipped_limit', 'skipped_policy',
                                                    'skipped_read_only', 'skipped_live')),
    command_job_id uuid,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, rule_id, external_ref),
    FOREIGN KEY (tenant_id, rule_id) REFERENCES conversation.automation_rules (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.automation_runs');
CREATE INDEX automation_runs_subject ON conversation.automation_runs (tenant_id, rule_id, subject_ref, created_at DESC);
CREATE INDEX automation_runs_job ON conversation.automation_runs (tenant_id, command_job_id)
    WHERE command_job_id IS NOT NULL;

-- View công bố (docs/views.md): M7 đọc "mỗi Page có dòng cấu hình" cho báo cáo sẵn sàng (B2-11); không có config.
CREATE VIEW conversation.automation_rules_v WITH (security_invoker = true) AS
SELECT tenant_id, id, channel_account_id, kind, enabled, updated_at, version
FROM conversation.automation_rules;

REVOKE ALL ON conversation.automation_rules, conversation.automation_runs FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON conversation.automation_rules TO crm_app;
GRANT SELECT, INSERT, UPDATE ON conversation.automation_runs TO crm_app;
GRANT SELECT ON conversation.automation_rules_v TO crm_app;
