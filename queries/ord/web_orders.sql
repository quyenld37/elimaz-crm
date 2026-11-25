-- Đơn từ website (kế hoạch M5, Task 193 / M5-10).

-- name: WebOrderByExternalRef :one
-- Đơn web đã có của mã ngoài external_ref (khóa nghiệp vụ, unique orders_external_ref_key của M2).
SELECT id, code, status, grand_total, price_review
FROM ord.orders
WHERE seller_id = @seller_id AND channel = 'web' AND external_ref = @external_ref;

-- name: SetOrderWebFields :execrows
-- Ghi cột web lên đơn id vừa chèn trong cùng transaction.
UPDATE ord.orders
SET price_review = @price_review, web_quote = sqlc.narg(web_quote)::jsonb, submitted_via = @submitted_via,
    web_shipping = sqlc.narg(web_shipping)::jsonb
WHERE seller_id = @seller_id AND id = @id;

-- name: SetOrderItemWebCampaign :execrows
-- Ghi chiến dịch web lên dòng của biến thể variant_id trong đơn order_id vừa chèn trong cùng transaction.
UPDATE ord.order_items
SET web_campaign_id = @campaign_id, web_campaign_revision = @campaign_revision,
    web_campaign_percent = @campaign_percent, web_campaign_discount = @campaign_discount
WHERE seller_id = @seller_id AND order_id = @order_id AND variant_id = @variant_id;

-- name: OrderWebShipping :one
-- Hãng/dịch vụ khách chọn trên website của đơn web id (Task 490; NULL: không có). order.Get chỉ gọi với đơn kênh web:
-- GetOrderBase không đọc cột này để test dựng lược đồ trước 0530 vẫn đọc được đơn tay.
SELECT web_shipping FROM ord.orders WHERE seller_id = @seller_id AND id = @id;
