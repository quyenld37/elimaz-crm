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

-- name: SetPostImage :one
-- Ảnh bài (Task 501): đặt image_file_id của bài (Page, mã ngoài) khi bài chưa có ảnh; đã có ảnh → 0 dòng (không ghi
-- tệp mới).
UPDATE conversation.posts
SET image_file_id = @file_id
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id
  AND image_file_id IS NULL
RETURNING id;

-- name: ListPagePosts :many
-- Bài của Page (Task 501: chọn bài ngoại lệ tự ẩn, bài của tự trả lời comment), mới gặp trước theo id (uuidv7), trang
-- theo id trước @before_id.
SELECT id, external_id, kind, snippet, created_time, permalink
FROM conversation.posts
WHERE channel_account_id = @channel_account_id
  AND id < @before_id::uuid
ORDER BY id DESC
LIMIT @lim::int;
