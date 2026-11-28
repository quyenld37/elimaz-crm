-- Sự kiện hãng của vận đơn ngoài hệ thống (kế hoạch M7, Task 265). Chạy trong ngữ cảnh tenant (RLS).

-- name: RecordUnmatchedEvent :exec
-- Một sự kiện của (hãng, mã vận đơn): dòng mới event_count 1, đã có thì +1 trong một câu (không đọc ra rồi ghi lại).
-- Mốc cuối không lùi (greatest); tài khoản, trạng thái thô giữ giá trị đã biết khi sự kiện này không mang.
INSERT INTO fulfillment.unmatched_carrier_events AS u
    (tenant_id, carrier_key, carrier_account_id, tracking_code, last_raw_status, first_seen_at, last_seen_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @carrier_key, sqlc.narg(carrier_account_id), @tracking_code,
        sqlc.narg(raw_status), @seen_at, @seen_at)
ON CONFLICT (tenant_id, carrier_key, tracking_code) DO UPDATE
SET event_count        = u.event_count + 1,
    last_seen_at       = greatest(u.last_seen_at, EXCLUDED.last_seen_at),
    last_raw_status    = coalesce(EXCLUDED.last_raw_status, u.last_raw_status),
    carrier_account_id = coalesce(EXCLUDED.carrier_account_id, u.carrier_account_id);

-- name: ListUnmatchedEvents :many
-- Trang "mới thấy trước" (first_seen_at DESC, id DESC; khóa không đổi khi dòng được cập nhật). Lọc hãng (rỗng: mọi
-- hãng) và khoảng [since, until) theo "có sự kiện trong khoảng": last_seen_at >= since, first_seen_at < until.
SELECT id, carrier_key, carrier_account_id, tracking_code, last_raw_status, first_seen_at, last_seen_at, event_count
FROM fulfillment.unmatched_carrier_events u
WHERE (@carrier::text = '' OR u.carrier_key = @carrier)
  AND (sqlc.narg(since)::timestamptz IS NULL OR u.last_seen_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR u.first_seen_at < sqlc.narg(until))
  AND (sqlc.narg(after_id)::uuid IS NULL OR (u.first_seen_at, u.id) < (
      SELECT a.first_seen_at, a.id FROM fulfillment.unmatched_carrier_events a WHERE a.id = sqlc.narg(after_id)))
ORDER BY u.first_seen_at DESC, u.id DESC
LIMIT @lim;

-- name: UnmatchedEventExists :one
-- Con trỏ trang có phải một dòng của tenant không.
SELECT EXISTS (SELECT 1 FROM fulfillment.unmatched_carrier_events WHERE id = @id);
