-- Chỉ mục cho danh sách "Cần xem" (kế hoạch M3, Task M3-20): việc mở cũ nhất trước, trang theo (created_at, id)
-- (queries/fulfillment/lists.sql ListOpenAnomalies). Unique một phần shipment_anomalies_open (theo vận đơn, loại) không
-- sắp được theo thời gian. Pha mở rộng: chỉ thêm chỉ mục. Không CONCURRENTLY (lý do như 0307: chờ khóa migrate làm
-- hai lần migrate song song kẹt nhau; bảng còn nhỏ). Đơn kẹt dùng index có sẵn của 0300 (shipments_open_status,
-- shipments_stuck); test ExplainAsApp ở lists_stuck_test.go.

-- +goose Up
CREATE INDEX shipment_anomalies_open_created ON fulfillment.shipment_anomalies (tenant_id, created_at, id)
    WHERE status = 'open';
