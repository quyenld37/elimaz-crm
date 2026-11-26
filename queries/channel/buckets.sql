-- Thùng token giới hạn gửi theo tài khoản kênh (04 mục 5.3, kế hoạch M4 Task 145). Lấy token là một câu UPDATE có
-- điều kiện: READ COMMITTED đánh giá lại điều kiện sau khi chờ khóa dòng, nên nhiều lần lấy cùng lúc không bao giờ
-- vượt số token có. Đồng hồ lùi (now < updated_at) không nạp thêm và không lùi updated_at.

-- Dòng đầy: tokens = capacity mặc định 50, khớp DEFAULT của cột capacity.
-- name: EnsureBucket :exec
INSERT INTO channel.send_buckets (tenant_id, channel_account_id, tokens, updated_at)
VALUES (sqlc.arg(tenant_id), sqlc.arg(channel_account_id), 50, sqlc.arg(now)::timestamptz)
ON CONFLICT (tenant_id, channel_account_id) DO NOTHING;

-- name: TakeToken :one
UPDATE channel.send_buckets
SET tokens = LEAST(capacity, tokens + GREATEST(0, extract(epoch FROM sqlc.arg(now)::timestamptz - updated_at)) * refill_per_sec) - 1,
    updated_at = GREATEST(updated_at, sqlc.arg(now)::timestamptz)
WHERE tenant_id = sqlc.arg(tenant_id) AND channel_account_id = sqlc.arg(channel_account_id)
  AND LEAST(capacity, tokens + GREATEST(0, extract(epoch FROM sqlc.arg(now)::timestamptz - updated_at)) * refill_per_sec) >= 1
RETURNING tokens::double precision;

-- PeekBucket: số token hiện có lúc now (đã nạp) và tốc độ nạp, để tính thời gian chờ khi không lấy được.
-- name: PeekBucket :one
SELECT LEAST(capacity, tokens + GREATEST(0, extract(epoch FROM sqlc.arg(now)::timestamptz - updated_at)) * refill_per_sec)::double precision AS available,
    refill_per_sec
FROM channel.send_buckets
WHERE tenant_id = sqlc.arg(tenant_id) AND channel_account_id = sqlc.arg(channel_account_id);
