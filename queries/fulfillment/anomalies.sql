-- Vận đơn cần xem (kế hoạch M3, Task M3-08). Chạy trong tenancy.WithTenant, sau khi đã khóa vận đơn.

-- name: OpenShipmentAnomaly :execrows
-- Mở bất thường loại kind cho vận đơn; đã có bất thường mở cùng loại → 0 dòng, không lỗi. detail chỉ mang mã, trạng
-- thái, số tiền.
INSERT INTO fulfillment.shipment_anomalies (tenant_id, shipment_id, kind, detail)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @shipment_id, @kind, @detail)
ON CONFLICT (tenant_id, shipment_id, kind) WHERE status = 'open' DO NOTHING;
