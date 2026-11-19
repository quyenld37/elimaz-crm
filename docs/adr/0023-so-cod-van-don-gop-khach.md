# ADR-23 — Sổ COD; một vận đơn hiện hành; đơn và vận đơn là hai trục; gộp khách không dời dữ liệu

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); mở rộng ADR-07
Ngày: 12/11/2026
Kiểm bằng: internal/modules/fulfillment/create_shipment_test.go; internal/modules/order/shipment_progress_test.go; internal/modules/inventory (reserve_test.go, stocktake_test.go); internal/modules/cash/cod_test.go; M2: internal/modules/order (confirm_test.go, cancel_test.go, drafts_test.go), internal/modules/customer (merge_test.go, merge_property_test.go); phần còn lại: mốc M3

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-23. Tệp này không chép lại quyết định.

Mỗi đơn một vận đơn chưa hủy; trạng thái đơn tách khỏi vận chuyển, thêm `draft` và `cancel_requested`; giữ hàng bằng
`stock_reservations` đổi bằng `UPDATE` có điều kiện; kiểm kho so với `tồn sổ − đang giao`; sổ COD chỉ ghi thêm,
bảng kê được băm; gộp khách chỉ đổi `root_id`. Chi tiết ở spec 05.

## Kiểm ở đâu

- Điểm 1: `TestShipment_SecondForSameOrder`, `TestShipment_DatabaseGuards` (internal/modules/fulfillment/create_shipment_test.go);
  `TestApplyShipmentProgress_OnlyCurrentShipment` (internal/modules/order/shipment_progress_test.go).
- Điểm 2: trục vận chuyển tách khỏi đơn: `TestApplyShipmentProgress_Flow` (shipment_progress_test.go); `draft`: `TestConfirm_Draft` (internal/modules/order/confirm_test.go), `TestExpireDrafts` (drafts_test.go); `cancel_requested`: `TestCancel_TD03RequestThenCarrierConfirms`, `TestCancel_CarrierRejects` (internal/modules/order/cancel_test.go) (M2-32).
- Điểm 3: `TestReserve_FiftyOnTen`, `TestReserve_PartialShortageRollsBack` (internal/modules/inventory/reserve_test.go);
  `TestRelease_Twice` (internal/modules/inventory/movement_test.go); `TestInventoryInvariants` (internal/modules/inventory/property_test.go).
- Điểm 4: `TestStocktake_InTransitNotAdjusted`, `TestStocktake_DeliveredAfterCount` (internal/modules/inventory/stocktake_test.go).
- Điểm 5: `TestLedgerIsAppendOnly` (internal/modules/cash/cod_test.go); băm bảng kê chống nhập trùng: chưa có, làm ở mốc M3.
- Điểm 6 (gộp khách chỉ đổi `root_id`, hoàn tác được): `TestMergeDH14`, `TestUndoMergeNotLatest`, `TestUndoMergeWindow` (internal/modules/customer/merge_test.go); quy luật gộp rồi hoàn tác trả về đúng trạng thái: `TestMergeUndoProperty` (merge_property_test.go) (M2-32).
