-- Vận đơn (kế hoạch M1, Task M1-31; 05 mục 2). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 8; đường bắt
-- đầu từ vận đơn mà đổi đơn khóa ord.orders (nhóm 7, qua order.Lock) trước LockShipment.

-- name: InsertShipment :one
-- Chèn vận đơn creating; trùng mã hay đơn đã có vận đơn chưa hủy (unique một phần) → không dòng nào (pgx.ErrNoRows).
INSERT INTO fulfillment.shipments
    (id, tenant_id, seller_id, code, order_id, carrier_key, carrier_account_id, status, cod_amount, created_by)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @code, @order_id, @carrier_key,
        @carrier_account_id, 'creating', @cod_amount, sqlc.narg(created_by)::uuid)
ON CONFLICT DO NOTHING
RETURNING created_at;

-- name: HasActiveShipment :one
SELECT EXISTS (SELECT 1 FROM fulfillment.shipments WHERE order_id = @order_id AND status <> 'cancelled');

-- name: GetShipment :one
-- Vận đơn kèm tài khoản hãng (kho, người bán, mã tài khoản ở hãng).
SELECT s.id, s.code, s.order_id, s.carrier_key, s.carrier_account_id, s.tracking_code, s.status, s.cod_amount, s.fee,
       s.version, s.created_at, a.warehouse_id, a.seller_id, a.external_account_ref
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.id = @id;

-- name: LockShipment :one
SELECT id, order_id, tracking_code, status, cod_amount, version
FROM fulfillment.shipments
WHERE id = @id
FOR UPDATE;

-- name: ShipmentByTracking :one
-- Vận đơn theo mã hãng của đúng tài khoản (tín hiệu webhook); không có → pgx.ErrNoRows.
SELECT s.id
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.carrier_key = @carrier_key AND s.tracking_code = @tracking_code AND a.external_account_ref = @account_ref;

-- name: UpdateShipmentProgress :one
-- Đổi trạng thái, điền mã hãng, phí (chỉ khi còn trống) trên dòng đã khóa; trả version mới. Có mã hãng → hãng đã tạo
-- (create_state = 'created', như dòng M1 được điền ở 0300).
UPDATE fulfillment.shipments
SET status = @status,
    tracking_code = COALESCE(tracking_code, sqlc.narg(tracking_code)::text),
    create_state = CASE WHEN COALESCE(tracking_code, sqlc.narg(tracking_code)::text) IS NOT NULL THEN 'created'
                        ELSE create_state END,
    fee = COALESCE(fee, sqlc.narg(fee)::bigint),
    last_status_at = now(), updated_at = now(), version = version + 1
WHERE id = @id
RETURNING version;

-- name: CancelCreatingShipment :one
-- Vận đơn còn creating (dòng đã khóa) → cancelled khi lệnh tạo kết thúc mà chắc chắn hãng chưa tạo; trả version mới.
-- Không còn creating, hay lệnh tạo từng in_doubt (create_in_doubt_at: hãng có thể đã có kiện) → không dòng nào
-- (pgx.ErrNoRows). Loại hủy create_rejected: lệnh tạo kết thúc mà hãng chắc chắn không tạo.
UPDATE fulfillment.shipments
SET status = 'cancelled', cancel_kind = 'create_rejected', last_status_at = now(), updated_at = now(), version = version + 1
WHERE id = @id AND status = 'creating' AND create_in_doubt_at IS NULL
RETURNING version;

-- name: MarkCreateInDoubt :exec
-- Lệnh tạo vào in_doubt: ghi dấu "hãng có thể đã nhận" cho vận đơn còn creating (giữ mốc lần đầu). Không đổi trạng
-- thái, không tăng version (không phải đổi của người dùng, không phát sự kiện).
UPDATE fulfillment.shipments
SET create_in_doubt_at = COALESCE(create_in_doubt_at, now()), updated_at = now()
WHERE id = @id AND status = 'creating';

-- Áp kết quả tra (kế hoạch M3, Task M3-08). Thứ tự khóa: ShipmentOrderRef (không khóa) → order.LockSnapshot (nhóm 7)
-- → LockShipmentForApply (nhóm 8).

-- name: ShipmentOrderRef :one
-- Đơn, người bán của vận đơn, đọc không khóa để khóa đơn trước; không có (hay tenant khác) → pgx.ErrNoRows.
SELECT order_id, seller_id
FROM fulfillment.shipments
WHERE id = @id;

-- name: LockShipmentForApply :one
-- Khóa vận đơn (chỉ dòng vận đơn, FOR UPDATE OF s) kèm kho của tài khoản hãng (phạm vi báo hiệu).
SELECT s.id, s.tenant_id, s.order_id, s.seller_id, s.carrier_key, s.tracking_code, s.status, s.cod_amount, s.version,
       s.last_carrier_event_at, s.picked_up_at, s.cancel_intent, a.warehouse_id
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.id = @id
FOR UPDATE OF s;

-- name: ApplyShipmentStatus :one
-- Áp một bước của máy trạng thái lên dòng đã khóa; trả version mới. Mốc picked_up_at, delivered_at,
-- carrier_reported_returned_at giữ lần đầu; last_carrier_event_at không lùi. Sang cancelled: cancel_kind theo ý định
-- hủy của nhân viên, không có thì hãng tự hủy.
UPDATE fulfillment.shipments
SET status = @status,
    sub_status = @sub_status,
    last_status_at = now(),
    last_carrier_event_at = GREATEST(last_carrier_event_at, @event_at::timestamptz),
    picked_up_at = COALESCE(picked_up_at, sqlc.narg(picked_up_at)::timestamptz),
    delivered_at = COALESCE(delivered_at, sqlc.narg(delivered_at)::timestamptz),
    carrier_reported_returned_at = COALESCE(carrier_reported_returned_at, sqlc.narg(returned_reported_at)::timestamptz),
    cancel_kind = CASE WHEN @status = 'cancelled' THEN COALESCE(cancel_kind, cancel_intent, 'carrier_cancelled')
                       ELSE cancel_kind END,
    updated_at = now(),
    version = version + 1
WHERE id = @id
RETURNING version;

-- name: AdvanceShipmentLastEvent :exec
-- Dời mốc sự kiện mới nhất đã xét (Decision.AdvanceLast mà không áp); không lùi, không tăng version.
UPDATE fulfillment.shipments
SET last_carrier_event_at = GREATEST(last_carrier_event_at, @event_at::timestamptz), updated_at = now()
WHERE id = @id;

-- name: FinishShipmentSync :exec
-- Cuối một lần áp kết quả tra: mốc đồng bộ, xóa lỗi đồng bộ và dấu kẹt; phí thật, tiền COD hãng báo khi có.
UPDATE fulfillment.shipments
SET last_synced_at = @synced_at,
    last_sync_error = NULL,
    stuck_since = NULL,
    fee_actual = COALESCE(sqlc.narg(fee_actual)::bigint, fee_actual),
    cod_collected_amount = COALESCE(sqlc.narg(cod_collected)::bigint, cod_collected_amount),
    updated_at = now()
WHERE id = @id;

-- name: MarkShipmentNeedsReview :exec
UPDATE fulfillment.shipments SET needs_review = true, updated_at = now() WHERE id = @id AND NOT needs_review;

-- name: AttachShipmentTracking :one
-- Vận đơn creating (dòng đã khóa) nhận mã hãng: created, awaiting_pickup, phí (chỉ khi còn trống). version + 2: hai
-- sự kiện shipment.created (v+1) và shipment.status_changed (v+2). Trùng mã của vận đơn khác cùng hãng → 23505
-- shipments_tracking_key (bên gọi chạy trong savepoint).
UPDATE fulfillment.shipments
SET tracking_code = @tracking_code,
    create_state = 'created',
    fee = COALESCE(fee, sqlc.narg(fee)::bigint),
    status = 'awaiting_pickup',
    last_status_at = now(), updated_at = now(), version = version + 2
WHERE id = @id AND status = 'creating'
RETURNING version, fee;
