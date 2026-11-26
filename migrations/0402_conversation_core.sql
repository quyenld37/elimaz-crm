-- Lược đồ hội thoại lõi (kế hoạch M4, Task 151 / M4-11; spec 05 mục 3, 04 mục 7). Pha mở rộng: CREATE SCHEMA, TABLE,
-- INDEX, VIEW, FUNCTION, TRIGGER, GRANT; không thu hẹp gì.
-- - Mọi bảng có tenant_id, UNIQUE (tenant_id, id), khóa ngoại ghép (tenant_id, …), RLS ép (platform.enable_tenant_rls).
-- - channel_account_id chưa có khóa ngoại tới channel.channel_accounts: bảng đó do 0401 (Task 145) tạo, chưa gộp lúc
--   viết; migration sau thêm khóa ngoại (tệp task-151, Quyết định 1).
-- - body, sender_name, last_snippet, attachments là dữ liệu cá nhân: không vào log, audit.
-- - Ràng buộc nghiệp vụ trong database: ghi chú nội bộ không bao giờ ra ngoài (CH07), một tin riêng đang sống mỗi
--   comment (CH05); conversation_assignments, message_revisions chỉ ghi thêm (DELETE message_revisions chỉ khi
--   app.retention = on, dọn theo hạn giữ của Task 164).
-- - Thứ tự khóa: nhóm 6 ADR-46 (tags → reply_templates → page_settings → posts → conversations →
--   conversation_tag_links → conversation_assignments → … → messages → message_revisions …).

-- +goose Up
CREATE SCHEMA conversation;
GRANT USAGE ON SCHEMA conversation TO crm_app;

-- Nhãn hội thoại; xóa mềm (deleted_at), tên không trùng (không phân biệt hoa thường) giữa các nhãn còn sống.
CREATE TABLE conversation.tags (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    color      text NOT NULL CHECK (color ~ '^[a-z][a-z0-9-]{0,39}$'), -- tên token màu
    sort       int NOT NULL DEFAULT 0,
    deleted_at timestamptz,
    version    int NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.tags');
CREATE UNIQUE INDEX tags_name_live ON conversation.tags (tenant_id, lower(name)) WHERE deleted_at IS NULL;

-- Cài đặt theo Page (một dòng mỗi tài khoản kênh). Mẫu xin địa chỉ, nguồn khách: khóa ngoại thêm khi bảng đích có
-- (reply_templates, Task 161) hoặc kiểm ở Go (ord.traffic_sources_v, module khác).
CREATE TABLE conversation.page_settings (
    tenant_id                  uuid NOT NULL REFERENCES platform.tenants (id),
    channel_account_id         uuid NOT NULL,
    round_robin_enabled        boolean NOT NULL DEFAULT false,
    round_robin_user_ids       uuid[] NOT NULL DEFAULT '{}',
    round_robin_last_user_id   uuid,
    ask_address_template_id    uuid,
    inbox_traffic_source_id    uuid,
    comment_traffic_source_id  uuid,
    live_slots                 jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(live_slots) = 'array'),
    comment_poll_mode          text NOT NULL DEFAULT 'auto' CHECK (comment_poll_mode IN ('auto', 'on', 'off')),
    automation_max_age_minutes int NOT NULL DEFAULT 15 CHECK (automation_max_age_minutes BETWEEN 5 AND 120),
    version                    int NOT NULL DEFAULT 1,
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, channel_account_id)
);
SELECT platform.enable_tenant_rls('conversation.page_settings');

-- Bài viết của Page (bài thường, live, quảng cáo) mà comment thuộc về.
CREATE TABLE conversation.posts (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    channel_account_id uuid NOT NULL,
    external_id        text NOT NULL CHECK (length(external_id) BETWEEN 1 AND 200),
    kind               text NOT NULL DEFAULT 'unknown' CHECK (kind IN ('post', 'live', 'ad', 'unknown')),
    permalink          text,
    snippet            text,
    created_time       timestamptz,
    fetched_at         timestamptz,
    UNIQUE (tenant_id, id),
    CONSTRAINT posts_external_key UNIQUE (tenant_id, channel_account_id, external_id)
);
SELECT platform.enable_tenant_rls('conversation.posts');

-- Hội thoại: inbox (một luồng Messenger với một khách) hoặc comment (luồng comment của một khách dưới một bài).
CREATE TABLE conversation.conversations (
    id                       uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id                uuid NOT NULL REFERENCES platform.tenants (id),
    channel_account_id       uuid NOT NULL,
    customer_id              uuid NOT NULL,
    kind                     text NOT NULL CHECK (kind IN ('inbox', 'comment')),
    external_thread_ref      text NOT NULL CHECK (length(external_thread_ref) BETWEEN 1 AND 400),
    post_id                  uuid,
    status                   text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'pending', 'done', 'spam')),
    assignee_id              uuid,
    avatar_file_id           uuid,
    last_message_at          timestamptz NOT NULL,
    last_customer_message_at timestamptz,
    last_staff_message_at    timestamptz,
    unread_count             int NOT NULL DEFAULT 0 CHECK (unread_count >= 0),
    last_snippet             text,
    blocked_by_user          boolean NOT NULL DEFAULT false,
    version                  int NOT NULL DEFAULT 1,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT conversations_thread_key UNIQUE (tenant_id, channel_account_id, kind, external_thread_ref),
    CONSTRAINT conversations_comment_has_post CHECK ((kind = 'comment') = (post_id IS NOT NULL)),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id),
    FOREIGN KEY (tenant_id, post_id) REFERENCES conversation.posts (tenant_id, id),
    FOREIGN KEY (tenant_id, assignee_id) REFERENCES identity.users (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.conversations');
CREATE INDEX conversations_page_status ON conversation.conversations
    (tenant_id, channel_account_id, status, last_message_at DESC, id DESC);
CREATE INDEX conversations_assignee_status ON conversation.conversations
    (tenant_id, assignee_id, status, last_message_at DESC, id DESC);
CREATE INDEX conversations_customer ON conversation.conversations (tenant_id, customer_id);
CREATE INDEX conversations_recent ON conversation.conversations (tenant_id, last_message_at DESC, id DESC);

CREATE TABLE conversation.conversation_tag_links (
    tenant_id       uuid NOT NULL REFERENCES platform.tenants (id),
    conversation_id uuid NOT NULL,
    tag_id          uuid NOT NULL,
    created_by      uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, tag_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES conversation.conversations (tenant_id, id),
    FOREIGN KEY (tenant_id, tag_id) REFERENCES conversation.tags (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.conversation_tag_links');
CREATE INDEX conversation_tag_links_tag ON conversation.conversation_tag_links (tenant_id, tag_id, conversation_id);

-- Lịch sử phân công, chỉ ghi thêm.
CREATE TABLE conversation.conversation_assignments (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id       uuid NOT NULL REFERENCES platform.tenants (id),
    conversation_id uuid NOT NULL,
    from_user_id    uuid,
    to_user_id      uuid,
    actor_id        uuid,
    reason          text NOT NULL CHECK (reason IN ('self', 'manager', 'round_robin', 'unassign')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES conversation.conversations (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.conversation_assignments');
CREATE INDEX conversation_assignments_conversation ON conversation.conversation_assignments
    (tenant_id, conversation_id, created_at);

-- Tin: tin nhắn, comment, tin riêng trả comment, ghi chú nội bộ, tin hệ thống.
CREATE TABLE conversation.messages (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           uuid NOT NULL REFERENCES platform.tenants (id),
    conversation_id     uuid NOT NULL,
    channel_account_id  uuid NOT NULL,
    external_id         text,
    client_ref          uuid,
    direction           text NOT NULL CHECK (direction IN ('in', 'out')),
    kind                text NOT NULL CHECK (kind IN ('message', 'comment', 'private_reply', 'note', 'system')),
    origin              text NOT NULL CHECK (origin IN ('customer', 'staff', 'automation', 'page_external',
                                                        'own_echo_unmatched', 'history', 'system')),
    parent_external_id  text,
    post_id             uuid,
    body                text,
    attachments         jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(attachments) = 'array'),
    quick_replies       jsonb,
    quick_reply_payload text,
    sender_staff_id     uuid,
    sender_name         text,
    send_status         text CHECK (send_status IN ('queued', 'sent', 'failed', 'checking', 'in_doubt', 'expired',
                                                    'discarded')),
    fail_reason         text,
    command_job_id      uuid,
    send_checking_since timestamptz,
    is_hidden           boolean NOT NULL DEFAULT false,
    removed_at          timestamptz,
    edited_at           timestamptz,
    is_live             boolean NOT NULL DEFAULT false,
    delivered_at        timestamptz,
    read_at             timestamptz,
    reactions           jsonb,
    sent_at             timestamptz NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT messages_external_key UNIQUE (tenant_id, channel_account_id, external_id),
    CONSTRAINT messages_client_ref_key UNIQUE (tenant_id, client_ref),
    -- CH07: ghi chú nội bộ không bao giờ ra ngoài.
    CONSTRAINT messages_note_internal CHECK (kind <> 'note' OR (send_status IS NULL AND external_id IS NULL
                                             AND command_job_id IS NULL AND direction = 'out')),
    CONSTRAINT messages_direction_origin CHECK ((direction = 'in') = (origin = 'customer')),
    -- Tin của nhân viên, tự động (trừ ghi chú) luôn có trạng thái gửi.
    CONSTRAINT messages_outgoing_status CHECK (origin NOT IN ('staff', 'automation') OR kind = 'note'
                                               OR send_status IS NOT NULL),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES conversation.conversations (tenant_id, id),
    FOREIGN KEY (tenant_id, post_id) REFERENCES conversation.posts (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.messages');
-- CH05: một tin riêng đang sống mỗi comment.
CREATE UNIQUE INDEX messages_one_live_private_reply ON conversation.messages
    (tenant_id, channel_account_id, parent_external_id)
    WHERE kind = 'private_reply' AND send_status IN ('queued', 'sent', 'checking', 'in_doubt');
CREATE INDEX messages_conversation ON conversation.messages (tenant_id, conversation_id, sent_at DESC, id DESC);
CREATE INDEX messages_live_by_post ON conversation.messages (tenant_id, post_id, sent_at, id) WHERE is_live;
CREATE INDEX messages_checking ON conversation.messages (tenant_id, send_status, send_checking_since)
    WHERE send_status = 'checking';

-- Lịch sử sửa, xóa, ẩn của một tin, chỉ ghi thêm.
CREATE TABLE conversation.message_revisions (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    message_id  uuid NOT NULL,
    body        text,
    is_hidden   boolean,
    removed     boolean,
    source      text NOT NULL CHECK (source IN ('customer', 'staff', 'page_external', 'automation')),
    recorded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, message_id) REFERENCES conversation.messages (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.message_revisions');
CREATE INDEX message_revisions_message ON conversation.message_revisions (tenant_id, message_id, recorded_at);

-- Trigger theo câu lệnh như identity.audit_logs (0004): chặn mọi role, kể cả chủ bảng và cả khi RLS lọc hết dòng.
-- +goose StatementBegin
CREATE FUNCTION conversation.conversation_assignments_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'conversation_assignments is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER conversation_assignments_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON conversation.conversation_assignments
    FOR EACH STATEMENT EXECUTE FUNCTION conversation.conversation_assignments_append_only();

-- message_revisions: DELETE được khi transaction đặt app.retention = on (việc dọn theo hạn giữ, Task 164).
-- +goose StatementBegin
CREATE FUNCTION conversation.message_revisions_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('app.retention', true) = 'on' THEN
        RETURN NULL;
    END IF;
    RAISE EXCEPTION 'message_revisions is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER message_revisions_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON conversation.message_revisions
    FOR EACH STATEMENT EXECUTE FUNCTION conversation.message_revisions_append_only();

-- View công bố (docs/views.md): M6, M7 và dòng thời gian khách đọc hội thoại; M7 đọc nhãn còn sống.
CREATE VIEW conversation.conversations_v WITH (security_invoker = true) AS
SELECT tenant_id, id, channel_account_id, customer_id, kind, status, assignee_id, post_id, last_message_at,
       last_customer_message_at, created_at
FROM conversation.conversations;

CREATE VIEW conversation.tags_v WITH (security_invoker = true) AS
SELECT tenant_id, id, name, color, sort
FROM conversation.tags
WHERE deleted_at IS NULL;

REVOKE ALL ON ALL TABLES IN SCHEMA conversation FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON conversation.tags, conversation.page_settings, conversation.posts,
    conversation.conversations, conversation.messages TO crm_app;
GRANT SELECT, INSERT, DELETE ON conversation.conversation_tag_links TO crm_app;
GRANT SELECT, INSERT ON conversation.conversation_assignments TO crm_app;
GRANT SELECT, INSERT, DELETE ON conversation.message_revisions TO crm_app;
GRANT SELECT ON conversation.conversations_v, conversation.tags_v TO crm_app;
