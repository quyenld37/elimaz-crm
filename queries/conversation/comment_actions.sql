-- Trả lời comment, nhắn riêng một lần, ẩn/hiện comment (Task 158; kế hoạch M4-18). Thứ tự khóa nhóm 6 (ADR-46):
-- conversations (nhiều hội thoại: theo id tăng dần) → messages → message_revisions; mọi câu chạy trong tx tenant (WriteTx
-- của API, transaction ghi kết quả của lệnh, transaction ngắn của Lookup).

-- name: GetCommentForAction :one
-- Comment id (không khóa): hội thoại, Page, mã ngoài, chiều, nguồn, bài, nội dung, trạng thái ẩn/xóa, mốc tạo trên kênh.
SELECT id, conversation_id, channel_account_id, external_id, direction, origin, post_id, body, is_hidden, removed_at,
       sent_at
FROM conversation.messages
WHERE id = @id
  AND kind = 'comment';

-- name: GetCommentByExternalID :one
-- Comment mã ngoài external_id của Page (không khóa): mốc tạo, mốc xóa (lệnh nhắn riêng hỏi lại chính sách).
SELECT id, conversation_id, sent_at, removed_at
FROM conversation.messages
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id
  AND kind = 'comment';

-- name: LockCommentByID :one
-- Khóa comment id để đổi trạng thái ẩn (như LockComment của đường vào: chỉ khóa dòng tin, không chạm hội thoại; bên
-- gọi đã khóa hội thoại trước khi cần, ADR-46). Dùng cho ẩn/hiện của nhân viên (Task 158) và tự ẩn (Task 165).
SELECT id, conversation_id, channel_account_id, body, is_hidden, removed_at, edited_at
FROM conversation.messages
WHERE id = @id
  AND kind = 'comment'
FOR UPDATE;

-- name: LatestPrivateReply :one
-- Tin riêng mới nhất đã "dùng lượt" nhắn riêng comment mã ngoài parent_external_id của Page: đang sống (queued, sent,
-- checking, in_doubt) hay failed vì Meta báo đã nhắn (private_reply_used); bỏ qua tin exclude_id (lệnh hỏi lại chính
-- sách cho chính tin của nó). Không có → pgx.ErrNoRows (chưa dùng).
SELECT id, sent_at
FROM conversation.messages
WHERE channel_account_id = @channel_account_id
  AND parent_external_id = @parent_external_id
  AND kind = 'private_reply'
  AND id IS DISTINCT FROM sqlc.narg(exclude_id)::uuid
  AND (send_status IN ('queued', 'sent', 'checking', 'in_doubt')
    OR (send_status = 'failed' AND fail_reason = 'private_reply_used'))
ORDER BY sent_at DESC, id DESC
LIMIT 1;

-- name: GetConversationThread :one
-- Hội thoại id (không khóa): Page, loại, khách, khóa luồng (comment: <bài>:<người comment>; hộp thư: PSID).
SELECT id, channel_account_id, kind, customer_id, external_thread_ref, last_customer_message_at
FROM conversation.conversations
WHERE id = @id;

-- name: FindInboxForCommenter :one
-- Hội thoại hộp thư của người comment trên Page: ưu tiên đúng khóa luồng thread_ref (PSID = mã người comment), không
-- có thì hội thoại hộp thư mới nhất của khách customer_id trên Page. Không khóa (bên gọi khóa theo id tăng dần).
SELECT id
FROM conversation.conversations
WHERE channel_account_id = @channel_account_id
  AND kind = 'inbox'
  AND (external_thread_ref = @thread_ref OR customer_id = @customer_id)
ORDER BY (external_thread_ref = @thread_ref) DESC, last_message_at DESC, id DESC
LIMIT 1;

-- name: InsertOutgoingAction :one
-- Câu trả lời comment (kind comment, trong hội thoại comment, kèm bài) hay tin riêng (kind private_reply, trong hội
-- thoại hộp thư) của nhân viên (origin staff) hay của quy tắc tự động (origin automation, Task 166), chờ lệnh
-- (queued); parent_external_id là mã comment được trả lời.
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, client_ref, direction, kind, origin,
                                   parent_external_id, post_id, body, sender_staff_id, send_status, sent_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, @channel_account_id, @client_ref,
        'out', @kind, @origin, @parent_external_id, sqlc.narg(post_id)::uuid, @body, @sender_staff_id, 'queued',
        @sent_at)
RETURNING id;

-- name: GetOutgoingAction :one
-- Câu trả lời comment hay tin riêng của một lệnh (không khóa): loại, comment cha, nội dung, trạng thái, mốc bấm gửi.
SELECT id, conversation_id, channel_account_id, client_ref, kind, parent_external_id, body, send_status, sent_at
FROM conversation.messages
WHERE id = @id;

-- name: SetMergeReplyFlag :exec
-- Bật/tắt cờ app.merge_reply của transaction (trigger messages_delete_guard, migration 0415): chỉ quanh câu
-- DeletePageReplyEcho.
SELECT set_config('app.merge_reply', @value::text, true);

-- name: DeletePageReplyEcho :execrows
-- Webhook feed của chính câu trả lời CRM đã chèn dòng page_external cùng mã comment: xóa dòng đó (giữ dòng của CRM).
-- Dòng đã có lịch sử sửa/ẩn không xóa (khóa ngoại của message_revisions; dòng CRM giữ không mã ngoài).
DELETE FROM conversation.messages AS m
WHERE m.channel_account_id = @channel_account_id
  AND m.external_id = @external_id
  AND m.origin = 'page_external'
  AND m.kind = 'comment'
  AND NOT EXISTS (SELECT 1 FROM conversation.message_revisions AS r WHERE r.message_id = m.id);

-- name: SetOwnExternalID :execrows
-- Tra lại thấy câu trả lời đã lên Page: ghi mã comment vào dòng của CRM khi dòng chưa có mã và mã chưa thuộc dòng khác;
-- dòng đã mang đúng mã đó (lần tra trước) cũng tính 1 dòng. 0 dòng: mã thuộc dòng khác (khớp nhầm câu trả lời cũ cùng
-- chữ), bên gọi không coi là đã gửi.
UPDATE conversation.messages AS m
SET external_id = @external_id
WHERE m.id = @id
  AND (m.external_id = @external_id
    OR (m.external_id IS NULL
      AND NOT EXISTS (SELECT 1
                      FROM conversation.messages AS o
                      WHERE o.channel_account_id = m.channel_account_id
                        AND o.external_id = @external_id)));

-- name: RekeyInboxConversation :execrows
-- Tin riêng tới PSID khác mã người comment: hội thoại hộp thư vừa dựng theo mã người comment (chưa có tin khách) đổi
-- khóa luồng sang PSID thật để lần gửi sau tới đúng người; PSID đã có hội thoại khác thì giữ nguyên.
UPDATE conversation.conversations AS c
SET external_thread_ref = @psid,
    updated_at          = now()
WHERE c.id = @id
  AND c.external_thread_ref = @old_ref
  AND c.last_customer_message_at IS NULL
  AND NOT EXISTS (SELECT 1
                  FROM conversation.conversations AS o
                  WHERE o.channel_account_id = c.channel_account_id
                    AND o.kind = 'inbox'
                    AND o.external_thread_ref = @psid);

-- name: InsertSystemNote :exec
-- Tin hệ thống trong hội thoại (không gửi ra ngoài): gợi ý cho nhân viên, không có dữ liệu cá nhân.
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, direction, kind, origin, body, sent_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, @channel_account_id, 'out', 'system',
        'system', @body, @sent_at);

-- name: CountCommentEdits :one
-- Số lần khách sửa comment (dòng lịch sử nguồn customer có nội dung cũ).
SELECT count(*)::int AS edits
FROM conversation.message_revisions
WHERE message_id = @message_id
  AND source = 'customer'
  AND body IS NOT NULL;

-- name: GetMessageCommandJob :one
-- Lệnh vừa gắn vào tin id (tin tự động, Task 166: mã lệnh ghi vào lượt quy tắc).
SELECT command_job_id::uuid
FROM conversation.messages
WHERE id = @id;
