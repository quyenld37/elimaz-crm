-- Tệp đính kèm của tin vào và hồ sơ khách từ kênh (Task 155).

-- name: GetMessageAttachments :one
-- Tin của lệnh tải tệp: Page và danh sách đính kèm (có URL của mục còn pending).
SELECT id, conversation_id, channel_account_id, attachments
FROM conversation.messages
WHERE id = @id;

-- name: PatchPendingAttachment :one
-- Đổi mục đính kèm thứ idx của tin id khi mục đó còn pending: giữ index, type, stickerId, bỏ url, ghép patch (status,
-- fileId, downloadOnly). Một câu UPDATE: hai lệnh của cùng tin chờ khóa dòng rồi ghép trên bản mới (không mất mục của
-- nhau). Tin không còn hay mục không còn pending → 0 dòng.
UPDATE conversation.messages
SET attachments = jsonb_set(attachments, ARRAY[(@idx::int)::text], ((attachments -> @idx::int) - 'url') || @patch::jsonb)
WHERE id = @id
  AND (attachments -> @idx::int ->> 'status') = 'pending'
RETURNING conversation_id, channel_account_id;

-- name: GetConversationForProfile :one
-- Hội thoại của lệnh đọc hồ sơ: Page và mã người dùng trên kênh (PSID).
SELECT id, channel_account_id, kind, external_thread_ref
FROM conversation.conversations
WHERE id = @id;

-- name: MarkConversationCreatedCustomer :exec
-- Hội thoại id đã tạo hồ sơ khách của nó (CH08 "Khách tiềm năng mới", Task 501).
UPDATE conversation.conversations
SET created_customer = true
WHERE id = @id;

-- name: SetConversationAvatar :one
-- Ảnh đại diện của khách (từ hồ sơ kênh) cho hội thoại.
UPDATE conversation.conversations
SET avatar_file_id = @file_id, updated_at = now()
WHERE id = @id
RETURNING channel_account_id;

-- name: ConversationHasFile :one
-- Page của hội thoại id và có tin nào của hội thoại mang tệp file_id trong danh sách đính kèm không, hay file_id là
-- ảnh bài của hội thoại comment (Task 501).
SELECT c.channel_account_id,
       (EXISTS (SELECT 1
                FROM conversation.messages m
                WHERE m.conversation_id = c.id
                  AND m.attachments @> jsonb_build_array(jsonb_build_object('fileId', (@file_id::uuid)::text)))
        OR EXISTS (SELECT 1
                   FROM conversation.posts p
                   WHERE p.tenant_id = c.tenant_id AND p.id = c.post_id AND p.image_file_id = @file_id::uuid))::boolean AS has_file
FROM conversation.conversations c
WHERE c.id = @id;
