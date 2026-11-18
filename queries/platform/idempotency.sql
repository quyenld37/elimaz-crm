-- Khóa chống trùng (kế hoạch M0, Task 9; ADR-41). Mọi câu chạy trong tenancy.WithTenant (bảng có RLS).
-- Thẻ chủ của một Claim là locked_until mà nó đặt: Complete và Release chỉ chạm dòng running còn đúng thẻ đó, nên
-- Claim cũ (đã bị nhận lại sau 60 s) không ghi "xong" hay xóa được dòng của bên đã nhận lại.

-- name: InsertIdempotencyKey :execrows
INSERT INTO platform.idempotency_keys (tenant_id, actor, route, key, request_hash, status, locked_until, expires_at)
VALUES (@tenant_id, @actor, @route, @key, @request_hash, 'running', @locked_until, @expires_at)
ON CONFLICT (tenant_id, actor, route, key) DO NOTHING;

-- name: LockIdempotencyKey :one
-- Câu riêng sau INSERT (ảnh chụp mới của READ COMMITTED thấy dòng bên kia vừa commit), khóa dòng tới hết transaction.
-- Thân đã lưu: response_raw (byte nguyên, Task 396); dòng do bản trước 0013 ghi (raw NULL) thì lấy response_body.
SELECT request_hash, status, locked_until, response_status,
       COALESCE(response_raw, convert_to(response_body::text, 'UTF8'))::bytea AS response_raw, expires_at
FROM platform.idempotency_keys
WHERE tenant_id = @tenant_id AND actor = @actor AND route = @route AND key = @key
FOR UPDATE;

-- name: ReclaimIdempotencyKey :execrows
-- Nhận lại dòng running quá hạn (tiến trình trước đã chết).
UPDATE platform.idempotency_keys
SET locked_until = @locked_until
WHERE tenant_id = @tenant_id AND actor = @actor AND route = @route AND key = @key
  AND status = 'running' AND locked_until < @now;

-- name: ResetExpiredIdempotencyKey :execrows
-- Dòng đã quá expires_at mà việc dọn chưa xóa: coi như khóa mới.
UPDATE platform.idempotency_keys
SET request_hash = @request_hash, status = 'running', locked_until = @locked_until, response_status = NULL,
    response_body = NULL, response_raw = NULL, created_at = @now, expires_at = @expires_at
WHERE tenant_id = @tenant_id AND actor = @actor AND route = @route AND key = @key
  AND expires_at <= @now;

-- name: CompleteIdempotencyKey :execrows
-- Ghi cả hai cột (pha mở rộng của 0013): response_body cho bản cũ còn chạy lúc deploy cuốn, response_raw đúng byte.
UPDATE platform.idempotency_keys
SET status = 'done', locked_until = NULL, response_status = @response_status,
    response_body = convert_from(@response_raw, 'UTF8')::jsonb, response_raw = @response_raw
WHERE tenant_id = @tenant_id AND actor = @actor AND route = @route AND key = @key
  AND status = 'running' AND locked_until = @locked_until;

-- name: ReleaseIdempotencyKey :execrows
DELETE FROM platform.idempotency_keys
WHERE tenant_id = @tenant_id AND actor = @actor AND route = @route AND key = @key
  AND status = 'running' AND locked_until = @locked_until;
