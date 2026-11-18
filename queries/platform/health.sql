-- Tóm tắt sức khỏe cho GET /api/v1/platform/health (kế hoạch M0 bản 5, Task 20). Chỉ internal/platform/observability
-- dùng. Đếm của cả hệ thống (bảng nền tảng không RLS), không đọc payload của việc.

-- Tuổi nhịp sống của mọi tiến trình, theo đồng hồ database (giây).
-- name: ListHeartbeatAges :many
SELECT process, role, extract(epoch FROM now() - at)::double precision AS age_seconds
FROM platform.process_heartbeats
ORDER BY role, process;

-- Số việc đang chờ, dead, in_doubt.
-- name: CountJobsByAttention :one
SELECT count(*) FILTER (WHERE status = 'pending')::bigint  AS pending,
       count(*) FILTER (WHERE status = 'dead')::bigint     AS dead,
       count(*) FILTER (WHERE status = 'in_doubt')::bigint AS in_doubt
FROM platform.jobs
WHERE status IN ('pending', 'dead', 'in_doubt');

-- Giá trị cuối của gauge spool_files của từng tiến trình ingest còn ghi trong since.
-- name: ListLatestSpoolFiles :many
SELECT DISTINCT ON (process) process, sum::double precision AS files
FROM platform.metrics_minute
WHERE name = 'spool_files' AND bucket >= @since
ORDER BY process, bucket DESC;
