-- Bản nhận webhook (kế hoạch M0 bản 5, Task 14; Task M2-33). Chỉ internal/platform/webhooks dùng.

-- Chống trùng theo (source, body_sha256): trùng thì không trả dòng nào (không ghi thêm việc tách gói). route_key rỗng
-- với đường /hooks/{source}.
-- name: InsertWebhookDelivery :one
INSERT INTO platform.webhook_deliveries (source, body_sha256, headers, body, received_at, route_key)
VALUES (@source, @body_sha256, @headers, @body, @received_at, sqlc.narg(route_key))
ON CONFLICT (source, body_sha256) DO NOTHING
RETURNING id;

-- Ghi một sự kiện đã chuẩn hóa (EnqueueInbound): body_kind 'event', header rỗng. Trùng (cùng sự kiện ghi lại) thì không
-- trả dòng nào; bên gọi đọc mã của dòng có sẵn bằng FindWebhookDelivery.
-- name: InsertWebhookEvent :one
INSERT INTO platform.webhook_deliveries (source, body_sha256, headers, body, received_at, body_kind)
VALUES (@source, @body_sha256, '{}', @body, @received_at, 'event')
ON CONFLICT (source, body_sha256) DO NOTHING
RETURNING id;

-- name: FindWebhookDelivery :one
SELECT id FROM platform.webhook_deliveries WHERE source = @source AND body_sha256 = @body_sha256;

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
SELECT source, body, route_key, body_kind, received_at FROM platform.webhook_deliveries WHERE id = @id;
