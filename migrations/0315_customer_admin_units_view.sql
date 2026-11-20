-- View công bố danh mục đơn vị hành chính (kế hoạch M3, Task M3-18, Q-19): fulfillment đọc mọi tỉnh, xã của bộ 2025
-- để khớp với địa giới của từng hãng và liệt kê mã chưa khớp, mà không nhập gói customer (ranh giới M3: fulfillment
-- chỉ gọi order; ngoại lệ "chỉ đọc view" như inventory.warehouses_v). Pha mở rộng: chỉ thêm view và quyền đọc.
-- Bảng không có tenant_id (dữ liệu tham chiếu chung); security_invoker giữ quyền của người đọc. Số 0315: 0313 để cho
-- Task 101 chạy song song, 0314 là của Task 120 (goose cho chạy lệch số, Task 318).

-- +goose Up
CREATE VIEW customer.admin_units_v WITH (security_invoker = true) AS
SELECT code, level, parent_code, name
FROM customer.admin_units;

GRANT SELECT ON customer.admin_units_v TO crm_app;
