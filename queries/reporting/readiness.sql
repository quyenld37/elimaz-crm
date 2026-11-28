-- Báo cáo sẵn sàng dữ liệu trước đêm mở (Task 269, M7-24). Chạy trong tenancy.WithTenant, chỉ đọc, chỉ qua view công bố
-- của module chủ (docs/views.md); chỉ trả số đếm.

-- name: ReadinessCatalog :one
-- Mẫu nháp và số mẫu nháp đủ điều kiện lên kệ; ảnh đang xử lý có mốc đổi trạng thái cũ hơn stuck_before.
SELECT (SELECT count(*) FROM catalog.product_readiness_v WHERE status = 'draft')::bigint AS draft_total,
       (SELECT count(*) FROM catalog.product_readiness_v WHERE status = 'draft' AND ready)::bigint AS draft_ready,
       (SELECT count(*) FROM catalog.images_processing_v WHERE since < sqlc.arg(stuck_before)::timestamptz)::bigint AS images_stuck;

-- name: ReadinessWarehouses :one
-- Kho đang hoạt động và số kho đó đã có dòng sổ tồn (đã mở sổ).
SELECT count(*)::bigint AS active_count,
       count(*) FILTER (WHERE EXISTS (
           SELECT 1 FROM inventory.stock_ledger_v AS l WHERE l.warehouse_id = w.warehouse_id
       ))::bigint AS with_ledger
FROM inventory.warehouses_v AS w
WHERE w.status = 'active';

-- name: ReadinessPages :one
-- Page Facebook: tổng; nối đạt (active, token kiểm sau checked_after, chỉ đọc); đủ ba cấu hình (trả lời bình luận,
-- chào, ẩn bình luận đang tắt).
SELECT count(*)::bigint AS total,
       count(*) FILTER (WHERE a.status = 'active' AND a.token_checked_at >= sqlc.arg(checked_after)::timestamptz
                          AND a.send_mode = 'read_only')::bigint AS connected,
       count(*) FILTER (WHERE
           EXISTS (SELECT 1 FROM conversation.automation_rules_v AS r
                   WHERE r.channel_account_id = a.id AND r.kind = 'auto_reply_comment')
           AND EXISTS (SELECT 1 FROM conversation.automation_rules_v AS r
                       WHERE r.channel_account_id = a.id AND r.kind = 'greeting')
           AND EXISTS (SELECT 1 FROM conversation.automation_rules_v AS r
                       WHERE r.channel_account_id = a.id AND r.kind = 'auto_hide' AND NOT r.enabled)
       )::bigint AS prepared
FROM channel.channel_accounts_v AS a
WHERE a.channel = 'facebook';

-- name: ReadinessConversation :one
-- Mẫu trả lời và nhãn hội thoại chưa xóa.
SELECT (SELECT count(*) FROM conversation.reply_templates_v)::bigint AS templates,
       (SELECT count(*) FROM conversation.tags_v)::bigint AS tags;

-- name: ReadinessSources :one
-- Nguồn đơn đang bật; nguồn key 'fallback' có không, đang bật không.
SELECT count(*) FILTER (WHERE active)::bigint AS active_count,
       count(*) FILTER (WHERE key = 'fallback')::bigint AS fallback_count,
       count(*) FILTER (WHERE key = 'fallback' AND active)::bigint AS fallback_active
FROM ord.traffic_sources_v;

-- name: ReadinessCarriers :one
-- Tài khoản hãng mode api; số đã đạt (active, kiểm sau checked_after).
SELECT count(*)::bigint AS total,
       count(*) FILTER (WHERE status = 'active' AND last_check_at >= sqlc.arg(checked_after)::timestamptz)::bigint AS tested
FROM fulfillment.carrier_accounts_v
WHERE mode = 'api';

-- name: ReadinessCoupons :one
-- Mã đang chạy tại now: bật, trong hạn, còn lượt.
SELECT count(*)::bigint AS running
FROM promotion.coupons_v
WHERE status = 'active' AND valid_from <= sqlc.arg(now)::timestamptz AND valid_to > sqlc.arg(now)::timestamptz
  AND (max_uses IS NULL OR used_count < max_uses);
