-- Cài đặt theo tenant (kế hoạch M3, Task M3-04; Q-35). Chạy trong tenancy.WithTenant: RLS chỉ cho thấy, cho ghi dòng
-- của tenant trong ngữ cảnh (app.tenant_id). Gói internal/platform/settings là chỗ duy nhất dùng các câu này.

-- name: GetSetting :one
SELECT value, version
FROM platform.settings
WHERE key = @key::text;

-- name: InsertSetting :one
-- version = 0 ở Put: chèn mới; đã có dòng (lần ghi khác đi trước) → không dòng nào (ErrVersionConflict).
INSERT INTO platform.settings (tenant_id, key, value, updated_by)
VALUES (current_setting('app.tenant_id')::uuid, @key::text, @value::jsonb, sqlc.narg(updated_by)::uuid)
ON CONFLICT (tenant_id, key) DO NOTHING
RETURNING version;

-- name: UpdateSetting :one
-- So version trong cùng câu (khóa dòng bởi UPDATE): lệch → không dòng nào (ErrVersionConflict).
UPDATE platform.settings
SET value      = @value::jsonb,
    version    = version + 1,
    updated_by = sqlc.narg(updated_by)::uuid,
    updated_at = now()
WHERE key = @key::text
  AND version = @version::bigint
RETURNING version;
