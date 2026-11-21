-- Ghi chú của yêu cầu đổi địa chỉ (kế hoạch M3, Task M3-17): hợp đồng AddressChange.note (ghi chú khi yêu cầu, thay
-- bằng ghi chú khi nhân viên xác nhận). Ứng dụng che SĐT, địa chỉ có số (logging.ScrubText) trước khi ghi; không vào
-- view. Pha mở rộng: chỉ thêm cột cho phép NULL và quyền sửa cột đó. Bảng còn nhỏ: không khóa lâu.

-- +goose Up
ALTER TABLE fulfillment.address_change_requests
    ADD COLUMN note text CHECK (char_length(note) <= 1000);
GRANT UPDATE (note) ON fulfillment.address_change_requests TO crm_app;
