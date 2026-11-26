-- Nhắn lại khách chưa trả lời (Task 168; kế hoạch M4-28, CH16). Mọi câu chạy trong tx tenant.

-- name: ListFollowUpCandidates :many
-- Hội thoại hộp thư cần nhắn lại trên các Page được xem: đang mở hay chờ, khách không chặn Page, khách (hồ sơ gốc)
-- không do_not_contact; tin người thật cuối của Page (nhân viên trong CRM hay trên Meta, không phải tin tự động, không
-- thất bại, không phải tin page_external sau tin tự động bị bỏ của cùng lần khách im) mới hơn tin khách cuối và cũ hơn @silent_before; cửa sổ 24 giờ còn mở tới sau @window_after. Cửa sổ đóng sớm
-- trước (index conversations_page_status lọc Page, trạng thái; tin cuối đọc theo messages_conversation). @rule_id khác
-- NULL (lịch): bỏ hội thoại mà tin nhân viên cuối đã có lượt của quy tắc đó (unique của automation_runs), để lô không
-- bị chiếm bởi hội thoại đã xử lý.
SELECT c.id, c.channel_account_id, c.customer_id, cu.full_name AS customer_name, c.last_customer_message_at,
       s.id AS staff_message_id, s.sent_at AS staff_message_at
FROM conversation.conversations AS c
JOIN customer.customers_v AS cu ON cu.id = c.customer_id
JOIN customer.customers_v AS root ON root.id = cu.root_id
JOIN LATERAL (
    SELECT m.id, m.sent_at
    FROM conversation.messages AS m
    WHERE m.tenant_id = c.tenant_id
      AND m.conversation_id = c.id
      AND m.direction = 'out'
      AND m.kind = 'message'
      AND m.origin IN ('staff', 'page_external')
      AND coalesce(m.send_status, 'sent') NOT IN ('failed', 'expired', 'discarded')
      -- Tin page_external sau một tin tự động bị bỏ vì không rõ đã gửi (auto_dropped) mà khách chưa nhắn lại có thể
      -- là echo hỏi bù của chính tin tự động (không mã CRM, không mid để ghép): không coi là tin người thật (Task 497).
      AND NOT (m.origin = 'page_external' AND EXISTS (
          SELECT 1
          FROM conversation.messages AS a
          WHERE a.tenant_id = c.tenant_id
            AND a.conversation_id = c.id
            AND a.direction = 'out'
            AND a.origin = 'automation'
            AND a.send_status = 'discarded'
            AND a.fail_reason = 'auto_dropped'
            AND a.sent_at > c.last_customer_message_at
            AND a.sent_at <= m.sent_at))
    ORDER BY m.sent_at DESC, m.id DESC
    LIMIT 1
) AS s ON true
WHERE c.kind = 'inbox'
  AND c.status IN ('open', 'pending')
  AND (@all_pages::boolean OR c.channel_account_id = ANY(@pages::uuid[]))
  AND NOT c.blocked_by_user
  AND NOT root.do_not_contact
  AND c.last_customer_message_at > @window_after::timestamptz
  AND s.sent_at > c.last_customer_message_at
  AND s.sent_at <= @silent_before::timestamptz
  AND (sqlc.narg(rule_id)::uuid IS NULL OR NOT EXISTS (
      SELECT 1 FROM conversation.automation_runs AS ar
      WHERE ar.tenant_id = c.tenant_id
        AND ar.rule_id = sqlc.narg(rule_id)::uuid
        AND ar.external_ref = 'fu:' || c.id::text || ':' || s.id::text))
ORDER BY c.last_customer_message_at, c.id
LIMIT @lim::int;

-- name: ListEnabledRulesOfKind :many
-- Quy tắc đang bật của loại kind trên mọi Page của tenant (lịch nhắn lại).
SELECT id, channel_account_id, kind, config, enabled, version, updated_at
FROM conversation.automation_rules
WHERE kind = @kind
  AND enabled
ORDER BY channel_account_id;

-- name: FollowUpTemplateUsable :one
-- Mẫu id còn sống và dùng được cho Page (mẫu chung hay mẫu riêng của Page).
SELECT EXISTS (
    SELECT 1 FROM conversation.reply_templates
    WHERE id = @id
      AND deleted_at IS NULL
      AND (channel_account_id IS NULL OR channel_account_id = @channel_account_id::uuid)
)::boolean AS usable;
