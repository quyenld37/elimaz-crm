-- Danh sách đơn và dòng thời gian của đơn đọc qua view (kế hoạch M3, Task M3-28; spec 04 mục 3 điểm 2, C5): gói
-- internal/queries ghép ord.orders_v với fulfillment.shipments_v, cash.shipment_cod_v. Pha mở rộng: CREATE OR REPLACE
-- VIEW chỉ nối cột vào cuối, giữ tên, kiểu, thứ tự cột đã có (Q-X2); cột đúng docs/views.md.
--   - ord.orders_v + ship_phone_reversed: tìm đơn theo đuôi SĐT qua view vẫn dùng index orders_ship_phone_reversed.
--   - fulfillment.shipment_events_v + actor_id, cash.payments_v + actor_id: dòng thời gian của đơn ghi tên nhân viên đã
--     làm (I09).

-- +goose Up
CREATE OR REPLACE VIEW ord.orders_v WITH (security_invoker = true) AS
SELECT id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id,
       created_at,
       pending_reason, packing_status, payment_status, channel_account_id, traffic_source_id, conversation_id,
       external_ref, order_type, warehouse_id, ship_phone, address_complete, subtotal, line_discount_total,
       order_discount, coupon_discount, discount_total, shipping_fee_charged, prepaid_total, created_by, care_by,
       confirmed_at, cancelled_at, cancel_reason, draft_expires_at, duplicate_suspect, version,
       ship_phone_reversed
FROM ord.orders;

CREATE OR REPLACE VIEW fulfillment.shipment_events_v WITH (security_invoker = true) AS
SELECT tenant_id, id, shipment_id, source, raw_status, canonical, status, sub_status, occurred_at, recorded_at, applied,
       ignored_reason, actor_id
FROM fulfillment.shipment_events;

CREATE OR REPLACE VIEW cash.payments_v WITH (security_invoker = true) AS
SELECT tenant_id, id, order_id, method, amount, received_at, actor_id
FROM cash.payments;
