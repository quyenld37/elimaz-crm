-- Đường tin vào (Task 153; kế hoạch M4-13). Thứ tự khóa nhóm 6 (ADR-46): conversations → messages →
-- message_revisions; mọi câu chạy trong tx tenant của việc inbound.<nguồn>.

-- name: UpsertInboxConversation :one
-- Hội thoại inbox của (Page, PSID): chưa có thì tạo với mốc at; có rồi thì khóa dòng (DO UPDATE) để các câu sau của
-- cùng tx cập nhật. inserted = true khi lần này vừa tạo.
INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind, external_thread_ref,
                                        last_message_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @channel_account_id, @customer_id, 'inbox',
        @external_thread_ref, @at)
ON CONFLICT (tenant_id, channel_account_id, kind, external_thread_ref) DO UPDATE SET updated_at = now()
RETURNING id, (xmax = 0)::boolean AS inserted;

-- name: FindInboxConversation :one
-- Mã hội thoại inbox của (Page, PSID), không khóa.
SELECT id
FROM conversation.conversations
WHERE channel_account_id = @channel_account_id
  AND kind = 'inbox'
  AND external_thread_ref = @external_thread_ref;

-- name: InsertInboundMessage :one
-- Chèn một tin theo mã ngoài của kênh; đã có mã đó → 0 dòng (pgx.ErrNoRows), bên gọi coi là tin trùng.
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id, direction, kind, origin,
                                   body, attachments, quick_reply_payload, sent_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, @channel_account_id, @external_id,
        @direction, @kind, @origin, sqlc.narg(body)::text, @attachments, sqlc.narg(quick_reply_payload)::text, @sent_at)
ON CONFLICT (tenant_id, channel_account_id, external_id) DO NOTHING
RETURNING id;

-- name: BumpConversationInbound :one
-- Tin khách vừa chèn: mốc tin cuối, mốc tin khách không lùi; chưa đọc + 1; đoạn trích chỉ đổi khi tin mới hơn (hay
-- bằng) tin cuối; done/pending → open (version + 1 khi đổi trạng thái). Trả trạng thái mới.
UPDATE conversation.conversations
SET last_message_at          = GREATEST(last_message_at, @at),
    last_customer_message_at = GREATEST(last_customer_message_at, @at),
    unread_count             = unread_count + 1,
    last_snippet             = CASE WHEN @at >= last_message_at THEN sqlc.narg(snippet)::text ELSE last_snippet END,
    status                   = CASE WHEN status IN ('done', 'pending') THEN 'open' ELSE status END,
    version                  = version + CASE WHEN status IN ('done', 'pending') THEN 1 ELSE 0 END,
    updated_at               = now()
WHERE id = @id
RETURNING status;

-- name: BumpConversationOutbound :exec
-- Tin của Page gửi ngoài CRM vừa chèn: mốc tin cuối, mốc tin nhân viên không lùi; đoạn trích như tin vào; không đổi
-- chưa đọc, trạng thái.
UPDATE conversation.conversations
SET last_message_at       = GREATEST(last_message_at, @at),
    last_staff_message_at = GREATEST(last_staff_message_at, @at),
    last_snippet          = CASE WHEN @at >= last_message_at THEN sqlc.narg(snippet)::text ELSE last_snippet END,
    updated_at            = now()
WHERE id = @id;

-- name: ConfirmOwnEcho :one
-- Echo của CRM: tin client_ref chưa sent (queued, checking, in_doubt; cả failed, expired, discarded — sự thật thắng,
-- Task 157) thành sent với mã ngoài mid, sent_at = mốc của kênh. Không đổi khi mid đã thuộc dòng khác (giữ unique,
-- không vòng thử lại). Trả mã tin, mã hội thoại, trạng thái trước đó.
WITH prev AS (SELECT p.id, p.send_status
              FROM conversation.messages AS p
              WHERE p.client_ref = @client_ref
                AND p.channel_account_id = @channel_account_id
                AND p.send_status IN ('queued', 'checking', 'in_doubt', 'failed', 'expired', 'discarded')
              FOR UPDATE)
UPDATE conversation.messages AS m
SET external_id         = @external_id,
    send_status         = 'sent',
    sent_at             = @sent_at,
    fail_reason         = NULL,
    send_checking_since = NULL
FROM prev
WHERE m.id = prev.id
  AND NOT EXISTS (SELECT 1
                  FROM conversation.messages AS o
                  WHERE o.channel_account_id = @channel_account_id
                    AND o.external_id = @external_id)
RETURNING m.id, m.conversation_id, prev.send_status::text AS prev_status;

-- name: MarkDelivered :execrows
-- Đã nhận tới mốc wm: tin ra (kind message) của hội thoại, gửi từ since tới wm, chưa có delivered_at.
UPDATE conversation.messages
SET delivered_at = @wm
WHERE conversation_id = @conversation_id
  AND direction = 'out'
  AND kind = 'message'
  AND sent_at <= @wm
  AND sent_at > @since
  AND delivered_at IS NULL;

-- name: MarkRead :execrows
-- Đã xem tới mốc wm: như MarkDelivered với read_at; đã xem thì đã nhận (delivered_at còn trống được đặt cùng mốc).
UPDATE conversation.messages
SET read_at      = @wm,
    delivered_at = coalesce(delivered_at, @wm)
WHERE conversation_id = @conversation_id
  AND direction = 'out'
  AND kind = 'message'
  AND sent_at <= @wm
  AND sent_at > @since
  AND read_at IS NULL;

-- name: SetCustomerReaction :one
-- Khách thả (react) hay bỏ (unreact) cảm xúc trên tin mid: khóa "customer" trong reactions.
UPDATE conversation.messages
SET reactions = CASE
                    WHEN @react::boolean THEN jsonb_set(coalesce(reactions, '{}'::jsonb), '{customer}',
                                                        to_jsonb(@reaction::text))
                    ELSE coalesce(reactions, '{}'::jsonb) - 'customer'
                END
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id
RETURNING conversation_id;

-- name: LockMessageByExternal :one
-- Khóa tin mid của Page để sửa; trả nội dung cũ, hướng, mốc sửa gần nhất.
SELECT id, conversation_id, body, direction, edited_at
FROM conversation.messages
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id
FOR UPDATE;

-- name: InsertMessageRevision :exec
-- Ghi nội dung cũ của tin trước khi sửa (chỉ ghi thêm).
INSERT INTO conversation.message_revisions (tenant_id, message_id, body, source)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @message_id, sqlc.narg(body)::text, @source);

-- name: ApplyMessageEdit :exec
-- Đổi nội dung tin đã khóa thành bản sửa lúc edited_at.
UPDATE conversation.messages
SET body      = sqlc.narg(body)::text,
    edited_at = @edited_at
WHERE id = @id;
