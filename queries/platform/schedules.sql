-- Việc định kỳ và dọn dữ liệu (kế hoạch M0, Task 13; spec 04 mục 5.1). Chỉ internal/platform/schedules dùng.

-- name: InsertSchedule :exec
-- Lịch mới chạy kỳ đầu ngay (next_run_at = now của Ticker); lịch đã có giữ next_run_at.
INSERT INTO platform.schedules (name, every_seconds, per_tenant, next_run_at)
VALUES (@name, @every_seconds, @per_tenant, @next_run_at)
ON CONFLICT (name) DO NOTHING;

-- name: UpdateScheduleDefinition :exec
-- Chỉ khóa dòng khi định nghĩa trong mã khác dòng (không chờ Ticker khác đang giữ dòng đúng định nghĩa).
UPDATE platform.schedules
SET every_seconds = @every_seconds, per_tenant = @per_tenant
WHERE name = @name AND (every_seconds <> @every_seconds OR per_tenant <> @per_tenant);

-- name: LockDueSchedules :many
-- Lịch tới hạn trong số lịch tiến trình này định nghĩa; lịch Ticker khác đang giữ thì bỏ qua (kỳ đó bên kia làm).
SELECT name, every_seconds, per_tenant, next_run_at
FROM platform.schedules
WHERE name = ANY (@names::text[]) AND next_run_at <= @now
ORDER BY name
FOR UPDATE SKIP LOCKED;

-- name: SetScheduleNextRun :exec
UPDATE platform.schedules
SET next_run_at = @next_run_at, last_enqueued_at = @enqueued_at::timestamptz
WHERE name = @name;

-- name: DeleteFinishedJobs :execrows
-- Việc done/expired kết thúc trước @before, tối đa @max_rows dòng; dòng đang bị giữ thì để lần sau.
-- id = ANY (ARRAY(...)): xóa theo khóa chính; với IN (câu con) planner chọn Hash Semi Join quét cả bảng jobs (Task 394).
DELETE FROM platform.jobs
WHERE id = ANY (ARRAY(
    SELECT j.id FROM platform.jobs AS j
    WHERE j.status IN ('done', 'expired') AND j.finished_at < @before::timestamptz
    ORDER BY j.finished_at
    LIMIT @max_rows
    FOR UPDATE SKIP LOCKED));

-- name: DeleteOldMetrics :execrows
DELETE FROM platform.metrics_minute
WHERE (bucket, process, name, label) IN (
    SELECT m.bucket, m.process, m.name, m.label FROM platform.metrics_minute AS m
    WHERE m.bucket < @before
    ORDER BY m.bucket
    LIMIT @max_rows
    FOR UPDATE SKIP LOCKED);

-- name: DeleteExpiredIdempotencyKeys :execrows
-- Chạy trong WithTenant: RLS giới hạn ở tenant của việc dọn (index (tenant_id, expires_at)).
DELETE FROM platform.idempotency_keys
WHERE (tenant_id, actor, route, key) IN (
    SELECT k.tenant_id, k.actor, k.route, k.key FROM platform.idempotency_keys AS k
    WHERE k.expires_at < @before
    ORDER BY k.expires_at
    LIMIT @max_rows
    FOR UPDATE SKIP LOCKED);
