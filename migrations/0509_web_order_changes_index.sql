-- Luồng thay đổi đơn web (kế hoạch M5, Task 196 / M5-13; operation webOrderChanges, gói internal/queries/weborders).
-- Pha mở rộng (Q-X2): CREATE OR REPLACE VIEW chỉ nối cột vào cuối, giữ tên, kiểu, thứ tự cột đã có; chỉ thêm index.
--   - ord.orders_v + updated_at: lúc đổi đơn gần nhất (gói đọc chung chỉ đọc view, không đọc bảng của order).
--   - Index đơn kênh web theo updated_at, và vận đơn theo last_status_at (đổi trạng thái vận đơn không đổi dòng đơn):
--     câu WebOrderChanges lấy hợp hai tập rồi xếp keyset (changed_at, id). Không CONCURRENTLY: như 0211, CRM chưa chạy
--     dữ liệu thật, bảng còn nhỏ.
-- Số sửa chữa đầu tiên của dải 0509–0519 (kế hoạch M5, Global Constraints).

-- +goose Up
CREATE OR REPLACE VIEW ord.orders_v WITH (security_invoker = true) AS
SELECT id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id,
       created_at,
       pending_reason, packing_status, payment_status, channel_account_id, traffic_source_id, conversation_id,
       external_ref, order_type, warehouse_id, ship_phone, address_complete, subtotal, line_discount_total,
       order_discount, coupon_discount, discount_total, shipping_fee_charged, prepaid_total, created_by, care_by,
       confirmed_at, cancelled_at, cancel_reason, draft_expires_at, duplicate_suspect, version,
       ship_phone_reversed, updated_at
FROM ord.orders;

CREATE INDEX orders_web_updated ON ord.orders (tenant_id, seller_id, updated_at, id) WHERE channel = 'web';
CREATE INDEX shipments_last_status ON fulfillment.shipments (tenant_id, seller_id, last_status_at);
