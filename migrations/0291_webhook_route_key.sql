-- Webhook có khóa tuyến và sự kiện vào không đến từ webhook (kế hoạch M2, Task M2-33; Q-X3, A25; M3 Q-8, M4).
-- Pha mở rộng: hai cột cho phép rỗng hoặc có DEFAULT, hai CHECK; bản cũ của ingest (không ghi hai cột) vẫn chạy.
-- Thứ tự khóa không đổi: docs/adr/0046-thu-tu-khoa.md, "Bảng M0 ngoài thứ tự nhóm" (a), chỉ chèn ON CONFLICT DO NOTHING.
--   route_key: khóa tuyến của POST /hooks/{source}/{routeKey} (32 ký tự chữ số; M3: mỗi tài khoản hãng một khóa, tìm
--     tenant qua platform.tenant_routes kind carrier_account). Rỗng với đường /hooks/{source} của M0. Với bản nhận có
--     route_key, body_sha256 là băm của route_key cùng thân (webhooks.Save) để hai tuyến không nuốt gói của nhau mà vẫn
--     giữ UNIQUE (source, body_sha256) của 0009.
--   body_kind: 'packet' = thân gốc của một lần nhận webhook (tách bằng Source); 'event' = một sự kiện đã chuẩn hóa ghi
--     bằng webhooks.EnqueueInbound (M4 hỏi bù, đọc comment, luồng live), để payload của việc vào vẫn chỉ mang mã bản
--     nhận (spec 04 mục 11). Dọn sau 30 ngày như mọi bản nhận.

-- +goose Up
ALTER TABLE platform.webhook_deliveries
    ADD COLUMN route_key text CHECK (route_key ~ '^[A-Za-z0-9]{32}$'),
    ADD COLUMN body_kind text NOT NULL DEFAULT 'packet' CHECK (body_kind IN ('packet', 'event')),
    ADD CONSTRAINT webhook_deliveries_event_no_route CHECK (body_kind = 'packet' OR route_key IS NULL);
