-- Tạm dừng nhận đơn web (kế hoạch M5, Task 456 / M5-44). Pha mở rộng: chỉ CREATE TABLE, RLS, GRANT.
-- - ord.web_intake: một dòng mỗi tenant. Bảng riêng (không thêm cột vào ord.order_settings) để version của màn Cài đặt
--   đơn và màn Nhận đơn web không đụng nhau (Task 450, Q10). Chưa có dòng = đang nhận đơn, version 1 (order.GetWebIntake);
--   lần ghi đầu chèn dòng rồi khóa (order.Writer.SetWebIntake).
-- - Tạm dừng thì phải có lý do (1–300 ký tự); changed_by là nhân viên đổi lần cuối, changed_at lúc đổi.
-- - Thứ tự khóa: nhóm 7 ADR-46, bảng cuối nhóm (sau order_request_files). Tạo đơn web chỉ đọc, không khóa.

-- +goose Up
CREATE TABLE ord.web_intake (
    tenant_id  uuid PRIMARY KEY REFERENCES platform.tenants (id),
    paused     boolean NOT NULL DEFAULT false,
    reason     text CHECK (length(reason) <= 300),
    changed_by uuid,
    changed_at timestamptz,
    version    int NOT NULL DEFAULT 1,
    CONSTRAINT web_intake_reason_when_paused CHECK (NOT paused OR coalesce(length(reason), 0) >= 1)
);
SELECT platform.enable_tenant_rls('ord.web_intake');

GRANT SELECT, INSERT ON ord.web_intake TO crm_app;
GRANT UPDATE (paused, reason, changed_by, changed_at, version) ON ord.web_intake TO crm_app;
