-- Mẫu trả lời có biến (kế hoạch M4, Task 161 / M4-21; CH07, 01 mục 6). Pha mở rộng: CREATE TABLE, INDEX, VIEW, GRANT;
-- không thu hẹp gì.
-- - channel_account_id NULL = mẫu dùng chung mọi Page; có giá trị = chỉ dùng ở Page đó.
-- - Xóa mềm (deleted_at); phím tắt không trùng (không phân biệt hoa thường) giữa các mẫu còn sống của shop.
-- - body là chữ shop soạn (có biến {ten_khach}…), không phải dữ liệu khách; view công bố vẫn không có body (B2-11).
-- - Thứ tự khóa: nhóm 6 ADR-46 (tags → reply_templates → page_settings → …).

-- +goose Up
CREATE TABLE conversation.reply_templates (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    channel_account_id uuid,
    shortcut           text NOT NULL CHECK (shortcut ~ '^[a-z0-9_-]{1,32}$'),
    title              text NOT NULL CHECK (length(title) BETWEEN 1 AND 100),
    body               text NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
    attachment_file_id uuid,
    quick_replies      jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(quick_replies) = 'array'
                                                          AND jsonb_array_length(quick_replies) <= 13),
    deleted_at         timestamptz,
    version            int NOT NULL DEFAULT 1,
    created_by         uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, channel_account_id) REFERENCES channel.channel_accounts (tenant_id, id),
    FOREIGN KEY (tenant_id, attachment_file_id) REFERENCES platform.files (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES identity.users (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.reply_templates');
CREATE UNIQUE INDEX reply_templates_shortcut_live ON conversation.reply_templates (tenant_id, lower(shortcut))
    WHERE deleted_at IS NULL;
CREATE INDEX reply_templates_page ON conversation.reply_templates (tenant_id, channel_account_id)
    WHERE deleted_at IS NULL;

-- View công bố (docs/views.md): M7 đọc mẫu còn sống cho báo cáo sẵn sàng (B2-11); không có body.
CREATE VIEW conversation.reply_templates_v WITH (security_invoker = true) AS
SELECT tenant_id, id, channel_account_id, shortcut, title, created_at
FROM conversation.reply_templates
WHERE deleted_at IS NULL;

REVOKE ALL ON conversation.reply_templates FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON conversation.reply_templates TO crm_app;
GRANT SELECT ON conversation.reply_templates_v TO crm_app;
