-- Mục đích tệp xuất của M6 (kế hoạch M6, Task 226 / M6-03; R5, B2-4; cùng mẫu với 0306 của M3): report_export (tệp
-- Excel báo cáo, danh sách sản phẩm) và customer_export (danh sách khách), cả hai bucket private, chế độ standard, chỉ
-- máy chủ ghi. Go đăng ký cùng khóa bằng reporting.RegisterFilePurposes() lúc khởi động. Pha mở rộng: chỉ thêm dòng
-- danh mục, không đụng CHECK hay bảng platform.files.

-- +goose Up
INSERT INTO platform.file_purposes (key, bucket)
VALUES ('report_export', 'private'), ('customer_export', 'private')
ON CONFLICT DO NOTHING;
