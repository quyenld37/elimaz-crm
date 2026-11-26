-- Dọn hội thoại quá hạn giữ (Task 164, Q17). Mọi câu chạy trong transaction tenant đã đặt app.retention = on; thứ tự
-- khóa: hội thoại (LockConversationsForReminders, id tăng) → tin, lịch sử sửa → hội thoại rỗng → platform.files.

-- name: EnableRetention :exec
-- Bật cờ dọn theo hạn giữ cho tới hết transaction (trigger chỉ ghi thêm của message_revisions, messages,
-- conversation_assignments cho DELETE khi cờ bật).
SELECT set_config('app.retention', 'on', true);

-- name: ExpiredMessageBatch :many
-- Lô tin gửi trước @cutoff (cũ nhất trước), tối đa @max_rows; không khóa (bên gọi khóa hội thoại của lô trước).
SELECT id, conversation_id
FROM conversation.messages
WHERE sent_at < @cutoff::timestamptz
ORDER BY sent_at, id
LIMIT @max_rows;

-- name: ExpiredEmptyConversations :many
-- Hội thoại không còn tin, tin cuối trước @cutoff, không có hẹn gọi lại hay nhật ký gọi gắn vào (giữ để không mất
-- dữ liệu của reminders, call_logs); tối đa @max_rows, không khóa.
SELECT c.id
FROM conversation.conversations AS c
WHERE c.last_message_at < @cutoff::timestamptz
  AND NOT EXISTS (SELECT 1 FROM conversation.messages AS m WHERE m.conversation_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM conversation.reminders AS r WHERE r.conversation_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM conversation.call_logs AS l WHERE l.conversation_id = c.id)
ORDER BY c.id
LIMIT @max_rows;

-- name: DeleteRevisionsOfMessages :execrows
DELETE FROM conversation.message_revisions
WHERE message_id = ANY(@ids::uuid[]);

-- name: DeleteClaimsOfMessages :execrows
-- Ghi nhận chốt từ comment live (Task 172) của các tin @ids (comment đã chốt hay tin xin địa chỉ): xóa trước tin vì khóa
-- ngoại không ON DELETE; đơn nháp giữ nguyên.
DELETE FROM conversation.live_comment_claims
WHERE message_id = ANY(@ids::uuid[]) OR ask_message_id = ANY(@ids::uuid[]);

-- name: DeleteExpiredMessages :many
-- Xóa các tin @ids còn trước @cutoff; trả danh sách đính kèm của từng tin đã xóa (tìm tệp cần đánh dấu xóa).
DELETE FROM conversation.messages
WHERE id = ANY(@ids::uuid[]) AND sent_at < @cutoff::timestamptz
RETURNING attachments;

-- name: DeleteEmptyConversations :many
-- Xóa các hội thoại @ids (bên gọi đã khóa) còn rỗng và quá hạn ở câu này (snapshot mới), kèm nhãn, lịch sử phân công
-- (trong một câu: khóa ngoại kiểm cuối câu); hiện diện tự xóa theo ON DELETE CASCADE. Trả mã đã xóa và ảnh đại diện
-- (avatar_file_id, có thể NULL) để đánh dấu xóa tệp không còn chỗ dùng (Task 496).
WITH gone AS (
    SELECT c.id
    FROM conversation.conversations AS c
    WHERE c.id = ANY(@ids::uuid[])
      AND c.last_message_at < @cutoff::timestamptz
      AND NOT EXISTS (SELECT 1 FROM conversation.messages AS m WHERE m.conversation_id = c.id)
      AND NOT EXISTS (SELECT 1 FROM conversation.reminders AS r WHERE r.conversation_id = c.id)
      AND NOT EXISTS (SELECT 1 FROM conversation.call_logs AS l WHERE l.conversation_id = c.id)
), links AS (
    DELETE FROM conversation.conversation_tag_links WHERE conversation_id IN (SELECT id FROM gone)
), assignments AS (
    DELETE FROM conversation.conversation_assignments WHERE conversation_id IN (SELECT id FROM gone)
)
DELETE FROM conversation.conversations
WHERE id IN (SELECT id FROM gone)
RETURNING id, avatar_file_id;

-- name: ReferencedFiles :many
-- Trong các tệp @ids, tệp còn được một tin (đính kèm), một mẫu trả lời (kể cả mẫu đã xóa mềm) hay một hội thoại còn
-- lại (ảnh đại diện, Task 496) dùng: không đánh dấu xóa. Ảnh đại diện kiểm bằng IN (một lần quét, hashed subplan).
SELECT f.id::uuid AS id
FROM unnest(@ids::uuid[]) AS f(id)
WHERE EXISTS (SELECT 1 FROM conversation.messages AS m
              WHERE m.attachments @> jsonb_build_array(jsonb_build_object('fileId', f.id::text)))
   OR EXISTS (SELECT 1 FROM conversation.reply_templates AS t
              WHERE t.attachment_file_id = f.id)
   OR f.id IN (SELECT c.avatar_file_id FROM conversation.conversations AS c WHERE c.avatar_file_id IS NOT NULL);
