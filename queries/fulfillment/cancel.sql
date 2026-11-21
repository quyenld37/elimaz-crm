-- Hủy vận đơn, đổi hãng, hủy đơn đã gửi hãng (kế hoạch M3, Task M3-14). Chạy trong tenancy.WithTenant / WriteTx. Thứ
-- tự khóa ADR-46: ord.orders (nhóm 7, qua order.LockSnapshot) → shipments (nhóm 8) → shipment_anomalies →
-- platform.jobs (nhóm 13, qua queue/commands, cuối cùng).

-- name: LockShipmentForCancel :one
-- Khóa vận đơn (chỉ dòng vận đơn) kèm kho của tài khoản hãng (phạm vi quyền, báo hiệu); đơn đã khóa trước.
SELECT s.id, s.order_id, s.seller_id, s.carrier_account_id, s.carrier_key, s.mode, s.status, s.create_state,
       s.create_job_id, s.create_attempts, s.cancel_intent, s.cod_amount, s.version, s.tracking_code, a.warehouse_id
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.id = @id
FOR UPDATE OF s;

-- name: RequestShipmentCancel :one
-- Vận đơn chờ thu gom (dòng đã khóa) → cancel_requested với ý định hủy; không đúng trạng thái → pgx.ErrNoRows.
UPDATE fulfillment.shipments
SET status = 'cancel_requested', cancel_intent = @cancel_intent, last_status_at = now(), updated_at = now(),
    version = version + 1
WHERE id = @id AND status = 'awaiting_pickup'
RETURNING version;

-- name: SetShipmentCancelIntent :one
-- Ghi ý định hủy (lệnh tạo còn chờ: creating; đang hủy để đổi hãng mà khách hủy đơn: cancel_requested replace → order_cancel).
UPDATE fulfillment.shipments
SET cancel_intent = @cancel_intent, updated_at = now(), version = version + 1
WHERE id = @id AND status IN ('creating', 'cancel_requested')
RETURNING version;

-- name: CancelTarget :one
-- Vận đơn của lệnh hủy kèm chế độ tài khoản (Execute, Lookup; đọc không khóa, ngoài transaction của lệnh).
SELECT s.id, s.code, s.carrier_key, s.carrier_account_id, s.status, s.tracking_code, a.mode
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.id = @id;

-- name: ShipmentLastEvent :one
-- Mốc sự kiện hãng mới nhất đã xét của vận đơn (sự kiện tổng hợp của lệnh hủy không được cũ hơn mốc này).
SELECT last_carrier_event_at FROM fulfillment.shipments WHERE id = @id;

-- name: ResolveCancelAnomaly :exec
-- Đóng việc "không rõ hãng đã hủy chưa" (in_doubt_cancel) khi lệnh hủy có kết quả (tra lại ra done, hay chắc chưa
-- hủy) rồi tính lại needs_review theo việc còn mở.
WITH closed AS (
    UPDATE fulfillment.shipment_anomalies
    SET status = 'resolved', resolution = @resolution, resolved_at = now(), updated_at = now()
    WHERE shipment_id = @shipment_id AND kind = 'in_doubt_cancel' AND status = 'open'
    RETURNING id
)
UPDATE fulfillment.shipments s
SET needs_review = EXISTS (
        SELECT 1 FROM fulfillment.shipment_anomalies x
        WHERE x.shipment_id = s.id AND x.status = 'open' AND x.id NOT IN (SELECT id FROM closed)
    ),
    updated_at = now()
WHERE s.id = @shipment_id;
