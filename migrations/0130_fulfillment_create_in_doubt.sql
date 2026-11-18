-- Dấu "hãng có thể đã nhận yêu cầu tạo" của vận đơn (Task 408, D-03 của review độc lập M1). Lệnh
-- fulfillment.create_shipment vào in_doubt (quá giờ sau khi gửi, chết giữa chừng) → CreateCommand.OnTerminal ghi
-- create_in_doubt_at. Lệnh kết thúc dead/expired chỉ hủy vận đơn creating khi chưa có dấu: lệnh từng in_doubt rồi được
-- người chạy lại vẫn có thể có kiện ở hãng từ lần gửi đầu, nên không hủy tự động (người tra hãng rồi xử lý).
-- Pha mở rộng: thêm một cột cho phép rỗng, không giá trị mặc định, không đổi dòng có sẵn; crm_app được UPDATE cột này
-- (quyền UPDATE của bảng theo từng cột, 0118).

-- +goose Up
ALTER TABLE fulfillment.shipments ADD COLUMN create_in_doubt_at timestamptz NULL;
GRANT UPDATE (create_in_doubt_at) ON fulfillment.shipments TO crm_app;
