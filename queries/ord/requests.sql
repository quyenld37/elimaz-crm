-- Yêu cầu về đơn hàng (kế hoạch M5, Task 195 / M5-12). Chạy trong tenancy.WithTenant (RLS). Thứ tự khóa ADR-46 nhóm 7:
-- đơn (LockWebOrderForRequest) trước, rồi chèn order_requests, order_request_messages, order_request_files; đổi trạng
-- thái chỉ khóa dòng yêu cầu (LockOrderRequest).

-- name: LockWebOrderForRequest :one
-- Khóa đơn kênh web của mã ngoài external_ref (người bán seller_id) trước khi kiểm và chèn yêu cầu: các yêu cầu của một
-- đơn được tạo tuần tự (số còn đổi được, một yêu cầu hủy đang mở, giới hạn số yêu cầu mỗi ngày).
SELECT id, status, current_shipment_id
FROM ord.orders
WHERE seller_id = @seller_id AND channel = 'web' AND external_ref = @external_ref::text
FOR UPDATE;

-- name: LockOrderForRequest :one
-- Như LockWebOrderForRequest theo mã đơn (CreateCancelRequest, M5-13).
SELECT id, status, current_shipment_id
FROM ord.orders
WHERE seller_id = @seller_id AND id = @id AND channel = 'web'
FOR UPDATE;

-- name: WebOrderIDByRef :one
-- Mã đơn kênh web của mã ngoài external_ref, không khóa (website đọc yêu cầu của đơn).
SELECT id
FROM ord.orders
WHERE seller_id = @seller_id AND channel = 'web' AND external_ref = @external_ref::text;

-- name: OrderDeliveredAt :one
-- Lần cuối đơn chuyển sang delivered (lịch sử trạng thái chỉ ghi thêm).
SELECT occurred_at
FROM ord.order_status_history
WHERE order_id = @order_id AND to_status = 'delivered'
ORDER BY occurred_at DESC
LIMIT 1;

-- name: OrderItemQuantities :many
-- Số đã mua theo biến thể của đơn (dòng cùng biến thể cộng lại), kèm mã, tên chụp lúc đặt.
SELECT variant_id, sum(quantity)::int AS quantity, min(sku_snapshot)::text AS sku, min(name_snapshot)::text AS name
FROM ord.order_items
WHERE seller_id = @seller_id AND order_id = @order_id
GROUP BY variant_id
ORDER BY variant_id;

-- name: HeldRequestLines :many
-- Dòng của các yêu cầu đổi, trả chưa bị từ chối hay rút của đơn (đang giữ số lượng).
SELECT lines
FROM ord.order_requests
WHERE order_id = @order_id AND kind IN ('exchange', 'return') AND status NOT IN ('rejected', 'cancelled');

-- name: OpenCancelRequestExists :one
SELECT EXISTS (
    SELECT 1 FROM ord.order_requests
    WHERE order_id = @order_id AND kind = 'cancel' AND status IN ('received', 'in_review', 'needs_info')
)::bool;

-- name: CountRecentOrderRequests :one
SELECT count(*)::int
FROM ord.order_requests
WHERE order_id = @order_id AND created_at > @since;

-- name: AttachedFiles :many
-- Tệp trong ids đã gắn vào một yêu cầu.
SELECT file_id
FROM ord.order_request_files
WHERE file_id = ANY (@ids::uuid[]);

-- name: InsertOrderRequest :one
INSERT INTO ord.order_requests (tenant_id, order_id, ref, kind, reason_code, lines, source, customer_note,
                                condition_confirmed, created_at, updated_at)
VALUES (@tenant_id, @order_id, @ref, @kind, @reason_code, @lines::jsonb, @source, sqlc.narg(customer_note)::text,
        @condition_confirmed, @now, @now)
ON CONFLICT DO NOTHING
RETURNING id;

-- name: InsertOrderRequestMessage :one
INSERT INTO ord.order_request_messages (tenant_id, request_id, author_type, author_id, body, visible_to_customer,
                                        created_at)
VALUES (@tenant_id, @request_id, @author_type, sqlc.narg(author_id)::text, @body, @visible_to_customer, @now)
RETURNING id, created_at;

-- name: InsertOrderRequestFile :execrows
-- Tệp đã gắn yêu cầu khác (unique tenant_id, file_id) → 0 dòng.
INSERT INTO ord.order_request_files (tenant_id, request_id, file_id, position)
VALUES (@tenant_id, @request_id, @file_id, @position)
ON CONFLICT DO NOTHING;

-- name: GetOrderRequest :one
SELECT r.id, r.order_id, r.ref, r.kind, r.status, r.reason_code, r.lines, r.source, r.customer_note,
       r.condition_confirmed, r.ship_back, r.outcome, r.created_at, r.updated_at, r.resolved_at, r.resolved_by,
       r.version, o.code AS order_code, o.external_ref AS order_external_ref, o.warehouse_id AS order_warehouse_id
FROM ord.order_requests AS r
JOIN ord.orders AS o ON o.tenant_id = r.tenant_id AND o.id = r.order_id
WHERE r.id = @id AND o.seller_id = @seller_id;

-- name: LockOrderRequest :one
-- Khóa dòng yêu cầu (nhóm 7, sau orders) trước khi so version, trạng thái.
SELECT id, order_id, ref, kind, status, version
FROM ord.order_requests
WHERE id = @id
FOR UPDATE;

-- name: LockOrderRequestByRef :one
-- Khóa yêu cầu ref của đơn order_id (website rút yêu cầu, trả lời).
SELECT id, order_id, ref, kind, status, version
FROM ord.order_requests
WHERE order_id = @order_id AND ref = @ref::text
FOR UPDATE;

-- name: UpdateOrderRequestStatus :one
UPDATE ord.order_requests
SET status      = @status,
    ship_back   = coalesce(sqlc.narg(ship_back)::jsonb, ship_back),
    outcome     = coalesce(sqlc.narg(outcome)::jsonb, outcome),
    resolved_at = sqlc.narg(resolved_at)::timestamptz,
    resolved_by = sqlc.narg(resolved_by)::uuid,
    updated_at  = @now,
    version     = version + 1
WHERE id = @id AND version = @version
RETURNING version;

-- name: TouchOrderRequest :exec
UPDATE ord.order_requests
SET updated_at = @now
WHERE id = @id;

-- name: ListOrderRequestMessages :many
SELECT id, author_type, author_id, body, visible_to_customer, created_at
FROM ord.order_request_messages
WHERE request_id = @request_id
ORDER BY created_at, id;

-- name: ListOrderRequestFiles :many
SELECT file_id
FROM ord.order_request_files
WHERE request_id = @request_id
ORDER BY position;

-- name: ListOrderRequestsOfOrder :many
-- Yêu cầu của một đơn (website), mới trước.
SELECT id
FROM ord.order_requests
WHERE order_id = @order_id
ORDER BY created_at DESC, id DESC;

-- name: ListOrderRequests :many
-- Danh sách cho nhân viên, mới trước (keyset created_at, id). Bộ lọc NULL: không lọc; statuses: một trong các trạng
-- thái. customer_roots: đơn của khách thuộc các nhóm gốc đó (tìm theo tên, không dấu, qua customer.Search). Kèm tên gốc
-- hiện tại của khách (Task 486), số ảnh, hướng dẫn gửi trả (hạn gửi).
SELECT r.id, r.order_id, r.ref, r.kind, r.status, r.reason_code, r.created_at, r.updated_at, r.version, r.ship_back,
       o.code AS order_code, rt.full_name AS customer_name,
       (SELECT count(*) FROM ord.order_request_files AS f WHERE f.request_id = r.id)::int AS photo_count
FROM ord.order_requests AS r
JOIN ord.orders AS o ON o.tenant_id = r.tenant_id AND o.id = r.order_id
LEFT JOIN customer.customers_v AS cu ON cu.tenant_id = o.tenant_id AND cu.id = o.customer_id
LEFT JOIN customer.customers_v AS rt ON rt.tenant_id = cu.tenant_id AND rt.id = cu.root_id
WHERE o.seller_id = @seller_id
  AND (sqlc.narg(statuses)::text[] IS NULL OR r.status = ANY (sqlc.narg(statuses)::text[]))
  AND (sqlc.narg(kind)::text IS NULL OR r.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(ref)::text IS NULL OR r.ref = sqlc.narg(ref)::text)
  AND (sqlc.narg(order_code)::text IS NULL OR o.code = sqlc.narg(order_code)::text)
  AND (sqlc.narg(ship_phone)::text IS NULL OR o.ship_phone = sqlc.narg(ship_phone)::text)
  AND (sqlc.narg(phone_tail)::text IS NULL OR o.ship_phone_reversed LIKE sqlc.narg(phone_tail)::text || '%')
  AND (sqlc.narg(customer_roots)::uuid[] IS NULL OR o.customer_id IN (
        SELECT g.id FROM customer.customers_v AS g WHERE g.root_id = ANY (sqlc.narg(customer_roots)::uuid[])))
  AND (sqlc.narg(after_at)::timestamptz IS NULL
       OR (r.created_at, r.id) < (sqlc.narg(after_at)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim::int;

-- name: CountOrderRequests :many
-- Số yêu cầu theo trạng thái của tenant (Task 486; index order_requests_status). Chỉ đọc bảng yêu cầu: yêu cầu chỉ có
-- trên đơn web của người bán mặc định (tạo qua LockWebOrderForRequest).
SELECT status, count(*)::int AS n
FROM ord.order_requests
GROUP BY status;

-- name: RequestWantVariants :many
-- Biến thể khách muốn đổi sang (Task 486): mã, tên mẫu, màu, cỡ; số còn bán được ở kho warehouse_id của đơn (chưa có
-- dòng tồn → 0) và tên kho (NULL khi không đọc được kho: khi đó bỏ số còn).
SELECT v.variant_id, v.sku, v.product_name, v.color, v.size, coalesce(s.available, 0)::int AS available,
       w.name AS warehouse_name
FROM catalog.variants_v AS v
LEFT JOIN inventory.warehouses_v AS w ON w.warehouse_id = @warehouse_id::uuid
LEFT JOIN inventory.stock_available_v AS s
       ON s.variant_id = v.variant_id AND s.warehouse_id = w.warehouse_id AND s.seller_id = v.seller_id
WHERE v.seller_id = @seller_id AND v.variant_id = ANY (@ids::uuid[]);

-- name: InsertOrderRequestEvent :exec
-- Một dòng lịch sử (chỉ ghi thêm) khi yêu cầu đổi trạng thái (Task 486).
INSERT INTO ord.order_request_events (tenant_id, request_id, from_status, to_status, actor_type, actor_id, created_at)
VALUES (@tenant_id, @request_id, @from_status, @to_status, @actor_type, sqlc.narg(actor_id)::text, @now);

-- name: ListOrderRequestEvents :many
SELECT from_status, to_status, actor_type, actor_id, created_at
FROM ord.order_request_events
WHERE request_id = @request_id
ORDER BY created_at, id;
