-- Callback xóa dữ liệu của Meta (kế hoạch M4, Task 179 / M4-39; spec 02 mục 5; Q30). Pha mở rộng: CREATE TABLE, ADD
-- COLUMN (có thể null), INDEX, GRANT; không thu hẹp gì.
-- - platform.data_deletion_requests: yêu cầu xóa Meta gửi tới /hooks/facebook/data-deletion. Bảng nền tảng, không
--   tenant_id, không RLS (Meta không cho biết shop): việc nền platform.data_deletion xóa ở mọi tenant. code là mã xác
--   nhận 16 byte ngẫu nhiên (hex) trả cho Meta; user_ref_hash là hex SHA-256 của mã người dùng app, không bao giờ lưu
--   mã gốc. Chỉ internal/platform/** nhắc tên bảng.
-- - channel.oauth_sessions.fb_user_ref_hash: cùng kiểu băm, ghi lúc đổi mã đăng nhập (phiên Page). Phiên tạo trước
--   migration này hết hạn sau 30 phút nên không điền ngược.
-- - Thứ tự khóa: ADR-46 nhóm 3 (oauth_sessions) trước nhóm 13 (data_deletion_requests đứng đầu nhóm).

-- +goose Up
CREATE TABLE platform.data_deletion_requests (
    code          text PRIMARY KEY CHECK (code ~ '^[0-9a-f]{32}$'),
    source        text NOT NULL CHECK (source <> ''),
    user_ref_hash text NOT NULL CHECK (user_ref_hash ~ '^[0-9a-f]{64}$'),
    received_at   timestamptz NOT NULL DEFAULT now(),
    completed_at  timestamptz
);
-- Việc nền đọc yêu cầu chưa xong theo giờ nhận.
CREATE INDEX data_deletion_requests_pending ON platform.data_deletion_requests (received_at) WHERE completed_at IS NULL;

ALTER TABLE channel.oauth_sessions
    ADD COLUMN fb_user_ref_hash text CHECK (fb_user_ref_hash ~ '^[0-9a-f]{64}$');
CREATE INDEX oauth_sessions_fb_user_ref ON channel.oauth_sessions (tenant_id, fb_user_ref_hash)
    WHERE fb_user_ref_hash IS NOT NULL;

GRANT SELECT, INSERT ON platform.data_deletion_requests TO crm_app;
GRANT UPDATE (completed_at) ON platform.data_deletion_requests TO crm_app;
