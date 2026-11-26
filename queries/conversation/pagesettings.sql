-- Cài đặt hộp chat theo Page (Task 160): một dòng mỗi tài khoản kênh; chưa có dòng = mặc định, version 0.

-- name: GetPageSettings :one
SELECT channel_account_id, round_robin_enabled, round_robin_user_ids, round_robin_last_user_id, ask_address_template_id,
       inbox_traffic_source_id, comment_traffic_source_id, live_slots, comment_poll_mode, automation_max_age_minutes,
       version
FROM conversation.page_settings
WHERE channel_account_id = @channel_account_id;

-- name: LockPageSettings :one
SELECT channel_account_id, round_robin_enabled, round_robin_user_ids, round_robin_last_user_id, ask_address_template_id,
       inbox_traffic_source_id, comment_traffic_source_id, live_slots, comment_poll_mode, automation_max_age_minutes,
       version
FROM conversation.page_settings
WHERE channel_account_id = @channel_account_id
FOR UPDATE;

-- name: SavePageSettings :execrows
-- Lưu cài đặt: chưa có dòng → chèn (version 1); có → sửa khi version = @expected_version (version + 1). 0 dòng: dòng
-- vừa được người khác chèn hay sửa (version_conflict).
INSERT INTO conversation.page_settings (tenant_id, channel_account_id, round_robin_enabled, round_robin_user_ids,
                                        ask_address_template_id, inbox_traffic_source_id, comment_traffic_source_id,
                                        live_slots, comment_poll_mode, automation_max_age_minutes)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @channel_account_id, @round_robin_enabled,
        @round_robin_user_ids::uuid[], sqlc.narg(ask_address_template_id)::uuid,
        sqlc.narg(inbox_traffic_source_id)::uuid, sqlc.narg(comment_traffic_source_id)::uuid, @live_slots::jsonb,
        @comment_poll_mode, @automation_max_age_minutes)
ON CONFLICT (tenant_id, channel_account_id) DO UPDATE
SET round_robin_enabled        = EXCLUDED.round_robin_enabled,
    round_robin_user_ids       = EXCLUDED.round_robin_user_ids,
    ask_address_template_id    = EXCLUDED.ask_address_template_id,
    inbox_traffic_source_id    = EXCLUDED.inbox_traffic_source_id,
    comment_traffic_source_id  = EXCLUDED.comment_traffic_source_id,
    live_slots                 = EXCLUDED.live_slots,
    comment_poll_mode          = EXCLUDED.comment_poll_mode,
    automation_max_age_minutes = EXCLUDED.automation_max_age_minutes,
    version                    = conversation.page_settings.version + 1,
    updated_at                 = now()
WHERE conversation.page_settings.version = @expected_version::int;

-- name: SetRoundRobinLast :exec
-- Con trỏ xoay vòng: người vừa được giao (không tăng version: không phải sửa cài đặt).
UPDATE conversation.page_settings
SET round_robin_last_user_id = @user_id
WHERE channel_account_id = @channel_account_id;
