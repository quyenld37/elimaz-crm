-- Sự kiện vận đơn chỉ ghi thêm (kế hoạch M1, Task M1-31). Chạy trong tenancy.WithTenant.

-- name: InsertTrackedEvent :one
-- Sự kiện của một lần áp kết quả tra (M3, Task M3-08) kèm quyết định của máy trạng thái. Trùng (shipment_id,
-- event_key) → pgx.ErrNoRows (đã xét, bỏ qua).
INSERT INTO fulfillment.shipment_events
    (tenant_id, shipment_id, event_key, raw_status, status, occurred_at, raw, canonical, source, sub_status, applied,
     ignored_reason, actor_id)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @shipment_id, @event_key, @raw_status, @status,
        @occurred_at, @raw, @canonical, @source, @sub_status, @applied, sqlc.narg(ignored_reason)::text,
        sqlc.narg(actor_id)::uuid)
ON CONFLICT (tenant_id, shipment_id, event_key) DO NOTHING
RETURNING id;

-- name: ListShipmentEvents :many
-- Lịch sử vận đơn, mới nhất trước (occurred_at, id giảm dần); after_id: dòng cuối trang trước (không có → từ đầu).
SELECT e.id, e.occurred_at, e.recorded_at, COALESCE(e.source, 'carrier_api')::text AS source, e.raw_status,
       e.status, e.applied, e.ignored_reason
FROM fulfillment.shipment_events e
WHERE e.shipment_id = @shipment_id
  AND (sqlc.narg(after_id)::uuid IS NULL OR (e.occurred_at, e.id) < (
        SELECT a.occurred_at, a.id FROM fulfillment.shipment_events a
        WHERE a.shipment_id = @shipment_id AND a.id = sqlc.narg(after_id)::uuid))
ORDER BY e.occurred_at DESC, e.id DESC
LIMIT @lim;

-- name: ShipmentEventExists :one
-- Dòng id có phải sự kiện của vận đơn shipment_id không (kiểm con trỏ trang).
SELECT EXISTS (SELECT 1 FROM fulfillment.shipment_events WHERE shipment_id = @shipment_id AND id = @id);
