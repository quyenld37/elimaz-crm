-- Đơn từ website (kế hoạch M5, Task 193 / M5-10; QĐ15, QĐ33). Pha mở rộng: chỉ ADD COLUMN (null hoặc có mặc định),
-- CHECK thêm NOT VALID rồi VALIDATE, GRANT UPDATE cột mới. Không bảng mới, không đụng ord.traffic_sources (nguồn
-- "Website" có key = 'web' từ M2).
-- - ord.orders.price_review: đơn gửi từ hàng đợi của website mà giá, mã CRM khác số website đã báo (QĐ15); nhân viên
--   gọi khách khi xác nhận. Cột riêng, không thêm vào pending_reason (Quyết định 1 của Task 193).
-- - ord.orders.web_quote: báo giá website đã hiện cho khách khi price_review (chỉ số tiền, mã biến thể, mã giảm; không
--   dữ liệu cá nhân). submitted_via: website gửi thẳng (sync) hay từ hàng đợi (queued); NULL ở đơn không phải web.
-- - ord.order_items.web_campaign_*: chiến dịch web CRM áp cho dòng lúc tạo đơn (QĐ33; mã, revision, phần trăm, số
--   giảm của dòng lúc tạo). Không ràng với line_discount: nhân viên sửa đơn (order.Edit) có thể đổi giảm dòng sau đó.
--   Không khóa ngoại sang promotion (bảng của module khác; chiến dịch có thể bị xóa).
-- - ord.order_items.flash_sale_item_id: món flash sale của dòng; khóa ngoại thêm ở M5-16.

-- +goose Up
ALTER TABLE ord.orders
    ADD COLUMN price_review boolean NOT NULL DEFAULT false,
    ADD COLUMN web_quote jsonb,
    ADD COLUMN submitted_via text;
ALTER TABLE ord.orders
    ADD CONSTRAINT orders_submitted_via_check CHECK (submitted_via IN ('sync', 'queued')) NOT VALID,
    ADD CONSTRAINT orders_web_quote_object_check CHECK (web_quote IS NULL OR jsonb_typeof(web_quote) = 'object') NOT VALID;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_submitted_via_check;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_web_quote_object_check;

ALTER TABLE ord.order_items
    ADD COLUMN web_campaign_id uuid,
    ADD COLUMN web_campaign_revision int,
    ADD COLUMN web_campaign_percent int,
    ADD COLUMN web_campaign_discount bigint,
    ADD COLUMN flash_sale_item_id uuid;
ALTER TABLE ord.order_items
    ADD CONSTRAINT order_items_web_campaign_check CHECK (
        (web_campaign_id IS NULL AND web_campaign_revision IS NULL AND web_campaign_percent IS NULL
            AND web_campaign_discount IS NULL)
        OR (web_campaign_id IS NOT NULL AND web_campaign_revision >= 1 AND web_campaign_percent BETWEEN 1 AND 99
            AND web_campaign_discount >= 0)) NOT VALID;
ALTER TABLE ord.order_items VALIDATE CONSTRAINT order_items_web_campaign_check;

GRANT UPDATE (price_review, web_quote, submitted_via) ON ord.orders TO crm_app;
GRANT UPDATE (web_campaign_id, web_campaign_revision, web_campaign_percent, web_campaign_discount)
    ON ord.order_items TO crm_app;
