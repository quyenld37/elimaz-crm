-- Realtime (kế hoạch M0 bản 5, Task 16; spec 04 mục 6). Chỉ internal/platform/realtime dùng.
-- Kênh duy nhất 'rt'; payload là JSON báo hiệu (tenant, chủ đề, quyền, phạm vi, mã đối tượng), không có nội dung.

-- Phát báo hiệu trong transaction của bên gọi: PostgreSQL chỉ giao khi transaction commit.
-- name: Notify :exec
SELECT pg_notify('rt', sqlc.arg(payload)::text);

-- Nghe kênh 'rt' trên kết nối riêng của hub.
-- name: Listen :exec
LISTEN rt;
