-- Báo phí các hãng trên đơn (kế hoạch M3, Task M3-13): crm_app được xóa dòng fulfillment.shipping_quotes. Hai chỗ xóa:
-- kết quả báo phí thay dòng pending của (lần báo phí, tài khoản) bằng một dòng mỗi dịch vụ, và lệnh dọn báo phí quá 7
-- ngày (trừ báo phí đã được vận đơn dùng: khóa ngoại shipments_quote_fk chặn). 0300 chỉ cấp SELECT, INSERT, UPDATE.
-- Pha mở rộng: chỉ thêm quyền, không đổi bảng. Số 0320 do điều phối viên cấp (các task M3 chạy song song).

-- +goose Up
GRANT DELETE ON fulfillment.shipping_quotes TO crm_app;
