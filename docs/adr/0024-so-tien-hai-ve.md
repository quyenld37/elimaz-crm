# ADR-24 — Sổ tiền ghi hai vế

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); bổ sung ADR-07, ADR-23
Ngày: 12/11/2026
Kiểm bằng: `TestDatabaseRejectsUnbalancedTransfer`, `TestDatabaseRejectsLaterUnbalancedLine` (internal/modules/cash/cod_test.go); `TestCODLedgerProperty` (internal/modules/cash/property_test.go); sổ hoa hồng: chưa có, làm ở P2

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-24. Tệp này không chép lại quyết định.

Sổ COD và sổ hoa hồng ghi hai vế: mỗi bút toán gồm từ hai dòng trở lên, mỗi dòng thuộc một ngăn, tổng các dòng bằng 0;
một trigger kiểm lúc commit. Chống trùng đặt ở bút toán (`idempotency_key`). Sổ tồn giữ một vế.

## Kiểm ở đâu

- Bút toán lệch bị từ chối lúc commit: `TestDatabaseRejectsUnbalancedTransfer`, `TestDatabaseRejectsLaterUnbalancedLine` (internal/modules/cash/cod_test.go).
- Chuỗi sự kiện ngẫu nhiên, tổng mọi ngăn bằng 0 (rapid): `TestCODLedgerProperty` (internal/modules/cash/property_test.go).
- Chống trùng ở bút toán: `TestPostValidatesAndIsIdempotent`, `TestConsumerIsIdempotent` (internal/modules/cash/cod_test.go).
- Hoàn, bảng kê, điều chỉnh trong chuỗi sự kiện: chưa có, làm ở mốc M3. Sổ hoa hồng: chưa có, làm ở P2.
