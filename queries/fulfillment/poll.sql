-- Tra trạng thái và tra bù theo lịch (kế hoạch M3, Task M3-11). Chạy trong tenancy.WithTenant. Không khóa dòng: lệnh
-- tra đọc trong transaction ngắn rồi gọi hãng ngoài transaction; áp kết quả qua ApplyTracking (khóa theo ADR-46).

-- name: TrackTarget :one
-- Vận đơn cần tra kèm chế độ, trạng thái của tài khoản hãng; closed_at là mốc giao xong (dòng M1 chưa có delivered_at
-- thì lấy last_status_at, rồi created_at). Không có (hay tenant khác) → pgx.ErrNoRows.
SELECT s.id, s.code, s.carrier_key, s.carrier_account_id, s.tracking_code, s.status,
       COALESCE(s.delivered_at, s.last_status_at, s.created_at) AS closed_at, a.mode, a.status AS account_status
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.id = @id;

-- name: PollCandidates :many
-- Vận đơn mở cần tra bù: trạng thái là khóa của minutes (JSON {trạng thái: phút}), last_synced_at rỗng hay cũ hơn số
-- phút của trạng thái đó (so với now của đồng hồ ứng dụng), tài khoản active, mode api, hãng thuộc carrier_keys (adapter có Track). Mỗi tài
-- khoản tối đa per_account vận đơn, chưa đồng bộ trước, rồi cũ nhất trước.
SELECT x.id
FROM (
    SELECT s.id, s.carrier_account_id,
           row_number() OVER (PARTITION BY s.carrier_account_id
                              ORDER BY s.last_synced_at ASC NULLS FIRST, s.id) AS rn
    FROM fulfillment.shipments s
    JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
    CROSS JOIN LATERAL (SELECT jsonb_extract_path_text(@minutes::jsonb, s.status)::int AS minutes) p
    WHERE a.status = 'active' AND a.mode = 'api' AND a.carrier_key = ANY (@carrier_keys::text[])
      AND p.minutes IS NOT NULL
      AND (s.last_synced_at IS NULL
           OR s.last_synced_at < sqlc.arg(now)::timestamptz - p.minutes * interval '1 minute')
) x
WHERE x.rn <= @per_account::bigint
ORDER BY x.carrier_account_id, x.rn;

-- name: CountUnsynced :one
-- Số vận đơn mở (cùng điều kiện PollCandidates, không giới hạn mỗi tài khoản) chưa đồng bộ hay cũ hơn minutes phút
-- của trạng thái: gauge shipments_unsynced_over_threshold.
SELECT count(*)
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
CROSS JOIN LATERAL (SELECT jsonb_extract_path_text(@minutes::jsonb, s.status)::int AS minutes) p
WHERE a.status = 'active' AND a.mode = 'api' AND a.carrier_key = ANY (@carrier_keys::text[])
  AND p.minutes IS NOT NULL
  AND (s.last_synced_at IS NULL
       OR s.last_synced_at < sqlc.arg(now)::timestamptz - p.minutes * interval '1 minute');

-- name: SetSyncError :exec
-- Lần tra không đọc được hay không áp được: ghi mã lỗi ngắn; last_synced_at giữ giờ cũ (màn hình "cập nhật lần cuối").
UPDATE fulfillment.shipments SET last_sync_error = @code::text, updated_at = now() WHERE id = @id;

-- name: CountSyncErrors :one
-- Số vận đơn chưa kết thúc đang mang mã lỗi đồng bộ code (nội dung cảnh báo: chỉ số lượng).
SELECT count(*)
FROM fulfillment.shipments
WHERE last_sync_error = @code::text AND status NOT IN ('delivered', 'returned', 'cancelled');
