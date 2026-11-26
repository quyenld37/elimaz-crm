-- Tài khoản kênh (kế hoạch M4, Task 145). Mọi câu chạy trong tx tenant của bên gọi (RLS). Không câu đọc nào trả
-- credential_enc ngoài GetCredentials.

-- name: GetAccount :one
SELECT id, tenant_id, seller_id, channel, external_id, name, token_kind, status, status_reason, send_mode,
    human_agent_enabled, subscribed_fields, token_checked_at, last_webhook_at, last_feed_at, blocked_until, version
FROM channel.channel_accounts
WHERE id = @id;

-- name: LockAccount :one
-- Khóa dòng trước khi so version (ADR-46 nhóm 3, bảng đầu nhóm).
SELECT id, tenant_id, seller_id, channel, external_id, name, token_kind, status, status_reason, send_mode,
    human_agent_enabled, subscribed_fields, token_checked_at, last_webhook_at, last_feed_at, blocked_until, version
FROM channel.channel_accounts
WHERE id = @id
FOR UPDATE;

-- name: GetAccountByExternal :one
SELECT id, tenant_id, seller_id, channel, external_id, name, token_kind, status, status_reason, send_mode,
    human_agent_enabled, subscribed_fields, token_checked_at, last_webhook_at, last_feed_at, blocked_until, version
FROM channel.channel_accounts
WHERE channel = @channel AND external_id = @external_id;

-- name: ListAccounts :many
-- all_accounts = true: mọi tài khoản; không thì chỉ các id trong ids (rỗng → không dòng nào).
SELECT id, tenant_id, seller_id, channel, external_id, name, token_kind, status, status_reason, send_mode,
    human_agent_enabled, subscribed_fields, token_checked_at, last_webhook_at, last_feed_at, blocked_until, version
FROM channel.channel_accounts
WHERE @all_accounts::boolean OR id = ANY(@ids::uuid[])
ORDER BY name, id;

-- name: GetCredentials :one
SELECT tenant_id, credential_enc, credential_kid
FROM channel.channel_accounts
WHERE id = @id;

-- name: SetCredentials :execrows
UPDATE channel.channel_accounts
SET credential_enc = @credential_enc, credential_kid = @credential_kid, token_kind = @token_kind, updated_at = now()
WHERE id = @id;

-- name: ClearCredentials :execrows
UPDATE channel.channel_accounts
SET credential_enc = NULL, credential_kid = NULL, token_kind = NULL, updated_at = now()
WHERE id = @id;

-- name: SetSendMode :one
UPDATE channel.channel_accounts
SET send_mode = @send_mode, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING version;

-- name: SetHumanAgent :one
UPDATE channel.channel_accounts
SET human_agent_enabled = @human_agent_enabled, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING version;

-- name: MarkStatus :execrows
-- Chỉ ghi khi trạng thái hay lý do khác hiện tại (changed = 1 dòng).
UPDATE channel.channel_accounts
SET status = @status, status_reason = sqlc.narg('status_reason')::text, updated_at = now()
WHERE id = @id
  AND (status <> @status OR status_reason IS DISTINCT FROM sqlc.narg('status_reason')::text);

-- name: NoteBlockedUntil :execrows
-- Chỉ dời mốc chặn gửi về sau, không rút ngắn (GREATEST bỏ qua NULL).
UPDATE channel.channel_accounts
SET blocked_until = GREATEST(blocked_until, @until::timestamptz), updated_at = now()
WHERE id = @id;

-- name: GetBlockedUntil :one
SELECT blocked_until
FROM channel.channel_accounts
WHERE id = @id;

-- name: NoteInbound :execrows
-- Ghi mốc webhook (và mốc feed khi feed) tối đa mỗi phút một lần mỗi cột: một câu có điều kiện, không đọc ra Go rồi
-- ghi lại; mốc cũ hơn không đè mốc mới. cutoff = at - 1 phút (Go tính).
UPDATE channel.channel_accounts
SET last_webhook_at = CASE WHEN coalesce(last_webhook_at < sqlc.arg(cutoff)::timestamptz, true)
        THEN sqlc.arg(at)::timestamptz ELSE last_webhook_at END,
    last_feed_at = CASE WHEN sqlc.arg(feed)::boolean AND coalesce(last_feed_at < sqlc.arg(cutoff)::timestamptz, true)
        THEN sqlc.arg(at)::timestamptz ELSE last_feed_at END
WHERE id = sqlc.arg(id)
  AND (coalesce(last_webhook_at < sqlc.arg(cutoff)::timestamptz, true)
       OR (sqlc.arg(feed)::boolean AND coalesce(last_feed_at < sqlc.arg(cutoff)::timestamptz, true)));

-- name: UpsertAccount :one
-- Kết nối Page (Task 149): mới → read_only, active (mặc định của bảng); đã có → đổi tên, về active, xóa lý do; giữ
-- send_mode, seller_id, version. inserted = true khi là dòng mới. Khóa dòng (ADR-46 nhóm 3).
INSERT INTO channel.channel_accounts (tenant_id, seller_id, channel, external_id, name)
VALUES (@tenant_id, @seller_id, @channel, @external_id, @name)
ON CONFLICT (tenant_id, channel, external_id) DO UPDATE
SET name = EXCLUDED.name, status = 'active', status_reason = NULL, updated_at = now()
RETURNING id, (xmax = 0) AS inserted;

-- name: SetSubscribedFields :execrows
UPDATE channel.channel_accounts
SET subscribed_fields = @subscribed_fields, updated_at = now()
WHERE id = @id;

-- name: SetTokenChecked :execrows
-- Mốc kiểm sức khỏe gần nhất (Task 150); không tăng version.
UPDATE channel.channel_accounts
SET token_checked_at = @checked_at, updated_at = now()
WHERE id = @id;
