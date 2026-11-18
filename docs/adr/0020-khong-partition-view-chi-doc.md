# ADR-20 — Không partition ở bản đầu; báo cáo trên view; module công bố view chỉ đọc

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026)
Ngày: 12/11/2026
Kiểm bằng: `TestSQLUsageProductCode`, `TestSQLUsageViewRules` (tests/arch/sqlusage_test.go); `TestViewsDocMatchesDatabase` (tests/arch/viewsdoc_test.go); internal/platform/schedules/cleanup_test.go; báo cáo: chưa có, làm ở mốc M6

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-20. Tệp này không chép lại quyết định.

Không partition; dọn dữ liệu cũ bằng việc định kỳ xóa theo lô. Báo cáo là câu SQL trên view; chỉ chụp tồn theo ngày.
Mỗi module công bố view chỉ đọc; lớp truy vấn được ghép các view đó; ghi vào bảng module khác vẫn bị cấm.

## Kiểm ở đâu

- Điểm 1 (dọn theo lô): `TestCleanupRemovesOnlyOldRows`, `TestCleanupBatchLimit` (internal/platform/schedules/cleanup_test.go).
- Điểm 2 (báo cáo trên view, chụp tồn theo ngày): chưa có, làm ở mốc M6.
- Điểm 3 (chỉ đọc view đã công bố, không ghi bảng module khác): `TestSQLUsageProductCode`, `TestSQLUsageViewRules`
  (tests/arch/sqlusage_test.go); `TestViewsDocParses`, `TestViewsDocMatchesDatabase` (tests/arch/viewsdoc_test.go);
  luật (f) "internal/queries chỉ đọc view" trong `TestBoundariesNotCalledModule` (tests/arch/boundaries_test.go).
