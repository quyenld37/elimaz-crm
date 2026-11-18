# ADR-37 — Sao lưu: điều kiện cho WAL-G; ba bậc tới máy dự phòng

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); bổ sung ADR-12, ADR-19
Ngày: 15/11/2026
Kiểm bằng: `scripts/backup/restore-drill.sh` (khôi phục có bấm giờ), `scripts/backup/weekly-verify.sh`, `make verify-drill` (kết quả `docs/do-luong/phep-thu-ngan.md`); `TestRuleBackupVerify` (internal/platform/observability/rules_test.go); trên kho ngoài thật: Task 23 bước 7

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-37. Tệp này không chép lại quyết định.

Giữ WAL-G, mã hóa bằng libsodium, kèm năm điều kiện: `archive_timeout` không quá 5 phút; diễn tập khôi phục có bấm giờ
trước khi nhận đơn thật, mỗi quý; kiểm bản sao lưu hằng tuần ghi vào `backup_runs`; sao lưu đi thẳng ra kho ngoài;
khóa giữ hai nơi. Máy dự phòng đi theo ba bậc; bậc 1 từ M0.

## Kiểm ở đâu

- Điểm 1: `docker/postgres/conf.d/crm.conf` (`archive_timeout`); `scripts/backup/restore-drill.sh` dừng nếu cấu hình chưa nạp.
- Điểm 2: `scripts/backup/restore-drill.sh` (`restore_seconds`, `rpo_seconds`); trên máy thử với kho ngoài thật: Task 23 bước 7.
- Điểm 3: `scripts/backup/weekly-verify.sh`, `scripts/backup/verify-drill.sh`; cảnh báo `TestRuleBackupVerify` (internal/platform/observability/rules_test.go).
- Điểm 4, 5: cấu hình kho ngoài và khóa do chủ shop tạo: Task 23 bước 7 (cần OK chủ shop).
- Bậc 2 (diễn tập chuyển máy dự phòng): chưa có, chỉ làm khi chủ shop chốt mục tiêu 99,9% (D4) hoặc kế hoạch hạ tầng có máy thứ hai.
