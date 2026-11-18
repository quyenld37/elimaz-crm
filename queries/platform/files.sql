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

-- name: InsertPrivateImageFile :one
-- Ảnh riêng tư do máy chủ ghi (R10, Task M1-43): uploaded, chờ file.process_private. Cùng id (hay cùng khóa) lần hai
-- → không trả dòng nào (bên gọi đọc id cũ, không xếp lệnh thêm).
INSERT INTO platform.files (id, tenant_id, purpose, bucket, key, original_name, declared_type, size_bytes, status,
    created_by)
VALUES (@id, @tenant_id, @purpose, 'private', @key, @original_name, @declared_type, @size_bytes, 'uploaded',
    sqlc.narg(created_by))
ON CONFLICT DO NOTHING
RETURNING id;

-- name: FindReadyDuplicate :one
-- Tệp ready khác cùng tenant, cùng mục đích, cùng nội dung, cùng bộ kích thước (chống trùng, Task M1-43).
SELECT id
FROM platform.files
WHERE tenant_id = @tenant_id AND content_sha256 = @content_sha256 AND purpose = @purpose
    AND preset_version = @preset_version AND status = 'ready' AND deleted_at IS NULL AND id <> @id
ORDER BY created_at, id
LIMIT 1;

-- name: ListFileVariantsFull :many
SELECT preset_version, width, format, bucket, key, size_bytes
FROM platform.file_variants
WHERE file_id = @file_id
ORDER BY preset_version, format, width;

-- name: SetFileReady :one
-- Kết quả xử lý ảnh (dòng đã khóa bằng LockFile): chỉ đổi từ uploaded, processing, lost.
UPDATE platform.files
SET status = 'ready', reject_reason = NULL, detected_type = @detected_type, width = sqlc.narg(width),
    height = sqlc.narg(height), content_sha256 = @content_sha256, preset_version = @preset_version, bucket = @bucket,
    key = @key, lost_at = NULL, updated_at = now(), version = version + 1
WHERE id = @id AND status IN ('uploaded', 'processing', 'lost')
RETURNING id;

-- name: SetFileRejected :execrows
-- Từ chối khi xử lý ảnh (dòng đã khóa bằng LockFile): chỉ đổi từ uploaded, processing.
UPDATE platform.files
SET status = 'rejected', reject_reason = @reject_reason, updated_at = now(), version = version + 1
WHERE id = @id AND status IN ('uploaded', 'processing');

-- name: InsertFileVariant :exec
INSERT INTO platform.file_variants (tenant_id, file_id, preset_version, width, format, bucket, key, size_bytes)
VALUES (@tenant_id, @file_id, @preset_version, @width, @format, @bucket, @key, @size_bytes)
ON CONFLICT DO NOTHING;

-- name: ListPublicVariants :many
-- Biến thể công khai của các tệp ready, chưa xóa, của mục đích chế độ standard (R10: không bao giờ có ảnh riêng tư).
SELECT v.file_id, v.preset_version, v.width, v.format, v.key
FROM platform.file_variants v
JOIN platform.files f ON f.tenant_id = v.tenant_id AND f.id = v.file_id
JOIN platform.file_purposes p ON p.key = f.purpose
WHERE v.file_id = ANY(@file_ids::uuid[]) AND v.bucket = 'public' AND f.status = 'ready' AND f.deleted_at IS NULL
    AND p.mode = 'standard'
ORDER BY v.file_id, v.preset_version, v.format, v.width;

-- name: SetFileLost :execrows
-- Đánh dấu tệp mất trên kho (dựng lại kho tệp, TP12): chỉ từ ready. Task M1-44 dùng trong filestest; Task M1-46 dùng
-- khi đối chiếu.
UPDATE platform.files
SET status = 'lost', lost_at = now(), updated_at = now(), version = version + 1
WHERE id = @id AND status = 'ready';

-- name: ListFilesMissingPreset :many
-- Tệp ready, chưa xóa, đã có mã băm, của mục đích chế độ standard, chưa có biến thể nào của bộ kích thước
-- @preset_version (tạo lại kích thước, Task M1-44d, TP07). Mục đích private không giữ ảnh gốc nên không bao giờ có
-- trong kết quả (R10). Tenant theo RLS của ngữ cảnh; bên gọi lọc tiếp mục đích ảnh theo sổ mục đích.
SELECT f.id, f.tenant_id, f.purpose
FROM platform.files f
JOIN platform.file_purposes p ON p.key = f.purpose
WHERE f.status = 'ready' AND f.deleted_at IS NULL AND f.content_sha256 IS NOT NULL AND p.mode = 'standard'
    AND NOT EXISTS (
        SELECT 1 FROM platform.file_variants v
        WHERE v.tenant_id = f.tenant_id AND v.file_id = f.id AND v.preset_version = @preset_version
    )
ORDER BY f.id;
