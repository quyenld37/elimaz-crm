-- Bucket tệp xuất exports (Task 515; kế hoạch M6 G5 chốt (a) 27/11/2026): tệp Excel báo cáo và danh sách khách nằm ở
-- bucket riêng <S3_BUCKET_PREFIX>exports, không giữ phiên bản, không chép ra kho ngoài, tự xóa theo tiền tố khóa
-- (reports/ 7 ngày, customers/ 1 ngày; docker/seaweedfs/bootstrap.sh). Pha mở rộng: nới CHECK bucket của danh mục mục
-- đích và của platform.files (giữ đủ bốn giá trị của 0124, thêm một), rồi trỏ hai mục đích tệp xuất của M6 (0604, Task
-- 226) sang bucket mới. Chưa có dòng nào của hai mục đích đó thì UPDATE không đổi gì (0604 chạy trước theo số). Không
-- tệp xuất nào đã nằm ở bucket private (M6 chưa ghi tệp). CHECK của platform.file_variants (public, private) không đổi:
-- tệp xuất không có biến thể ảnh. Bảng danh mục rất nhỏ; platform.files kiểm lại CHECK rộng hơn, không đổi dữ liệu.

-- +goose Up
ALTER TABLE platform.file_purposes
    DROP CONSTRAINT file_purposes_bucket_check,
    ADD CONSTRAINT file_purposes_bucket_check CHECK (bucket IN ('tmp', 'public', 'originals', 'private', 'exports'));

ALTER TABLE platform.files
    DROP CONSTRAINT files_bucket_check,
    ADD CONSTRAINT files_bucket_check CHECK (bucket IN ('tmp', 'public', 'originals', 'private', 'exports'));

UPDATE platform.file_purposes SET bucket = 'exports' WHERE key IN ('report_export', 'customer_export');
