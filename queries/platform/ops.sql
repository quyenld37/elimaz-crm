-- Cảnh báo, kiểm sâu, theo dõi PostgreSQL (kế hoạch M0 bản 5, Task 19). Chỉ internal/platform/observability dùng.
-- Không câu nào đọc payload hay last_error của việc (có thể mang dữ liệu khách).

-- Đã gửi cảnh báo key trong window_s giây vừa qua (theo đồng hồ database)?
-- name: AlertSentWithin :one
SELECT EXISTS (
    SELECT 1 FROM platform.alert_events
    WHERE key = @key AND sent_at > now() - make_interval(secs => sqlc.arg(window_s)::float8)
)::bool AS recent;

-- name: AlertKeyExists :one
SELECT EXISTS (SELECT 1 FROM platform.alert_events WHERE key = @key)::bool AS found;

-- sent_at: lúc đọc trạng thái của lượt luật (mốc của luật dead:, in-doubt:); NULL → now().
-- name: InsertAlertEvent :exec
INSERT INTO platform.alert_events (key, severity, text, sent_at)
VALUES (@key, @severity, @text, coalesce(sqlc.narg(sent_at)::timestamptz, now()));

-- Số cảnh báo theo key từ since (trừ tin tổng kết).
-- name: AlertCountsSince :many
SELECT key, count(*)::bigint AS n
FROM platform.alert_events
WHERE sent_at > sqlc.arg(since)::timestamptz AND key NOT LIKE 'digest:%'
GROUP BY key
ORDER BY key;

-- Việc pending đã tới hạn từ trước now - lag_s.
-- name: QueueLag :one
SELECT count(*)::bigint AS n,
       coalesce(extract(epoch FROM sqlc.arg(now)::timestamptz - min(run_at)), 0)::float8 AS oldest_s
FROM platform.jobs
WHERE status = 'pending' AND run_at < sqlc.arg(now)::timestamptz - make_interval(secs => sqlc.arg(lag_s)::float8);

-- Việc ở trạng thái status đã đủ tuổi min_age_s lúc now, và đủ tuổi sau mốc của lần cảnh báo cuối của key
-- "<key_prefix><kind>" (sent_at là lúc đọc của lượt đó), theo kind; tối đa 3 mã việc mới nhất làm mẫu. Điều kiện
-- status IN (...) để planner dùng index jobs_attention cả với kế hoạch chung (generic plan).
-- name: NewTerminalJobs :many
SELECT j.kind,
       count(*)::bigint AS n,
       (array_agg(j.id ORDER BY j.finished_at DESC, j.id))[1:3]::uuid[] AS sample_ids
FROM platform.jobs j
WHERE j.status IN ('dead', 'in_doubt') AND j.status = @status
  AND j.finished_at <= sqlc.arg(now)::timestamptz - make_interval(secs => sqlc.arg(min_age_s)::float8)
  AND j.finished_at + make_interval(secs => sqlc.arg(min_age_s)::float8) > coalesce(
      (SELECT max(a.sent_at) FROM platform.alert_events a WHERE a.key = sqlc.arg(key_prefix)::text || j.kind),
      '-infinity'::timestamptz)
GROUP BY j.kind
ORDER BY j.kind;

-- name: OpenAttentionCounts :one
SELECT count(*) FILTER (WHERE status = 'dead')::bigint     AS dead,
       count(*) FILTER (WHERE status = 'in_doubt')::bigint AS in_doubt
FROM platform.jobs
WHERE status IN ('dead', 'in_doubt');

-- Tuổi nhịp sống mới nhất của mỗi vai trò (giây, so với now).
-- name: HeartbeatAgesByRole :many
SELECT role, extract(epoch FROM sqlc.arg(now)::timestamptz - max(at))::float8 AS age_s
FROM platform.process_heartbeats
GROUP BY role
ORDER BY role;

-- Chuỗi chỉ số theo phút của các tên names trong [since, now].
-- name: MetricSeries :many
SELECT process, name, label, bucket, sum
FROM platform.metrics_minute
WHERE name = ANY (sqlc.arg(names)::text[]) AND bucket >= sqlc.arg(since)::timestamptz AND bucket <= sqlc.arg(now)::timestamptz
ORDER BY process, name, label, bucket;

-- Tổng giá trị cuối của gauge name trên mọi tiến trình còn ghi trong 5 phút qua.
-- name: LatestGaugeSum :one
SELECT coalesce(sum(m.sum), 0)::float8 AS total
FROM (
    SELECT DISTINCT ON (process, label) sum
    FROM platform.metrics_minute
    WHERE name = @name AND bucket >= now() - interval '5 minutes'
    ORDER BY process, label, bucket DESC
) m;

-- Lần sao lưu thành công gần nhất của kind.
-- name: LastBackupAt :one
SELECT finished_at FROM platform.backup_runs WHERE kind = @kind AND ok ORDER BY finished_at DESC LIMIT 1;

-- Tuổi (giây) lần sao lưu thành công gần nhất của kind, theo đồng hồ của database: mốc do scripts/backup ghi bằng now()
-- của PostgreSQL nên tuổi cũng tính bằng now(), không so với đồng hồ của tiến trình (Task 445).
-- name: LastBackupAgeSeconds :one
SELECT extract(epoch FROM now() - finished_at)::float8 AS age_s
FROM platform.backup_runs WHERE kind = @kind AND ok ORDER BY finished_at DESC LIMIT 1;

-- name: DBAndWALBytes :one
SELECT pg_database_size(current_database())::bigint AS db_bytes, platform.wal_bytes()::bigint AS wal_bytes;

-- Lần chạy gần nhất của kind, thành công hay hỏng (tóm tắt sức khỏe: lần diễn tập khôi phục gần nhất kèm ok); age_s:
-- tuổi (giây) theo đồng hồ của database như LastBackupAgeSeconds.
-- name: LastBackupRun :one
SELECT finished_at, ok, extract(epoch FROM now() - finished_at)::float8 AS age_s
FROM platform.backup_runs WHERE kind = @kind ORDER BY finished_at DESC LIMIT 1;

-- Cảnh báo đã gửi từ since, mới nhất trước, trừ tin tổng kết (tóm tắt sức khỏe). Không trả text.
-- name: ListAlertsSince :many
SELECT key, severity, sent_at
FROM platform.alert_events
WHERE sent_at >= sqlc.arg(since)::timestamptz AND key NOT LIKE 'digest:%'
ORDER BY sent_at DESC, id DESC
LIMIT sqlc.arg(max_rows);
