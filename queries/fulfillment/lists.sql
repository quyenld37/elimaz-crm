-- Đơn kẹt, "Cần xem" (kế hoạch M3, Task M3-20). Chạy trong tenancy.WithTenant. Vận đơn mở: mọi trạng thái trừ
-- delivered, returned, cancelled (viết bằng hằng để planner dùng index một phần shipments_open_status). Phạm vi kho:
-- kho của tài khoản hãng (all_warehouses, hoặc warehouse_ids).

-- name: StuckCreatingJobs :many
-- Vận đơn creating (mode api) còn chờ kết quả lệnh tạo theo dữ liệu của fulfillment (pending, unknown): lịch đọc trạng
-- thái lệnh (commands.Status) để biết lệnh đã expired/dead mà không qua OnTerminal (bỏ tay, Task 408).
SELECT s.id, s.create_job_id::uuid AS create_job_id
FROM fulfillment.shipments s
WHERE s.status = 'creating' AND s.mode = 'api' AND s.create_state IN ('pending', 'unknown')
  AND s.create_job_id IS NOT NULL
ORDER BY s.id;

-- name: SyncStuckShipments :many
-- Đánh dấu / bỏ dấu kẹt của mọi vận đơn có dấu khác với điều kiện kẹt lúc now: kẹt khi vận đơn mở, mode api, và (sự
-- kiện cuối của hãng — không có thì mốc đổi trạng thái — cũ hơn hours[trạng thái] giờ; hoặc creating mà lệnh tạo đã
-- kết thúc chắc chưa gửi (not_sent) hay nằm trong ended_creating; hoặc có anomaly stock_short mở). Đánh dấu ghi
-- stuck_since = now (chỉ khi đang rỗng), bỏ dấu ghi NULL. Khóa dòng theo id tăng dần, bỏ qua dòng đang bị khóa (lượt
-- sau làm); điều kiện nằm trong WHERE của câu khóa nên được kiểm lại trên bản mới nhất của dòng. Trả dấu mới mỗi dòng.
WITH todo AS (
    SELECT s.id
    FROM fulfillment.shipments s
    WHERE (s.status IN ('creating', 'awaiting_pickup', 'shipping', 'delivery_failed', 'returning', 'cancel_requested')
           OR s.stuck_since IS NOT NULL)
      AND coalesce(
            s.status IN ('creating', 'awaiting_pickup', 'shipping', 'delivery_failed', 'returning', 'cancel_requested')
            AND s.mode = 'api'
            AND (coalesce(s.last_carrier_event_at, s.last_status_at)
                     < sqlc.arg(now)::timestamptz
                       - jsonb_extract_path_text(sqlc.arg(hours)::jsonb, s.status)::int * interval '1 hour'
                 OR (s.status = 'creating'
                     AND (s.create_state = 'not_sent' OR s.id = ANY (sqlc.arg(ended_creating)::uuid[])))
                 OR EXISTS (SELECT 1 FROM fulfillment.shipment_anomalies x
                            WHERE x.shipment_id = s.id AND x.kind = 'stock_short' AND x.status = 'open')),
            false) IS DISTINCT FROM (s.stuck_since IS NOT NULL)
    ORDER BY s.id
    FOR UPDATE OF s SKIP LOCKED
)
UPDATE fulfillment.shipments s
SET stuck_since = CASE WHEN s.stuck_since IS NULL THEN sqlc.arg(now)::timestamptz END, updated_at = now()
FROM todo
WHERE s.id = todo.id
RETURNING (s.stuck_since IS NOT NULL)::bool AS stuck;

-- name: CountStuckByStatus :many
-- Số vận đơn mở đang kẹt theo trạng thái (cảnh báo ngày: chỉ số lượng).
SELECT s.status, count(*) AS n
FROM fulfillment.shipments s
WHERE s.stuck_since IS NOT NULL
  AND s.status IN ('creating', 'awaiting_pickup', 'shipping', 'delivery_failed', 'returning', 'cancel_requested')
GROUP BY s.status
ORDER BY s.status;

-- name: ListStuckShipments :many
-- Một trang đơn kẹt (vận đơn mở có stuck_since) của người bán seller_id trong phạm vi kho, kẹt lâu nhất trước (stuck_since, id); lọc trạng thái,
-- tài khoản hãng; after_at/after_id: con trỏ trang trước. lim = cỡ trang + 1 (bên gọi biết còn trang sau).
SELECT s.id, s.order_id, o.code AS order_code, s.carrier_key, a.name AS account_name, s.tracking_code, s.status,
       s.sub_status, s.stuck_since::timestamptz AS stuck_since, s.last_carrier_event_at, s.last_status_at,
       s.last_synced_at, s.last_sync_error
FROM fulfillment.shipments s
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
JOIN ord.orders_v o ON o.tenant_id = s.tenant_id AND o.id = s.order_id
WHERE s.stuck_since IS NOT NULL
  AND s.seller_id = @seller_id
  AND s.status IN ('creating', 'awaiting_pickup', 'shipping', 'delivery_failed', 'returning', 'cancel_requested')
  AND (sqlc.narg(status)::text IS NULL OR s.status = sqlc.narg(status)::text)
  AND (sqlc.narg(account_id)::uuid IS NULL OR s.carrier_account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.arg(all_warehouses)::bool OR a.warehouse_id = ANY (sqlc.arg(warehouse_ids)::uuid[]))
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (s.stuck_since, s.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY s.stuck_since, s.id
LIMIT sqlc.arg(lim)::int;

-- name: ListOpenAnomalies :many
-- Một trang việc "Cần xem" (anomaly mở) của người bán seller_id trong phạm vi kho, cũ nhất trước (created_at, id); con trỏ như
-- ListStuckShipments. detail chỉ mang mã, trạng thái, số tiền.
SELECT x.id, x.shipment_id, s.order_id, o.code AS order_code, x.kind, x.detail, x.created_at
FROM fulfillment.shipment_anomalies x
JOIN fulfillment.shipments s ON s.tenant_id = x.tenant_id AND s.id = x.shipment_id
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
JOIN ord.orders_v o ON o.tenant_id = s.tenant_id AND o.id = s.order_id
WHERE x.status = 'open'
  AND s.seller_id = @seller_id
  AND (sqlc.arg(all_warehouses)::bool OR a.warehouse_id = ANY (sqlc.arg(warehouse_ids)::uuid[]))
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (x.created_at, x.id) > (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY x.created_at, x.id
LIMIT sqlc.arg(lim)::int;

-- name: ReviewCounts :one
-- Số đếm của các danh sách vận hành của người bán seller_id trong phạm vi kho: anomaly mở, vận đơn mở kẹt, việc gọi lại mở (open, in_progress),
-- trong đó quá hạn (due_at < now), anomaly "không rõ" mở (loại thuộc in_doubt_kinds: lists.InDoubtKinds). Mỗi số đi
-- từ tập nhỏ (anomaly mở, vận đơn có stuck_since, việc gọi lại mở) qua index rồi mới nối vận đơn, tài khoản hãng.
SELECT
    (SELECT count(*) FROM fulfillment.shipment_anomalies x
     JOIN fulfillment.shipments s ON s.tenant_id = x.tenant_id AND s.id = x.shipment_id
     JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
     WHERE x.status = 'open' AND s.seller_id = sqlc.arg(seller_id)::uuid
       AND (sqlc.arg(all_warehouses)::bool OR a.warehouse_id = ANY (sqlc.arg(warehouse_ids)::uuid[]))) AS review,
    (SELECT count(*) FROM fulfillment.shipments s
     JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
     WHERE s.stuck_since IS NOT NULL AND s.seller_id = sqlc.arg(seller_id)::uuid
       AND s.status IN ('creating', 'awaiting_pickup', 'shipping', 'delivery_failed', 'returning', 'cancel_requested')
       AND (sqlc.arg(all_warehouses)::bool OR a.warehouse_id = ANY (sqlc.arg(warehouse_ids)::uuid[]))) AS stuck,
    (SELECT count(*) FROM fulfillment.callback_tasks c
     JOIN fulfillment.shipments s ON s.tenant_id = c.tenant_id AND s.id = c.shipment_id
     JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
     WHERE c.status IN ('open', 'in_progress') AND s.seller_id = sqlc.arg(seller_id)::uuid
       AND (sqlc.arg(all_warehouses)::bool OR a.warehouse_id = ANY (sqlc.arg(warehouse_ids)::uuid[]))) AS callbacks_open,
    (SELECT count(*) FROM fulfillment.callback_tasks c
     JOIN fulfillment.shipments s ON s.tenant_id = c.tenant_id AND s.id = c.shipment_id
     JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
     WHERE c.status IN ('open', 'in_progress') AND s.seller_id = sqlc.arg(seller_id)::uuid AND c.due_at < sqlc.arg(now)::timestamptz
       AND (sqlc.arg(all_warehouses)::bool OR a.warehouse_id = ANY (sqlc.arg(warehouse_ids)::uuid[])))
        AS callbacks_overdue,
    (SELECT count(*) FROM fulfillment.shipment_anomalies x
     JOIN fulfillment.shipments s ON s.tenant_id = x.tenant_id AND s.id = x.shipment_id
     JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
     WHERE x.status = 'open' AND s.seller_id = sqlc.arg(seller_id)::uuid AND x.kind = ANY (sqlc.arg(in_doubt_kinds)::text[])
       AND (sqlc.arg(all_warehouses)::bool OR a.warehouse_id = ANY (sqlc.arg(warehouse_ids)::uuid[]))) AS in_doubt;

-- name: AnomalyForResolve :one
-- Anomaly cần đóng kèm kho của tài khoản hãng của vận đơn (đọc không khóa: bên gọi khóa vận đơn rồi đọc lại).
SELECT x.id, x.shipment_id, a.warehouse_id
FROM fulfillment.shipment_anomalies x
JOIN fulfillment.shipments s ON s.tenant_id = x.tenant_id AND s.id = x.shipment_id
JOIN fulfillment.carrier_accounts a ON a.tenant_id = s.tenant_id AND a.id = s.carrier_account_id
WHERE x.id = @id;

-- name: LockShipmentForAnomaly :exec
-- Khóa vận đơn trước anomaly của nó (ADR-46 nhóm 8: shipments → shipment_anomalies).
SELECT id FROM fulfillment.shipments WHERE id = @id FOR UPDATE;

-- name: AnomalyKindState :one
-- Loại, trạng thái hiện tại của anomaly (sau khi đã khóa vận đơn).
SELECT kind, status FROM fulfillment.shipment_anomalies WHERE id = @id;

-- name: ResolveAnomaly :one
-- Đóng anomaly mở id (resolution, người đóng) rồi tính lại needs_review của vận đơn theo việc còn mở. Đã đóng → 0 dòng.
WITH closed AS (
    UPDATE fulfillment.shipment_anomalies
    SET status = 'resolved', resolution = sqlc.arg(resolution)::text, resolved_by = sqlc.narg(resolved_by)::uuid, resolved_at = now(),
        updated_at = now()
    WHERE id = sqlc.arg(id)::uuid AND status = 'open'
    RETURNING id AS anomaly_id, shipment_id AS anomaly_shipment_id, kind, status, resolution, resolved_at, created_at
), shipment AS (
    UPDATE fulfillment.shipments s
    SET needs_review = EXISTS (
            SELECT 1 FROM fulfillment.shipment_anomalies x
            WHERE x.shipment_id = s.id AND x.status = 'open' AND x.id NOT IN (SELECT cl.anomaly_id FROM closed cl)
        ),
        updated_at = now()
    FROM closed
    WHERE s.id = closed.anomaly_shipment_id
)
SELECT c.anomaly_id AS id, c.anomaly_shipment_id AS shipment_id, c.kind, c.status, c.resolution::text AS resolution,
       c.resolved_at::timestamptz AS resolved_at, c.created_at
FROM closed c;
