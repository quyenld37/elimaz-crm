-- Vận đơn và luồng thay đổi của đơn web cho website (kế hoạch M5, Task 196 / M5-13). Chỉ đọc view đã công bố
-- (docs/views.md): ord.orders_v, fulfillment.shipments_v, fulfillment.carrier_accounts_v. Chạy trong tenancy.WithTenant.

-- name: WebShipment :one
-- Phần vận đơn khách được thấy của vận đơn id (vận đơn hiện hành của một đơn web); tên hãng là tên tài khoản hãng.
SELECT s.status::text AS status, s.carrier_key::text AS carrier_key, coalesce(a.name, '')::text AS carrier_name,
       coalesce(s.tracking_code, '')::text AS tracking_code, s.picked_up_at, s.delivered_at
FROM fulfillment.shipments_v AS s
LEFT JOIN fulfillment.carrier_accounts_v AS a ON a.id = s.carrier_account_id
WHERE s.id = @id AND s.seller_id = @seller_id;

-- name: WebOrderChanges :many
-- Đơn web (có mã ngoài; đơn kênh web nhân viên nhập không mã ngoài thì website không biết) của người bán seller_id có (changed_at, id) > (since, after_id), changed_at là lúc muộn hơn của lần đổi đơn
-- và lần đổi trạng thái vận đơn hiện hành; xếp tăng dần, tối đa lim dòng. Ứng viên lấy từ hai index (0509) rồi hợp.
WITH cand AS (
    SELECT o.id
    FROM ord.orders_v AS o
    WHERE o.seller_id = @seller_id AND o.channel = 'web' AND o.external_ref IS NOT NULL AND o.updated_at >= @since
    UNION
    SELECT o.id
    FROM fulfillment.shipments_v AS s
    JOIN ord.orders_v AS o ON o.current_shipment_id = s.id AND o.seller_id = s.seller_id
    WHERE s.seller_id = @seller_id AND s.last_status_at >= @since AND o.channel = 'web'
      AND o.external_ref IS NOT NULL
), changed AS (
    SELECT o.id, o.external_ref::text AS external_ref, o.status, o.pending_reason, o.packing_status,
           coalesce(s.status, '')::text AS shipment_status, s.delivered_at,
           greatest(o.updated_at, coalesce(s.last_status_at, o.updated_at))::timestamptz AS changed_at
    FROM cand
    JOIN ord.orders_v AS o ON o.id = cand.id AND o.seller_id = @seller_id
    LEFT JOIN fulfillment.shipments_v AS s ON s.id = o.current_shipment_id AND s.seller_id = o.seller_id
)
SELECT id, external_ref, status, pending_reason, packing_status, shipment_status, delivered_at, changed_at
FROM changed
WHERE (changed_at, id) > (@since::timestamptz, @after_id::uuid)
ORDER BY changed_at, id
LIMIT @lim;
