-- Sự kiện vận đơn chỉ ghi thêm (kế hoạch M1, Task M1-31). Chạy trong tenancy.WithTenant.

-- name: InsertShipmentEvent :execrows
-- Trùng (shipment_id, event_key) → 0 dòng, không lỗi.
INSERT INTO fulfillment.shipment_events (tenant_id, shipment_id, event_key, raw_status, status, occurred_at, raw)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @shipment_id, @event_key, @raw_status, @status,
        @occurred_at, @raw)
ON CONFLICT (tenant_id, shipment_id, event_key) DO NOTHING;
