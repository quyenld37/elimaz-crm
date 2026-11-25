-- Hãng/dịch vụ khách chọn trên website (Task 490; website Task 214). Pha mở rộng: chỉ ADD COLUMN null, CHECK thêm
-- NOT VALID rồi VALIDATE, GRANT UPDATE cột mới (như 0503).
-- - ord.orders.web_shipping: lựa chọn giao hàng của khách lúc đặt (mã tài khoản hãng, khóa, tên hãng, dịch vụ, phí của
--   lựa chọn, có phải phí dự phòng, vấn đề khi đơn từ hàng đợi không còn khớp). Chỉ để nhân viên gửi đúng hãng; không
--   dữ liệu cá nhân. NULL ở đơn không phải web hay website không gửi lựa chọn. Không khóa ngoại sang fulfillment (bảng
--   của module khác; tài khoản hãng có thể bị thu hồi).

-- +goose Up
ALTER TABLE ord.orders ADD COLUMN web_shipping jsonb;
ALTER TABLE ord.orders
    ADD CONSTRAINT orders_web_shipping_object_check
        CHECK (web_shipping IS NULL OR jsonb_typeof(web_shipping) = 'object') NOT VALID;
ALTER TABLE ord.orders VALIDATE CONSTRAINT orders_web_shipping_object_check;

GRANT UPDATE (web_shipping) ON ord.orders TO crm_app;
