-- Chỉ mục phục vụ báo cáo, thuộc module chủ bảng, pha mở rộng, không đổi dữ liệu (kế hoạch M6, Task 226 / M6-03; G16
-- sau Task 223). Ba chỉ mục còn thiếu cho câu lọc theo khoảng thời gian của báo cáo M6:
-- - ord.order_status_history theo trạng thái đích và mốc đổi (đơn lên trạng thái X trong khoảng ngày; M1 chỉ có chỉ
--   mục theo đơn).
-- - inventory.stock_ledger theo mốc ghi sổ (xuất nhập tồn, chụp tồn; M1 chỉ có theo biến thể, kho).
-- - ord.returns theo mốc nhận hàng hoàn.
-- fulfillment.shipments (tenant_id, created_at) và catalog.variants (tenant_id, seller_id, sku text_pattern_ops) đã có
-- (0300, 0123). ord.order_items không thêm: mọi câu M6 lọc seller_id (ADR-29), dùng được chỉ mục
-- (tenant_id, seller_id, order_id, …) có sẵn.
-- Không CONCURRENTLY (QĐ-226-1, như 0307, 0328, 0527): CREATE INDEX CONCURRENTLY chờ mọi phiên đang mở, kể cả lần
-- migrate thứ hai đang chờ khóa cố vấn của db.Migrate, nên hai lần migrate song song kẹt nhau
-- (TestMigrateConcurrentRunsApplyOnce); CRM chưa chạy dữ liệu thật, bảng còn nhỏ. IF NOT EXISTS: chỉ mục đã tạo tay
-- CONCURRENTLY trước khi migrate thì bỏ qua.

-- +goose Up
CREATE INDEX IF NOT EXISTS order_status_history_report
    ON ord.order_status_history (tenant_id, to_status, occurred_at);
CREATE INDEX IF NOT EXISTS stock_ledger_created ON inventory.stock_ledger (tenant_id, created_at);
CREATE INDEX IF NOT EXISTS returns_received ON ord.returns (tenant_id, received_at);
