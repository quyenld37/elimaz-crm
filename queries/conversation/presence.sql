-- Ai đang xem, đang gõ, đang lên đơn (Task 163). Giờ "bây giờ" do Go truyền vào (một đồng hồ cho ghi, đọc, dọn).

-- name: ConversationPage :one
-- Page (tài khoản kênh) của một hội thoại, để kiểm quyền theo Page; không khóa.
SELECT channel_account_id
FROM conversation.conversations
WHERE id = @id;

-- name: RefreshPresence :execrows
-- Gia hạn dòng còn hạn có cùng trạng thái (nhịp tim không đổi gì): 1 dòng → không phát báo hiệu.
UPDATE conversation.presence
SET expires_at = @expires_at
WHERE conversation_id = @conversation_id
  AND user_id = @user_id
  AND state = @state
  AND expires_at > @now;

-- name: UpsertPresence :exec
-- Trạng thái mới xuất hiện, đổi, hoặc dòng cũ đã hết hạn: ghi đè trạng thái và hạn.
INSERT INTO conversation.presence (tenant_id, conversation_id, user_id, state, expires_at)
VALUES (@tenant_id, @conversation_id, @user_id, @state, @expires_at)
ON CONFLICT (tenant_id, conversation_id, user_id) DO UPDATE
SET state = EXCLUDED.state, expires_at = EXCLUDED.expires_at;

-- name: ListPresence :many
-- Người khác (trừ viewer) còn hạn trong hội thoại, theo tên hiển thị.
SELECT p.user_id, u.display_name, p.state
FROM conversation.presence p
JOIN identity.users_v u ON u.id = p.user_id
WHERE p.conversation_id = @conversation_id
  AND p.user_id <> @viewer_id
  AND p.expires_at > @now
ORDER BY u.display_name, p.user_id;

-- name: DeleteExpiredPresence :many
-- Xóa dòng hết hạn của tenant hiện tại; trả mỗi hội thoại bị đổi một lần kèm Page của nó (để phát báo hiệu).
WITH gone AS (
    DELETE FROM conversation.presence
    WHERE expires_at <= @now
    RETURNING conversation_id
)
SELECT c.id, c.channel_account_id
FROM conversation.conversations c
WHERE c.id IN (SELECT g.conversation_id FROM gone g)
ORDER BY c.id;
