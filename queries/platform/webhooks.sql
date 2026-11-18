-- Bản nhận webhook (kế hoạch M0 bản 5, Task 14). Chỉ internal/platform/webhooks dùng.

-- Chống trùng theo (source, body_sha256): trùng thì không trả dòng nào (không ghi thêm việc tách gói).
-- name: InsertWebhookDelivery :one
INSERT INTO platform.webhook_deliveries (source, body_sha256, headers, body, received_at)
VALUES (@source, @body_sha256, @headers, @body, @received_at)
ON CONFLICT (source, body_sha256) DO NOTHING
RETURNING id;

-- Dọn bản nhận cũ hơn @before, mỗi lần tối đa @max_rows dòng.
-- name: DeleteOldWebhookDeliveries :execrows
DELETE FROM platform.webhook_deliveries
WHERE id IN (
    SELECT old.id FROM platform.webhook_deliveries AS old
    WHERE old.received_at < sqlc.arg(before)::timestamptz
    ORDER BY old.received_at
    LIMIT sqlc.arg(max_rows)::int
);

-- Đọc lại một bản nhận để tách gói (việc webhook.split) và lấy nội dung sự kiện (việc inbound.*).
-- name: GetWebhookDelivery :one
SELECT source, body FROM platform.webhook_deliveries WHERE id = @id;
