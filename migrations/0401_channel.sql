-- Module channel (kế hoạch M4, Task 145 / M4-05): tài khoản kênh (Page), token mã hóa, chế độ chỉ xem, giới hạn gửi,
-- phiên đăng nhập Facebook. Pha mở rộng: chỉ CREATE, RLS, GRANT.
-- - channel_accounts: một dòng mỗi (tenant, kênh, mã Page). Kênh 'facebook' hay 'fakechat' (kênh giả ngoài production;
--   'fake' đã là khóa hãng giả M3, tệp task 143 Quyết định 6). Token Page mã hóa bằng fieldcrypt (AAD
--   tenant|channel.channel_accounts|credential_enc|id), không bao giờ ra view, API, log. Page mới luôn read_only
--   (MO03); version chỉ tăng khi nhân viên đổi cài đặt (send_mode, human_agent_enabled), không tăng khi hệ thống ghi
--   trạng thái, token, mốc webhook.
-- - send_buckets: thùng token giới hạn gửi theo Page (04 mục 5.3), một câu UPDATE có điều kiện mỗi lần lấy.
-- - oauth_sessions: phiên đăng nhập Facebook trong tab (Task 149 dùng), token người dùng mã hóa, hạn ngắn.
-- - Thứ tự khóa: ADR-46 nhóm 3 (channel_accounts → send_buckets → oauth_sessions).

-- +goose Up
CREATE SCHEMA channel;
GRANT USAGE ON SCHEMA channel TO crm_app;

CREATE TABLE channel.channel_accounts (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id           uuid NOT NULL REFERENCES platform.tenants (id),
    seller_id           uuid NOT NULL,
    channel             text NOT NULL CHECK (channel IN ('facebook', 'fakechat')),
    external_id         text NOT NULL CHECK (external_id <> ''),
    name                text NOT NULL,
    credential_enc      bytea,
    credential_kid      text,
    token_kind          text CHECK (token_kind IN ('page_user', 'system_user')),
    status              text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'degraded', 'disconnected')),
    status_reason       text,
    send_mode           text NOT NULL DEFAULT 'read_only' CHECK (send_mode IN ('read_only', 'enabled')),
    human_agent_enabled boolean NOT NULL DEFAULT false,
    subscribed_fields   text[] NOT NULL DEFAULT '{}',
    token_checked_at    timestamptz,
    last_webhook_at     timestamptz,
    last_feed_at        timestamptz,
    blocked_until       timestamptz,
    version             int NOT NULL DEFAULT 1,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT channel_accounts_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT channel_accounts_external_key UNIQUE (tenant_id, channel, external_id),
    FOREIGN KEY (tenant_id, seller_id) REFERENCES catalog.sellers (tenant_id, id),
    CHECK ((credential_enc IS NULL) = (credential_kid IS NULL))
);
COMMENT ON INDEX channel.channel_accounts_external_key IS
    'seller-exempt: một Page (kênh, mã Page) chỉ nối một lần trong shop, thuộc đúng một người bán (Task 145)';
SELECT platform.enable_tenant_rls('channel.channel_accounts');

CREATE TABLE channel.send_buckets (
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    channel_account_id uuid NOT NULL,
    tokens             double precision NOT NULL,
    capacity           int NOT NULL DEFAULT 50 CHECK (capacity > 0),
    refill_per_sec     double precision NOT NULL DEFAULT 20 CHECK (refill_per_sec > 0),
    updated_at         timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, channel_account_id),
    FOREIGN KEY (tenant_id, channel_account_id) REFERENCES channel.channel_accounts (tenant_id, id)
);
SELECT platform.enable_tenant_rls('channel.send_buckets');

CREATE TABLE channel.oauth_sessions (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid NOT NULL REFERENCES platform.tenants (id),
    user_id        uuid NOT NULL,
    state_hash     bytea NOT NULL,
    user_token_enc bytea,
    user_token_kid text,
    pages          jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(pages) = 'array'),
    expires_at     timestamptz NOT NULL,
    used_at        timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT oauth_sessions_tenant_id_key UNIQUE (tenant_id, id),
    CONSTRAINT oauth_sessions_state_key UNIQUE (tenant_id, state_hash),
    CHECK ((user_token_enc IS NULL) = (user_token_kid IS NULL))
);
SELECT platform.enable_tenant_rls('channel.oauth_sessions');

-- View công bố cho mốc sau (M6, M7; Q-X2): không có credential_enc, credential_kid.
CREATE VIEW channel.channel_accounts_v WITH (security_invoker = true) AS
SELECT id, tenant_id, seller_id, channel, external_id, name, token_kind, status, status_reason, send_mode,
    human_agent_enabled, subscribed_fields, token_checked_at, last_webhook_at, last_feed_at, blocked_until, version,
    created_at, updated_at
FROM channel.channel_accounts;

GRANT SELECT, INSERT ON channel.channel_accounts TO crm_app;
GRANT UPDATE (name, credential_enc, credential_kid, token_kind, status, status_reason, send_mode, human_agent_enabled,
    subscribed_fields, token_checked_at, last_webhook_at, last_feed_at, blocked_until, version, updated_at)
    ON channel.channel_accounts TO crm_app;
GRANT SELECT, INSERT ON channel.send_buckets TO crm_app;
GRANT UPDATE (tokens, updated_at) ON channel.send_buckets TO crm_app;
GRANT SELECT, INSERT, DELETE ON channel.oauth_sessions TO crm_app;
GRANT UPDATE (used_at, pages) ON channel.oauth_sessions TO crm_app;
GRANT SELECT ON channel.channel_accounts_v TO crm_app;
