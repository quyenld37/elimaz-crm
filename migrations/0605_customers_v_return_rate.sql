-- Tỷ lệ hoàn của khách trên view công bố (kế hoạch M6, Task 238 / M6-15; G10, A2-13, R3). Pha mở rộng: chỉ nối một
-- cột vào cuối customer.customers_v, không đổi bảng.
-- - Định nghĩa chép nguyên bản hiện hành (0403: 4 cột M1, 11 cột M2, do_not_contact, name_source của M4), giữ tên, thứ
--   tự, kiểu mọi cột cũ (giới hạn của CREATE OR REPLACE VIEW), nối return_rate_bp sau name_source (mốc sau nối sau).
-- - return_rate_bp: hoàn × 10000 / (giao xong + hoàn), chia nguyên như customer.statsOf (Stats.ReturnRateBP của M2,
--   Q-X2); NULL khi mẫu số 0 (chưa có đơn kết thúc). Chỉ đúng ở dòng id = root_id như các cột chỉ số khác.
-- - CREATE OR REPLACE giữ GRANT SELECT cho crm_app (0201); security_invoker giữ RLS của bảng gốc.
-- - Thứ tự khóa: không bảng mới (nhóm 5 ADR-46).

-- +goose Up
CREATE OR REPLACE VIEW customer.customers_v WITH (security_invoker = true) AS
SELECT id, root_id, full_name, created_at, tenant_id, code, lifecycle, birthday, order_count, delivered_count,
       returned_count, total_spent, last_order_at, updated_at, version, do_not_contact, name_source,
       (CASE WHEN delivered_count + returned_count > 0
             THEN returned_count * 10000 / (delivered_count + returned_count) END)::int AS return_rate_bp
FROM customer.customers;
