-- Chỉ số mua hàng của khách (kế hoạch M2, Task M2-25; Q19, 05 mục 1). Tính lại toàn bộ từ dữ liệu gốc — đơn qua
-- ord.orders_v, phiếu trả qua ord.returns_v (ngoại lệ đọc view của customer, không nhập Go order) — của mọi hồ sơ trong
-- nhóm; ghi vào hồ sơ gốc, hồ sơ con về 0. Chạy trong tenancy.WithTenant, sau khi đã khóa cả nhóm (ADR-46 nhóm 5).

-- name: ComputeGroupStats :one
-- order_count: đơn không draft, không cancelled; delivered_count: delivered; returned_count: returned (hoàn qua hãng —
-- khách trả đủ đơn đã giao vẫn để đơn delivered); total_spent: Σ grand_total đơn delivered − Σ refund_amount phiếu
-- customer_return/exchange đã restocked hoặc closed của các đơn đó, không âm; last_order_at: created_at lớn nhất của
-- các đơn được đếm.
WITH o AS (
    SELECT id, status, grand_total, created_at
    FROM ord.orders_v
    WHERE customer_id = ANY (@ids::uuid[])
),
refunds AS (
    SELECT coalesce(sum(r.refund_amount), 0)::bigint AS total
    FROM ord.returns_v AS r
    JOIN o ON o.id = r.order_id AND o.status = 'delivered'
    WHERE r.kind IN ('customer_return', 'exchange')
      AND r.status IN ('restocked', 'closed')
)
SELECT
    (count(*) FILTER (WHERE o.status NOT IN ('draft', 'cancelled')))::int AS order_count,
    (count(*) FILTER (WHERE o.status = 'delivered'))::int                 AS delivered_count,
    (count(*) FILTER (WHERE o.status = 'returned'))::int                  AS returned_count,
    greatest(coalesce(sum(o.grand_total) FILTER (WHERE o.status = 'delivered'), 0)::bigint
        - (SELECT total FROM refunds), 0)::bigint                         AS total_spent,
    -- Không đơn được đếm → 'epoch' (bên gọi dùng order_count = 0 để ghi NULL; sqlc không sinh kiểu rỗng cho max()).
    coalesce(max(o.created_at) FILTER (WHERE o.status NOT IN ('draft', 'cancelled')), 'epoch')::timestamptz
                                                                          AS last_order_at
FROM o;

-- name: SetRootStats :exec
-- Ghi chỉ số vào hồ sơ gốc. Không tăng version (version là của người sửa hồ sơ; chỉ số đổi không làm hỏng lần sửa
-- đang mở).
UPDATE customer.customers
SET order_count      = @order_count,
    delivered_count  = @delivered_count,
    returned_count   = @returned_count,
    total_spent      = @total_spent,
    last_order_at    = sqlc.narg(last_order_at)::timestamptz,
    stats_updated_at = @now::timestamptz
WHERE id = @id;

-- name: ZeroMemberStats :exec
-- Hồ sơ con của nhóm (chỉ số chỉ đúng trên hồ sơ đại diện): về 0, chỉ dòng còn số.
UPDATE customer.customers
SET order_count      = 0,
    delivered_count  = 0,
    returned_count   = 0,
    total_spent      = 0,
    last_order_at    = NULL,
    stats_updated_at = @now::timestamptz
WHERE id = ANY (@ids::uuid[])
  AND (order_count <> 0 OR delivered_count <> 0 OR returned_count <> 0 OR total_spent <> 0 OR last_order_at IS NOT NULL);

-- name: GetStats :one
SELECT order_count, delivered_count, returned_count, total_spent, last_order_at
FROM customer.customers
WHERE id = @id;
