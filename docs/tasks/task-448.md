# Task 448 — Tài khoản hãng nhập mã tay tạo được khi chưa nối API hãng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (task mới của điều phối viên 21/11, M3-x1;
quyết định CS-21/11-1, CS-21/11-3, Q-22; việc để lại ghi ở Task 49 = 140 "phần `manual` phải sửa trước khi chạy thật",
Task 100, 106, 439). Loại việc (quy trình mục 3): A

Lỗi: ở production `cmd/crm/wire_m3.go` chỉ nối `carrier.All` khi `carrier.FakeOn` (hãng giả) nên bộ adapter rỗng;
`validateCreate` (accounts.go) đòi khóa hãng có adapter nên tạo tài khoản `mode = manual` của `spx`, `vtp`, `best` trả
422 `capability_not_supported`; chỉ `self` tạo được (Task 106).

Cách sửa: danh mục hãng nhập tay (`carrier.ManualKeys()`: `self`, `vtp`, `best`, `spx` — đúng tập khóa của khung adapter
trong `carrier.All`, không có hãng giả) luôn được nối vào `fulfillment.Deps.ManualCarriers`, kể cả production. Tài khoản
`mode = manual` tạo được khi khóa hãng có adapter **hoặc** có trong danh mục; `mode = api` vẫn đòi adapter như cũ (production
không có adapter nào → vẫn 422). Bộ adapter (`Carriers`) không đổi: không bật lệnh, nguồn webhook hay gọi API hãng nào.
Thêm: `SyncShipment` (nút "Tra lại") từ chối vận đơn `mode = manual` (không ghi việc tra; xem Quyết định 2).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `fulfillment.Deps` thêm trường `ManualCarriers []string` (thêm trường, 5 chỗ dựng `fulfillment.Deps{` không phải đổi:
    `cmd/crm/wire_m3.go` (nối danh mục), `internal/testkit/shiptest/shop.go`, `tests/e2e/stack_test.go` ×2, test của
    module — để trống = chỉ hãng có adapter và `self` như cũ).
  - `carrier.ManualKeys()` mới (gói `internal/adapters/carrier`); `carrier.All` dùng lại hằng khóa, hành vi không đổi.
  - `(*fulfillment.Service).SyncShipment`: chữ ký giữ nguyên, thêm lỗi `ErrCarrierNotSupported` cho vận đơn manual;
    chỗ gọi: `internal/httpapi/handlers_shipment_sync.go:30`, test `track_command_test.go:89,313`,
    `cancel_flow_test.go:117` (cả ba gọi trên vận đơn api hoặc vận đơn không có → không đổi).
  - `validateCreate` (không xuất ra ngoài) đổi điều kiện.
- Gói phụ thuộc ngược (go list): `internal/adapters/carrier` chỉ có `cmd/crm` nhập (cùng test `tests/arch`, `tests/e2e`);
  `internal/modules/fulfillment` có 17 gói nhập (httpapi, cash, order, cmd/crm, adapters/carrier/*, testkit/shiptest…) —
  chỉ thêm trường nên không gói nào phải sửa; ci-task chạy test các gói phụ thuộc.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. `createCarrierAccount` trả 201 thay cho 422 với tài
  khoản manual của hãng trong danh mục; `syncShipment` thêm đường 422 `capability_not_supported` (mã có sẵn trong
  `x-error-codes`, ánh xạ có sẵn ở `errors_fulfillment.go`) → không sửa `errors_fulfillment.go`, không mã lỗi mới.
- Chỗ dùng trên màn hình: chưa có màn "Kết nối vận chuyển" và chưa màn nào gọi `syncShipment`
  (`grep createCarrierAccount|syncShipment console/src` ngoài `src/api` rỗng) → không đổi màn hình.
- Câu sqlc, bảng, pha migration: không có (không câu SQL mới, không migration).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/adapters/carrier/... ./cmd/crm/...
  ./internal/httpapi/ -run 'CarrierAccount|ShipmentSync'`, rồi `scripts/agent/ci-task.sh` (gói đổi + phụ thuộc; fulfillment
  thuộc vùng rủi ro M3).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có danh mục hãng);
  `fulfillment.Registry` (bộ adapter theo khóa, kéo theo lệnh, nguồn webhook, lịch tra — không dùng cho danh mục nhập
  tay được vì thêm vào sẽ bật phần chạy nền cho hãng); `carrier.All`, `carrier.SelfKey`, `vtp.Key`, `best.Key`, `spx.Key`;
  `fulfillment.SelfCarrierKey`; nhánh tay của `Ship` (area_create.go:131, đã cho manual khi không có adapter),
  `RequestCancel` (area_cancel.go:117 chỉ gọi hãng khi `mode = api`), `refresh_status.go` (`errManualAccount`).
- Dùng lại / viết mới vì: dùng lại các hằng khóa hãng của gói khung (một nguồn), `ErrCarrierNotSupported` và ánh xạ HTTP
  có sẵn, helper test `shipEnv` (`order`, `ship`, `cancelShipment`, `commandJobs`, `count`), `shiptest.NewShop`. Viết mới
  `carrier.ManualKeys()` (danh mục không gồm hãng giả; test giữ nó trùng khóa khung của `All`) và trường `ManualCarriers`
  (module không được nhập gói adapter nên danh mục phải truyền vào qua Deps, như `Carriers`).

## Soát thiết kế

- 5.1–5.3: không áp dụng: không dữ liệu, bảng hay view mới; chỉ đổi kiểm ở Go của module fulfillment (chủ bảng
  `carrier_accounts`).
- 5.4: không đổi transaction: `CreateAccount` vẫn một tx của bên gọi; `SyncShipment` kiểm trước khi `Enqueue`, cùng tx.
- 5.5, 5.6: đúng trọng tâm: tài khoản manual không có lệnh ra ngoài nào — tạo vận đơn đi nhánh tay (`create_state
  manual`), hủy thành việc `manual_cancel`, "Tra lại" bị từ chối trước khi ghi việc. Không bật adapter, lệnh, nguồn
  webhook nào (bộ `Carriers` production vẫn rỗng; test `TestWireM3_KeepsM1Parts` giữ nguyên).
- 5.7: test chạy trong `tenancy.WithTenant` (crm_app).
- 5.8: module không nhập gói adapter; danh mục truyền vào từ `cmd/crm` qua Deps (luật go-backend 4, 7).
- 5.9: không đường ghi mới; `createCarrierAccount`, `syncShipment` giữ quyền và khóa chống trùng có sẵn.
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: theo mẫu Deps của module (Carriers), lỗi sentinel có sẵn.
- 6.1–6.9: không áp dụng: không câu khóa, không thứ tự khóa mới, không việc nền mới (chỉ bớt một đường ghi việc).
- 6.10: không áp dụng: không tiền.
- 6.11: không áp dụng: điều kiện kiểm thuần, test bảng phủ đủ tổ hợp (hãng trong/ngoài danh mục × api/manual × production/dev).
- 6.12: không dữ liệu cá nhân mới vào log, lỗi chỉ mang khóa hãng và mode.
- 6.13: quyền không đổi: `fulfillment.carrier_accounts.manage` cho tạo, `fulfillment.read` cho tra.

## Quyết định thay chủ shop

1. **Danh mục nhập tay là một danh sách khóa riêng, không nối khung adapter vào bộ `Carriers` ở production.** Chọn: thêm
   `carrier.ManualKeys()` và `Deps.ManualCarriers`. Vì sao: nối các khung (`best.New`, `spx.New`, `manual.New(vtp)`) vào
   `Carriers` ở production sẽ bật lệnh `fulfillment.create_shipment`, `refresh_status`, `command.shipment.cancel`,
   `update_address` và cho tạo tài khoản `mode = api` cho hãng chưa nối (trái yêu cầu "api vẫn bị từ chối", trái
   CS-21/11-1). Phương án khác: (b) nối khung vào `Carriers` và lọc `mode = api` theo cờ khả năng — đổi nhiều đường chạy
   nền, khó đảo; (c) bỏ kiểm khóa hãng cho manual — cho tạo hãng gõ sai (`ghn`, `fake` ở production). Đảo lại: xóa trường
   và hàm, không có dữ liệu phải đổi.
2. **"Tra lại" (`syncShipment`) trên vận đơn nhập tay trả 422 `capability_not_supported`, không ghi việc tra.** Vì sao:
   yêu cầu của task "không lệnh gửi/hủy/tra nào được ghi cho tài khoản manual"; trước đây việc tra vẫn được ghi rồi worker
   từ chối (`manual_account`), còn ở production không có handler `refresh_status` nên việc nằm chờ tới hết hạn. Phương
   án khác: giữ ghi việc rồi để worker từ chối (sinh việc thừa, production treo). Dùng mã có sẵn, không đổi hợp đồng.
3. **Ở dev (có hãng giả) `mode = api` của khung `spx`, `best` vẫn tạo được như trước** ("vẫn bị từ chối như cũ" = giữ
   nguyên luật cũ: có adapter mới được api). Siết thêm theo cờ khả năng là việc của task nối hãng thật (Task 140), không
   làm ở đây.
4. Hãng giả `fake` không nằm trong danh mục: tài khoản `fake` (cả manual) chỉ tạo được khi hãng giả được nối (dev).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Lỗi tái hiện trước khi sửa: production từ chối spx/vtp/best manual | `TestManualCatalog_CreateAccount/production`, `TestManualCatalog_ProductionShipmentFlow`, `TestWireM3_ManualAccountsWithoutCarrierAPI` (viết trước; mã mới chỉ có `ManualKeys` và trường Deps) | `go test -race -count=1 ./internal/modules/fulfillment/ -run TestManualCatalog`; `go test -race -count=1 ./cmd/crm/ -run TestWireM3_Manual` | FAIL: `spx/manual: hãng "spx", mode manual: fulfillment: hãng không được hỗ trợ, muốn tạo được` (cả vtp, best; cả hai ca production của cmd/crm) |
| Production (bộ hãng rỗng): manual spx, vtp, best, self → active, không khóa; api spx, vtp, best, self; fake (cả manual), ghn, khóa rỗng → ErrCarrierNotSupported | `TestManualCatalog_CreateAccount/production` | `go test -race -count=1 ./internal/modules/fulfillment/ -run 'TestManual\|TestAccounts\|TestCancel'` | `ok elimaz.com.vn/crm/internal/modules/fulfillment 20.872s` |
| Dev (`carrier.All` có hãng giả): manual spx, vtp, best, self, fake; api fake, spx, best (như cũ); self api, ghn → từ chối | `TestManualCatalog_CreateAccount/dev` | như trên | ok |
| Nối thật của tiến trình (`wireM3`, PostgreSQL thật): production (có/không FAKECARRIER_URL) manual spx/vtp/best → active; api → ErrCarrierNotSupported (422); ghn manual → từ chối; dev manual tạo được | `TestWireM3_ManualAccountsWithoutCarrierAPI` (3 ca) | `go test -race -count=1 ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 2.729s` |
| Không bật lệnh, nguồn webhook nào ở production | `TestWireM3_KeepsM1Parts/production` (không sửa: lệnh chỉ `command.cash.statement_parse`, 0 nguồn) | như trên | ok |
| Vận đơn tài khoản tay spx/vtp/best (production): dán mã → awaiting_pickup, create_state manual; "Tra lại" → ErrCarrierNotSupported; hủy → cancel_requested + manual_cancel; ghi tay đã hủy → cancelled; 0 việc `mode = 'command'` (cả lúc tạo tài khoản) | `TestManualCatalog_ProductionShipmentFlow/{spx,vtp,best}`, `TestManualCatalog_CreateAccount` | `go test -race -count=1 ./internal/modules/fulfillment/ -run TestManual` | ok |
| HTTP `syncShipment` trên vận đơn tài khoản manual → 422 `capability_not_supported`, số việc `refresh_status` giữ 1 | `TestSyncShipmentAPI` (ca cuối) | `go test -race -count=1 ./internal/httpapi/ -run 'TestSyncShipmentAPI\|CarrierAccount'` | `ok elimaz.com.vn/crm/internal/httpapi 4.812s`; trước khi sửa: `vận đơn tay = 202 {"jobId":…}, muốn 422` |
| Danh mục = đúng khóa khung của `All` không có hãng giả, trả bản sao | `TestManualKeys` | `go test -race -count=1 ./internal/adapters/carrier/` | `ok elimaz.com.vn/crm/internal/adapters/carrier 2.100s` |
| Cổng kiểm của task trên bản đã gộp main 501f55c | build, vet, lint, catalog, jscpd, gitleaks, oasdiff, test-guard, test (toàn bộ vì main đổi testkit: 78 gói ok) | `ci-task.sh` (thư mục script agent) | `CI-TASK XANH (so với điểm tách nhánh 501f55c107b0239e96c168ee1edace3df3495aa3)`; lượt trước đó bị dừng ở giới hạn 10 phút, có `TestListEntityAudit` chập chờn (Việc phát hiện thêm) |
| Lint test mới | golangci-lint trên 4 gói đổi | `golangci-lint run ./cmd/crm/... ./internal/modules/fulfillment/... ./internal/httpapi/... ./internal/adapters/carrier/...` | `0 issues.` (lượt cổng kiểm 1 đỏ vì gocognit, wrapcheck ở test mới → đã tách hàm) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm vào phạm vi: `SyncShipment` từ chối vận đơn của tài khoản manual (Quyết định 2) — cần cho tiêu chí "không lệnh tra
  nào được ghi cho tài khoản manual" của task. Mode của vận đơn đọc theo `ShipmentView.Mode` (= `a.mode` của tài khoản,
  cùng nguồn với worker tra `refresh_status.go`).
- Gộp main 501f55c (Task 424) trước review: `testkit.RecordingBus` đã dời sang `realtime/realtimetest` → test mới dùng
  `realtimetest.RecordingBus`.

## Việc phát hiện thêm

- Kế hoạch M3 (Task 140, "Việc để lại"): câu "`createCarrierAccount` trả 422 cả với `manual`… phần `manual` phải sửa
  trước khi chạy thật" đã xong ở task này; điều phối viên cập nhật câu đó (tệp ngoài `apps/crm`, không tự sửa).
- Ở dev, khung `spx`, `best` (mọi khả năng false) vẫn nhận tài khoản `mode = api` (luật cũ, Quyết định 3): khi mở lại
  tích hợp hãng nên đòi cờ khả năng `Create` cho mode api (Task 140).
- Test chập chờn có sẵn (không do task này): `identity.TestListEntityAudit` (audit_list_test.go:62) dò chuỗi "0933" trong
  JSON có cả UUID ngẫu nhiên → đỏ khi UUID chứa "0933" (gặp ở lượt cổng kiểm thứ ba: id `…-09330b9a059d`); chạy riêng
  `go test -race -count=1 ./internal/modules/identity/ -run TestListEntityAudit` → ok. Sửa: dò SĐT đầy đủ thay cho 4 chữ số.

## Reviewer

Kết luận: ĐẠT (lượt 1, phần đã stage sau khi gộp main 501f55c).

- Phát hiện phải sửa: không có.
- Đã kiểm mọi đường ra hãng cho tài khoản manual khi không có adapter: tra định kỳ (`PollCandidates` lọc `a.mode = 'api'`),
  báo giá (`canQuote` đòi api), kiểm tài khoản (`accounts_check.go:102`), đổi địa chỉ (`area_address.go:162`), tạo vận đơn
  (`area_create.go:130-148`), nhãn — không đường nào ghi lệnh ra hãng.
- `UpdateAccount` không đổi được `Mode` → không có đường vòng tạo tài khoản api cho hãng chưa nối.
- Ghi chú không bắt buộc: mục Ảnh hưởng ghi "5 chỗ dựng `fulfillment.Deps{`" là chỗ ngoài test của module (thực tế khoảng
  30 chỗ tính cả test); không ảnh hưởng vì chỉ thêm trường. Phép kiểm "số việc vẫn 1" trong `TestSyncShipmentAPI` yếu do
  chống trùng theo phút; phép kiểm 422 đã đủ. `errManualAccount` của worker giữ làm chốt phòng (việc tra do webhook ghi ở
  dev), không phải cách cũ còn sót.
