-- Bài viết (Task 151).

-- name: UpsertPost :one
-- Ghi bài viết của Page theo mã ngoài (gửi lại không tạo trùng); loại đã biết không bị 'unknown' ghi đè, trường
-- rỗng giữ giá trị cũ.
INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id, kind, permalink, snippet, created_time,
                                fetched_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @channel_account_id, @external_id, @kind,
        sqlc.narg(permalink)::text, sqlc.narg(snippet)::text, sqlc.narg(created_time)::timestamptz, @fetched_at)
ON CONFLICT (tenant_id, channel_account_id, external_id) DO UPDATE
SET kind         = CASE WHEN EXCLUDED.kind = 'unknown' THEN conversation.posts.kind ELSE EXCLUDED.kind END,
    permalink    = coalesce(EXCLUDED.permalink, conversation.posts.permalink),
    snippet      = coalesce(EXCLUDED.snippet, conversation.posts.snippet),
    created_time = coalesce(EXCLUDED.created_time, conversation.posts.created_time),
    fetched_at   = EXCLUDED.fetched_at
RETURNING id, kind;
