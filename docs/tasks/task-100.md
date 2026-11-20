# Task 100 — M3-09: Tài khoản hãng, khóa API mã hóa, kiểm tra kết nối, địa chỉ lấy hàng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 100, M3-09). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Commit 1 (cấu trúc, không đổi hành vi): `fulfillment.AccountInput` (M1, kiểu đầu vào của `EnsureCarrierAccount`)
    đổi tên thành `fulfillment.SeedAccountInput` để nhường tên `AccountInput` cho kiểu M3 của kế hoạch. 5 chỗ dùng:
    `cmd/crm/seed.go:78`, `internal/testkit/shiptest/shop.go:53`, `internal/httpapi/handlers_shipment_test.go:29`,
    `internal/modules/fulfillment/create_shipment_test.go:207`, `internal/modules/fulfillment/schema_m3_test.go:214`.
  - Commit 2 (hành vi), mới ở `fulfillment`: `AccountInput`, `AccountPatch`, `AccountView`, `AccountFilter`,
    `Service.{CreateAccount, UpdateAccount, PutCredentials, CheckAccount, SetStatus, ListAccounts, GetAccount,
    ResolveAccount, ActiveAccounts}`, `MarkAuthFailed`, `CheckKind`, `NewAuthAlertCommand`, `AuthAlertKind`, sentinel
    `ErrCarrierNotSupported`, `ErrAccountUnavailable`, `ErrAccountTransition`, `ErrAccountTaken`, `ErrAccountInvalid`,
    `ErrAccountTargetNotFound`, `ErrKeysMissing`. `accountsHandlers()` trả lệnh kiểm kết nối. Không đổi chữ ký nào có
    sẵn.
  - Mới ở `inventory`: `PickupAddress`, `WarehousePickup`, `GetWarehousePickup`, `SetWarehousePickup`.
  - `httpapi.Deps` thêm trường `Fulfillment *fulfillment.Service` (nối ở `cmd/crm/main.go`); 7 handler của
    `handlers_carrier_accounts.go`, `handlers_warehouse_pickup.go` thay thân 501; `errors.go` thêm ánh xạ lỗi mới.
- Gói phụ thuộc ngược (go list): `fulfillment` ← `cmd/crm`, `internal/httpapi`, `internal/modules/cash`,
  `internal/adapters/carrier/{best,spx,vtp,fake,manual,carriertest}`, `internal/testkit/shiptest`,
  `tools/fakecarrier/server`. `inventory` ← `order`, `promotion`, `catalog`? (không), `httpapi`, `cmd/crm`,
  `inventory/invtest`, `testkit/shiptest`, `fulfillment` (chỉ test).
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng (Task 95 đã khai đủ 7 operation và schema). Hiện thực
  `listCarrierAccounts`, `createCarrierAccount`, `updateCarrierAccount`, `putCarrierCredentials`, `checkCarrierAccount`,
  `setCarrierAccountStatus`, `setWarehousePickup`; xóa 7 dòng đó khỏi `tests/arch/testdata/m3_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn hình tài khoản hãng là task console sau).
- Câu sqlc, bảng, pha migration: `queries/fulfillment/accounts.sql` thêm câu đọc/ghi tài khoản (chèn đủ cột M3, khóa
  dòng, sửa có version, ghi khóa, đổi trạng thái, ghi kết quả kiểm, đánh dấu auth_failed, danh sách, tài khoản active,
  đọc địa chỉ lấy hàng qua `inventory.warehouses_v`); `queries/inventory/pickup.sql` (đọc, ghi hai cột địa chỉ lấy hàng
  có version). Migration mới `0309_inventory_warehouse_pickup_version.sql` (pha mở rộng, chỉ GRANT UPDATE (version,
  updated_at) trên `inventory.warehouses` cho `crm_app` — Quyết định 3). Bảng chạm: `fulfillment.carrier_accounts`,
  `platform.tenant_routes` (qua `platform.put_tenant_route`), `inventory.warehouses`, `platform.jobs`,
  `identity.audit_logs`.
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/...`, `./internal/modules/inventory/...`,
  `./internal/httpapi/ -run 'CarrierAccount|WarehousePickup|M3Routes|Shipment'`, `./cmd/crm/`, rồi `ci-task.sh`
  (migration mới, fieldcrypt và inventory là vùng rủi ro cao: cổng chạy cả bộ vùng đó).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` — `fieldcrypt.Keyring`
  (Seal/Open/NeedsRewrap/CurrentKeyID), `tenancy.PutRoute` + `ErrRouteTaken` (hàm `platform.put_tenant_route` của task
  chung), `tenancy.Resolve`, `commands.CommandKind`/`Handler`/`Looker`/`DoneHook`, `queue.NewQueue().Enqueue`,
  `realtime.Bus.Publish`, `identity.RecordAudit` (qua `handler.audit`), `refusableWrite`/`WriteTx`, `canAt`,
  `customer.NormalizePhone`, `catalog.DefaultSeller`, `couponCursor` (mẫu con trỏ id), `testkit.CaptureLog`,
  `testkit.CaptureSpans`, `testkit.AssertNoPersonalData`; mẫu lệnh cảnh báo `inventory.NewShortageAlertCommand`.
- Dùng lại / viết mới vì: mã hóa chỉ qua `fieldcrypt` (Q-20), tuyến chỉ qua `tenancy.PutRoute` (Q-X8, không câu SQL
  riêng); không viết hàm mã hóa, chuẩn hóa SĐT, ghi audit mới. Viết mới: phần nghiệp vụ tài khoản hãng (thuộc
  fulfillment), lệnh kiểm kết nối và lệnh cảnh báo `carrier-auth` (theo mẫu `NewShortageAlertCommand`, không gộp: khác
  module, khác lý do thay đổi), hàm ghi địa chỉ lấy hàng của `inventory` (chủ dữ liệu). Không thêm thư viện.

## Soát thiết kế

- 5.1 chủ dữ liệu: `fulfillment.carrier_accounts` của fulfillment; cột địa chỉ lấy hàng của `inventory.warehouses` do
  `inventory.SetWarehousePickup` ghi (fulfillment chỉ đọc qua `inventory.warehouses_v`); `platform.tenant_routes` chỉ
  ghi qua `platform.put_tenant_route` (SECURITY DEFINER của task chung).
- 5.2, 5.3: fulfillment không nhập inventory, catalog, identity (luật (a)): đọc kho qua view; người bán mặc định do lớp
  HTTP lấy bằng `catalog.DefaultSeller` (Quyết định 5); audit ghi ở lớp HTTP như mọi route M3.
- 5.4 transaction: mọi đường ghi trong `WriteTx` của handler (một transaction tenant). Lệnh kiểm: `Execute` đọc trong
  một transaction ngắn chỉ đọc, giải mã trong bộ nhớ, gọi hãng **ngoài** transaction; `OnDone` ghi kết quả trong
  transaction của Runner.
- 5.5 sự kiện, báo hiệu: báo hiệu realtime `fulfillment.carrier_account` (quyền `fulfillment.carrier_accounts.read`)
  trong tx, chỉ mã tài khoản; không thêm sự kiện nội bộ (chưa có consumer nào cần).
- 5.6 lệnh ra ngoài: `carrier.check` (CommandKind, hết hạn 10 phút, timeout 20 s, AutoRetryNotSent, Looker luôn
  `not_sent` vì chỉ đọc); cảnh báo Telegram là lệnh riêng `fulfillment.carrier_auth_alert` ghi trong tx của
  `MarkAuthFailed`, Notify chạy ngoài transaction.
- 5.7 truy vấn: SQL tĩnh trong `queries/**` qua sqlc, mọi câu trong `WithTenant`; không simple protocol.
- 5.8: fulfillment, inventory không nhập `net/http`; adapter gọi mạng nằm ở `internal/adapters/carrier/fake`.
- 5.9 đường ghi: Idempotency-Key bắt buộc (hợp đồng), lỗi nghiệp vụ qua `refusableWrite` (không lưu vào khóa chống
  trùng), đổi sang HTTP chỉ ở `errors.go`.
- 5.10, 5.11: không Valkey, không thư viện mới.
- 5.12 theo module mẫu: catalog (sửa có version, khóa dòng rồi so), Task 12 (đường ghi), lệnh `commands`.
- 6.1 khóa: thứ tự ADR-46: `platform.tenant_routes` (nhóm 1, `PutRoute` gọi **đầu** transaction tạo tài khoản) →
  `fulfillment.carrier_accounts` (nhóm 8). Sửa, đổi trạng thái: `SELECT … FOR UPDATE` dòng tài khoản rồi so version.
  `inventory.warehouses`: một câu UPDATE có điều kiện version (không đọc ra Go rồi ghi lại).
- 6.2 chạy lại trong RetryTx: mọi biến kết quả gán lại trong thân hàm (refusableWrite), route_key, mã dòng, khóa mã hóa
  sinh lại mỗi lần chạy (bản mã gắn AAD với mã dòng mới).
- 6.3 hai yêu cầu song song: unique `(tenant, seller, warehouse, carrier) WHERE is_default_for_warehouse` và
  `(tenant, carrier_key, external_account_ref)` của database bắt trùng → 409 `already_exists`; sửa cùng lúc → khóa dòng +
  version → 409 `version_conflict`.
- 6.4 kết quả kiểm cũ: `OnDone` chỉ ghi khi `credential_updated_at` còn bằng lúc `Execute` đọc (ghi khóa mới giữa chừng
  thì bỏ kết quả cũ) và chỉ đổi trạng thái khi tài khoản đang `draft|checking|active|auth_failed` (không bật lại tài
  khoản `paused`, `revoked`).
- 6.5 không chắc đã gửi: lệnh kiểm chỉ đọc nên lỗi không rõ → `not_sent` tự thử lại (có trần 8 lượt, hết hạn 10 phút);
  401/403 → `done` với kết quả `auth_failed` (không thử lại); khóa không giải mã được → `rejected: decrypt_failed`.
- 6.6 mốc thời gian: `last_check_at` ghi khi có kết quả (OnDone), không lúc ghi lệnh.
- 6.7–6.9: không bộ nhớ đệm, không goroutine mới, không lịch.
- 6.10 tiền: không có (`feeOnRefuse` trong tùy chọn mặc định là int64 đồng).
- 6.11 quy luật: tài khoản không `active` thì `ResolveAccount` luôn `ErrAccountUnavailable`, không đổi sang tài khoản
  khác (VC15); `revoked` không quay lại trạng thái nào và giữ tuyến (route_key không cấp lại).
- 6.12 dữ liệu bí mật: khóa API chỉ ở `credential_enc` (AES-GCM, AAD tenant|bảng|cột|dòng); không vào log, dấu vết,
  audit (chỉ tên trường `credentials`), payload/kết quả/last_error của việc, câu trả lời API; `Credentials` tự che khi
  log; `last_check_result` là mã lỗi đã lọc `[a-z0-9_]`, không thông điệp thô của hãng. Địa chỉ lấy hàng là của shop;
  audit chỉ ghi tên trường đổi.
- 6.13 quyền: đọc `fulfillment.carrier_accounts.read` (không có `webhookUrl`), ghi `…manage` (toàn shop); địa chỉ lấy
  hàng `fulfillment.settings.manage` (bảng route sinh từ hợp đồng; guard kiểm trước handler).

## Quyết định thay chủ shop

1. **Đổi tên `AccountInput` của M1 thành `SeedAccountInput`** (commit cấu trúc riêng). Kế hoạch đặt tên `AccountInput`
   cho đầu vào của `CreateAccount` (có `SellerID *uuid.UUID`, credentials…), trùng tên kiểu M1 của `EnsureCarrierAccount`.
   Phương án khác: đặt tên khác cho kiểu M3 (lệch Interface mà các task sau sẽ dùng). Đảo: đổi tên lại.
2. **Mã nằm ở gói `fulfillment` (tệp `accounts.go`, `accounts_check.go`), không ở `fulfillment/internal/accounts`.**
   Kiểu công khai (`AccountInput`, `AccountView`, `Account`, `Credentials`, `Registry`) ở gói gốc; gói `internal` con
   không nhập được gói cha (vòng nhập). Test ở `fulfillment/accounts_test.go`.
3. **Migration mới `0309_inventory_warehouse_pickup_version.sql`: GRANT UPDATE (version, updated_at) ON
   inventory.warehouses TO crm_app.** 0301 chỉ cấp hai cột địa chỉ; không tăng được `version` thì "version cũ → 409"
   của kế hoạch không làm được. Số 0309 vì 0307 (Task 22), 0308 (Task 49) đã giữ chỗ; goose cho chạy lệch số (Task 318).
   Pha mở rộng (chỉ thêm quyền). Đảo: migration mới REVOKE.
4. **`revoked` là trạng thái cuối**: từ `revoked` không đổi sang trạng thái nào, không sửa, không ghi khóa (409
   `invalid_transition`). Kế hoạch: "giữ tuyến để route_key không bao giờ được cấp lại"; bật lại tài khoản đã thu hồi
   thì tạo tài khoản mới.
5. **`SellerID` nil → lớp HTTP điền `catalog.DefaultSeller`** (fulfillment không được nhập catalog, luật (a)); `CreateAccount`
   nhận `SellerID` nil thì lỗi `ErrAccountInvalid`. Người bán hay kho không có → 404 `not_found`
   (`ErrAccountTargetNotFound`, bắt lỗi khóa ngoại).
6. **Mã lỗi theo danh sách có sẵn của hợp đồng (không thêm mã):** `SetStatus active` thiếu khóa, đổi từ `revoked`, kiểm
   tài khoản manual/chưa khóa → 409 `invalid_transition`; trùng mặc định/trùng mã ở hãng → 409 `already_exists`; hãng
   không có adapter, `self` với `api` → 422 `capability_not_supported`; `ResolveAccount` không active → 409
   `carrier_account_unavailable` (chỉ worker dùng, chưa có route); đổi `externalAccountRef` (khóa, crm_app không có
   quyền sửa cột) hay khóa API ở tài khoản manual → 400 `invalid_input`.
7. **Kết quả kiểm:** OK → `active`, `last_check_result = ok`; 401/403 → `auth_failed` + cảnh báo `carrier-auth:<id>`;
   hãng từ chối khác (4xx), `Health.OK = false`, adapter không có `CheckConnection` → `auth_failed` với mã lỗi, không
   cảnh báo; 429, 503, lỗi mạng, quá giờ → `not_sent` thử lại (chỉ đọc nên an toàn). Lệnh kiểm không ghi khi tài khoản
   `paused` (chỉ ghi `last_check_*`), `revoked` → `rejected: account_unavailable`.
8. **`checkCarrierAccount` không đặt `checking`** (tài khoản đang `active` vẫn giao hàng trong lúc kiểm lại);
   `PutCredentials` đặt `checking` và ghi lệnh kiểm (như kế hoạch). Trả 202 `{jobId}`.
9. **Cảnh báo `carrier-auth` là lệnh `fulfillment.carrier_auth_alert`** nối cùng chỗ với lệnh cảnh báo của kho
   (`alertSchedules` ở `cmd/crm/main.go`, dùng chung Alerter) — `fulfillment.Deps.Alerter` chưa được nối (Việc phát hiện
   thêm của Task 95) và Notify gọi mạng nên không chạy trong transaction.
10. **`webhookUrl` = `/hooks/<carrier_key>/<route_key>`** (đường tương đối, đúng chữ kế hoạch); tên nguồn webhook thật
    của từng hãng do Task 10 (vào) quyết.
11. **`inventory.SetWarehousePickup` trả `(version mới, error)`** (kế hoạch ghi chỉ `error`): câu trả lời
    `WarehousePickup` của hợp đồng cần `version`. Thêm `GetWarehousePickup` để lớp HTTP biết trường nào đổi (audit chỉ ghi
    tên trường).
12. **`AccountView` thêm `SellerID`, `DefaultOptions`** (hợp đồng `CarrierAccount` có hai trường này).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1: đổi tên không đổi hành vi, test M1 xanh | cả gói `fulfillment` (`TestCreate*`, `TestSchemaM3*`, …), `TestShipmentAPI` | `go vet ./...`; `go test -race -count=1 ./internal/modules/fulfillment/`; `go test -race ./internal/httpapi/ -run TestShipmentAPI` | `VET-OK`; `ok … fulfillment 6.816s`; `ok … httpapi 1.909s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Quyết định 1, 2, 3, 5, 9, 11, 12. Tệp test: `fulfillment/accounts_test.go`, `inventory/pickup_test.go`,
  `httpapi/carrier_accounts_test.go` (không có `internal/accounts/*_test.go`).

## Việc phát hiện thêm

(không có)

## Reviewer

- Commit 1/2 (đổi tên `AccountInput` → `SeedAccountInput`): **ĐẠT**, không có phát hiện phải sửa. Reviewer grep
  `-w AccountInput` cả cây (Go, test, docs, console, tools): chỉ còn trong tệp task; diff chỉ đổi tên ở 7 dòng.
