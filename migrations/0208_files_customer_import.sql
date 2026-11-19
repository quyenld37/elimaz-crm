-- Mục đích tệp customer_import (kế hoạch M2, Task M2-13; R5, G11): tệp Excel nhập khách, bucket private, mode mặc
-- định (standard). Cơ chế duy nhất là bảng platform.file_purposes của M1 (0124); Go đăng ký cùng khóa bằng
-- customer.RegisterFilePurposes() lúc khởi động. Pha mở rộng: chỉ thêm một dòng.

-- +goose Up
INSERT INTO platform.file_purposes (key, bucket) VALUES ('customer_import', 'private') ON CONFLICT DO NOTHING;

