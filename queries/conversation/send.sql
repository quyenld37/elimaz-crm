-- Gửi tin của nhân viên (Task 156; kế hoạch M4-16). Thứ tự khóa nhóm 6 (ADR-46): conversations → messages; mọi câu
-- chạy trong tx tenant (WriteTx của API, transaction ghi kết quả của lệnh command.fb.send_message).

-- name: LockConversationForSend :one
-- Khóa hội thoại trước khi chèn tin gửi hay đổi trạng thái tin của nó; trả phần SendPolicy cần.
SELECT id, channel_account_id, kind, external_thread_ref, last_customer_message_at, blocked_by_user
FROM conversation.conversations
WHERE id = @id
FOR UPDATE;

-- name: GetConversationForSend :one
-- Như LockConversationForSend nhưng không khóa (lệnh gửi đọc lại trước khi gọi kênh).
SELECT id, channel_account_id, kind, external_thread_ref, last_customer_message_at, blocked_by_user
FROM conversation.conversations
WHERE id = @id;

-- name: InsertOutgoingMessage :one
-- Tin nhân viên gửi, chờ lệnh gửi (queued); sent_at là lúc bấm gửi (đồng hồ của API) tới khi kênh xác nhận.
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, client_ref, direction, kind, origin,
                                   body, attachments, sender_staff_id, send_status, sent_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, @channel_account_id, @client_ref,
        'out', 'message', 'staff', sqlc.narg(body)::text, @attachments, @sender_staff_id, 'queued', @sent_at)
RETURNING id;

-- name: SetMessageCommandJob :exec
-- Gắn mã lệnh gửi vào tin vừa chèn.
UPDATE conversation.messages
SET command_job_id = @command_job_id
WHERE id = @id;

-- name: GetOutgoingMessage :one
-- Tin của một lệnh gửi (không khóa): trạng thái, nội dung, ảnh, mốc bấm gửi.
SELECT id, conversation_id, channel_account_id, client_ref, origin, body, attachments, send_status, fail_reason, sent_at
FROM conversation.messages
WHERE id = @id;

-- name: LockOutgoingMessage :one
-- Khóa tin của lệnh gửi (sau khi đã khóa hội thoại của nó).
SELECT id, conversation_id, channel_account_id, send_status, fail_reason
FROM conversation.messages
WHERE id = @id
FOR UPDATE;

-- name: NoteSendRejection :execrows
-- Lệnh gửi biết lý do bị từ chối hay chưa gửi được (chính sách, token, kênh bận) trước khi Runner ghi kết quả: ghi mã
-- lý do khi tin còn queued; OnTerminal giữ mã đó khi đổi tin sang failed, expired (tệp task-156, Quyết định 3).
UPDATE conversation.messages
SET fail_reason = @fail_reason
WHERE id = @id
  AND send_status = 'queued';

-- name: DeleteUnmatchedEcho :many
-- Echo của CRM không có metadata đã tới trước khi lệnh gửi ghi xong (Q23): xóa dòng own_echo_unmatched cùng mid,
-- trả mốc của kênh, đã nhận, đã xem, cảm xúc để chép sang tin của mình.
DELETE FROM conversation.messages
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id
  AND origin = 'own_echo_unmatched'
RETURNING sent_at, delivered_at, read_at, reactions;

-- name: MarkOwnMessageSent :execrows
-- Kênh xác nhận đã gửi: tin chưa sent (chờ, kiểm tra, không rõ, quá hạn, bỏ — sự thật thắng) thành sent với mã ngoài
-- mid khi mid chưa thuộc dòng khác (giữ unique), mốc của kênh, đã nhận, đã xem, cảm xúc (giữ cái đã có).
UPDATE conversation.messages AS m
SET send_status         = 'sent',
    external_id         = CASE
                              WHEN sqlc.narg(external_id)::text IS NULL OR EXISTS (SELECT 1
                                                                             FROM conversation.messages AS o
                                                                             WHERE o.channel_account_id = m.channel_account_id
                                                                               AND o.external_id = sqlc.narg(external_id)::text
                                                                               AND o.id <> m.id)
                                  THEN m.external_id
                              ELSE sqlc.narg(external_id)::text
                          END,
    sent_at             = @sent_at,
    delivered_at        = coalesce(m.delivered_at, sqlc.narg(delivered_at)::timestamptz),
    read_at             = coalesce(m.read_at, sqlc.narg(read_at)::timestamptz),
    reactions           = coalesce(m.reactions, sqlc.narg(reactions)::jsonb),
    fail_reason         = NULL,
    send_checking_since = NULL
WHERE m.id = @id
  AND m.send_status <> 'sent';

-- name: SettleOutgoingMessage :execrows
-- Lệnh gửi kết thúc không thành (dead → failed, expired → expired, in_doubt → checking): chỉ đổi tin còn queued; lý
-- do rỗng thì giữ mã lý do lệnh đã ghi (NoteSendRejection).
UPDATE conversation.messages
SET send_status         = @send_status,
    fail_reason         = coalesce(sqlc.narg(fail_reason)::text, fail_reason),
    send_checking_since = sqlc.narg(send_checking_since)::timestamptz
WHERE id = @id
  AND send_status = 'queued';

-- name: MarkConversationBlocked :exec
-- Kênh báo khách đã chặn Page: lần gửi sau bị SendPolicy từ chối ngay ở API.
UPDATE conversation.conversations
SET blocked_by_user = true,
    updated_at      = now()
WHERE id = @id;
