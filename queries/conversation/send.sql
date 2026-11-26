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
-- Tin nhân viên gửi, chờ lệnh gửi (queued); sent_at là lúc bấm gửi (đồng hồ của API) tới khi kênh xác nhận. origin
-- staff; nhân viên gửi lại tin tự động (Task 497) thì giữ origin automation và nút trả lời nhanh của tin cũ.
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, client_ref, direction, kind, origin,
                                   body, attachments, quick_replies, sender_staff_id, send_status, sent_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, @channel_account_id, @client_ref,
        'out', 'message', @origin, sqlc.narg(body)::text, @attachments, sqlc.narg(quick_replies)::jsonb,
        @sender_staff_id, 'queued', @sent_at)
RETURNING id;

-- name: InsertAutomatedMessage :one
-- Tin do quy tắc tự động gửi (tin chào Task 167, nhắn lại Task 168), chờ lệnh command.fb.auto_send (queued); không có
-- người gửi; quick_replies NULL khi không có nút.
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, client_ref, direction, kind, origin,
                                   body, attachments, quick_replies, send_status, sent_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, @channel_account_id, @client_ref,
        'out', 'message', 'automation', @body::text, '[]', sqlc.narg(quick_replies)::jsonb, 'queued', @sent_at)
RETURNING id;

-- name: BumpConversationAutomated :exec
-- Tin tự động vừa chèn: mốc tin cuối, đoạn trích; không đổi mốc tin nhân viên (tin tự động không phải nhân viên trả
-- lời: hội thoại vẫn là khách chờ nhân viên), chưa đọc, trạng thái.
UPDATE conversation.conversations
SET last_message_at = GREATEST(last_message_at, @at),
    last_snippet    = CASE WHEN @at >= last_message_at THEN sqlc.narg(snippet)::text ELSE last_snippet END,
    updated_at      = now()
WHERE id = @id;

-- name: SetMessageCommandJob :exec
-- Gắn mã lệnh gửi vào tin vừa chèn.
UPDATE conversation.messages
SET command_job_id = @command_job_id
WHERE id = @id;

-- name: GetOutgoingMessage :one
-- Tin của một lệnh gửi (không khóa): trạng thái, nội dung, ảnh, mốc bấm gửi.
SELECT id, conversation_id, channel_account_id, client_ref, origin, body, attachments, quick_replies, send_status,
       fail_reason, sent_at
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

-- Không rõ đã gửi, chưa gửi được: chờ echo, gửi lại, bỏ (Task 157; kế hoạch M4-17).

-- name: GetSendLookup :one
-- Tra lại lệnh gửi in_doubt (Looker): tin đã có mã ngoài (echo đã khớp) hay đã sent thì kênh đã nhận.
SELECT (external_id IS NOT NULL OR send_status = 'sent')::boolean AS confirmed
FROM conversation.messages
WHERE id = @id;

-- name: LockRecoverableMessage :one
-- Khóa tin nhân viên/tự động gửi ra (sau khi đã khóa hội thoại) để gửi lại hay bỏ: tin thường, câu trả lời comment,
-- tin riêng (Task 497); trạng thái, lý do, nội dung, ảnh, nút trả lời nhanh, nguồn, loại, comment cha, bài, mã lệnh.
SELECT id, conversation_id, channel_account_id, origin, kind, parent_external_id, post_id, body, attachments,
       quick_replies, send_status, fail_reason, sent_at, command_job_id
FROM conversation.messages
WHERE id = @id
  AND direction = 'out'
  AND kind IN ('message', 'comment', 'private_reply')
  AND origin IN ('staff', 'automation')
FOR UPDATE;

-- name: GetRecoverableMessageConversation :one
-- Hội thoại của tin cần gửi lại hay bỏ (không khóa; khóa hội thoại trước, rồi tin).
SELECT conversation_id
FROM conversation.messages
WHERE id = @id
  AND direction = 'out'
  AND kind IN ('message', 'comment', 'private_reply')
  AND origin IN ('staff', 'automation');

-- name: MarkMessageDiscarded :execrows
-- Nhân viên gửi lại hay bỏ: tin còn ở trạng thái from (đã kiểm khi khóa) thành discarded; 0 dòng = đã đổi.
UPDATE conversation.messages
SET send_status         = 'discarded',
    send_checking_since = NULL
WHERE id = @id
  AND send_status = @from_status;

-- name: LockConversationsDueReveal :many
-- Lịch send_reveal: khóa (mã tăng dần, ADR-46 nhóm 6) các hội thoại có tin checking từ trước mốc before.
SELECT c.id
FROM conversation.conversations AS c
WHERE c.id IN (SELECT m.conversation_id
               FROM conversation.messages AS m
               WHERE m.send_status = 'checking'
                 AND m.send_checking_since < @before
               LIMIT @lim)
ORDER BY c.id
FOR UPDATE OF c;

-- name: RevealInDoubtMessages :many
-- Tin checking quá 60 giây chưa thấy echo của các hội thoại đã khóa thành in_doubt ("không rõ đã gửi chưa").
UPDATE conversation.messages
SET send_status = 'in_doubt'
WHERE conversation_id = ANY (@conversation_ids::uuid[])
  AND send_status = 'checking'
  AND send_checking_since < @before
RETURNING conversation_id, channel_account_id;

-- name: RelabelExternalEcho :execrows
-- Echo đọc qua hỏi bù (không app id, không metadata → page_external) mang đúng mã ngoài kênh vừa trả cho lệnh gửi: là
-- echo của chính tin đó, đổi nhãn thành own_echo_unmatched để DeleteUnmatchedEcho gộp (tệp task-157, Quyết định 5).
UPDATE conversation.messages
SET origin = 'own_echo_unmatched'
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id
  AND origin = 'page_external'
  AND direction = 'out';
