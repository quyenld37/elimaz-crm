-- Tín hiệu webhook hãng (Task M3-10): tìm tài khoản theo khóa tuyến, vận đơn theo đúng tài khoản (Q-7: không bao giờ
-- chỉ theo mã hãng). Chạy trong ngữ cảnh tenant của việc vào (RLS).

-- name: InboundAccountByRouteKey :one
-- Tài khoản hãng của khóa tuyến route_key trong tenant hiện tại (unique (tenant_id, route_key)).
SELECT id, carrier_key, status
FROM fulfillment.carrier_accounts
WHERE route_key = @route_key;

-- name: InboundShipmentByCode :one
-- Vận đơn theo mã của CRM trên đúng tài khoản hãng.
SELECT id
FROM fulfillment.shipments
WHERE carrier_account_id = @carrier_account_id AND code = @code;

-- name: InboundShipmentByTracking :one
-- Vận đơn theo mã hãng trên đúng tài khoản hãng.
SELECT id
FROM fulfillment.shipments
WHERE carrier_account_id = @carrier_account_id AND tracking_code = @tracking_code;
