-- Ghi chú nội bộ trong hội thoại (Task 161; CH07): tin kind note, không trạng thái gửi, không mã ngoài, không lệnh
-- (CHECK messages_note_internal giữ ở database). body là dữ liệu cá nhân: không vào log, audit, báo hiệu.

-- name: InsertNote :one
INSERT INTO conversation.messages (tenant_id, conversation_id, channel_account_id, direction, kind, origin, body,
                                   sender_staff_id, sender_name, sent_at)
VALUES (current_setting('app.tenant_id')::uuid, @conversation_id, @channel_account_id, 'out', 'note', 'staff', @body,
        @sender_staff_id, sqlc.narg(sender_name), @sent_at)
RETURNING id, sent_at;
