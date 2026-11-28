-- Người bán của đối tượng mà API gọi theo mã (ADR-29, Task 527; CheckSeller): đọc theo khóa chính, không khóa.
-- seller_id của vận đơn không bao giờ đổi, nên kiểm trước khi khóa vẫn đúng.

-- name: SellerOfShipment :one
SELECT seller_id FROM fulfillment.shipments WHERE id = @id;

-- name: SellerOfCallbackTask :one
SELECT s.seller_id
FROM fulfillment.callback_tasks c
JOIN fulfillment.shipments s ON s.tenant_id = c.tenant_id AND s.id = c.shipment_id
WHERE c.id = @id;

-- name: SellerOfAddressChange :one
SELECT s.seller_id
FROM fulfillment.address_change_requests r
JOIN fulfillment.shipments s ON s.tenant_id = r.tenant_id AND s.id = r.shipment_id
WHERE r.id = @id;

-- name: SellerOfAnomaly :one
SELECT s.seller_id
FROM fulfillment.shipment_anomalies x
JOIN fulfillment.shipments s ON s.tenant_id = x.tenant_id AND s.id = x.shipment_id
WHERE x.id = @id;

-- name: SellerOfLabelBatch :one
-- Lô nhãn thuộc một tài khoản hãng; tài khoản thuộc một người bán (vận đơn của lô cùng tài khoản đó).
SELECT a.seller_id
FROM fulfillment.label_batches b
JOIN fulfillment.carrier_accounts a ON a.tenant_id = b.tenant_id AND a.id = b.carrier_account_id
WHERE b.id = @id;
