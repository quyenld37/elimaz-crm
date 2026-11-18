-- Kho tệp (kế hoạch M1, Task M1-41). Mọi câu trên files, file_variants chạy trong tenancy.WithTenant (RLS); câu đọc trả
-- đủ cột của platform.files theo thứ tự bảng để sqlc dùng chung một kiểu (PlatformFile).

-- name: ListFilePurposes :many
SELECT key, bucket, mode
FROM platform.file_purposes
ORDER BY key;

-- name: CurrentTenant :one
-- Tenant của ngữ cảnh (tenancy.WithTenant); NULL khi ngoài ngữ cảnh.
SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS tenant_id;

-- name: InsertUploadFile :one
INSERT INTO platform.files (id, tenant_id, purpose, bucket, key, original_name, declared_type, size_bytes, status, created_by)
VALUES (@id, @tenant_id, @purpose, 'tmp', @key, @original_name, @declared_type, @size_bytes, 'awaiting_upload', sqlc.narg(created_by))
RETURNING id, tenant_id, purpose, bucket, key, original_name, declared_type, detected_type, size_bytes, content_sha256,
    width, height, status, reject_reason, preset_version, created_by, expires_at, lost_at, deleted_at, deleted_by,
    created_at, updated_at, version;

-- name: InsertServerFile :one
-- Tệp do máy chủ ghi (A8): vào thẳng ready. Khóa đã có dòng → không trả dòng nào (bên gọi đọc id cũ).
INSERT INTO platform.files (id, tenant_id, purpose, bucket, key, original_name, declared_type, size_bytes, content_sha256,
    status, created_by, expires_at)
VALUES (@id, @tenant_id, @purpose, @bucket, @key, @original_name, @declared_type, @size_bytes, sqlc.narg(content_sha256),
    'ready', sqlc.narg(created_by), sqlc.narg(expires_at))
ON CONFLICT (tenant_id, bucket, key) DO NOTHING
RETURNING id;

-- name: FileIDByKey :one
SELECT id
FROM platform.files
WHERE bucket = @bucket AND key = @key;

-- name: GetFile :one
SELECT id, tenant_id, purpose, bucket, key, original_name, declared_type, detected_type, size_bytes, content_sha256,
    width, height, status, reject_reason, preset_version, created_by, expires_at, lost_at, deleted_at, deleted_by,
    created_at, updated_at, version
FROM platform.files
WHERE id = @id;

-- name: LockFile :one
-- Khóa dòng tệp (ADR-46 nhóm 12b) trước khi đổi trạng thái.
SELECT id, tenant_id, purpose, bucket, key, original_name, declared_type, detected_type, size_bytes, content_sha256,
    width, height, status, reject_reason, preset_version, created_by, expires_at, lost_at, deleted_at, deleted_by,
    created_at, updated_at, version
FROM platform.files
WHERE id = @id
FOR UPDATE;

-- name: SetUploadResult :one
-- Kết quả của complete: chỉ đổi từ awaiting_upload (dòng đã khóa bằng LockFile).
UPDATE platform.files
SET status = @status, reject_reason = sqlc.narg(reject_reason), updated_at = now(), version = version + 1
WHERE id = @id AND status = 'awaiting_upload'
RETURNING id, tenant_id, purpose, bucket, key, original_name, declared_type, detected_type, size_bytes, content_sha256,
    width, height, status, reject_reason, preset_version, created_by, expires_at, lost_at, deleted_at, deleted_by,
    created_at, updated_at, version;

-- name: MarkFileDeleted :execrows
UPDATE platform.files
SET deleted_at = now(), deleted_by = sqlc.narg(deleted_by), updated_at = now(), version = version + 1
WHERE id = @id AND deleted_at IS NULL;

-- name: ListFileVariants :many
SELECT width, format, bucket, key
FROM platform.file_variants
WHERE file_id = @file_id
ORDER BY format, width;
