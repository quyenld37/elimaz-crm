-- Yêu cầu xóa dữ liệu của Meta (kế hoạch M4, Task 179). Chỉ internal/platform/datadeletion dùng. Bảng không RLS.

-- name: InsertDataDeletionRequest :exec
-- Trùng mã (nạp lại tệp tạm đã nạp) thì bỏ qua.
INSERT INTO platform.data_deletion_requests (code, source, user_ref_hash, received_at)
VALUES (@code, @source, @user_ref_hash, @received_at)
ON CONFLICT (code) DO NOTHING;

-- name: GetDataDeletionRequest :one
SELECT code, completed_at
FROM platform.data_deletion_requests
WHERE code = @code;

-- name: ListPendingDataDeletions :many
-- Không khóa: câu xóa phiên (ADR-46 nhóm 3) chạy trước câu đánh dấu xong (nhóm 13).
SELECT code, user_ref_hash
FROM platform.data_deletion_requests
WHERE completed_at IS NULL
ORDER BY received_at, code
LIMIT @max_rows;

-- name: CompleteDataDeletion :execrows
UPDATE platform.data_deletion_requests
SET completed_at = @now
WHERE code = @code AND completed_at IS NULL;
