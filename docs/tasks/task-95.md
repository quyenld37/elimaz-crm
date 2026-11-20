# Task 95 — M3-04: Hợp đồng API, quyền, cài đặt, thứ tự khóa và khung nối của M3

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 95 = M3-04). Loại việc (quy trình mục 3): A

Ca kiểm (11-kich-ban-kiem-thu.md): không có mã ca riêng; task là hợp đồng và khung cho các ca VC/TI/COD của Task 96–139.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mã sinh `internal/httpapi/gen`: 52 operation mới (phương thức mới của `gen.Handler`; `handler` nhúng
    `gen.UnimplementedHandler`, nên không gãy nơi nào). Kiểu đổi của operation M1, M2 (Q-X2, chỉ thêm trường tùy
    chọn): `gen.CreateShipmentRequest` (thêm 10 trường `Opt…`), `gen.Shipment` (thêm trường ShipmentView tùy chọn;
    `Status` thành `$ref ShipmentStatus` — tên Go `gen.ShipmentStatus` giữ nguyên, thêm 4 giá trị),
    `gen.ListOrdersParams` (4 tham số tùy chọn), `gen.OrderListItem` (`shipment`, `cod` tùy chọn),
    `gen.ReceiveReturnRequestItemsItem` (thêm `sellable`, `damaged`, `missing`). Chỗ dùng: `handlers_shipment.go`
    (`shipmentOut`, `CreateShipment` — chỉ đọc `CarrierAccountId`, vẫn biên dịch), `handlers_order_read.go`,
    `handlers_order_returns.go` (không đọc trường mới); test `handlers_shipment_test.go`.
  - `identity.Permissions` (+18), `identity.DefaultRoles` (vai trò mẫu nhận quyền M3; `manager` không nhận 5 quyền sổ
    COD chỉ Chủ, Kế toán), `permissionLabels` (+18). Dùng ở `tools/routegen`, `httpapi/guard.go`, màn Vai trò
    (`PermissionCatalog`), `SyncDefaultRoles`.
  - Gói mới `internal/platform/settings` (`Get`, `Put`, `ErrVersionConflict`, `ErrInvalid`); `background.Collect`,
    `background.UniqueNames`, `background.ErrDuplicatePart` (mới, thêm vào gói có sẵn).
  - `fulfillment`: mới `Deps`, `Service`, `Module`, `New`, `Module.{CommandHandlers, InboundHandlers, WebhookSources,
    Consumers, Schedules, Cleanups, Check}`, `Settings`, `ReturnRisk`, `AgedDays`, `DefaultSettings`, `StuckStatuses`,
    `LoadSettings`, `SaveSettings`, `SettingsKey`, `ErrSettingOutOfRange`, `SettingOutOfRangeError`,
    `ErrVersionConflict`, `LabelPurpose`, `LabelPurposeKey`, `MaxLabelBytes`, `RegisterFilePurposes`. Hàm M1 giữ chữ ký.
  - `cash`: mới `Deps`, `Service`, `Module`, `New`, `Module.{CommandHandlers, InboundHandlers, Consumers, Schedules,
    Cleanups, Check}`, `StatementPurpose`, `StatementPurposeKey`, `MaxStatementBytes`, `RegisterFilePurposes`.
  - `cmd/crm`: `newModules(cfg, d, bus, reg)` (thêm hai tham số; chỗ gọi: `runWorker`, `serveHTTP`, `main_test.go`),
    `newInbound(reg, bus, m3)`, `newCommands(…, m3, logger)`, `ingestSources(cfg) ([]Source, error)`; mới `wireM3`,
    `m3Deps`, `m3Parts`.
- Gói phụ thuộc ngược (go list): `internal/modules/identity` ← gần như mọi gói (thêm phần tử, không đổi hàm);
  `internal/modules/fulfillment` ← `httpapi`, `cash`, `internal/adapters/carrier/**`, `cmd/crm`, `testkit/shiptest`;
  `internal/modules/cash` ← `httpapi` (mới, test), `internal/platform/files` (chỉ test), `cmd/crm`;
  `internal/platform/background` ← `customer`, `order`, `promotion`, `fulfillment`, `cash`, `cmd/crm`;
  `internal/platform/settings` ← `fulfillment`. `internal/httpapi/gen` ← `httpapi`.
- Operation OpenAPI bị chạm, oasdiff: 52 operation mới (bảng của kế hoạch, `tests/arch/m3contract_test.go`); 5 operation
  mở rộng chỉ thêm trường tùy chọn (`createShipment`, `getShipment`, `listOrders`, `getOrderTimeline` chỉ mô tả,
  `receiveReturn`); 13 mã lỗi mới trong `x-error-codes`. oasdiff chỉ báo 8 dòng WARN "thêm giá trị enum ở thân trả"
  của `Shipment.status` (createShipment, getShipment) → hai dòng trong `openapi/breaking-approved.yaml` (Quyết định 2).
- Chỗ dùng trên màn hình: client sinh lại (`console/src/api/**`); `console/src/app/errors.ts` thêm câu cho 13 mã mới
  (`errors.test.ts` đòi đúng tập mã). Không sửa `router.tsx` (Q-T91-6). Màn Vai trò hiện thêm 18 quyền (nhãn có sẵn).
- Câu sqlc, bảng, pha migration: `migrations/0304_settings.sql` (bảng mới `platform.settings`, RLS, GRANT SELECT,
  INSERT, UPDATE cho `crm_app`; pha mở rộng); `0306_file_purposes_m3.sql` (hai dòng `INSERT … ON CONFLICT DO NOTHING`);
  0305 bỏ trống (Q-T91-5). Câu sqlc mới `queries/platform/settings.sql` (`GetSetting`, `InsertSetting`, `UpdateSetting`).
  ADR-46: đối chiếu nhóm 8, 9, 12c với migration 0300, 0303, 0304 (chỉ sửa cột "Mốc thêm" và mục (f)).
- Test sẽ chạy: `./internal/platform/settings/...`, `./internal/platform/background/...`,
  `./internal/modules/fulfillment/...` (vùng rủi ro cao: `fulfillment/status.go` không đổi, chạy cả gói),
  `./internal/modules/cash/...` (vùng rủi ro cao M3, cả gói), `./internal/modules/identity/...` (vùng rủi ro cao, cả
  gói), `./internal/httpapi/...`, `./internal/platform/files/...` (vùng rủi ro cao), `./cmd/crm/...`, `./tests/arch/...`;
  `make oasdiff`; `cd console && npx tsc --noEmit && npx vitest run src/app/errors.test.ts`; cuối cùng
  `scripts/agent/ci-task.sh` một lần (đổi migration nên chạy toàn bộ test).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`background.Deps/Parts`,
  `schedules.CleanupFunc`, `RegisterTenantCleanup`, `files.RegisterPurpose`, `fieldcrypt.ParseKeyring`,
  `tenancy.WithTenant`); grep `platform.settings`, `Settings struct` (chỉ `order.Settings` của M2, bảng riêng
  `ord.order_settings`); mẫu cài đặt M2 `handlers_order_settings.go` (`refusableWrite`, `audit`, `catalogRead`); mẫu
  mục đích tệp `customer.RegisterFilePurposes`; mẫu hợp đồng `tests/arch/m2contract_test.go`; mẫu ranh giới
  `boundaries_test.go` (`hasPathPrefix`, `isStdlib`, `checkBoundaries`); khung thứ tự khóa `lockorder_frame_test.go`.
- Dùng lại / viết mới vì:
  - `internal/platform/settings` mới: kế hoạch (Q-35, B17) giao M3 làm chủ bảng `platform.settings`; `order.Settings`
    là bảng cột riêng của M2, không chung. `fulfillment.Settings` theo đúng mẫu `order.Settings`
    (`SettingOutOfRangeError` + `Is`, `AuditFields`); lỗi `setting_out_of_range` dùng lại mã M2.
  - `background.Collect`, `UniqueNames` thêm vào gói `background` có sẵn (việc của gói: phần chạy nền của module) để
    `fulfillment`, `cash`, `cmd/crm` dùng chung, không chép hai bản giống nhau trong hai module.
  - Handler cài đặt dùng `refusableWrite` + `isShipmentRefusal` (bảng lỗi `fulfillmentError` có sẵn, thêm hai dòng),
    `catalogRead`, `h.audit` (changed_fields tự tính).
  - Test hợp đồng M3 theo đúng khuôn `m2contract_test.go`; danh sách 501 đọc tệp, không lấy route cụ thể làm mẫu
    (bẫy Task 427). 20 tệp handler vùng sinh từ chữ ký `gen.UnimplementedHandler` (jscpd: tên hàm, kiểu khác nhau).

## Soát thiết kế

- 5.1 chủ dữ liệu: `platform.settings` thuộc nền tảng (Q-35), khóa `fulfillment` do module `fulfillment` sở hữu kiểu,
  mặc định, luật kiểm; mục đích tệp `carrier_label` của `fulfillment`, `cod_statement` của `cash`.
- 5.2, 5.3: không ghi bảng module khác; `cash.Deps.Carriers` là kiểu công khai của `fulfillment` (cột "Được gọi").
  Luật ranh giới M3 kiểm bằng `boundaries_test` (a) có sẵn + (j), (k) mới.
- 5.4: PUT cài đặt là một transaction (`WriteTx`: đọc trước, ghi có version, audit, khóa chống trùng).
- 5.5, 5.6: không áp dụng: không sự kiện, không lệnh ra ngoài mới (khung vùng trả nil; lệnh M1 giữ nguyên điều kiện).
- 5.7: `platform.settings` có `tenant_id NOT NULL`, khóa chính `(tenant_id, key)`, RLS bằng `enable_tenant_rls`; mọi câu
  trong `WithTenant` (test chạy bằng `crm_app` thật, tenant B không thấy dòng của A).
- 5.8: module không nhập `net/http`; lớp HTTP chỉ đổi kiểu và gọi `fulfillment.LoadSettings/SaveSettings`.
- 5.9: mọi operation ghi mới có `IdempotencyKey` và `x-permission` thuộc `identity.Permissions` (`TestM3Contract_Operations`,
  `TestRoutesPublicSetAndPermissions`, `TestRoutesIdempotencyModes`).
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: theo mẫu đường ghi (Task 12) và module mẫu catalog/order (cài đặt có `version`, sentinel trong module, đổi HTTP ở
  `errors.go`).
- 6.1: PUT so `version` trong cùng câu `UPDATE … WHERE version = $v` (dòng bị khóa bởi UPDATE); chèn lần đầu
  `ON CONFLICT DO NOTHING` → hai lần chèn cùng lúc: một lần thắng, lần kia `version_conflict`.
- 6.2: `Idempotency-Key` bắt buộc; lần bị từ chối (422, 409) không lưu theo khóa (`refusableWrite`).
- 6.3: `platform.settings` nhóm 12c, sửa một mình trong transaction của PUT (đọc trước bằng SELECT không khóa); đối
  chiếu ADR-46 nhóm 8, 9, 12c với migration (test `TestLockOrderFrame`, `TestLockOrder_M3TablesListed`).
- 6.4–6.9: không áp dụng: không việc nền, không gọi ra ngoài mới; transaction ngắn của `WriteTx`.
- 6.10: mọi trường tiền mới là `Money`/`SignedMoney` (2^53−1; `money_fields_test` chạy trên toàn hợp đồng).
- 6.11: không áp dụng: không quy luật tiền/tồn mới; luật kiểm cài đặt có test bảng (15 ca).
- 6.12: cài đặt không chứa dữ liệu cá nhân; audit chỉ số. Hợp đồng: `CarrierAccount` chỉ `hasCredentials`, khóa API
  chỉ ghi; `Shipment` không có `ship_to`; `AddressChange` không trả lại địa chỉ.
- 6.13: quyền mặc định từ chối; quyền theo kho cho thao tác của kho (`TestCan_M3WarehouseScope`); Kho không sửa được
  cài đặt (403); user chỉ có `fulfillment.read` gọi báo phí → 403.

## Quyết định thay chủ shop

1. **Hai quyền `fulfillment.carrier_accounts.manage`, `fulfillment.settings.manage` là loại `toàn shop` (ScopeShop)**,
   không phải `none` như cột "Phạm vi: không" của kế hoạch. Vì sao: luật của `docs/quyen.md` (Task 315 lượt 2, sau khi
   kế hoạch viết) đòi quyền ghi chính sách áp cả shop (kết nối kênh, phí ship, cài đặt) là `toàn shop`, như
   `order.settings.manage` của M2; gán theo một kho không được sửa kết nối hãng của cả shop. Phương án khác: `none`
   (đúng chữ kế hoạch, nhưng nhân viên Kho gán theo một kho có quyền này sẽ sửa được cả shop). Đảo: đổi hai dòng trong
   `permissions.go`, `docs/quyen.md`, test.
2. **`Shipment.status` thêm 4 giá trị (`delivery_failed`, `returning`, `returned`, `cancel_requested`) và hai dòng
   trong `openapi/breaking-approved.yaml` ngày 20/11.** oasdiff coi "thêm giá trị enum ở thân trả" là thay đổi phá vỡ
   (mức WARN) và cả cách bỏ enum cũng bị chặn. Database (0300, Task 92) đã nhận 9 trạng thái nên hợp đồng phải nói
   đúng; client duy nhất là màn hình của chính CRM (chỉ đọc, kiểu TypeScript rộng thêm). Dòng duyệt ghi rõ là quyết
   định thay chủ shop theo ủy quyền; điều phối viên xóa hai dòng sau khi gộp (luật của tệp). Phương án khác: giữ enum
   M1 (hợp đồng sai so với database, task sau gãy khi trả trạng thái mới) hoặc dừng chờ chủ shop (chặn cả M3).
3. **Hàm rỗng của vùng là phương thức của `*handler` trong tệp vùng**, không phải "struct vùng nhúng vào Handler".
   Vì sao: nhúng nhiều struct cùng tầng với `gen.UnimplementedHandler` làm Go báo phương thức mơ hồ (không biên dịch);
   phương thức trên `*handler` che phương thức nhúng và vẫn giữ "mỗi task chỉ sửa tệp của vùng mình". Đảo: không cần.
4. **Tệp vùng danh sách vận đơn là `handlers_shipment_lists.go`** (kế hoạch ghi `handlers_lists.go`, đã là tệp của M2
   cho danh sách đơn).
5. **`fulfillment.Deps` có thêm `Events *events.Registry`** (kế hoạch không liệt kê): lệnh M1 và đường ghi của
   `order.Writer` cần sổ sự kiện. `Files`, `Alerter` để nil ở task này (chưa vùng nào dùng; task cần thì nối thêm trong
   `wire_m3.go` — xem Việc phát hiện thêm).
6. **Lệnh vận chuyển M1 giữ đúng điều kiện cũ**: `Module.CommandHandlers` chỉ thêm `fulfillment.create_shipment`,
   `fulfillment.refresh_status` khi `Carriers` có adapter, và `wireM3` chỉ dựng `Carriers` từ `carrier.All(cfg)` khi
   `carrier.FakeOn` (như M1). Nguồn webhook và handler tín hiệu của hãng giả nối trong `wireM3` (adapter thuộc
   `cmd/crm`, module không nhập adapter). Không đổi hành vi production.
7. **Luật ranh giới M3 đặt tên (j), (k)** (kế hoạch ghi f–i, đã dùng cho luật M1, M2); (f), (g) của kế hoạch đã do
   luật (a) theo bảng 04 mục 3 bắt (có ca test riêng). (j) cho phép thêm `internal/platform/webhooks` (kiểu `Source`
   mà adapter hãng giả M1 đã dùng, Q-T91-7) và các gói `internal/adapters/carrier/**` nhập nhau; chỉ áp cho
   `internal/adapters/carrier/**` (`cfaccess` là adapter của identity).
8. **Cài đặt vận chuyển: PUT ghi cả bộ; sai luật → 422 `setting_out_of_range` không kèm `field`** (enum `field` của thân
   lỗi chỉ có trường của cài đặt đơn; thêm giá trị là thay đổi phá vỡ thân trả). `stuckHours`, `pollMinutes` phải có
   đúng 5 trạng thái.
9. **Danh sách 501 hai chiều**: `TestM3Routes_PendingList` gọi mọi route bằng owner; 501 ⇒ có trong `m3_pending.txt`;
   có trong danh sách ⇒ chỉ 501 hoặc 400 (thân `{}` không tới handler). Task làm xong operation phải xóa dòng.
11. **Vai trò mẫu Kế toán nhận thêm `files.upload`** (quyền M1). Vì sao: `importCodStatement` nhận `fileId` của tệp
    tải lên qua `createUpload`/`completeUpload` (đòi `files.upload`); không có quyền này thì Kế toán — vai trò được giao
    `cash.statements.import` — không nhập được bảng kê (reviewer commit 2, lượt 1). Rủi ro: `files.upload` chỉ tạo tệp
    tạm theo mục đích; gắn tệp vào sản phẩm, nhập khách vẫn đòi `catalog.manage`, `customer.import`. Phương án khác:
    quyền riêng cho tải bảng kê (thêm một quyền, đổi luồng tải lên của M1) hoặc để Task M3-31 quyết. Đảo: bỏ một phần tử
    khỏi `roles.go`, `docs/quyen.md`; vai trò đã nhận qua `SyncDefaultRoles` phải gỡ tay.
10. Commit chia hai: cấu trúc (khung nối, `wireM3`, dời phần đăng ký M1) rồi hành vi (hợp đồng, quyền, cài đặt,
    migration, test), mỗi commit một lần reviewer.

## Bằng chứng

Hai commit (Quyết định 10). **Commit 1 — cấu trúc** (khung `fulfillment.Module`, `cash.Module`, `background.Collect`,
`UniqueNames`, `wireM3`, dời phần đăng ký M1 khỏi `main.go`, không đổi hành vi). **Commit 2 — hành vi** (hợp đồng,
quyền, cài đặt, migration, mục đích tệp, test hợp đồng). Commit 1 được kiểm riêng trên bản sao chỉ phần đã stage
(`git checkout-index -a --prefix=<scratchpad>/c1/`).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| C1: phần M1 giữ nguyên điều kiện (consumer cash luôn có; hai lệnh vận chuyển, nguồn và handler tín hiệu hãng giả chỉ khi ngoài production và có FAKECARRIER_URL); khóa trường sai dạng → lỗi khởi động | `TestWireM3_KeepsM1Parts` (3 ca + FIELD_ENC_KEYS sai) | `go test -race ./cmd/crm/` trong bản sao c1 | `ok elimaz.com.vn/crm/cmd/crm 0.931s` |
| C1: sổ sự kiện vẫn có event.cash, customer_stats, order_duplicates; lịch M2 giữ | `TestNewModulesRegistersCash` | như trên | `ok` |
| C1: gom phần chạy nền, phát hiện trùng tên | `TestCollect`, `TestUniqueNames` | `go test -race ./internal/platform/background/` (c1) | `ok … background 0.421s` |
| C1: lệnh M1 vẫn chạy như cũ (fulfillment, cash cả gói) | toàn bộ test gói | `go test -race ./internal/modules/fulfillment/ ./internal/modules/cash/` (c1) | `ok … fulfillment 10.442s`, `ok … cash 24.322s` |
| C1: build, lint, catalog, jscpd của phần stage | — | `go build ./...`, `golangci-lint run` 4 gói, `go run ./tools/catalog` + `diff`, `jscpd` (c1) | build xanh, `0 issues.`, `CATALOG_OK`, `Found 0 clones.` |
| routegen sinh không lỗi; mọi operation mới có quyền thuộc `identity.Permissions`, route ghi có `IdempotencyKey` | `TestM3Contract_Operations` (52 + 5 mở rộng), `TestRoutesPublicSetAndPermissions`, `TestRoutesIdempotencyModes` | `go test -race ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch` (cổng kiểm task: `34.189s`) |
| Danh sách 501 đúng hai chiều; owner gọi `requestShippingQuotes` → 501 (có trong danh sách) | `TestM3Routes_PendingList`, `TestM3Contract_PendingList` | `go test -race -run TestM3Routes -v ./internal/httpapi/` | `PASS`; log `23 operation trả 501, danh sách chờ có 50 dòng` |
| User chỉ có `fulfillment.read` gọi báo phí → 403 | `TestM3Routes_Forbidden` | như trên | `PASS` |
| `createShipment` M1 vẫn chạy với thân `{carrierAccountId}` | test M1 trong `handlers_shipment_test.go` | `go test -race -run Shipment ./internal/httpapi/` | `ok … httpapi 2.078s` |
| Quyền mới có dòng `docs/quyen.md` cùng loại phạm vi; không trùng khóa M1, M2 | `TestPermissionsDoc_MatchesCode`, `TestPermissionsList` (khai hai lần → lỗi) | `go test -race ./internal/modules/identity/` | `ok … identity 48.357s` |
| `fulfillment.ship` theo kho W1: không có ở W2; `ScopesFor` = [W1] | `TestCan_M3WarehouseScope` | như trên | `ok` |
| Vai trò mẫu: Kho có `fulfillment.print`, không `cash.adjust`; sale giữ `fulfillment.ship`; manager không có 5 quyền sổ COD | `TestDefaultRoles_Content`, `TestEnsureDefaultRoles_M3` | như trên | `ok` |
| Tenant có trước M3: `SyncDefaultRoles` đưa quyền M3 vào vai trò mẫu chưa sửa, owner đủ, vai trò đã sửa không nhận | `TestSyncDefaultRoles_M3` | như trên | `ok` |
| `LoadSettings` tenant chưa có dòng → mặc định (`shipping` 72, `HighRateBP` 3000), version 0 | `TestLoadSettings_Defaults` | `go test -race ./internal/modules/fulfillment/` | `ok` |
| Luật kiểm cài đặt (15 ca, biên 1 và 10.000) | `TestSettings_Validate` | như trên | `ok` |
| `Get`/`Put`: mặc định gộp theo khóa, version, xung đột, RLS tenant, đầu vào sai | `TestGetPut_DefaultsAndMerge`, `TestPut_VersionConflictAndTenant`, `TestPut_Invalid`, `TestSaveSettings` | `go test -race ./internal/platform/settings/ ./internal/modules/fulfillment/` | `ok … settings 2.328s` |
| PUT cài đặt: `HighRateBP = 0` → 422; hợp lệ → audit có `changed_fields`; version cũ → 409; version âm → 400 (`minimum: 0` của hợp đồng); Kho → 403 | `TestFulfillmentSettingsAPI` | `go test -race -run TestFulfillmentSettingsAPI ./internal/httpapi/` | `ok` |
| `boundaries_m3/`: đúng 3 lỗi (a, j, k); mã thật 0 | `TestBoundariesTestdataM3`, `TestBoundariesM3Rules`, `TestBoundariesRealCode` | `go test -race ./tests/arch/` | `ok` |
| ADR-46 đủ bảng M3 (nhóm 8, 9, 12c, mục f) | `TestLockOrderFrame`, `TestLockOrder_M3TablesListed` | như trên | `ok` |
| `platform.file_purposes` có `carrier_label`, `cod_statement` khớp Go; Kế toán (vai trò mẫu) tải `cod_statement` .xlsx 3 MiB được, 11 MiB → `file_too_large` | `TestPurposes_GoMatchesTable`, `TestCodStatementUploadPurpose` | `go test -race ./internal/platform/files/ ./internal/httpapi/` | cổng kiểm task: `ok … files 91.697s`, httpapi `ok` |
| `make oasdiff` chỉ thêm (8 dòng enum đã duyệt, Quyết định 2) | — | `make oasdiff` | 8 dòng `đã duyệt: …`, thoát 0 |
| Màn hình: câu cho 13 mã mới, kiểu sinh lại | `errors.test.ts` | `cd console && npx tsc --noEmit && npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| Cổng kiểm của task (lần 1, trước khi stage) | — | ci-task | test toàn bộ xanh (235 s); đỏ gen-check, catalog-check (do chưa stage), jscpd (6 đoạn trùng → đã tách helper `checkContractOps`, `checkViolations`, `checkCases`, `syncOldTenant`, `get`), prettier `errors.ts` (đã sửa); chạy lại sau khi stage commit 2 |
| Cổng kiểm của task (lần 2, sau khi stage commit 2) | — | ci-task (lần chạy đầu bị dừng do hết giờ của lệnh nền khi máy chậm, chạy lại cùng phần stage) | `CI-TASK XANH (so với điểm tách nhánh 2235d7c)`; `ok … httpapi 122.259s`, `identity 230.159s`, `fulfillment 37.351s`, `cash 96.394s`, `files 135.684s`, `settings 2.669s`, `tests/arch 30.673s` |
| Sau sửa reviewer commit 2 lượt 1 (Kế toán `files.upload`; `version` âm) | `TestCodStatementUploadPurpose`, `TestFulfillmentSettingsAPI`, `TestDefaultRoles_Content`, cả gói | `make gen`, `make oasdiff`, `go test -race ./internal/httpapi/ ./internal/modules/identity/ ./tests/arch/ ./cmd/crm/`, `golangci-lint` hai gói | `ok … httpapi 70.998s`, `identity 89.573s`, `tests/arch 53.295s`, `cmd/crm 2.901s`; `0 issues.`; oasdiff chỉ 8 dòng đã duyệt |

## Test bị xóa hoặc tắt

(không có) — `TestPurposes_GoMatchesTable` sửa danh sách mục đích (thêm hai mục đích M3 qua `allPurposes`), không bớt ca.

## Khác kế hoạch

- Quyết định 1, 3, 4, 5, 7 ở trên.
- Không có `0305_m3_role_permissions.sql` (Q-T91-5), không sửa `router.tsx` (Q-T91-6) — đúng kế hoạch đã sửa 20/11.
- Ca "POST shipping-quotes với owner → 501" kiểm qua `TestM3Routes_PendingList` (requestShippingQuotes có trong danh
  sách và trả 501), không viết ca cố định trên route thật (bẫy Task 427); ca 403 viết cố định (không đổi khi làm xong).

## Việc phát hiện thêm

- `wire_m3.go` chưa nối `Files`, `Alerter` vào `fulfillment.Deps`, `cash.Deps` (không có sẵn trong `newModules`); Task
  M3-16 (nhãn), M3-31 (bảng kê) cần thì phải sửa `wire_m3.go`/`newModules` (trái câu "task sau không sửa tệp này").
- `fulfillment.ErrOrderNotShippable` của M1 đổi sang 404 `not_found`; hợp đồng M3 có mã `order_not_shippable` — Task
  M3-12 đổi ánh xạ khi làm `Ship`.
- Hai dòng `openapi/breaking-approved.yaml` cần xóa sau khi gộp vào main.

## Reviewer

### Commit 1 (cấu trúc, 8ff686d)

Kết luận: ĐẠT (lượt 1). Không phát hiện phải sửa. Reviewer so từng điều kiện đăng ký trước/sau: consumer `cash` vẫn
đứng đầu sổ sự kiện; hai lệnh M1 chỉ khi `FakeOn`, vẫn vào `NewInDoubtResolver`; bí mật webhook chỉ ở ingest; mọi chỗ
gọi `newModules`, `newInbound`, `newCommands`, `ingestSources` đã sửa. Ghi chú không bắt buộc đã làm ở commit 2: chú
thích "trùng tên → lỗi khởi động" của `wireM3` chưa đúng hết (`Check` chỉ xét trong từng module) → thêm
`checkM3Unique` sau khi gom hai module và phần hãng giả, test `TestCheckM3Unique`.

### Commit 2 (hành vi)

Lượt 1: CẦN SỬA — (1) Kế toán có `cash.statements.import` mà không có `files.upload` nên không tải được bảng kê; ca
test ghi "kế toán" mà chạy bằng Chủ → Quyết định 11, ca test đổi sang Kế toán; (2) `PUT /fulfillment-settings` với
`version` âm ra 500 (`settings.ErrInvalid` không ánh xạ) → hợp đồng thêm `minimum: 0` (400), thêm ca test.

Lượt 2: ĐẠT. Không phát hiện phải sửa; hai sửa của lượt 1 đã đúng (vai trò mẫu Kế toán có `files.upload`, test chạy
bằng Kế toán; `version` có `minimum: 0`, ca -1 → 400). Ghi chú: hai dòng `openapi/breaking-approved.yaml` cần xóa ngay
sau khi gộp vào main.

Kết luận: ĐẠT
