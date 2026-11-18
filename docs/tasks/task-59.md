# Task 59 — M2-02: Hợp đồng API M2, thứ tự khóa, view

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 59, M2-02). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mã sinh `internal/httpapi/gen`: thêm 55 operation (phương thức mới của `gen.Handler`, đã có `gen.UnimplementedHandler`
    nhúng trong `handler` ở `handlers_health.go:19` nên không phải viết handler); kiểu đổi của hai operation M1:
    `gen.CreateOrderRequest.Customer` (bắt buộc → tùy chọn, thành `OptCreateOrderRequestCustomer`),
    `gen.CreateOrderRequest.ShipAddress` (bắt buộc → tùy chọn), `gen.CreateOrderRes` (thêm 200 cho lần gửi lại).
    Chỗ dùng: chỉ `internal/httpapi/handlers_order.go` (`CreateOrder`, `orderOut`), test `handlers_order_test.go`.
    `gen.OrderStatus` giữ tên (enum nội tuyến của `Order.status` thành `$ref: OrderStatus` cùng giá trị).
  - `order`: thêm 13 sentinel vào `order/errors.go` (không đổi sentinel có sẵn); `order.ErrStatusConflict` (M1, chưa
    ánh xạ HTTP) được ánh xạ. `customer`: tệp `errors.go` mới (13 sentinel, 2 kiểu lỗi). `promotion`: gói mới.
  - Ba `module.go` mới (`Background(background.Deps) background.Parts`), chỉ `cmd/crm` gọi; gói chung mới
    `internal/platform/background` (`Deps`, `Parts`).
  - `testkit.SeedCatalog`, `SeedOptions`, `SeedVariant`, `Catalog` mới; `catalogtest` thêm hai helper test (biến thể
    có SKU chọn được, ngừng bán biến thể).
- Gói phụ thuộc ngược (go list): `internal/httpapi/gen` ← `internal/httpapi`; `internal/modules/order` ←
  `internal/httpapi`, `internal/modules/cash`, `internal/modules/fulfillment`, `internal/testkit/shiptest`;
  `internal/modules/customer` ← `internal/httpapi`, `internal/modules/order`, `internal/testkit/shiptest`;
  `internal/testkit` ← gần như mọi gói test (thêm tệp, không đổi hàm có sẵn); `cmd/crm` (nối module).
- Operation OpenAPI bị chạm, oasdiff: 55 operation mới (bảng của kế hoạch); `createOrder` (thêm trường tùy chọn, nới
  `customer`, `shipAddress` thành tùy chọn, thêm câu trả lời 200), `getOrder` (thêm trường tùy chọn vào `Order`,
  `OrderItem`). Không xóa, không đổi tên, không thêm trường bắt buộc vào yêu cầu → oasdiff phải không báo phá vỡ.
  Thêm mã lỗi M2 vào `x-error-codes`.
- Chỗ dùng trên màn hình: client sinh lại (`console/src/api/**`); console chưa gọi `createOrder`/`getOrder`. Bảng câu
  lỗi `console/src/app/errors.ts` phải có câu cho mọi mã mới (test `errors.test.ts` đòi đúng tập mã).
- Câu sqlc, bảng, pha migration: không câu sqlc, không bảng, không migration. Chỉ tài liệu: dòng thứ tự khóa M2 trong
  `docs/adr/0046-thu-tu-khoa.md` (chèn), dòng view mới trong `docs/views.md` (thêm; không sửa dòng M1).
- Test sẽ chạy: `go test -race ./internal/httpapi/...` (route, lỗi, đơn M1), `./tests/arch/...` (ranh giới, SQL,
  view, tiền, lỗi), `./internal/testkit/...` (SeedCatalog), `./internal/modules/{order,customer,promotion}/...`,
  `./cmd/crm/...`, `cd console && npx tsc --noEmit && npx vitest run src/app/errors.test.ts`; `make oasdiff`; cuối
  cùng cổng kiểm theo task một lần (xem Quyết định 12).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit:
  `SeedSellers`, `ForEachSeller`; invtest: `NewShop`, `Shop.Variant`, `SeedStock`; catalogtest: `Sellers`, `Product`,
  `Variant`); grep `SeedCatalog` (không có); `gen.UnimplementedHandler` (đã nhúng); bảng ánh xạ lỗi theo module trong
  `internal/httpapi/errors.go` (mẫu `[]struct{err, status, code}`); `tests/arch` có sẵn `parseGoFiles`, `repoRoot`,
  `moduleCalls`, `checkRef`.
- Dùng lại / viết mới vì: `SeedCatalog` dựng trên `invtest.NewShop` (hai người bán, kho chính), `catalogtest.Product`,
  `invtest.SeedStock`; cần SKU chọn được và biến thể ngừng bán nên thêm hai helper vào `catalogtest` (dùng câu
  sqlc của catalog, không SQL thẳng). Bảng lỗi M2 theo đúng mẫu hàm `xxxError` có sẵn (một hàm mỗi module). Test kiến
  trúc mới dùng lại `parseGoFiles`/AST như `errlog_test`; `money_fields_test` đọc YAML bằng `ogen.Parse` như
  `errorcodes_test`. Luật `orderview` thêm vào `readerViolation` có sẵn (luật (i)), ngoại lệ đọc view vào `checkRef`.

## Soát thiết kế

- 5.1 chủ dữ liệu: view mới khai theo chủ (`customer`, `ord`, `promotion`); sentinel đặt ở module sinh ra nó.
- 5.2, 5.3: luật kiểm bằng `sqlusage_test` (ngoại lệ chỉ đọc view `customer` → bốn view `ord`, không nhập Go) và
  `boundaries_test` (`orderview` chỉ thư viện chuẩn).
- 5.4–5.6: không áp dụng: không đường ghi nghiệp vụ, không sự kiện, không gọi ra ngoài ở task này.
- 5.7: không áp dụng: không bảng mới (bảng của M2 tạo ở Task M2-03, 04, 05, 06).
- 5.8: `module.go` không nhập `net/http`; lớp HTTP đổi lỗi ở một chỗ (`errors.go`).
- 5.9: mọi operation ghi mới có `IdempotencyKey`, `x-permission` thuộc danh sách quyền (routegen và
  `TestRoutesPublicSetAndPermissions` kiểm); không `x-idempotency: exempt` nào (hai GET kiểm trước là đọc).
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: theo mẫu đường đọc/ghi (hợp đồng có `x-permission`, `IdempotencyKey`, `version` cho PATCH và chuyển trạng thái).
- 6.1: hợp đồng có `version` cho mọi PATCH và chuyển trạng thái (409 `version_conflict`).
- 6.2: mọi operation ghi có `Idempotency-Key`; khóa nghiệp vụ tạo đơn giữ cách M1 (Q-T57-4).
- 6.3: thứ tự khóa M2 chèn vào ADR-46 đúng Global Constraints và Q-T57-7.
- 6.4–6.9: không áp dụng: không transaction, việc nền mới.
- 6.10: mọi trường tiền là `Money` hoặc `int64` có `maximum` 2^53−1 (`money_fields_test`).
- 6.11: không áp dụng: không quy luật nghiệp vụ mới.
- 6.12: thân lỗi M2 chỉ thêm mã (`ownerId`, `blockingMergeId`, `reason`), không giá trị dữ liệu cá nhân; SĐT trong
  hợp đồng là `PhoneInput`; `TimelineItem.kind` là chuỗi mở.
- 6.13: mọi operation có quyền của Task M2-01 hoặc M1; không grant là 403 (test `GET /api/v1/customers`).

## Quyết định thay chủ shop

1. **`createOrder` có trường M2 trước khi Task M2-16, M2-17 làm xong → 501 `not_implemented`.** Kế hoạch: "mọi
   operation mới trả 501 cho tới khi task của nó làm xong", nhưng `createOrder` là operation M1 đang chạy. Chọn: thân
   kiểu M1 vẫn 201 như cũ; thân có bất kỳ trường M2 nào (`customerId`, `intent`, `lineDiscount`, mã, nguồn,
   `provinceCode`…) → 501, không lưu khóa, không tạo đơn. Vì sao: bỏ qua lặng lẽ một trường tiền (`lineDiscount`,
   `orderDiscount`, `couponCode`) sẽ tạo đơn sai giá. Phương án khác: bỏ qua trường lạ (nguy hiểm), hoặc 400. Đảo lại:
   Task M2-16 xóa `hasM2OrderFields` khi có nghiệp vụ.
2. **`customer`, `shipAddress` của `createOrder` nới thành tùy chọn** (Q-T57-5 cho `customer`; `shipAddress` vì đơn
   nháp có thể chưa có địa chỉ). Thân kiểu M1 thiếu một trong hai → 400 `invalid_input` như luật M1 (trước đây ogen trả
   400 `bad_request`; nay mã là `invalid_input` — vẫn 400). `ShipAddress` giữ bắt buộc `name, phone, line`.
3. **Thêm câu trả lời 200 cho `createOrder`** (gửi lại khi khóa nghiệp vụ trùng, `Replayed` của Task M2-16) ngay ở
   đây để Task M2-16 không phải sửa `openapi.yaml`. Handler M1 trả kiểu `gen.CreateOrderCreated` (cùng thân).
4. **`order.ErrStatusConflict` của M1 giữ là lỗi nội bộ (500, có log và dấu lỗi), không ánh xạ**;
   `ErrShipmentAlreadyAttached` → 409 `shipment_already_attached` (kế hoạch ghi). Bản đầu ánh xạ 422
   `invalid_transition`; reviewer chỉ ra lỗi này chỉ xảy ra khi dữ liệu đơn lệch (`fulfillment.CreateForOrder` đã khóa
   đơn và kiểm `confirmed` trước `AttachShipment`), ánh xạ 4xx sẽ giấu lỗi nội bộ; chuyển trạng thái sai thật đã có
   `ErrInvalidTransition`. Nên: `errors_mapped_test` có danh sách miễn trừ `internalSentinels` (kèm lý do; ánh xạ một
   sentinel trong danh sách cũng bị báo), `TestDomainErrors_M2` đòi `writeDomainError` không nhận lỗi này.
5. **`errors_mapped_test` quét mọi tệp không phải test của gói gốc ba module**, không chỉ ba tệp `errors.go` (để bắt cả
   `customer.ErrInvalidInput` của M1 ở `customer.go`). Chặt hơn kế hoạch, không đổi gì khác.
6. **`EditScope.shipping` thay vì `shippingFee`**: cờ boolean tên `…Fee` bị `money_fields_test` coi là trường tiền.
7. **Kiểu `promotion.Reason` và 10 hằng lý do khai ngay ở đây** (kế hoạch đặt ở Task M2-06) vì `RejectError{Reason}`
   phải có kiểu; Task M2-06 dùng lại, không khai lần hai.
8. **`customer_tags` có `version` trong hợp đồng** (`UpdateCustomerTagRequest.version`, luật "mọi PATCH có version")
   dù bảng của Task M2-03 chưa ghi cột `version` — xem Việc phát hiện thêm.
9. **Nối module ở `cmd/crm`**: thay `newEvents()` bằng `newModules(d, bus)` (tệp `cmd/crm/modules.go`) gọi đúng một lần
   `Background` của ba module cho mỗi vai trò; worker truyền sổ sự kiện vào `newCommands`
   thay vì dựng sổ thứ hai. Hành vi cũ (consumer `cash`) giữ nguyên.
11. **Một hàm `Background(d background.Deps) background.Parts{JobHandlers, Consumers, Schedules}` mỗi module thay cho
    ba hàm `JobHandlers`, `Consumers`, `Schedules` và ba kiểu `Deps` riêng của kế hoạch.** Ba tệp `module.go` theo đúng
    kế hoạch giống nhau từng token, jscpd (hook Stop) chặn. Phần giống nhau là phần nối tiến trình — cùng mục đích,
    cùng lý do thay đổi — nên gộp `Deps`, `Parts` vào gói chung `internal/platform/background` (có ghi chú "khi nào
    không dùng"). Task sau vẫn chỉ sửa `module.go` của module mình (điền trường của `Parts`). Đảo lại: tách lại ba hàm
    (đổi `cmd/crm/modules.go`).
12. **Cổng kiểm theo task chạy từng bước qua `make`, không gọi thẳng script.** Hook Bash chỉ cho phép
    `new-task`, `finish-task`, `task-check`, `stop-check` trong thư mục script quy trình nên lệnh gọi script cổng kiểm
    theo task bị chặn (không tìm cách vượt hook). Chạy đúng các bước của nó: build, vet, lint, gen-check, sqlc-vet,
    catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check (sau khi gộp, `main` là điểm tách nhánh nên so
    với `main` như script). Test: script sẽ chạy toàn bộ vì có tệp mới trong `internal/testkit`; chọn chạy gói đổi,
    gói phụ thuộc ngược (`go list` theo `Deps`/`TestImports`), cả bộ `inventory` (vùng cao, có trong danh sách phụ
    thuộc) và `catalog` — vì phần `testkit` chỉ thêm tệp, không đổi hàm có sẵn, và điều phối viên yêu cầu giảm tải
    máy. Đề nghị chủ shop thêm script cổng kiểm theo task vào danh sách cho phép của hook.
10. **Tên mã lỗi `code_taken` dùng chung catalog và promotion**: câu console đổi từ "Mã mẫu đã có" thành "Mã này đã có".

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi viết mã | các test dưới | `go test ./tests/arch/ -run '…'`, `go vet` gói httpapi, testkit | `TestErrorsMappedRealCode`: "chỉ thấy 10 sentinel"; `TestM2Contract_*`: 55 "thiếu operation" + 10 "thiếu dòng"; `TestBoundariesTestdata`: orderview ra "(f)" thay vì "(i)"; `TestSQLUsageViewRules`: `m2_view_ok.sql` 1 dòng thay vì 0; `errors_m2_test.go`, `catalog_test.go` không biên dịch (thiếu `order.ErrVersionConflict`, gói `promotion`, `testkit.SeedCatalog`) |
| 55 operation mới + 2 của M1: đúng đường, quyền, ghi có Idempotency-Key bắt buộc; không operationId trùng | `TestM2Contract_Operations` | `go test ./tests/arch -run TestM2Contract` | `ok` |
| Mọi route có quyền thuộc danh sách; mọi route được phục vụ | `TestRoutesPublicSetAndPermissions`, `TestRoutesAllServed`, `TestRoutesIdempotencyModes` | cổng kiểm theo task (bước test) | `ok elimaz.com.vn/crm/tests/arch 23.170s` |
| `GET /api/v1/customers` chủ → 501 kèm request_id; không grant → 403; không thẻ → 401; `POST /orders` thiếu Idempotency-Key → 400 | `TestM2Routes_Unimplemented` | `go test -race ./internal/httpapi -run TestM2Routes` | `ok` |
| Thân M1 của `createOrder` vẫn 201 (mở rộng không phá vỡ) | `TestOrderAPI_CreateGet`, `TestOrderAPI_Refusals`, `TestOrderAPI_LastUnitRace`, `TestOrderAPI_SameKey50` (M1) | `go test -race ./internal/httpapi -run TestOrderAPI` | `ok` |
| Trường M2 của `createOrder` chưa có nghiệp vụ → 501, thiếu khách/địa chỉ → 400 `invalid_input`, không đơn nào | `TestOrderAPI_M2FieldsPending` | như trên | `ok` |
| Mọi sentinel M2 → đúng mã HTTP, mã lỗi (kể cả khi bọc `%w`); ba lỗi có chi tiết `ownerId`, `blockingMergeId`, `reason` | `TestDomainErrors_M2`, `TestDomainErrors_M2Details` | `go test ./internal/httpapi -run TestDomainErrors` | `ok` |
| Mọi mã lỗi trong `errors.go` có ở `x-error-codes`; console có câu cho đúng tập mã | `TestErrorCodes_DeclaredInContract`, `errors.test.ts` | `make console-check` | `ok`; vitest `55 passed` (`src/app`) |
| `SeedCatalog`: 3 biến thể ELIMAZ + 1 người bán hai → tồn đúng; ngừng bán → `ErrNotForSale`; khác người bán → `ErrNotFound` | `TestSeedCatalog` | `go test -race ./internal/testkit -run TestSeedCatalog` | `ok … testkit 3.9s` |
| Tiền: mẫu `grandTotal` thiếu maximum, `lineDiscount` chuỗi, `refundAmount` int32 bị báo; hợp đồng thật 0 | `TestMoneyFieldsSample`, `TestMoneyFieldsContract` | `go test ./tests/arch -run TestMoneyFields` | `ok` (test mẫu xanh ngay lần đầu: hợp đồng M1 đã đúng; sau khi viết hợp đồng M2 bắt được `EditScope.shippingFee` → đổi tên) |
| Lỗi: mẫu `order.ErrFoo`, `promotion.ErrBaz` chưa ánh xạ bị báo; mã thật 0 | `TestErrorsMappedSample`, `TestErrorsMappedRealCode` | `go test ./tests/arch -run TestErrorsMapped` | `ok` |
| Ranh giới: `customer` nhập `order` bị báo (a); `orderview` nhập module/không phải thư viện chuẩn bị báo (i); mã thật 0 | `TestBoundariesTestdata`, `TestBoundariesNotCalledModule`, `TestBoundariesRealCode` | `go test ./tests/arch -run TestBoundaries` | `ok` |
| SQL: `queries/customer` nhắc `ord.orders_v` → 0; `ord.orders` → 1; `ord.traffic_sources_v` → 1; `queries/promotion` nhắc `ord.orders_v` → 1 | `TestSQLUsageViewRules`, `TestSQLUsageProductCode` | `go test ./tests/arch -run TestSQLUsage` | `ok` |
| View: 10 dòng mới đúng cột kế hoạch; `customers_v` 4 cột, `orders_v` 11 cột M1 giữ nguyên; `git diff docs/views.md` chỉ có dòng thêm | `TestM2Contract_ViewsDoc`, `TestViewsDocMatchesDatabase`, `TestViewsDocParses` | `go test ./tests/arch -run 'TestM2Contract|TestViewsDoc'`; `git diff --cached docs/views.md \| grep '^-[^-]'` | `ok`; không dòng nào bị xóa |
| oasdiff không có thay đổi phá vỡ (so với `main` sau khi gộp Task 325) | `make oasdiff` | `go run ./tools/apicheck -base-ref=main …` | `exit=0` |
| Client màn hình sinh lại, `tsc` sạch | — | `cd console && npx tsc --noEmit` | không lỗi |
| Ba module nối đúng một lần, consumer `cash` giữ nguyên | `TestNewModulesRegistersCash` | `go test ./cmd/crm` | `ok` |
| Cổng kiểm theo task trên bản đã gộp `main` `349268f` | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, console-check + test gói đổi và phụ thuộc | các bước `make` (Quyết định 12); `go test -race -count=1` 17 gói (`cmd/crm`, `httpapi`, `cash`, `fulfillment`, `inventory`, `order`, `catalog`, `files`, `realtime`, `testkit`, `tests/arch`, `tests/e2e`…) | mọi bước xanh, mọi gói `ok` (`httpapi` 216s, `inventory` 269s, `e2e` 122s); `gen-check` lần đầu đỏ vì mã sinh mới chưa stage, sau `git add` chạy lại `make gen-check`: sạch |
| Sau review: `order.ErrStatusConflict` không ánh xạ (ra 500 có log); ánh xạ lỗi trong danh sách nội bộ bị báo | `TestDomainErrors_M2`, `TestErrorsMappedRealCode` | `go test -race ./internal/httpapi -run 'TestDomainErrors\|TestOrderAPI\|TestErrorCodes'`; `go test ./tests/arch -run 'TestErrorsMapped\|TestErrorCodes'`; `make lint` | `ok … httpapi 12.2s`; `ok … tests/arch`; `0 issues.` |

## Test bị xóa hoặc tắt

- Không xóa, không tắt. Sửa một con số: `TestViewsDocParses` (`tests/arch/viewsdoc_test.go`) đếm 20 view của M1 → nay
  30 (20 của M1 + 10 view mới của M2 viết trước theo kế hoạch). Không nới luật: test vẫn đòi đúng số.
- `TestNewEventsRegistersCash` (`cmd/crm/main_test.go`) đổi tên thành `TestNewModulesRegistersCash` theo hàm mới
  `newModules`; vẫn đòi đúng một handler `event.cash`, thêm điều kiện ba module M2 chưa có việc nền, lịch.

## Khác kế hoạch

- Gộp `main` bốn lần, cả bốn **trước** khi chạy reviewer. Lần bốn (phiên tiếp tục, tới `349268f`: Task 323 nhập
  Excel — áp dụng, script cổng kiểm theo task): giữ phần đã stage thành bản vá (không stash), fast-forward, áp lại;
  xung đột `cmd/crm/main.go` giải bằng cách giữ `newModules` của task này và thêm `catalog.NewImportApplier` của
  Task 323 với `Events: mods.Events`; `openapi.yaml` lấy bản của task này rồi áp phần `main` thêm (áp sạch); mã sinh
  chạy lại `make gen`. Ba lần trước (lần ba: fast-forward tới `51b34a5` — Task 23, Task 64, đổi
  quy trình; không tệp nào trùng với phần của task này, `test-guard` đỏ vì so với `main` mới): lần đầu (fast-forward, chỉ mockup M2), lần
  hai vì Task 325 gộp vào `main` sau đó và sửa `openapi.yaml` (`setProductImageColor`), làm oasdiff đỏ. Lần hai: giữ
  phần đã stage thành bản vá trong scratchpad (không dùng stash), đưa nhánh về `main` (fast-forward), áp lại; xung
  đột `openapi.yaml` giải bằng cách chèn đúng phần Task 325 thêm vào bản của task này; mã sinh chạy lại `make gen`;
  mọi tệp viết tay so byte với bản sao lưu: khớp.

- `server.go` không sửa: `handler` đã nhúng `gen.UnimplementedHandler` từ M0 (`handlers_health.go`).
- `boundaries_test`: bảng "Được gọi" đã có `order → promotion`, `promotion → catalog, customer, inventory` từ trước;
  task này chỉ thêm luật (i) cho `internal/queries/orderview` (chỉ thư viện chuẩn) và ca thử.
- Thêm `console/src/app/errors.ts` (27 câu mới): `errors.test.ts` của M1 đòi đúng tập mã của `x-error-codes`.
- Thêm hai helper test vào `internal/modules/catalog/catalogtest/seed.go` (`VariantWithSKU`, `Discontinue`) để
  `SeedCatalog` có SKU chọn được và biến thể ngừng bán mà không ghi SQL thẳng.
- Thêm `tests/arch/m2contract_test.go`: đối chiếu 57 operation (đường, quyền, chế độ chống trùng) với bảng kế hoạch và
  cột của 12 dòng view.
- Thứ tự khóa: thêm mục (e) "Bảng M2 ngoài thứ tự nhóm" cho `customer_notes`, `order_notes` (chỉ chèn),
  `admin_units` (danh mục tĩnh), `customer_tags` (chỉ sửa trong transaction riêng: kế hoạch ghi "không khóa" nhưng
  `updateCustomerTag` có sửa dòng).

## Việc phát hiện thêm

- Task M2-03: bảng `customer.customer_tags` trong kế hoạch không có cột `version` mà hợp đồng `updateCustomerTag` đòi
  `version` (luật mọi PATCH có version). Task M2-03 nên thêm `version bigint NOT NULL DEFAULT 1`, `updated_at`.
- Task M2-16: xóa `hasM2OrderFields` trong `handlers_order.go` khi nối `Create`; đổi nhánh 400 "thiếu khách hay địa
  chỉ" theo `intent`.
- Kế hoạch ghi "16 nhãn" — bảng nhãn có đúng 16 hàng, khớp enum `OrderLabel`.

## Reviewer

Lần 1: CẦN SỬA — một phát hiện mức trung bình: `order.ErrStatusConflict` → 422 giấu lỗi dữ liệu lệch (trước ra 500
có log). Đã sửa (Quyết định 4): bỏ dòng ánh xạ, miễn trừ có lý do trong `errors_mapped_test`, test đòi không phải lỗi
nghiệp vụ. Ghi chú không bắt buộc đã làm: sửa chú thích `newEvents` → `newModules` ở `tests/e2e/stack_test.go`.
Chưa làm (không bắt buộc): thêm lần gửi lại cùng khóa sau 501 vào `TestOrderAPI_M2FieldsPending` (mã đúng: lỗi trả
trước `WriteTx`, khóa được nhả); `putCustomerTagRule` (PUT) không `version` — kế hoạch không đòi; câu chữ nhóm 7 ở
Global Constraints của kế hoạch nên sửa theo Q-T57-7.

Lần 2: **ĐẠT** — không phát hiện phải sửa. Phát hiện lần 1 đã sửa đúng (`ErrStatusConflict` về 500 có log; miễn
trừ có lý do; test đòi không phải lỗi nghiệp vụ). Ghi chú không bắt buộc còn ngỏ: ca testdata cho nhánh "sentinel nội
bộ mà vẫn bị ánh xạ"; ba ghi chú của lần 1 (không chặn commit).

Kết luận: ĐẠT
