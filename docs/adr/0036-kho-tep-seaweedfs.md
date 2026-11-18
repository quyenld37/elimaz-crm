# ADR-36 — Kho tệp SeaweedFS có bản sao ở kho ngoài; ảnh tạo sẵn kích thước khi tải lên

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); chữ "bằng sharp" thay bởi ADR-45; thư viện nối là aws-sdk-go-v2 (ADR-44)
Ngày: 15/11/2026
Kiểm bằng: tests/probe/seaweedfs_test.go (`make probe-test`, số đo `docs/do-luong/seaweedfs-m1.md`); internal/platform/files (s3store_test.go, process_test.go, upload_test.go); `TestProductImages_Lost` (internal/modules/catalog/images_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-36. Tệp này không chép lại quyết định.

Kho chính SeaweedFS tự chạy, ghim phiên bản; rclone chép mỗi giờ, chỉ chép thêm, sang kho ngoài không thuộc Cloudflare;
ứng dụng dùng qua interface `FileStore`; tài khoản ứng dụng không xóa vĩnh viễn được; ảnh tạo sẵn bộ kích thước khi
tải lên, tên theo mã băm, giữ ảnh gốc riêng tư.

## Kiểm ở đâu

- Tài khoản ứng dụng không xóa vĩnh viễn: `TestDelete_AppCannotPurge` (tests/probe/seaweedfs_test.go); `TestS3Store_DeleteKeepsVersion` (internal/platform/files/s3store_test.go).
- Đường dẫn ký sẵn hết hạn: `TestPresignedGet_Expires` (seaweedfs_test.go); `TestFileStore_PresignGetExpires` (s3store_test.go).
- Tệp giả ảnh bị từ chối: `TestFileProcess_RejectedNotImage` (internal/platform/files/process_test.go).
- Bucket tạm tự xóa, tải nhiều phần: `TestTmpExpiration`, `TestMultipartUpload` (seaweedfs_test.go).
- Bản sao mỗi giờ sang kho ngoài: `TestFilesyncIdentity` (seaweedfs_test.go). Cảnh báo quá 90 phút và đối chiếu số tệp mỗi ngày: chưa có, làm ở mốc M1 (Task M1-45).
- Dòng `files` mất tệp hiện ô trống: `TestProductImages_Lost` (internal/modules/catalog/images_test.go).
- Tắt đột ngột giữa lúc ghi; diễn tập dựng lại kho tệp từ bản sao: chưa có, làm ở mốc M1 (Task M1-46).
