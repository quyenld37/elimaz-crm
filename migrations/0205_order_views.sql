-- View công bố của đơn (kế hoạch M2, Task M2-05; ADR-27, Q-X2, A14, A20, B11). Cột đúng docs/views.md.
-- ord.orders_v có từ M1 (Task M1-30): CREATE OR REPLACE giữ 11 cột của M1 ở vị trí 1–11 cùng tên, kiểu; cột M2 nối
-- vào cuối (R3). Mốc sau chỉ nối thêm sau version.

-- +goose Up
CREATE OR REPLACE VIEW ord.orders_v WITH (security_invoker = true) AS
SELECT id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id,
       created_at,
       pending_reason, packing_status, payment_status, channel_account_id, traffic_source_id, conversation_id,
       external_ref, order_type, warehouse_id, ship_phone, address_complete, subtotal, line_discount_total,
       order_discount, coupon_discount, discount_total, shipping_fee_charged, prepaid_total, created_by, care_by,
       confirmed_at, cancelled_at, cancel_reason, draft_expires_at, duplicate_suspect, version
FROM ord.orders;

CREATE VIEW ord.order_items_v WITH (security_invoker = true) AS
SELECT id, tenant_id, seller_id, order_id, variant_id, position, sku_snapshot, name_snapshot, quantity, unit_price,
       line_discount, allocated_order_discount, net_amount
FROM ord.order_items;

-- Dòng tạo đơn của M1 ghi from_status NULL; view trả '' cho mọi dòng tạo đơn (một dạng cho người đọc).
CREATE VIEW ord.order_status_history_v WITH (security_invoker = true) AS
SELECT id, tenant_id, order_id, COALESCE(from_status, '') AS from_status, to_status, from_pending, to_pending,
       actor_type, actor_id, reason, occurred_at, recorded_at
FROM ord.order_status_history;

CREATE VIEW ord.returns_v WITH (security_invoker = true) AS
SELECT id, tenant_id, code, order_id, shipment_id, kind, status, refund_amount, replacement_order_id, received_at,
       created_at
FROM ord.returns;

CREATE VIEW ord.return_items_v WITH (security_invoker = true) AS
SELECT id, tenant_id, return_id, order_item_id, quantity, condition, refund_amount
FROM ord.return_items;

CREATE VIEW ord.traffic_sources_v WITH (security_invoker = true) AS
SELECT id, tenant_id, key, name, channel_account_id, active, position
FROM ord.traffic_sources;

GRANT SELECT ON ord.orders_v, ord.order_items_v, ord.order_status_history_v, ord.returns_v, ord.return_items_v,
    ord.traffic_sources_v TO crm_app;
