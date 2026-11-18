# ADR-43 — Thiết kế không gắn với một máy chủ cụ thể; điều kiện triển khai

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); bổ sung ADR-12, ADR-31; thêm `crm-imager` (ADR-45)
Ngày: 15/11/2026
Kiểm bằng: tests/deploy/compose_test.go (`TestProdCompose_AppServices`, `TestProdCompose_OnlyCloudflareIngress`, `TestProdCompose_PostgresVolume`, `TestProdCompose_ImagesPinnedNotBuilt`); `scripts/backup/recreate-check.sh`; trên bản thử: Task 23 bước 7

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-43. Tệp này không chép lại quyết định.

Thiết kế không dựa vào sức hay cách bố trí của một máy cụ thể; máy 13.140.64.56 là nơi thử. Hạ tầng chạy thật là một
kế hoạch riêng và phải đáp ứng bảng yêu cầu: mỗi dịch vụ một tài nguyên có kiểm tra sức khỏe, PostgreSQL 18 gắn đúng
`/var/lib/postgresql`, chỉ nhận kết nối qua Cloudflare, image dựng ở CI, sao lưu ở nhà cung cấp khác.

## Kiểm ở đâu

- Dịch vụ và kiểm tra sức khỏe: `TestProdCompose_AppServices`, `TestDevCompose_AppServices` (tests/deploy/compose_test.go); `TestHealthcheckCommand` (tests/e2e/boot_test.go).
- PostgreSQL gắn ổ đúng chỗ, tạo lại container dữ liệu còn: `TestProdCompose_PostgresVolume` (compose_test.go); `scripts/backup/recreate-check.sh`.
- Chỉ nhận kết nối qua Cloudflare: `TestProdCompose_OnlyCloudflareIngress` (compose_test.go).
- Image dựng ở CI, không dựng trên máy: `TestProdCompose_ImagesPinnedNotBuilt` (compose_test.go).
- Deploy trong lúc webhook gửi liên tục; đo bộ nhớ từng dịch vụ trên bản thử: Task 23 bước 7.8, 7.9 (cần OK chủ shop).
