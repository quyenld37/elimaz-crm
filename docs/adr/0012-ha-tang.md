# ADR-12 — Hạ tầng

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); phần "PostgreSQL chạy cùng VPS của website" thay bởi ADR-31 (13/11), rồi ADR-43 (15/11); bổ sung bởi ADR-19, 26, 36, 37
Ngày: 11/11/2026
Kiểm bằng: `scripts/backup/restore-drill.sh`, `scripts/backup/weekly-verify.sh`, `make verify-drill`; `TestRuleBackupAge`, `TestRuleBackupVerify` (internal/platform/observability/rules_test.go); trên máy thử: Task 23 bước 7 (cần OK chủ shop)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-12. Tệp này không chép lại quyết định.

Phần còn hiệu lực: WAL archiving liên tục và sao lưu đầy đủ hằng đêm sang object storage của nhà cung cấp khác, thử
khôi phục tự động hằng tuần; ứng dụng chỉ biết địa chỉ database qua cấu hình. Vị trí máy: ADR-31, ADR-43.

## Kiểm ở đâu

- Diễn tập khôi phục và kiểm bản sao lưu hằng tuần: `scripts/backup/restore-drill.sh`, `scripts/backup/weekly-verify.sh`
  (`make verify-drill`); cảnh báo `TestRuleBackupAge`, `TestRuleBackupVerify` (internal/platform/observability/rules_test.go).
- Địa chỉ database chỉ qua cấu hình: `TestLoadAPIEmptyListsEveryMissingVariable` (internal/platform/config/config_test.go).
- Chạy trên máy thử với kho ngoài thật: chưa có, Task 23 bước 7 (cần OK chủ shop).
