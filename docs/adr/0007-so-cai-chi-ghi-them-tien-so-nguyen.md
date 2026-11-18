# ADR-07 — Sổ cái chỉ ghi thêm; tiền là số nguyên

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); thêm sổ COD (ADR-23); hệ quả "partition theo thời gian" bỏ bởi ADR-20
Ngày: 11/11/2026
Kiểm bằng: `TestLedger_AppendOnly` (internal/modules/inventory/movement_test.go); `TestLedgerIsAppendOnly` (internal/modules/cash/cod_test.go); `TestOpenReadsInt8AsInt64` (internal/platform/db/db_test.go); sổ hoa hồng: chưa có, làm ở P2

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-07. Tệp này không chép lại quyết định.

Sổ tồn và sổ hoa hồng chỉ `INSERT`; database chặn `UPDATE`/`DELETE`. Số dư là bảng dẫn xuất, đối chiếu với sổ hằng
đêm. Tiền lưu `BIGINT` đồng, tỷ lệ lưu phần vạn.

## Kiểm ở đâu

- Sổ tồn chỉ ghi thêm: `TestLedger_AppendOnly` (internal/modules/inventory/movement_test.go).
- Sổ COD chỉ ghi thêm (ADR-23): `TestLedgerIsAppendOnly` (internal/modules/cash/cod_test.go).
- Đối chiếu số dư với sổ hằng đêm: `TestReconcile_EachCheck`, `TestReconcile_NightlySchedule` (internal/modules/inventory/reconcile_test.go).
- Tiền là số nguyên 64 bit: `TestOpenReadsInt8AsInt64` (internal/platform/db/db_test.go); `TestCreateManual_TotalOverflow` (internal/modules/order/create_test.go).
- Sổ hoa hồng (`affiliate`): chưa có, làm ở P2 (kế hoạch `2026-11-18-crm-p2-affiliate.md`).
