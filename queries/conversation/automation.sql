-- Quy tắc tự động và lượt chạy (Task 165; kế hoạch M4-25). Thứ tự khóa nhóm 6 (ADR-46): messages → message_revisions
-- → automation_rules → automation_runs; mọi câu chạy trong tx tenant.

-- name: ListAutomationRules :many
-- Mọi quy tắc đã lưu của Page (loại chưa có dòng: bên gọi trả mặc định).
SELECT id, channel_account_id, kind, config, enabled, version, updated_at
FROM conversation.automation_rules
WHERE channel_account_id = @channel_account_id
ORDER BY kind;

-- name: GetAutomationRule :one
-- Quy tắc loại kind của Page (không khóa: đường vào chỉ đọc cấu hình).
SELECT id, channel_account_id, kind, config, enabled, version, updated_at
FROM conversation.automation_rules
WHERE channel_account_id = @channel_account_id
  AND kind = @kind;

-- name: LockAutomationRule :one
-- Khóa quy tắc loại kind của Page để sửa.
SELECT id, channel_account_id, kind, config, enabled, version, updated_at
FROM conversation.automation_rules
WHERE channel_account_id = @channel_account_id
  AND kind = @kind
FOR UPDATE;

-- name: SaveAutomationRule :one
-- Lưu quy tắc: chưa có dòng → chèn (version 1); có → sửa khi version = @expected_version (version + 1). Không dòng nào
-- (pgx.ErrNoRows): dòng vừa được người khác chèn hay sửa (version_conflict).
INSERT INTO conversation.automation_rules (tenant_id, channel_account_id, kind, config, enabled, updated_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @channel_account_id, @kind, @config::jsonb, @enabled,
        @updated_by)
ON CONFLICT (tenant_id, channel_account_id, kind) DO UPDATE
SET config     = EXCLUDED.config,
    enabled    = EXCLUDED.enabled,
    updated_by = EXCLUDED.updated_by,
    version    = conversation.automation_rules.version + 1,
    updated_at = now()
WHERE conversation.automation_rules.version = @expected_version::int
RETURNING id, channel_account_id, kind, config, enabled, version, updated_at;

-- name: InsertAutomationRun :one
-- Ghi lượt chạy của quy tắc với sự kiện external_ref; đã có lượt (xử lý lại) → không dòng nào (pgx.ErrNoRows).
INSERT INTO conversation.automation_runs (tenant_id, rule_id, external_ref, subject_ref, outcome)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @rule_id, @external_ref,
        sqlc.narg(subject_ref)::text, @outcome)
ON CONFLICT (tenant_id, rule_id, external_ref) DO NOTHING
RETURNING id;

-- name: SetAutomationRunJob :exec
-- Gắn lệnh vừa ghi vào lượt chạy (outcome enqueued), hay đổi lượt sang kết quả bỏ qua do điều kiện riêng của loại.
UPDATE conversation.automation_runs
SET command_job_id = sqlc.narg(command_job_id)::uuid,
    outcome        = @outcome
WHERE id = @id;

-- name: FinishAutomationRun :execrows
-- Kết quả cuối của lệnh tự động job_id (done, failed, dropped).
UPDATE conversation.automation_runs
SET outcome = @outcome
WHERE command_job_id = @command_job_id;

-- name: GetAutomationRunByJob :one
-- Lượt chạy của lệnh job_id (đọc kết quả, test).
SELECT id, rule_id, external_ref, outcome
FROM conversation.automation_runs
WHERE command_job_id = @command_job_id;

-- name: GetCommentForHide :one
-- Comment cần ẩn (không khóa; lệnh đọc trước khi gọi kênh): mã ngoài, Page, trạng thái.
SELECT id, conversation_id, channel_account_id, external_id, is_hidden, removed_at, direction
FROM conversation.messages
WHERE id = @id
  AND kind = 'comment';

-- name: LockCommentByID :one
-- Khóa comment id (sau khi đã khóa hội thoại của nó) để đổi trạng thái ẩn.
SELECT id, conversation_id, body, is_hidden, removed_at, edited_at
FROM conversation.messages
WHERE id = @id
  AND kind = 'comment'
FOR UPDATE;

-- name: ConversationCustomer :one
-- Khách của hội thoại (không khóa).
SELECT customer_id
FROM conversation.conversations
WHERE id = @id;
