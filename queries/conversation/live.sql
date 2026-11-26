-- Phiên live (Task 170; Q3, Q5). Chạy trong tenancy.WithTenant. Trạng thái chỉ đi live → ended. Khóa thuê
-- (lease_owner, lease_until) tính theo đồng hồ của database (now() của transaction ngắn).

-- name: UpsertLiveSession :one
-- Ba đường nhận biết ghi cùng một dòng mỗi (Page, live_video_id): trùng thì chỉ bổ sung mã còn thiếu và chỉ chuyển
-- live → ended, không bao giờ ngược lại; version tăng khi phiên vừa kết thúc.
INSERT INTO conversation.live_sessions (tenant_id, channel_account_id, live_video_id, video_id, post_external_id,
                                        status, title, started_at, ended_at, detected_by)
VALUES (current_setting('app.tenant_id')::uuid, @channel_account_id, @live_video_id, sqlc.narg(video_id),
        sqlc.narg(post_external_id), @status, sqlc.narg(title), @started_at, sqlc.narg(ended_at), @detected_by)
ON CONFLICT (tenant_id, channel_account_id, live_video_id) DO UPDATE
SET video_id         = COALESCE(live_sessions.video_id, EXCLUDED.video_id),
    post_external_id = COALESCE(live_sessions.post_external_id, EXCLUDED.post_external_id),
    title            = COALESCE(live_sessions.title, EXCLUDED.title),
    status           = CASE WHEN EXCLUDED.status = 'ended' THEN 'ended' ELSE live_sessions.status END,
    ended_at         = COALESCE(live_sessions.ended_at, EXCLUDED.ended_at),
    version          = live_sessions.version + CASE WHEN live_sessions.status = 'live' AND EXCLUDED.status = 'ended' THEN 1 ELSE 0 END
RETURNING *;

-- name: FindLiveSessionByVideo :one
-- Phiên mới nhất của Page có mã live hay mã video là video (theo dõi tay không tạo phiên thứ hai cho cùng video).
SELECT *
FROM conversation.live_sessions
WHERE channel_account_id = @channel_account_id
  AND (live_video_id = @video::text OR video_id = @video::text)
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: MatchLiveComment :one
-- Phiên live chứa bài post_external_id của Page: khớp mã bài, mã live hay mã video (cách Meta đặt mã bài của video
-- live chưa xác minh, E4); phiên mới nhất trước.
SELECT id
FROM conversation.live_sessions
WHERE channel_account_id = @channel_account_id
  AND (post_external_id = @post::text OR live_video_id = @post::text OR video_id = @post::text)
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: GetLiveSession :one
SELECT *
FROM conversation.live_sessions
WHERE id = @id;

-- name: EndLiveSession :one
-- Kết thúc có điều kiện: chỉ phiên còn live; 0 dòng → đã kết thúc trước (hay không có).
UPDATE conversation.live_sessions
SET status = 'ended', ended_at = @ended_at, version = version + 1
WHERE id = @id
  AND status = 'live'
RETURNING *;

-- name: EndLiveSessionByVideo :one
-- Kênh báo phiên live_video_id của Page đã dừng; phiên chưa biết hay đã kết thúc → 0 dòng.
UPDATE conversation.live_sessions
SET status = 'ended', ended_at = @ended_at, version = version + 1
WHERE channel_account_id = @channel_account_id
  AND live_video_id = @live_video_id
  AND status = 'live'
RETURNING *;

-- name: ListLiveSessions :many
-- Phiên trên các Page ids (hay mọi Page khi all_pages), lọc trạng thái và mốc bắt đầu; mới nhất trước.
SELECT *
FROM conversation.live_sessions
WHERE (@all_pages::boolean OR channel_account_id = ANY(@ids::uuid[]))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(since)::timestamptz IS NULL OR started_at >= sqlc.narg(since)::timestamptz)
ORDER BY started_at DESC, id DESC
LIMIT @lim::int;

-- name: ListLiveSessionsOfAccount :many
-- Phiên đang live của một Page (lệnh nhận biết hỏi trạng thái phiên không còn trong danh sách của kênh).
SELECT *
FROM conversation.live_sessions
WHERE channel_account_id = @channel_account_id
  AND status = 'live'
ORDER BY id
LIMIT 50;

-- name: ClaimLiveSessions :many
-- Bộ quét nhận tối đa lim phiên live hết khóa (hay chưa có chủ) cho owner; SKIP LOCKED nên hai worker không nhận
-- cùng một phiên; khóa theo id tăng dần (ADR-46).
UPDATE conversation.live_sessions
SET lease_owner = @owner::text,
    lease_until = now() + @ttl::interval
WHERE id IN (SELECT c.id
               FROM conversation.live_sessions AS c
               WHERE c.status = 'live'
                 AND (c.lease_until IS NULL OR c.lease_until < now())
               ORDER BY c.id
               LIMIT @lim::int
               FOR UPDATE SKIP LOCKED)
RETURNING *;

-- name: RenewLiveLease :one
-- Gia hạn khóa của owner (phiên đã kết thúc thì giữ nguyên hạn); 0 dòng → đã mất khóa. Trả trạng thái phiên.
UPDATE conversation.live_sessions
SET lease_until = CASE WHEN status = 'live' THEN now() + @ttl::interval ELSE lease_until END
WHERE id = @id
  AND lease_owner = @owner::text
RETURNING status;

-- name: ReleaseLiveLease :exec
-- Nhả khóa của owner (tắt êm, luồng dừng); khóa đã thuộc worker khác thì không làm gì.
UPDATE conversation.live_sessions
SET lease_owner = NULL, lease_until = NULL
WHERE id = @id
  AND lease_owner = @owner::text;

-- name: RecordLiveComments :execrows
-- Ghi một lô comment của luồng: mốc comment mới nhất không lùi, số việc mới cộng dồn; chỉ khi owner còn giữ khóa.
UPDATE conversation.live_sessions
SET last_comment_at = GREATEST(last_comment_at, sqlc.narg(last_comment_at)::timestamptz),
    received_count  = received_count + @n::int
WHERE id = @id
  AND lease_owner = @owner::text;
