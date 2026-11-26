-- Khách hàng cho hộp chat (kế hoạch M4, Task 152 / M4-12). Pha mở rộng: chỉ ADD COLUMN có DEFAULT, nối cột vào cuối
-- view công bố, GRANT.
-- - do_not_contact: cờ "không liên hệ" (02 mục 3): chặn mọi tin tự động; chỉ đọc ở hồ sơ đại diện (root_id).
--   do_not_contact_at: lúc bật, NULL khi tắt.
-- - name_source: tên đang có từ đâu — placeholder (tên tạm "Khách Facebook"), channel (tên hồ sơ trên kênh), staff
--   (nhân viên nhập hay sửa; mặc định cho khách có sẵn). Tên từ kênh không bao giờ đè tên staff.
-- - customer.customers_v (M1 0117, M2 0201): giữ nguyên 15 cột, thứ tự, kiểu; nối do_not_contact, name_source vào cuối
--   (R3; mốc sau nối sau name_source). CREATE OR REPLACE giữ GRANT.
-- - Thứ tự khóa: không bảng mới (nhóm 5 ADR-46).

-- +goose Up
ALTER TABLE customer.customers
    ADD COLUMN do_not_contact    boolean NOT NULL DEFAULT false,
    ADD COLUMN do_not_contact_at timestamptz,
    ADD COLUMN name_source       text    NOT NULL DEFAULT 'staff'
        CHECK (name_source IN ('placeholder', 'channel', 'staff'));

CREATE OR REPLACE VIEW customer.customers_v WITH (security_invoker = true) AS
SELECT id, root_id, full_name, created_at, tenant_id, code, lifecycle, birthday, order_count, delivered_count,
       returned_count, total_spent, last_order_at, updated_at, version, do_not_contact, name_source
FROM customer.customers;

GRANT UPDATE (do_not_contact, do_not_contact_at, name_source) ON customer.customers TO crm_app;
