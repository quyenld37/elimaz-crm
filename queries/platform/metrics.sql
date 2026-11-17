-- Chỉ số theo phút và nhịp sống (kế hoạch M0 bản 5, Task 10). Chỉ internal/platform/metrics dùng.

-- Cộng dồn một dòng chỉ số vào phút của nó. kind: 'h' histogram, 'c' counter, 'g' gauge. Gauge giữ giá trị cuối
-- (sum) và lớn nhất (max); histogram cộng từng ngăn (mọi dòng cùng số ngăn, len(HistBoundsMs)+1).
-- name: UpsertMetricMinute :exec
INSERT INTO platform.metrics_minute AS m (bucket, process, name, label, count, sum, max, hist)
VALUES (@bucket, @process, @name, @label, @count, @sum, @max, sqlc.narg(hist)::int[])
ON CONFLICT (bucket, process, name, label) DO UPDATE
SET count = m.count + EXCLUDED.count,
    sum   = CASE WHEN @kind::text = 'g' THEN EXCLUDED.sum ELSE m.sum + EXCLUDED.sum END,
    max   = GREATEST(m.max, EXCLUDED.max),
    hist  = CASE
        WHEN m.hist IS NULL OR EXCLUDED.hist IS NULL THEN COALESCE(EXCLUDED.hist, m.hist)
        ELSE ARRAY(
            SELECT COALESCE(m.hist[i], 0) + COALESCE(EXCLUDED.hist[i], 0)
            FROM generate_subscripts(EXCLUDED.hist, 1) AS i
            ORDER BY i
        )
    END;

-- name: UpsertHeartbeat :exec
INSERT INTO platform.process_heartbeats (process, role, at)
VALUES (@process, @role, @at)
ON CONFLICT (process) DO UPDATE
SET role = EXCLUDED.role,
    at   = EXCLUDED.at;

-- Tuổi nhịp sống của một tiến trình, theo đồng hồ của database (giây).
-- name: HeartbeatAgeSeconds :one
SELECT extract(epoch FROM now() - at)::double precision AS age_seconds
FROM platform.process_heartbeats
WHERE process = @process;
