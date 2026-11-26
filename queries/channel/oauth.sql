-- Phiên đăng nhập Facebook trong tab (kế hoạch M4, Task 149). Hai loại dòng: dòng "start" (user_token_enc NULL, hạn 15
-- phút, state_hash là băm của state gửi cho Meta) và dòng "phiên Page" do exchange chèn (token người dùng mã hóa, danh
-- sách Page, hạn 30 phút). Mọi câu chạy trong tx tenant của bên gọi (RLS).

-- name: InsertOAuthSession :exec
INSERT INTO channel.oauth_sessions (id, tenant_id, user_id, state_hash, user_token_enc, user_token_kid, pages, expires_at)
VALUES (@id, current_setting('app.tenant_id')::uuid, @user_id, @state_hash, sqlc.narg('user_token_enc')::bytea,
    sqlc.narg('user_token_kid')::text, @pages, @expires_at);

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
