-- Sửa cột province_code của view công bố fulfillment.shipments_v (Task 442; lỗi Task 108 phát hiện): 0300 đọc
-- ship_to ->> 'province_code' mà Ship ghi bản chụp địa chỉ với khóa provinceCode (shipToDoc, area_create.go) → cột luôn
-- NULL. Chỉ đổi đúng biểu thức đó; giữ nguyên 35 cột, thứ tự, kiểu text, security_invoker (CREATE OR REPLACE giữ
-- GRANT). Không view nào khác đọc shipments_v. Pha mở rộng (không thu hẹp).

-- +goose Up
CREATE OR REPLACE VIEW fulfillment.shipments_v WITH (security_invoker = true) AS
SELECT id, order_id, code, carrier_key, carrier_account_id, tracking_code, status, cod_amount, fee,
       tenant_id, seller_id, mode, service, sub_status, cancel_kind, create_state, cod_collected_amount,
       declared_value, fee_actual, weight_grams, partial, picked_up_at, delivered_at, carrier_reported_returned_at,
       returned_at, last_status_at, last_carrier_event_at, last_synced_at, last_sync_error, stuck_since, needs_review,
       label_file_id, label_printed_at, created_at, ship_to ->> 'provinceCode' AS province_code
FROM fulfillment.shipments;
