-- Chép tệp sang kho ngoài (kế hoạch M1, Task M1-45). Chỉ internal/platform/observability đọc (luật cảnh báo,
-- /health/deep); container filesync ghi bằng role crm_filesync (scripts/filesync), không qua sqlc.

-- Mỗi bucket trong buckets (theo thứ tự): has_ok và lúc xong lần copy ok gần nhất (has_ok false: chưa có, last_ok_at
-- là mốc 1970 vô nghĩa); copy_failed: lần copy gần nhất ok = false; reconcile_failed: lần reconcile gần nhất ok = false
-- (chưa có lần nào: false).
-- name: FileSyncStatus :many
SELECT b.bucket::text AS bucket,
       (good.finished_at IS NOT NULL)::bool AS has_ok,
       coalesce(good.finished_at, 'epoch'::timestamptz)::timestamptz AS last_ok_at,
       (lc.ok IS NOT NULL AND NOT lc.ok)::bool AS copy_failed,
       (lr.ok IS NOT NULL AND NOT lr.ok)::bool AS reconcile_failed
FROM unnest(@buckets::text[]) WITH ORDINALITY AS b(bucket, ord)
LEFT JOIN LATERAL (
    SELECT r.finished_at FROM platform.file_sync_runs r
    WHERE r.bucket = b.bucket AND r.kind = 'copy' AND r.ok ORDER BY r.finished_at DESC LIMIT 1
) good ON true
LEFT JOIN LATERAL (
    SELECT r.ok FROM platform.file_sync_runs r
    WHERE r.bucket = b.bucket AND r.kind = 'copy' ORDER BY r.finished_at DESC, r.id DESC LIMIT 1
) lc ON true
LEFT JOIN LATERAL (
    SELECT r.ok FROM platform.file_sync_runs r
    WHERE r.bucket = b.bucket AND r.kind = 'reconcile' ORDER BY r.finished_at DESC, r.id DESC LIMIT 1
) lr ON true
ORDER BY b.ord;
