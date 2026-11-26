-- Nhắc việc trên hội thoại (Task 169; 01 mục 6 "nhắc tôi lúc…"). Chạy trong tenancy.WithTenant. note là dữ liệu cá
-- nhân: chỉ trả cho chính người được nhắc.

-- name: ReminderConversation :one
-- Page và khách của hội thoại mà nhắc việc, cuộc gọi gắn vào (không khóa).
SELECT id, channel_account_id, customer_id
FROM conversation.conversations
WHERE id = @id;

-- name: InsertReminder :one
INSERT INTO conversation.reminders (tenant_id, conversation_id, user_id, remind_at, note, source, created_by)
VALUES (current_setting('app.tenant_id')::uuid, @conversation_id, @user_id, @remind_at, sqlc.narg(note), @source,
        @created_by)
RETURNING id, conversation_id, user_id, remind_at, note, status, source, fired_at, version, created_at;

-- name: GetReminder :one
SELECT r.id, r.conversation_id, r.user_id, r.remind_at, r.note, r.status, r.source, r.fired_at, r.version,
       r.created_at, c.channel_account_id
FROM conversation.reminders AS r
JOIN conversation.conversations AS c ON c.tenant_id = r.tenant_id AND c.id = r.conversation_id
WHERE r.id = @id;

-- name: CloseReminder :one
-- Cập nhật có điều kiện: chỉ nhắc còn pending hay đã bắn mới đóng được (done, cancelled); 0 dòng → đã đóng trước.
UPDATE conversation.reminders
SET status = @status, version = version + 1
WHERE id = @id
  AND status IN ('pending', 'fired')
RETURNING id, conversation_id, user_id, remind_at, note, status, source, fired_at, version, created_at;

-- name: ListUserReminders :many
-- Nhắc của một người theo trạng thái, sớm nhất trước (index reminders_user_status).
SELECT id, conversation_id, user_id, remind_at, note, status, source, fired_at, version, created_at
FROM conversation.reminders
WHERE user_id = @user_id
  AND status = @status
ORDER BY remind_at, id
LIMIT @lim::int;

-- name: DueReminderConversations :many
-- Hội thoại của các nhắc tới hạn (không khóa nhắc; index reminders_due), để khóa hội thoại trước theo ADR-46.
SELECT DISTINCT conversation_id
FROM (SELECT conversation_id
      FROM conversation.reminders
      WHERE status = 'pending'
        AND remind_at <= @now::timestamptz
      ORDER BY remind_at, id
      LIMIT @lim::int) AS due;

-- name: LockConversationsForReminders :many
-- Khóa hội thoại theo id tăng dần (nhóm 6 ADR-46: conversations trước reminders).
SELECT id
FROM conversation.conversations
WHERE id = ANY(@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: FireDueReminders :many
-- Bắn các nhắc tới hạn của những hội thoại đã khóa; điều kiện status = 'pending' nên hai lần chạy không bắn hai lần.
UPDATE conversation.reminders AS r
SET status = 'fired', fired_at = @now::timestamptz, version = r.version + 1
FROM conversation.conversations AS c
WHERE c.tenant_id = r.tenant_id
  AND c.id = r.conversation_id
  AND r.conversation_id = ANY(@conversation_ids::uuid[])
  AND r.status = 'pending'
  AND r.remind_at <= @now::timestamptz
RETURNING r.id, r.conversation_id, c.channel_account_id;

-- name: ReopenConversations :many
-- Hội thoại đang chờ hay đã xong mở lại khi nhắc tới hạn.
UPDATE conversation.conversations
SET status = 'open', version = version + 1, updated_at = @now::timestamptz
WHERE id = ANY(@ids::uuid[])
  AND status IN ('pending', 'done')
RETURNING id, channel_account_id;
