# Task 108 — M3-17: Đổi địa chỉ sau khi đã gửi hãng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 108, M3-17; ca DH15, WAT27; Q-23). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `order`: thêm `CheckShipAddress` (kiểm và chuẩn hóa địa chỉ giao như `ApplyAddressChange`; tách từ thân
    `ApplyAddressChange`, hàm này gọi lại nó — không đổi hành vi). `ApplyAddressChange`: 1 chỗ gọi mã thật mới (fulfillment), 1 chỗ gọi trong test (hàm phụ `changeAddress`, `order/shipping_test.go:80`, dùng bởi 2 test).
  - `fulfillment`: thêm (`area_address.go`, `address_command.go`) `AddressChange`, `AddressChangeInput`, `AddressResolveInput`, `AddressChangeResult`,
    `AuditRecord`, `AuditFunc`, `UpdateAddressKind()`, `UpdateAddressCommand`, `NewUpdateAddressCommand`,
    `(*Module).AddressCommand`, `(*Service).RequestAddressChange`, `(*Service).ResolveAddressChange`, lỗi
    `ErrAddressChangeOpen`, `ErrAddressChangeNotAllowed`; hàm riêng `commandAccount` (tài khoản + bảng địa giới cho lệnh gọi
    hãng). Không ký hiệu cũ nào đổi chữ ký.
  - `httpapi`: thân `RequestAddressChange`, `ResolveAddressChange`; thêm `RecordFulfillmentAudit` (cmd/crm dùng làm
    `fulfillment.AuditFunc`); hai dòng ánh xạ lỗi cuối khối fulfillment của `errors.go`.
  - `cmd/crm/wire_m3.go`: thêm lệnh `command.shipment.update_address` khi có adapter (cuối khối M3); `wire_m3_test.go`
    thêm Kind đó vào danh sách lệnh mong đợi.
  - Mã sinh: `internal/modules/fulfillment/internal/store/address.sql.go`, mọi `internal/**/store/models.go` (sqlc thêm
    trường `Note` vào `FulfillmentAddressChangeRequest`; `make gen`).
  - `fulfillment/internal/carriers/fake`: thêm `SetUpdateMode` + hằng `UpdateMode*`; `Capabilities()` thêm cờ
    `UpdateAddress` (mặc định false: test cũ không đổi).
- Gói phụ thuộc ngược (go list): `order` ← cmd/crm, httpapi, cash, cash/internal/refunds, fulfillment, testkit/codtest,
  testkit/shiptest. `fulfillment` ← cmd/crm, adapters/carrier/{,best,carriertest,fake,manual,spx,vtp}, httpapi, cash
  (+ internal/formats, statement), fulfillment/internal/carriers/fake, testkit/{codtest,shiptest}, tools/fakecarrier/server.
- Operation OpenAPI bị chạm, oasdiff: `requestAddressChange`, `resolveAddressChange` (đã có trong hợp đồng từ Task M3-04;
  không sửa `openapi.yaml`, oasdiff không đổi); bỏ hai dòng khỏi `tests/arch/testdata/m3_pending.txt`. Mã lỗi chỉ dùng mã
  có sẵn: `already_exists`, `invalid_transition`, `address_unmapped`, `address_incomplete`, `invalid_input`, `not_found`.
- Chỗ dùng trên màn hình: chưa có (Task 134 M3-43 vẽ nút đổi địa chỉ). `console/src/api/**` không đổi (hợp đồng không đổi).
- Câu sqlc, bảng, pha migration: tệp mới `queries/fulfillment/address.sql` (khóa, chèn, đổi trạng thái yêu cầu; đọc
  target của lệnh; ghi `ship_to` vận đơn). Bảng `fulfillment.address_change_requests` (có từ 0300). Migration mới
  `0329_fulfillment_address_change_note.sql`: pha mở rộng, thêm cột `note text` (≤ 1000 ký tự, đã che SĐT/địa chỉ có số)
  và GRANT UPDATE (note) cho crm_app — vì hợp đồng `AddressChange.note` cần chỗ lưu.
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/... ./internal/modules/order/... ./internal/httpapi/...
  ./cmd/crm/... ./tests/arch/...`; ci-task.sh một lần (order, fulfillment là vùng rủi ro cao → cả bộ vùng).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands.CommandKind,
  commands.Abandon, logging.ScrubText, queue.Enqueue); trong fulfillment: `lockForCancel` (khóa đơn rồi vận đơn, kèm kho,
  mode, tracking), `checkMapped`/`LocationMap` (Task 109/M3-12), `classifyCreateError`, `targetFailed`, `ResolveAccount`,
  `publishShipment`, `commandBusy`, `shipToDoc`; trong order: `Writer.ApplyAddressChange` (Task 97), `cleanAddress`.
- Dùng lại / viết mới vì:
  - Dùng lại `lockForCancel` (đúng thứ tự khóa ADR-46 nhóm 7 → 8, có Authorize), `checkMapped`, `classifyCreateError`,
    `ResolveAccount` + `LocationMap` (qua hàm mới `commandAccount`, như lệnh hủy, lệnh tạo), `awaitingShipment` (test HTTP), `commands.Abandon` (bỏ lệnh in_doubt khi nhân viên tự
    xác nhận), `shipToDoc` (dạng cột `ship_to`), `ApplyAddressChange`.
  - Viết mới `order.CheckShipAddress`: fulfillment phải kiểm địa chỉ lúc tạo yêu cầu (trước khi gọi hãng) mà không được
    nhập `customer`; tách từ `ApplyAddressChange` để một chỗ kiểm (không chép luật).
  - Viết mới lệnh `UpdateAddressCommand` (không gộp với `CancelCommand`): cùng khung (target → gọi hãng → OnDone/OnTerminal)
    nhưng khác bảng, khác kết quả, khác lý do thay đổi. jscpd lần 1 báo 4 đoạn trùng: phần đọc tài khoản (giống lệnh hủy,
    lệnh tạo) → gom thành `commandAccount`; khối từ chối không gọi hãng → `addressRefusal`; đọc payload viết lại; test HTTP
    dùng lại `awaitingShipment`. Lần 2: `Found 0 clones.` Chưa sửa `cancel_command.go`, `create_shipment.go` cho dùng
    `commandAccount` (ngoài vùng; ghi ở Việc phát hiện thêm).
  - `AuditFunc`: fulfillment không được nhập identity (tests/arch moduleCalls) mà hook OnDone của lệnh (chạy trong worker)
    phải ghi audit khi địa chỉ đơn đổi (Task 97 dặn) → cmd/crm truyền hàm bọc `identity.RecordAudit`.

## Soát thiết kế

Mục 5:
- 5.1–5.3: yêu cầu đổi địa chỉ thuộc fulfillment (bảng của fulfillment); địa chỉ đơn chỉ đổi qua `order.Writer.ApplyAddressChange`
  (public API của order), không ghi `ord.orders`. Đọc đơn qua `order.Get`/`order.LockSnapshot`.
- 5.4: tạo yêu cầu + ghi lệnh trong một transaction (WriteTx); xác nhận (OnDone hay resolve) đổi yêu cầu, `ship_to`, địa
  chỉ đơn, audit trong cùng một transaction.
- 5.5: gọi hãng sau commit qua lệnh `command.shipment.update_address`.
- 5.6: gọi hãng chỉ qua lệnh có `in_doubt`; `AutoRetryNotSent = false`; không rõ → yêu cầu `unknown`, nhân viên xác nhận.
- 5.7: bảng có sẵn RLS, unique ghép tenant; mọi câu trong `tenancy.WithTenant`/WriteTx; chèn lấy tenant từ `app.tenant_id`.
- 5.8: module không nhập net/http; handler mỏng.
- 5.9: hai operation có `x-permission: fulfillment.ship`, `IdempotencyKey`; phạm vi kho kiểm dưới khóa (`shipAuthorize`).
- 5.10: không áp dụng: không dùng Valkey. 5.11: không thêm thư viện. 5.12: theo mẫu lệnh hủy (Task M3-14) và vùng manual.

Mục 6:
- 6.1: hai yêu cầu cùng vận đơn: cả hai khóa đơn rồi vận đơn (`lockForCancel`), kiểm "còn yêu cầu mở" dưới khóa; index
  unique `address_change_requests_open` là chốt cuối (vi phạm unique → `ErrAddressChangeOpen`). Có test hai yêu cầu song song.
  Resolve và OnDone cùng khóa vận đơn rồi yêu cầu (`FOR UPDATE`), chỉ đổi khi yêu cầu còn ở trạng thái cho phép.
- 6.2: Idempotency-Key ở HTTP; lệnh có DedupeKey `address:<id yêu cầu>`.
- 6.3: thứ tự khóa ADR-46: ord.orders (7) → shipments → address_change_requests (8) → platform.jobs (13, `Enqueue`,
  `Abandon` cuối). ApplyAddressChange khóa lại đơn đã khóa (không đổi thứ tự).
- 6.4: WriteTx/RetryTx chạy lại được: thân hàm gán lại kết quả mỗi lần.
- 6.5: READ COMMITTED + khóa dòng. 6.6: lệnh `dead` (chắc chưa gửi, hãng từ chối) → yêu cầu `rejected` (OnTerminal).
- 6.7: 429 → not_sent; không tự gửi lại (AutoRetryNotSent false) → dead → rejected, nhân viên tạo yêu cầu mới.
- 6.8: worker chết giữa lúc gọi hãng → in_doubt → OnTerminal → `unknown`; Looker luôn `unknown` (không tra được kết quả
  sửa địa chỉ ở mức chung).
- 6.9: lệnh timeout 20 s. 6.10: không áp dụng: không đụng tiền.
- 6.11: rapid: không áp dụng riêng ở task này (quyết định thuần nhỏ, test bảng đủ mọi ca trong `internal/address`); quy
  luật "địa chỉ đơn chỉ đổi khi hãng nhận" có test luồng mỗi nhánh.
- 6.12: địa chỉ mới là dữ liệu cá nhân: chỉ ở cột `new_address`, `ship_to`, `ord.orders`; không vào payload lệnh (chỉ id),
  không log, audit chỉ `changed_fields` `ship_address` (giá trị bị che theo khóa `address`); ghi chú qua `logging.ScrubText`.
- 6.13: quyền `fulfillment.ship` theo kho của tài khoản hãng của vận đơn, kiểm dưới khóa; thiếu → 403.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| DH15: hãng có UpdateAddress → pending api, địa chỉ đơn và ship_to vẫn cũ, payload lệnh không có địa chỉ; hãng nhận → confirmed, địa chỉ đơn và ship_to mới, một order.updated, audit ship_address (system) | `TestAddress_DH15CarrierAccepts` | `go test -race -count=1 -v -run 'TestAddress|TestUpdateAddressCommand' ./internal/modules/fulfillment/ ./internal/httpapi/` | `--- PASS: TestAddress_DH15CarrierAccepts (0.48s)` |
| Hãng không có UpdateAddress → manual, không lệnh; địa chỉ đơn chỉ đổi khi resolve confirmed (audit staff); xác nhận lần hai → ErrAddressChangeNotAllowed; kết quả lạ → ErrShipInvalid | `TestAddress_ManualResolve`, `TestAddress_ManualRejected` | như trên | `--- PASS: TestAddress_ManualResolve (0.34s)`, `--- PASS: TestAddress_ManualRejected (0.41s)` |
| Hãng từ chối (UpdateRejected), chắc chưa gửi (not_sent → dead) → rejected, địa chỉ cũ, không audit; yêu cầu mới nhận được | `TestAddress_CarrierRejects` (2 ca) | như trên | `--- PASS: TestAddress_CarrierRejects (0.54s)` |
| UpdatePending → sent, nhân viên xác nhận (cả khi lệnh done đã bị dọn khỏi platform.jobs — sửa sau reviewer lần 1); quá giờ sau khi gửi → in_doubt → unknown, Looker không biết, không gọi lần hai, resolve rejected bỏ lệnh (expired) | `TestAddress_PendingThenStaff`, `TestAddress_InDoubtThenStaff` | như trên | `--- PASS: TestAddress_PendingThenStaff` (lần chạy sau sửa: `ok elimaz.com.vn/crm/internal/modules/fulfillment 4.167s` với `-count=1 -run 'TestAddress|TestUpdateAddressCommand'`), `--- PASS: TestAddress_InDoubtThenStaff (0.20s)` |
| Yêu cầu thứ hai khi còn mở → ErrAddressChangeOpen; hai yêu cầu song song → đúng một nhận | `TestAddress_OneOpen` | như trên | `--- PASS: TestAddress_OneOpen (0.44s)` |
| Vận đơn delivered, creating → ErrAddressChangeNotAllowed; shipping → nhận; thiếu xã, xã lạ, Authorize, vận đơn lạ → từ chối, không yêu cầu; địa chỉ chưa khớp carrier_locations → ErrAddressUnmapped | `TestAddress_Refusals`, `TestAddress_Unmapped` | như trên | `--- PASS: TestAddress_Refusals (0.40s)`, `--- PASS: TestAddress_Unmapped (0.22s)` |
| Lệnh: AutoRetryNotSent false, tên Kind; AddressCommand đòi audit, không hãng → nil | `TestUpdateAddressCommand_Policy` | như trên | `--- PASS: TestUpdateAddressCommand_Policy (0.12s)` |
| HTTP DH15: updateOrder đổi địa chỉ khi có vận đơn → 409 order_locked_by_shipment; 403; 201 pending manual cùng khóa cùng thân; 409 already_exists; resolve 200, audit order.address_change có changed_fields ship_address, before/after không có địa chỉ, SĐT; resolve lần hai 409 invalid_transition; 404 | `TestAddressChangeAPI_DH15` | như trên | `--- PASS: TestAddressChangeAPI_DH15 (0.67s)` |
| HTTP: vận đơn lạ 404, xã lạ 422 unknown_admin_unit, đã giao 409 invalid_transition | `TestAddressChangeAPI_Refusals` | như trên | `--- PASS: TestAddressChangeAPI_Refusals (0.34s)` |
| Quyết định thuần (đổi được, xác nhận tay được, kết quả hãng, OnTerminal, còn mở, khóa chống trùng) | `TestChangeable`, `TestResolvable`, `TestResolution`, `TestFromCarrier`, `TestTerminal`, `TestOpen`, `TestDedupeKey` | `go test -race ./internal/modules/fulfillment/internal/address/` | `ok elimaz.com.vn/crm/internal/modules/fulfillment/internal/address 1.945s` |
| `order.CheckShipAddress`: chuẩn hóa, thiếu xã, SĐT sai, xã lạ | `TestCheckShipAddress` | `go test -race -run 'TestCheckShipAddress|TestApplyAddressChange' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 18.333s` |
| wireM3 có lệnh sửa địa chỉ khi có hãng giả, không có ở production | `TestWireM3_KeepsM1Parts` | `go test -race -run TestWireM3 ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 1.630s` |
| Mọi cổng tĩnh + toàn bộ test (đổi migration → cả bộ) | ci-task | `ci-task.sh` | lần 2: `CI-TASK XANH (so với điểm tách nhánh c382cc2…)`, test 569 giây (`ok …/fulfillment 410.9s`, `ok …/order 541.3s`, `ok …/httpapi 284.6s`, `ok …/tests/arch 49.1s`, `ok …/cmd/crm 1.6s`); lần 1 ĐỎ: lint (gocognit `RequestAddressChange`, gofumpt 2 tệp test), gen-check (mã chưa stage), jscpd 4 đoạn trùng — đã sửa (tách `checkAddressRequest`, `insertAddressChange`; `commandAccount`, `addressRefusal`; dùng lại `awaitingShipment`) |
| Commit 1 (cấu trúc): tách `checkShape`, `validateUnits` khỏi `ApplyAddressChange`, không đổi hành vi | `TestApplyAddressChange`, `TestApplyAddressChange_Delivered` và cả gói order | `go test -race ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 216.850s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `UpdateAddressKind` là hàm, không phải `var` như kế hoạch ghi: luật Go của dự án cấm biến cấp gói thay đổi được; theo mẫu
  `CancelCommandKind()`.
- Lệnh nằm ở `fulfillment/address_command.go`, không ở `internal/address/command.go` (gói internal không nhập được
  Service, adapter, ApplyTracking mà không vòng nhập); `internal/address/command.go` giữ phần thuần của lệnh (kết quả hãng
  → trạng thái, OnTerminal → trạng thái).
- Thêm `order.CheckShipAddress` (tách ở commit 1 cấu trúc, xuất ở commit 2): kế hoạch không nêu, cần để kiểm địa chỉ trước
  khi gọi hãng (fulfillment không nhập customer).
- Migration 0329 (cột `note`): kế hoạch không nêu, hợp đồng `AddressChange.note` cần chỗ lưu.
- Audit đổi địa chỉ đơn: tên trường `ship_address` (đúng kế hoạch), action `order.address_change`, thực thể `order`; trong
  hook của lệnh ghi qua `AuditFunc` do cmd/crm truyền (`httpapi.RecordFulfillmentAudit`).

## Quyết định thay chủ shop

1. **Lệnh kết thúc dead/expired (hãng từ chối hẳn, chắc chưa gửi mà không tự gửi lại)** → yêu cầu `rejected` (kế hoạch chỉ
   nói in_doubt → unknown). Vì sao: hãng chắc chưa nhận, địa chỉ đơn giữ cũ là đúng DH15; `rejected` đóng yêu cầu để nhân
   viên tạo yêu cầu mới (gửi lại) hay gọi hãng. Phương án khác: để `unknown` (nhân viên xác nhận tay) — bị loại vì sai nghĩa
   "không rõ đã gửi chưa". Đảo ngược dễ: một dòng ở `address.Terminal`.
2. **Mã lỗi chỉ dùng mã có sẵn trong hợp đồng** (như tệp task 100, 69): còn yêu cầu mở → 409 `already_exists`; vận đơn
   không đổi được địa chỉ, yêu cầu không xác nhận tay được → 409 `invalid_transition`. Phương án khác: thêm mã mới vào
   enum `Error` (đổi hợp đồng, console) — để task màn hình quyết.
3. **Ghi chú** lưu bản đã che SĐT, địa chỉ có số (`logging.ScrubText`) ngay trong Service; resolve có ghi chú thì thay
   ghi chú của yêu cầu (một cột, hợp đồng có một trường `note`); audit không ghi ghi chú.
4. **Nhân viên xác nhận yêu cầu api `unknown`/`sent`**: lệnh còn in_doubt bị bỏ (`commands.Abandon`, expired) để không còn
   trong danh sách "không rõ"; lệnh đang chạy lại → 409 `invalid_transition`; lệnh đã bị dọn (done quá 7 ngày) → bỏ qua;
   lỗi database của Abandon trả lên (WriteTx chạy lại / 503), không thành lỗi nghiệp vụ.
7. **Lỗi đọc database trong `UpdateAddressCommand.target`** → `not_sent` (như lệnh tạo, lệnh hủy); không tự gửi lại nên
   lệnh dead → yêu cầu `rejected`, nhân viên tạo lại yêu cầu. Chấp nhận vì nhất quán với Quyết định 1 và không bao giờ
   đổi địa chỉ đơn sai.
5. **Hãng nhận đổi địa chỉ mà đơn đã xong hay vận đơn không còn hiện hành** (`order.ErrInvalidTransition`): yêu cầu vẫn
   `confirmed`, `ship_to` mới, địa chỉ đơn không đổi, không audit đơn. Phương án khác: lỗi và lùi — bị loại vì hãng đã nhận
   thật.
6. **Khớp mã địa giới**: chỉ kiểm khi vận đơn api và adapter có bảng địa giới (`Capabilities.Locations`), cùng điều kiện
   với gửi hãng (Task M3-12).

## Việc phát hiện thêm

- `cancel_command.go` (`CancelCommand.target`) và `create_shipment.go` (`CreateCommand.target`) có cùng khối đọc tài khoản
  như `commandAccount` mới; nên chuyển sang dùng `commandAccount` ở một task cấu trúc riêng (ngoài vùng của task này).
- Lỗi có sẵn (ngoài task, reviewer nêu): view `fulfillment.shipments_v` đọc `ship_to ->> 'province_code'`
  (`migrations/0300_fulfillment_m3.sql:341`) mà `shipToDoc` ghi khóa `provinceCode` (`area_create.go`) → cột
  `province_code` của view có thể luôn NULL. Cần một migration sửa view ở task riêng.
- Hợp đồng `AddressInput` đòi `wardCode`, nên qua HTTP thiếu xã ra 400 `bad_request` (lớp ogen), không tới
  `address_incomplete`; đường Service vẫn trả `order.ErrAddressIncomplete` (có test).

## Reviewer

Commit 1 (cấu trúc, tách `checkShape`, `validateUnits`): ĐẠT, không phát hiện phải sửa (thứ tự kiểm, lỗi, JSON giữ nguyên). Ghi chú đã làm: sửa số chỗ gọi trong mục Ảnh hưởng; commit 2 phải có test nhánh mã địa giới không có.

Commit 2 (hành vi), lần 1: CẦN SỬA — `ResolveAddressChange` bọc mọi lỗi của `commands.Abandon` thành
`ErrAddressChangeNotAllowed`: lệnh done đã bị dọn (ErrCommandNotFound) làm yêu cầu `sent` kẹt mở mãi, lỗi database tạm
thành 409. Đã sửa: chỉ `ErrCommandRunning` → 409, `ErrCommandNotFound` bỏ qua, lỗi khác trả lên; thêm ca vào
`TestAddress_PendingThenStaff`. Ghi chú không bắt buộc đã ghi (view `shipments_v` ở Việc phát hiện thêm; lỗi đọc trong
target ở Quyết định 7).

Commit 2, lần 2: ĐẠT, không phát hiện phải sửa (ba nhánh lỗi của Abandon khớp `commands.Abandon`; ca mới đỏ với mã cũ).
Ghi chú không bắt buộc: nhánh `ErrCommandRunning` chưa có test (khó dựng); có thể kiểm thêm lệnh đã bị xóa thật trong ca
mới; deadlock có thể có giữa xử lý tay lệnh in_doubt sang done và ResolveAddressChange được PostgreSQL phát hiện (40P01),
RetryTx chạy lại — cùng khuôn với lệnh hủy, không sai dữ liệu.

Kết luận: ĐẠT
