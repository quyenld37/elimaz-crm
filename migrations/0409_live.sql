-- Phiên live của Page (kế hoạch M4, Task 170 / M4-30; Q3, Q5, LV01, LV10). Pha mở rộng: CREATE TABLE, INDEX, GRANT;
-- không thu hẹp gì.
-- - Ba đường nhận biết (webhook live_videos, hỏi live_videos mỗi 30 s, nút theo dõi tay) cùng ghi một dòng mỗi
--   (Page, live_video_id); trạng thái chỉ đi live → ended.
-- - Khóa thuê (lease_owner, lease_until): đúng một worker đọc comment của một phiên; hết hạn thì worker khác nhận.
-- - Thứ tự khóa: nhóm 6 ADR-46 (… conversations → … → reminders → live_sessions → live_comment_claims → messages …).

-- +goose Up
CREATE TABLE conversation.live_sessions (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    channel_account_id uuid NOT NULL,
    post_id            uuid,
    live_video_id      text NOT NULL CHECK (length(live_video_id) BETWEEN 1 AND 100),
    video_id           text CHECK (length(video_id) BETWEEN 1 AND 100),
    post_external_id   text CHECK (length(post_external_id) BETWEEN 1 AND 200),
    status             text NOT NULL CHECK (status IN ('live', 'ended')),
    title              text CHECK (length(title) <= 500),
    started_at         timestamptz NOT NULL,
    ended_at           timestamptz,
    lease_owner        text CHECK (length(lease_owner) <= 200),
    lease_until        timestamptz,
    last_comment_at    timestamptz,
    received_count     int NOT NULL DEFAULT 0 CHECK (received_count >= 0),
    meta_comment_count int,
    reconciled_at      timestamptz,
    source_mode        text NOT NULL DEFAULT 'auto' CHECK (source_mode IN ('auto', 'sse', 'poll')),
    active_source      text CHECK (active_source IN ('sse', 'poll')),
    detected_by        text NOT NULL CHECK (detected_by IN ('webhook', 'poll', 'manual')),
    version            int NOT NULL DEFAULT 1,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT live_sessions_video_key UNIQUE (tenant_id, channel_account_id, live_video_id),
    CONSTRAINT live_sessions_ended_at CHECK ((status = 'ended') = (ended_at IS NOT NULL)),
    CONSTRAINT live_sessions_lease CHECK ((lease_owner IS NULL) = (lease_until IS NULL)),
    FOREIGN KEY (tenant_id, channel_account_id) REFERENCES channel.channel_accounts (tenant_id, id),
    FOREIGN KEY (tenant_id, post_id) REFERENCES conversation.posts (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.live_sessions');
-- Bộ quét khóa thuê tìm phiên live hết khóa; danh sách phiên theo trạng thái.
CREATE INDEX live_sessions_lease ON conversation.live_sessions (tenant_id, status, lease_until);
-- Danh sách phiên mới nhất trước (màn Live), nhận biết comment live theo Page.
CREATE INDEX live_sessions_account ON conversation.live_sessions (tenant_id, channel_account_id, started_at DESC);

GRANT SELECT, INSERT, UPDATE ON conversation.live_sessions TO crm_app;
