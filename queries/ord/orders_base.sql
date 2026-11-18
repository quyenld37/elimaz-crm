-- Đọc đơn đủ cột M2 (kế hoạch M2, Task M2-05). Chạy trong tenancy.WithTenant; điều kiện seller_id ở tầng dưới cùng
-- (ADR-29). order.Get (M1) và các hàm đọc của M2 dùng câu này.

-- name: GetOrderBase :one
SELECT id, seller_id, code, checkout_group_id, customer_id, channel, channel_account_id, traffic_source_id,
       conversation_id, external_ref, status, pending_reason, packing_status, payment_status, current_shipment_id,
       order_type, warehouse_id, ship_address, ship_phone, address_complete, subtotal, line_discount_total,
       order_discount, coupon_discount, discount_total, shipping_fee_charged, grand_total, prepaid_total, cod_amount,
       draft_expires_at, carrier_note, created_by, care_by, confirmed_at, confirmed_by, discount_approved_by,
       discount_approved_at, cancel_reason, cancel_note, cancelled_at, duplicate_suspect, replaced_return_id,
       created_at, updated_at, version
FROM ord.orders
WHERE seller_id = @seller_id AND id = @id;
