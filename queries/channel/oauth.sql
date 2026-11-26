-- Phiên đăng nhập Facebook trong tab (kế hoạch M4, Task 149). Hai loại dòng: dòng "start" (user_token_enc NULL, hạn 15
-- phút, state_hash là băm của state gửi cho Meta) và dòng "phiên Page" do exchange chèn (token người dùng mã hóa, danh
-- sách Page, hạn 30 phút). Mọi câu chạy trong tx tenant của bên gọi (RLS).

-- name: InsertOAuthSession :exec
-- fb_user_ref_hash: hex SHA-256 của mã người dùng app (phiên Page, Task 179); NULL ở dòng start.
INSERT INTO channel.oauth_sessions (id, tenant_id, user_id, state_hash, user_token_enc, user_token_kid, pages, expires_at,
    fb_user_ref_hash)
VALUES (@id, current_setting('app.tenant_id')::uuid, @user_id, @state_hash, sqlc.narg('user_token_enc')::bytea,
    sqlc.narg('user_token_kid')::text, @pages, @expires_at, sqlc.narg('fb_user_ref_hash')::text);

-- name: GetOAuthStart :one
-- Dòng start theo băm state (chưa có token người dùng).
SELECT id, user_id, expires_at, used_at
FROM channel.oauth_sessions
WHERE state_hash = @state_hash AND user_token_enc IS NULL;

-- name: UseOAuthStart :one
-- Đánh dấu dòng start đã dùng: chỉ một lần, còn hạn, đúng người dùng (không dòng nào → state không dùng được); trả
-- tenant của dòng (AAD của token người dùng).
UPDATE channel.oauth_sessions
SET used_at = @now
WHERE id = @id AND user_id = @user_id AND used_at IS NULL AND expires_at > @now AND user_token_enc IS NULL
RETURNING tenant_id;

-- name: GetOAuthPages :one
-- Dòng phiên Page (có token người dùng).
SELECT tenant_id, user_id, user_token_enc, user_token_kid, pages, expires_at
FROM channel.oauth_sessions
WHERE id = @id AND user_token_enc IS NOT NULL;

-- name: DeleteExpiredOAuthSessions :execrows
DELETE FROM channel.oauth_sessions
WHERE expires_at < @now;

-- name: DeleteOAuthSessionsByUserRef :execrows
-- Callback xóa dữ liệu của Meta (Task 179): xóa mọi phiên của người dùng app có băm này (tenant của tx).
DELETE FROM channel.oauth_sessions
WHERE fb_user_ref_hash = @user_ref_hash::text;
