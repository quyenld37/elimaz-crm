-- Comment vào (Task 154; kế hoạch M4-14). Thứ tự khóa nhóm 6 (ADR-46): posts → conversations → messages →
-- message_revisions; mọi câu chạy trong tx tenant của việc inbound.<nguồn> (hay của lệnh đọc bài, của API đọc).

-- name: InsertPostIfMissing :one
-- Bài của Page theo mã ngoài, chưa có thì tạo (kind unknown, chưa đọc); đã có → 0 dòng (pgx.ErrNoRows), bên gọi đọc
-- lại bằng FindPost. Trả mã bài vừa tạo.
INSERT INTO conversation.posts (tenant_id, channel_account_id, external_id)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @channel_account_id, @external_id)
ON CONFLICT (tenant_id, channel_account_id, external_id) DO NOTHING
RETURNING id;

-- name: FindPost :one
-- Mã bài của Page theo mã ngoài, không khóa.
SELECT id
FROM conversation.posts
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id;

-- name: UpsertCommentConversation :one
-- Hội thoại comment của (Page, bài, người): chưa có thì tạo với mốc at; có rồi thì khóa dòng (DO UPDATE) cho các câu
-- sau của cùng tx. inserted = true khi lần này vừa tạo.
INSERT INTO conversation.conversations (tenant_id, channel_account_id, customer_id, kind, external_thread_ref, post_id,
                                        last_message_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @channel_account_id, @customer_id, 'comment',
        @external_thread_ref, @post_id, @at)
ON CONFLICT (tenant_id, channel_account_id, kind, external_thread_ref) DO UPDATE SET updated_at = now()
RETURNING id, (xmax = 0)::boolean AS inserted;

-- name: FindCommentConversation :one
-- Hội thoại của comment mã ngoài external_id trên Page (comment cha của câu trả lời từ Page), không khóa.
SELECT conversation_id
FROM conversation.messages
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id
  AND kind = 'comment';

-- name: LockConversation :exec
-- Khóa dòng hội thoại (trước khi chèn tin vào nó, đúng thứ tự conversations → messages).
SELECT id
FROM conversation.conversations
WHERE id = @id
FOR NO KEY UPDATE;

-- name: InsertComment :one
-- Chèn một comment theo mã ngoài, kèm trạng thái của sự kiện tới trước (sửa, xóa, ẩn tới trước add); đã có mã đó → 0
-- dòng (pgx.ErrNoRows): bên gọi coi là trùng hoặc khóa tin rồi áp đổi.
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, external_id, direction, kind, origin,
                                   parent_external_id, post_id, body, attachments, sender_name, is_hidden, is_live,
                                   removed_at, edited_at, sent_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @conversation_id, @channel_account_id, @external_id,
        @direction, 'comment', @origin, sqlc.narg(parent_external_id)::text, @post_id, sqlc.narg(body)::text,
        @attachments, sqlc.narg(sender_name)::text, @is_hidden, @is_live, sqlc.narg(removed_at)::timestamptz,
        sqlc.narg(edited_at)::timestamptz, @sent_at)
ON CONFLICT (tenant_id, channel_account_id, external_id) DO NOTHING
RETURNING id;

-- name: LockComment :one
-- Khóa comment mã ngoài external_id của Page để sửa, xóa, ẩn; trả trạng thái hiện tại.
SELECT id, conversation_id, body, is_hidden, removed_at, edited_at
FROM conversation.messages
WHERE channel_account_id = @channel_account_id
  AND external_id = @external_id
  AND kind = 'comment'
FOR UPDATE;

-- name: UpdateCommentState :exec
-- Đặt trạng thái mới của comment đã khóa (nội dung, mốc sửa, mốc xóa, ẩn).
UPDATE conversation.messages
SET body       = sqlc.narg(body)::text,
    edited_at  = sqlc.narg(edited_at)::timestamptz,
    removed_at = sqlc.narg(removed_at)::timestamptz,
    is_hidden  = @is_hidden
WHERE id = @id;

-- name: InsertRevisionState :exec
-- Ghi một dòng lịch sử (chỉ ghi thêm): nội dung trước khi đổi, trạng thái ẩn, xóa sau khi đổi, nguồn đổi.
INSERT INTO conversation.message_revisions (tenant_id, message_id, body, is_hidden, removed, source)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @message_id, sqlc.narg(body)::text, @is_hidden,
        @removed, @source);

-- name: MessagePage :one
-- Page (tài khoản kênh) của tin id, không khóa; ghi chú nội bộ không có lịch sử nên không trả.
SELECT channel_account_id
FROM conversation.messages
WHERE id = @id
  AND kind <> 'note';

-- name: ListMessageRevisions :many
-- Lịch sử sửa, xóa, ẩn của tin theo thứ tự ghi (index message_revisions_message). Dòng của đường tin nhắn (Task 153)
-- không ghi cờ: coi như không ẩn, không xóa.
SELECT body, coalesce(is_hidden, false)::boolean AS is_hidden, coalesce(removed, false)::boolean AS removed, source,
       recorded_at
FROM conversation.message_revisions
WHERE message_id = @message_id
ORDER BY recorded_at, id;
