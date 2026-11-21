-- Gửi hãng: lệnh tạo vận đơn, "không rõ", gửi lại (kế hoạch M3, Task M3-12). Chạy trong tenancy.WithTenant / WriteTx.
-- Thứ tự khóa ADR-46: ord.orders (nhóm 7, qua order.LockSnapshot) → carrier_accounts FOR SHARE, shipments (nhóm 8) →
-- shipment_anomalies → platform.jobs (nhóm 13, qua queue/commands, cuối cùng).

-- name: OrderShipRef :one
-- Người bán, kho của đơn (view công bố của order), đọc không khóa để khóa đơn đúng người bán; không có → pgx.ErrNoRows.
SELECT seller_id, warehouse_id
FROM ord.orders_v
WHERE id = @id;

-- name: ShareAccountForShip :one
-- Tài khoản hãng dùng để gửi, khóa chia sẻ tới hết transaction (đổi trạng thái tài khoản chờ vận đơn chèn xong).
SELECT id, seller_id, warehouse_id, carrier_key, status, mode, default_service, default_options
FROM fulfillment.carrier_accounts
WHERE id = @id
FOR SHARE;

-- name: QuoteForShip :one
SELECT id, order_id, carrier_account_id, status, service, input_hash, valid_until
FROM fulfillment.shipping_quotes
WHERE id = @id;

-- name: InsertShipmentForShip :one
-- Chèn vận đơn creating (create_state pending, lần gửi 1) kèm bản chụp địa chỉ giao và kiện; trùng mã hay đơn đã có vận
-- đơn chưa hủy (unique một phần) → không dòng nào (pgx.ErrNoRows).
INSERT INTO fulfillment.shipments
    (id, tenant_id, seller_id, code, order_id, carrier_key, carrier_account_id, status, cod_amount, created_by, mode,
     service, quote_id, weight_grams, length_cm, width_cm, height_cm, declared_value, options, ship_to,
     create_state, create_attempts)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @code, @order_id, @carrier_key,
        @carrier_account_id, 'creating', @cod_amount, sqlc.narg(created_by)::uuid, @mode, @service,
        sqlc.narg(quote_id)::uuid, sqlc.narg(weight_grams)::int, sqlc.narg(length_cm)::int, sqlc.narg(width_cm)::int,
        sqlc.narg(height_cm)::int, @declared_value, @options, @ship_to, 'pending', 1)
ON CONFLICT DO NOTHING
RETURNING created_at;

-- name: SetShipmentCreateJob :exec
-- Ghi mã lệnh tạo của lần gửi hiện tại (dòng vừa chèn hay vừa khóa trong cùng transaction).
UPDATE fulfillment.shipments SET create_job_id = @job_id WHERE id = @id;

-- name: CreateTarget :one
-- Vận đơn cần gửi kèm chế độ tài khoản và trạng thái đơn (view công bố của order) (Execute, Lookup của lệnh tạo; đọc
-- không khóa, ngoài transaction của lệnh).
SELECT s.id, s.code, s.order_id, s.seller_id, s.carrier_key, s.carrier_account_id, s.status, s.create_state,
       s.create_job_id, s.cod_amount, s.service, s.options, s.ship_to, s.weight_grams, s.length_cm, s.width_cm,
       s.height_cm, s.declared_value, a.mode, o.status AS order_status
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
JOIN ord.orders_v o ON o.tenant_id = s.tenant_id AND o.id = s.order_id
WHERE s.id = @id;

-- name: MarkCreateRejected :execrows
-- Execute: hãng chắc chắn từ chối lần gửi của lệnh job_id → ghi dấu trước khi lệnh thành dead (OnTerminal đọc dấu để
-- hủy vận đơn). Chỉ khi vận đơn còn chờ kết quả của đúng lệnh đó (vận đơn của M1 chưa có create_job_id).
UPDATE fulfillment.shipments
SET create_state = 'rejected', updated_at = now()
WHERE id = @id AND status = 'creating' AND create_state = 'pending'
  AND (create_job_id IS NULL OR create_job_id = @job_id);

-- name: LockShipmentForCreate :one
SELECT id, order_id, seller_id, carrier_account_id, status, create_state, create_job_id, create_attempts,
       cancel_intent, cod_amount, version
FROM fulfillment.shipments
WHERE id = @id
FOR UPDATE;

-- name: MarkCreateUnknown :exec
-- Lệnh tạo vào in_doubt: "không rõ hãng đã tạo chưa", cần người xem (giữ mốc in_doubt lần đầu). Dòng đã khóa.
UPDATE fulfillment.shipments
SET create_state = 'unknown', create_in_doubt_at = COALESCE(create_in_doubt_at, now()), needs_review = true,
    updated_at = now()
WHERE id = @id AND status = 'creating';

-- name: MarkCreateFound :exec
-- Tra lại thấy hãng đã có kiện nhưng chưa có mã hãng (ResolveInDoubt ra done): created ngay để không ai chọn "không tạo"
-- hay "Gửi lại" nữa; lệnh tra trạng thái điền mã sau. Dòng đã khóa.
UPDATE fulfillment.shipments
SET create_state = 'created', create_in_doubt_at = NULL, updated_at = now()
WHERE id = @id AND status = 'creating' AND create_state = 'unknown';

-- name: MarkCreateNotSent :one
-- Chắc chắn hãng chưa tạo (lệnh dead do chưa gửi, tra lại xác nhận chưa có, nhân viên xác nhận) → not_sent, xóa dấu
-- in_doubt; vận đơn vẫn creating, vẫn gắn đơn, chờ người bấm "Gửi lại". bump = 1 khi nhân viên đổi (version + 1).
UPDATE fulfillment.shipments
SET create_state = 'not_sent', create_in_doubt_at = NULL, updated_at = now(),
    version = version + sqlc.arg(bump)::bigint
WHERE id = @id AND status = 'creating'
RETURNING version;

-- name: CancelEndedCreate :one
-- Lệnh tạo kết thúc mà hãng chắc chắn không có kiện và vận đơn phải hủy (hãng từ chối, hay đơn đã yêu cầu hủy /
-- đổi hãng trong lúc gửi): cancelled với loại hủy cancel_kind, create_state theo kết quả. Dòng đã khóa.
UPDATE fulfillment.shipments
SET status = 'cancelled', cancel_kind = @cancel_kind, create_state = @create_state, create_in_doubt_at = NULL,
    last_status_at = now(), updated_at = now(), version = version + 1
WHERE id = @id AND status = 'creating'
RETURNING version;

-- name: ResendShipmentCreate :one
-- "Gửi lại": vận đơn creating mà chắc chắn hãng chưa tạo → lần gửi mới (create_attempts + 1), chờ kết quả. Không đúng
-- trạng thái → pgx.ErrNoRows.
UPDATE fulfillment.shipments
SET create_state = 'pending', create_attempts = create_attempts + 1, create_in_doubt_at = NULL, updated_at = now(),
    version = version + 1
WHERE id = @id AND status = 'creating' AND create_state = 'not_sent'
RETURNING create_attempts, version;

-- name: ResolveCreateAnomaly :exec
-- Đóng việc "không rõ đã tạo" của vận đơn rồi tính lại needs_review theo việc còn mở.
WITH closed AS (
    UPDATE fulfillment.shipment_anomalies
    SET status = 'resolved', resolution = @resolution, resolved_by = sqlc.narg(resolved_by)::uuid, resolved_at = now(),
        updated_at = now()
    WHERE shipment_id = @shipment_id AND kind = 'in_doubt_create' AND status = 'open'
    RETURNING id
)
UPDATE fulfillment.shipments s
SET needs_review = EXISTS (
        SELECT 1 FROM fulfillment.shipment_anomalies x
        WHERE x.shipment_id = s.id AND x.status = 'open' AND x.id NOT IN (SELECT id FROM closed)
    ),
    updated_at = now()
WHERE s.id = @shipment_id;

-- name: ShipmentViewByID :one
-- Một vận đơn cho màn (ShipmentView, không ship_to) kèm kho của tài khoản (phạm vi quyền).
SELECT s.id, s.code, s.order_id, s.carrier_key, s.carrier_account_id, a.name AS account_name, a.mode, s.tracking_code,
       s.service, s.status, s.sub_status, s.create_state, s.cancel_kind, s.cod_amount, s.cod_collected_amount, s.fee,
       s.fee_actual, s.weight_grams, s.options, s.picked_up_at, s.delivered_at, s.last_status_at,
       s.last_carrier_event_at, s.last_synced_at, s.last_sync_error, s.stuck_since, s.needs_review,
       (s.label_file_id IS NOT NULL)::boolean AS label_ready, s.partial, s.version, s.created_at, a.warehouse_id
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.id = @id;

-- name: OrderShipmentViews :many
-- Mọi vận đơn của đơn (cả đã hủy), mới nhất trước (id uuidv7 giảm dần), trang sau after.
SELECT s.id, s.code, s.order_id, s.carrier_key, s.carrier_account_id, a.name AS account_name, a.mode, s.tracking_code,
       s.service, s.status, s.sub_status, s.create_state, s.cancel_kind, s.cod_amount, s.cod_collected_amount, s.fee,
       s.fee_actual, s.weight_grams, s.options, s.picked_up_at, s.delivered_at, s.last_status_at,
       s.last_carrier_event_at, s.last_synced_at, s.last_sync_error, s.stuck_since, s.needs_review,
       (s.label_file_id IS NOT NULL)::boolean AS label_ready, s.partial, s.version, s.created_at, a.warehouse_id
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE s.order_id = @order_id AND (sqlc.narg(after)::uuid IS NULL OR s.id < sqlc.narg(after)::uuid)
ORDER BY s.id DESC
LIMIT @lim;
