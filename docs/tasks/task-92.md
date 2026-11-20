# Task 92 — M3-01: Mở rộng schema fulfillment, địa chỉ lấy hàng của kho, số lượng trên dòng phiếu trả

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 92 = M3-01; ca VC13 phần ràng buộc, KH14 cho vận đơn, nền TD04). Loại việc (quy trình mục 3): B (migration ALTER bảng của M1, M2 + bảng mới).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu công khai nào đổi chữ ký. Mã sinh nội bộ
  `fulfillment/internal/store`: `InsertShipmentParams` thêm trường `SellerID` (1 chỗ gọi: `insertShipment` trong
  `fulfillment.go`); thêm hàm đọc của `schema_probe.sql` (chỉ để `sqlc vet` kiểm, chưa ai gọi).
- Gói phụ thuộc ngược (go list) của `fulfillment`: `cmd/crm`, `internal/adapters/fakecarrier`, `internal/httpapi`,
  `internal/modules/cash`, `internal/testkit/shiptest` (và test của chúng). Của `order`: gần như mọi gói nghiệp vụ —
  đổi ở `order` chỉ là schema `ord.return_items` (không đổi câu SQL nào của `order`).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration (pha mở rộng; chỉ thêm cột, ràng buộc, bảng, view):
  - `0300_fulfillment_m3.sql`: `ALTER` `fulfillment.carrier_accounts`, `shipments`, `shipment_events` (bảng M1); bảng mới
    `shipping_quotes`, `callback_tasks`, `callback_task_logs`, `address_change_requests`, `shipment_anomalies`,
    `label_batches`, `carrier_locations`; view `shipments_v` (nối cột), `carrier_accounts_v`, `shipment_events_v`,
    `locations_v` (mới). Khóa ngoại mới sang `ord.orders` (ghép người bán), `catalog.sellers`, `inventory.warehouses`
    (đã có từ M1), `platform.files`.
  - `0301_inventory_warehouse_pickup.sql`: `ALTER inventory.warehouses` (bảng M1) + `warehouses_v` nối 2 cột.
  - `0302_ord_return_items_qty.sql`: `ALTER ord.return_items` (bảng M2) + `return_items_v` nối 3 cột; `condition` thêm `mixed`.
  - Câu sqlc M1 phải sửa để chạy trên schema mới: `InsertShipment` (ghi `seller_id` — cột mới NOT NULL),
    `CancelCreatingShipment` (ghi `cancel_kind` — CHECK mới), `UpdateShipmentProgress` (`create_state = 'created'`
    khi đã có mã hãng, cho khớp quy luật điền dòng cũ). Câu của `order`, `cash` không đổi.
- Khóa ngoại `ord.orders.current_shipment_id → fulfillment.shipments` (kế hoạch đòi) làm vỡ test đang gắn vận đơn
  hiện hành bằng `uuid.New()` không có dòng vận đơn: `order` (`shiphooks_test`, `shipment_progress_test`,
  `cancel_race_test`, `duplicates_test`, `env_test`), `customer/stats_test`, `httpapi/handlers_order_cancel_test`.
  Thêm `testkit.StubShipment` (mới, chỉ test) và gọi trong các helper gắn vận đơn của các test đó; không đổi ca nào.
  Test kiến trúc đếm view / cột M2 (`tests/arch/viewsdoc_test.go` 30 → 33 view, `m2contract_test.go` cột
  `return_items_v`), `order/schema_test.go` (cột `return_items_v`), `fulfillment/create_shipment_test.go`
  (`TestShipment_DatabaseGuards` chèn thẳng: thêm `seller_id`, hủy kèm `cancel_kind`) sửa theo schema mới.
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/modules/order/...` (mới + bộ M1 Task 31,
  M2 Task 05, 22), `./internal/modules/cash/... ./internal/httpapi/... ./internal/modules/inventory/... ./tests/arch/...`
  (đọc `shipments_v`, test views.md, kiến trúc); `ci-task.sh` (migration đổi → toàn bộ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung (`docs/danh-muc-dung-chung.md`) và tìm theo tên, theo việc: `platform.enable_tenant_rls`,
  trigger chỉ ghi thêm của M1 (`fulfillment.shipment_events_append_only`), `testkit.MigrateBefore`, `testkit.CheckViews`,
  `testkit.Columns`, `testkit.CheckSchemaRulesClean`, `shiptest.NewShop`, `fulfillment.NewShipmentCode`.
- Dùng lại / viết mới vì: dùng lại tất cả các thứ trên. `callback_task_logs` dùng lại hàm trigger chỉ ghi thêm của M1
  (`fulfillment.shipment_events_append_only` chỉ báo lỗi, không đọc tên bảng — gọi chung được; thông báo có tên bảng
  qua `TG_TABLE_NAME` thì phải đổi hàm M1, nên viết một hàm thứ hai cùng thân cho đúng thông báo — xem Khác kế hoạch
  nếu đổi). Hàm Go dùng chung mới (chỉ test): `testkit.StubShipment` — không có helper nào tạo dòng vận đơn mà không
  qua `fulfillment.CreateForOrder` (`shiptest.Shop.Shipment` đòi đơn confirmed, cùng kho với tài khoản, chưa có vận đơn
  chưa hủy, và `order_test` gắn S1 rồi S2 vào cùng đơn); dòng "Khi nào không dùng" trỏ về `shiptest`.

## Soát thiết kế

- 5.1 Chủ dữ liệu: bảng mới ở `fulfillment` (module chủ vận chuyển); cột kho ở `inventory.warehouses`, cột số lượng ở
  `ord.return_items` — migration của mốc M3 ALTER bảng của module khác theo luật mở rộng (ghi chủ ở đầu tệp; như 0204
  ALTER `inventory`). Hàm ghi các cột đó thuộc module chủ (Task 100: `inventory.SetWarehousePickup`; Task 114: `order`).
- 5.2, 5.3: migration không đọc bảng module khác ở mã Go; view mới cho module khác đọc (`carrier_accounts_v`,
  `shipment_events_v`, `locations_v`) không có cột bí mật, dữ liệu cá nhân (`credential_enc`, `route_key`, `ship_to`, `raw`).
- 5.4–5.6, 5.8–5.10: không áp dụng: task không thêm đường ghi, lệnh, sự kiện, HTTP.
- 5.7 Ngữ cảnh shop: mọi bảng mới `tenant_id NOT NULL`, RLS bằng `platform.enable_tenant_rls`, `UNIQUE (tenant_id, id)`,
  khóa ngoại ghép `tenant_id`; `CheckSchemaRules` = `[]` (luật (g): unique của bảng có `seller_id` có `seller_id` sau
  `tenant_id`, hoặc `seller-exempt` + dòng ở `docs/views.md`).
- 5.11 Không thêm thư viện. 5.12 Theo mẫu migration M1/M2 (`0118`, `0204`) và test schema `order/schema_test.go`.
- 6.1–6.2: ràng buộc database giữ quy luật khi chạy đồng thời: unique một phần "một vận đơn chưa hủy mỗi đơn" (M1) giữ;
  unique mã hãng theo hãng; một tài khoản mặc định mỗi (người bán, kho, hãng); một yêu cầu đổi địa chỉ mở mỗi vận đơn;
  một bất thường mở mỗi (vận đơn, loại); một việc gọi lại mỗi (vận đơn, lần giao).
- 6.3 Thứ tự khóa: migration chạy một mình; `ALTER` lấy khóa bảng theo thứ tự `carrier_accounts → shipments →
  shipment_events → ord.orders` (không có giao dịch song song lúc migrate). Bảng mới đặt vào nhóm 8 của ADR-46 sau
  `shipment_events` (ghi ở đầu tệp; ADR sửa ở task dùng bảng).
- 6.4–6.9: không áp dụng: không có đường yêu cầu, việc nền.
- 6.10 Tiền: cột tiền `bigint` đồng (`cod_collected_amount`, `declared_value`, `fee_actual`, `fee_total`), CHECK ≥ 0 nơi có nghĩa.
- 6.11: không áp dụng (quy luật nghiệp vụ ở task dùng bảng; ở đây là ràng buộc, test bằng ca cụ thể).
- 6.12 Dữ liệu cá nhân: `ship_to`, `new_address` chỉ ở bảng, không ở view; `shipment_anomalies.detail` ghi chú
  "chỉ mã, trạng thái, số tiền". `pickup_phone` của kho là SĐT shop (không phải khách) nằm trong `warehouses_v` theo kế hoạch.
- 6.13 Quyền database: `crm_app` chỉ được `UPDATE` đúng cột M3 sẽ sửa; `callback_task_logs` chỉ ghi thêm (trigger + REVOKE);
  `route_key`, `seller_id`, `carrier_key` không cho `crm_app` sửa.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Dữ liệu M1 (fake active, disabled không khóa; vận đơn creating/delivered/cancelled, mã `NewShipmentCode`, vận đơn hiện hành) chạy 0300–0302 không lỗi: creating giữ, `route_key` 32 ký tự khác nhau, disabled → paused, `seller_id` đúng của đơn (cả người bán đối tác), có mã → `created`, cancelled → `manual`, `last_status_at` có; luật schema sạch | `fulfillment/migrate_m1_data_test.go` `TestMigrateM1Data` | `go test -race -run TestMigrateM1Data ./internal/modules/fulfillment/` | `ok ... 2.371s` |
| `CheckSchemaRules` = `[]` | `TestSchemaM3_Rules` (và cuối `TestMigrateM1Data`) | `go test -race ./internal/modules/fulfillment/...` | `ok ... 6.524s` |
| Hai vận đơn chưa hủy cùng đơn → 23505; hủy (`replace`) rồi tạo lại được (hai người bán) | `TestSchemaM3_OneActiveShipmentPerOrder` | như trên | ok |
| Vận đơn trỏ tài khoản hãng / đơn của người bán khác → 23503 (KH14) | `TestSchemaM3_SellerMustMatch` | như trên | ok |
| VC13: `vtp` trùng mã → 23505; `best` cùng mã được | `TestSchemaM3_TrackingUniquePerCarrier` | như trên | ok |
| CHECK: awaiting_pickup không mã, cancelled không loại hủy, `requested`, mã `VD0001`, `cancel_kind` lạ → 23514; delivery_failed, creating không mã (M1) được | `TestSchemaM3_StatusChecks` (8 ca) | như trên | ok |
| `CreateForOrder` của M1 vẫn chạy (seller_id, pending, last_status_at; sau lệnh tạo: awaiting_pickup, created) | `TestSchemaM3_CreateForOrderStillWorks` + bộ M1 Task 31 (`create_shipment_test`, `refresh_status_test`, `terminal_test`) | như trên | ok |
| `current_shipment_id` trỏ vận đơn không có → 23503 (crm_app và chủ bảng) | `TestSchemaM3_CurrentShipmentFK` | `go test -race -run TestSchemaM3_CurrentShipmentFK ./internal/modules/fulfillment/` | `ok ... 1.815s` |
| `UPDATE shipment_events` bằng crm_app → 42501; `callback_task_logs` chỉ ghi thêm (crm_app 42501, chủ bảng bị trigger chặn) | `TestSchemaM3_AppendOnly` | `go test -race ./internal/modules/fulfillment/...` | ok |
| Tài khoản: `route_key` hợp lệ, trùng → 23505, sai mẫu → 23514, crm_app không sửa được → 42501; status mới / lạ; một mặc định mỗi (người bán, kho, hãng) | `TestSchemaM3_CarrierAccounts` | như trên | ok |
| Bốn view `fulfillment` đúng cột, thứ tự (chín cột M1 đầu `shipments_v`; không `credential_enc`, `route_key`, `ship_to`, `raw`; có `province_code`); `warehouses_v` nối `pickup_address, pickup_phone` cuối | `TestSchemaM3_Views` | như trên | ok |
| Tenant B (crm_app) không đọc được dòng nào của tenant A qua bốn view; tenant A đọc được | `TestSchemaM3_ViewsTenantIsolation` | như trên | ok |
| `return_items` quantity 2: 1/0/0 → 23514, 1/1/0 (`mixed`) được, 0/0/0 được, vượt, số âm, condition lạ → 23514 | `order/return_items_qty_schema_test.go` `TestReturnItemsQty_Checks` | `go test -race -run 'TestReturnItemsQty|TestSchemaViewsAndRules' ./internal/modules/order/` | `ok ... 2.683s` |
| Dòng phiếu đã nhận có sẵn điền theo condition; phiếu requested giữ 0/0/0 | `TestReturnItemsQty_BackfillM2` | như trên | ok |
| Bộ M2 Task 05, 22 (`order`), `customer`, `cash`, `inventory`, `httpapi`, `tests/arch` (views.md, sqlc vet) xanh | cả gói | `scripts/agent/ci-task.sh` (toàn bộ `go test -race ./...` vì đổi migration, testkit) | mọi gói `ok` trừ `internal/imager` `TestImager_HangKilledWithinDeadline` (test giờ thật chập chờn khi máy tải nặng; chạy lại `-count=3` → `ok 4.492s`); `gen-check` đỏ chỉ vì chưa stage — sau `git add`: `make gen-check` sạch |
| Lint, sqlc vet, catalog, jscpd, gitleaks, oasdiff, test-guard | — | `scripts/agent/ci-task.sh` | các bước đều xanh |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Unique tài khoản mặc định: kế hoạch ghi `UNIQUE (tenant_id, warehouse_id, carrier_key) WHERE is_default_for_warehouse`;
  làm `(tenant_id, seller_id, warehouse_id, carrier_key)` — luật (g) ADR-29 (unique của bảng có `seller_id` phải có
  `seller_id` sau `tenant_id`) và mục Soát thiết kế 6.1 ("một tài khoản mặc định mỗi (người bán, kho, hãng)"): hai người
  bán cùng kho mỗi người một mặc định.
- `shipment_events.recorded_at` đã có từ M1 (0118): không thêm lại. `shipment_events.status` nới CHECK thêm năm giá trị
  mới và `''` (kế hoạch: "chuỗi rỗng khi không chỉ tới trạng thái nào" — CHECK của M1 không cho).
- `route_key` giữ `DEFAULT replace(gen_random_uuid()::text, '-', '')` (kế hoạch: điền dòng có sẵn rồi `SET NOT NULL`):
  một lệnh vừa điền mỗi dòng một giá trị vừa để `EnsureCarrierAccount` của M1 (seed, test) chèn được; đường tạo tài
  khoản của M3 (Task 9x) tự sinh bằng `crypto/rand`.
- Ba unique của M1 trên `shipments` (`code_key`, `tracking_key`, `active_order`) thêm `COMMENT seller-exempt` + dòng ở
  `docs/views.md` (luật (g) áp khi bảng có `seller_id`); index không đổi.
- `callback_task_logs` dùng hàm trigger riêng `fulfillment.callback_task_logs_append_only` (cùng thân với hàm của
  M1) để thông báo lỗi nêu đúng tên bảng; hàm M1 giữ nguyên (không sửa migration đã commit).
- `0301`, `0302` thêm `GRANT UPDATE` cột mới cho `crm_app` (hàm ghi của Task 9, 114 chạy bằng `crm_app`).
- Thêm `testkit.StubShipment` và sửa helper test của `order`, `customer`, `httpapi` (xem Ảnh hưởng) — hệ quả bắt buộc
  của khóa ngoại `orders_current_shipment_fk`.

## Quyết định thay chủ shop

- Vận đơn M1 bị hủy vì lệnh tạo kết thúc mà hãng chắc chắn chưa tạo (`CancelCreatingShipment`) ghi
  `cancel_kind = 'create_rejected'` (CHECK mới đòi loại hủy). Khác: `manual` (sai nghĩa: không ai hủy tay). Dễ đổi: một
  câu SQL.
- `UpdateShipmentProgress` đặt `create_state = 'created'` khi vận đơn có mã hãng (cùng quy luật điền dòng M1). Khác:
  để `pending` tới Task tạo vận đơn M3 (lệch giữa dòng cũ và mới).
- Vận đơn giả của test (`testkit.StubShipment`) là dòng `cancelled`/`manual` của tài khoản `stub:<người bán>:<kho>`:
  không chặn unique "một vận đơn chưa hủy mỗi đơn" khi test gắn S1 rồi S2 vào cùng đơn. Khác: tạo vận đơn thật qua
  `fulfillment` (order không được nhập fulfillment; test chạy đúng hơn nhưng phải đổi mọi ca).
- Dòng phiếu trả có sẵn: chỉ phiếu `received`, `restocked`, `closed` được điền số lượng theo `condition`; phiếu
  `requested`, `cancelled` giữ 0/0/0 (CHECK cho phép 0 = chưa nhận).

## Việc phát hiện thêm

- `internal/imager` `TestImager_HangKilledWithinDeadline` đỏ một lần trong ci-task (script giả chưa kịp ghi pid
  trong 1 giây khi máy chạy toàn bộ test); không liên quan task này, chạy riêng xanh. Có thể cần nới thời gian chờ
  script giả khởi động (task riêng).

## Reviewer

Kết luận: ĐẠT (vòng 1, 20/11/2026).

- Phát hiện phải sửa: không có. Ảnh hưởng khớp hai chiều (mọi nơi gắn vận đơn hiện hành trong test đã có
  `StubShipment`; không đường M1 nào hủy thiếu `cancel_kind` hay qua `creating` thiếu mã hãng). Cả 16 dòng bằng chứng
  trỏ test có thật, kiểm đúng tiêu chí; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc (để task dùng bảng xử lý): `callback_tasks` chưa có khóa ngoại ghép bảo đảm vận đơn thuộc
  đúng đơn, `shipping_quotes.carrier_key` chưa buộc khớp tài khoản (Task 104, 110); `shipments.mode` chưa cấp `UPDATE`
  cho `crm_app` (Task 106 cấp nếu cần); `UpdateShipmentProgress` đặt `created` cả khi điền mã muộn cho vận đơn đã hủy
  (Task 98, 99 nhớ khi làm lại máy trạng thái).
