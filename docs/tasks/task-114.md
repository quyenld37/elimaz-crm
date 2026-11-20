# Task 114 — M3-23: Nhận hàng hoàn của hãng về kho theo số lượng tốt, hỏng, thiếu

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 114, M3-23). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới trong `order`: `ReceiveQtyLine`, `Writer.ReceiveReturnQty`, `ErrQtyMismatch`, hằng `ConditionMixed`. `Writer.ReceiveReturn` giữ chữ ký; chỗ gọi: `internal/httpapi/handlers_order_returns.go:174`, `internal/modules/order/returns_test.go:86`, `internal/modules/customer/stats_test.go:194` (không phải sửa).
  - `order.ReturnReceived` thêm trường `ShipmentID` (`shipmentId`, bỏ khi rỗng), `MissingQty` (`missingQty`): chỉ thêm trường; chỗ dùng chỉ trong `order/returns.go` (grep `ReturnReceived`). Consumer đang nghe `order.returned`: `customer.StatsConsumer` chỉ đọc `customerId` (không đổi). `cash` hiện không nghe `order.returned`/`order.partially_returned` (grep trong `internal/modules/cash`: 0 chỗ).
  - Phiếu `carrier_return`: `order.returned` đổi payload từ `Changed` sang `ReturnReceived` (nhúng `Changed`, giữ mọi khóa cũ; thêm `returnId`, `kind`, `refundAmount` = 0, `shipmentId`, `missingQty`).
  - Mới trong `fulfillment`: `ReturnReceivedConsumer` (tên `fulfillment_return_received`; mẫu tên consumer `[a-z][a-z0-9_]*` không cho dấu chấm như kế hoạch viết `fulfillment.return_received`), đăng ký qua `returnsConsumers()` của `area_returns.go` → `Module.Consumers()` (wire.go không đổi).
  - Nội bộ `order`: `transitionOpt.returned` (payload ReturnReceived cho cạnh confirmed → returned), `Writer.emitPayload` (emitChanged gọi lại nó), `missingOf`, `qtySplit` tự kiểm số.
  - `internal/httpapi`: `ReceiveReturn` chọn đường theo số lượng khi có `sellable`/`damaged`/`missing` ở bất kỳ dòng nào; `returnOut` bỏ `condition` của dòng `mixed`; `errors.go` thêm `order.ErrQtyMismatch` → 422 `qty_mismatch`.
  - `cmd/crm`: `TestWireM3_KeepsM1Parts`, `TestNewModulesRegistersCash` liệt kê đúng danh sách consumer đã nối → thêm `fulfillment_return_received` vào danh sách mong đợi (wire không đổi: consumer đi qua `Module.Consumers()`).
  - Bỏ: câu `SetReturnItemReceived` (thay bằng `SetReturnItemQty`), hàm riêng `receiveConditions` (thay bằng `conditionLines` + `qtySplit`).
- Gói phụ thuộc ngược (go list): `order` được nhập bởi `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`, `internal/testkit/shiptest`; test nhập `order`: `httpapi`, `cash`, `customer`, `fulfillment`, `order`. `fulfillment` được nhập bởi `cmd/crm`, `internal/httpapi`.
- Operation OpenAPI bị chạm, oasdiff: `receiveReturn` — thân đã có `sellable`, `damaged`, `missing` từ Task 95 (schema không đổi); chỉ thêm mã lỗi `qty_mismatch` vào `x-error-codes` của `Error` (phần mở rộng, không phá vỡ). Phản hồi `ReturnItem.condition` giữ enum cũ: dòng trộn (`mixed` trong database) thì bỏ trường `condition` ("trộn thì rỗng" của kế hoạch).
- Chỗ dùng trên màn hình: `console/src/app/errors.ts` thêm câu cho `qty_mismatch` (test `errors.test.ts` đòi đủ mã). Hộp nhận hàng (`ReceiveReturnDialog.tsx`) vẫn gửi `condition`, không đổi trong task này.
- Câu sqlc, bảng, pha migration: mới `queries/ord/returns_qty.sql` (`SetReturnItemQty`: `condition`, `refund_amount`, `sellable_qty`, `damaged_qty`, `missing_qty`; quyền UPDATE đã có ở 0204, 0302); bỏ `SetReturnItemReceived` ở `queries/ord/returns.sql`. Mới `queries/fulfillment/receipt.sql` (`LockShipmentForReceipt`, `MarkShipmentReturned`: `status`, `returned_at`, `last_status_at`, `version`; quyền UPDATE đã có ở 0300). Không migration mới (Quyết định thay chủ shop 3).
- Test sẽ chạy: `go test -race` cả bộ `./internal/modules/order/...`, `./internal/modules/inventory/...`, `./internal/modules/fulfillment/...` (vùng cao), `./internal/modules/customer/...`, `./internal/httpapi/...`; `cd console && npx vitest run src/app/errors.test.ts`; rồi ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm chung cho nhận hàng); trong module: `order.OnShipmentReturning`, `CarrierReturnFor`, `Writer.ReceiveReturn`, `lockReturn`, `changeReturn`, `receivedTopic`, `applyTransitionWith`; `inventory.ReleaseInTransitByItems`, `MoveToDamaged`, `ReceiveCustomerReturn`, `RecordLoss` (Task 96); `fulfillment.openAnomaly`, `publishShipment`, `StatusChanged`, `TopicShipmentStatusChanged`, `order.LockSnapshot` (Task 99).
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên, không viết đường tồn mới. `ReceiveReturn` (M2) và `ReceiveReturnQty` dùng chung một thân; `ReceiveReturn` chỉ dựng dòng số lượng từ `Condition` (cả dòng một tình trạng). Viết mới: kiểm số lượng (`qtySplit`), câu ghi dòng theo số lượng, consumer đưa vận đơn sang `returned` (máy trạng thái `shipstate` cố ý không có cạnh tới `returned`: chỉ kho nhận, Task 98). Consumer đặt trong gói gốc `fulfillment` (không phải `internal/receipt`) để dùng lại `openAnomaly`, `publishShipment`, `StatusChanged` mà không vòng nhập.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: dòng phiếu trả (`ord.return_items`) của `order`; vận đơn, bất thường của `fulfillment`; tồn của `inventory`. Mỗi module tự ghi bảng của mình.
- 5.2 Không ghi bảng module khác: `order` gọi public API của `inventory`; `fulfillment` chỉ ghi `fulfillment.*`, đọc đơn qua `order.LockSnapshot`.
- 5.3 Đọc qua public API: consumer không đọc `ord.*`; dữ liệu của phiếu nằm trong payload sự kiện (`kind`, `shipmentId`, `missingQty`).
- 5.4 Nguyên tử: nhận phiếu, nhả in_transit, chuyển hỏng, ghi thất lạc, đơn → returned, sự kiện: một transaction (WriteTx của HTTP).
- 5.5 Không nguyên tử: vận đơn → returned đi sau commit qua sự kiện `order.returned` (consumer), đúng kế hoạch.
- 5.6 Gọi ra ngoài: không áp dụng (không gọi hãng).
- 5.7 Ngữ cảnh shop: mọi câu trong tx tenant (WriteTx; worker chạy consumer trong WithTenant của sự kiện).
- 5.8 Không HTTP trong module: lớp HTTP chỉ đổi thân yêu cầu sang `ReceiveQtyLine`/`ReceiveLine`.
- 5.9 Đường ghi có khóa chống trùng, quyền: `receiveReturn` đã có `IdempotencyKey`, `x-permission: order.return.receive`.
- 5.10 Valkey: không áp dụng.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu: đường ghi Task 73 (Writer trong tx bên gọi), consumer như `customer.StatsConsumer`, bất thường như Task 99.

Mục 6:
- 6.1 Đồng thời: khóa đơn → phiếu (so version) → dòng phiếu như M2; `MarkReturnReceived` có điều kiện `version`, `status = 'requested'`. Consumer khóa đơn (LockSnapshot) → vận đơn FOR UPDATE; `MarkShipmentReturned` chỉ đổi khi `status = 'returning'`.
- 6.2 Gửi lại: HTTP chống trùng bằng Idempotency-Key; nhận lần hai bằng khóa khác → `version_conflict` 409 (version cũ) hoặc `return_not_allowed` 422 (version mới). Sự kiện lặp: vận đơn đã `returned` → không đổi gì, không mở thêm bất thường; `RecordLoss` cùng ref lần hai là nil.
- 6.3 Thứ tự khóa: ADR-46 nhóm 7 (đơn) → 10 (phiếu, dòng phiếu) → 11 (số dư). `ReleaseInTransitByItems` nhả mọi dòng của phiếu nên khóa trước mọi dòng số dư liên quan; `MoveToDamaged` (sắp theo biến thể) và `RecordLoss` (sắp theo biến thể, kho) chỉ chạm dòng đã khóa. Consumer: nhóm 7 → 8 như ApplyTracking.
- 6.4 Lỗi tạm: WriteTx và hàng đợi việc nền chạy lại như có sẵn.
- 6.5 Mức cách ly: READ COMMITTED + khóa dòng + câu có điều kiện.
- 6.6 Việc nền lỗi: consumer trả lỗi → lịch thử lại; payload hỏng, không có vận đơn, vận đơn của đơn khác → `queue.ErrPermanent`.
- 6.7 429: không áp dụng.
- 6.8 Chết giữa chừng: transaction lùi; consumer chạy lại được (vận đơn đã returned → không đổi).
- 6.9 Treo: theo cấu hình chung.
- 6.10 Tiền: phiếu hãng tiền hoàn 0; phiếu khách giữ luật M2 (dòng thiếu tiền hoàn 0). Không đổi sổ COD.
- 6.11 Quy luật: test rapid cho kiểm số lượng (`qtySplit`): chỉ nhận khi đủ mọi dòng và S+D+M = số của dòng; tình trạng suy ra khớp số.
- 6.12 Dữ liệu cá nhân: payload, detail bất thường chỉ mã và số.
- 6.13 Quyền: `order.return.receive` ở API, không đổi.

## Quyết định thay chủ shop

1. **Hàng thiếu của phiếu hoàn hãng đi theo M3, không giữ in_transit nữa.** M2 (Quyết định 2 của Task 22) để dòng `missing` treo ở in_transit "cho M3 xử lý thất lạc"; Interface của task này: nhả in_transit mọi dòng rồi `RecordLoss`. Áp cả cho thân M2 (tình trạng cả dòng `missing`), vì hai thân đi chung một đường. Test M2 `TestCarrierReturn_MissingStaysInTransit` đổi thành `TestCarrierReturn_MissingRecordsLoss` (xem mục dưới). Phương án khác: giữ hành vi M2 cho thân cũ, chỉ đường số lượng mới ghi thất lạc — bỏ vì hai đường cho cùng một kiện ra hai kết quả tồn khác nhau.
2. **`condition` của dòng trộn là `mixed`**, không phải "rỗng": cột `condition` NOT NULL và CHECK của 0302 (Task 92) đã thêm giá trị `mixed` cho đúng việc này. Ra API thì bỏ trường `condition` của dòng trộn (hợp đồng chỉ có enum sellable/damaged/missing) — đúng nghĩa "trộn thì rỗng" ở phía người đọc. Thêm ba số vào `ReturnItem` của phản hồi để màn hình hiển thị là việc sau (ghi ở Việc phát hiện thêm).
3. **Không thêm migration cho loại bất thường `return_without_returning`.** CHECK của `fulfillment.shipment_anomalies.kind` (0300) chưa có loại này; kế hoạch không cấp số migration cho Task 114 (0307–0308 đã dành cho Task 22, 49; 0309 vừa có từ Task 100). Dùng loại sẵn có `invalid_edge` với `detail {reason: "return_without_returning", status}`: vẫn vào "Cần xem", dễ đổi sau. Phương án khác: migration mở rộng CHECK — để Task 49 (migration contract M3) hoặc chủ shop quyết.
4. **Tên consumer `fulfillment_return_received`** (kế hoạch viết `fulfillment.return_received`): mẫu tên consumer của `events.Registry` là `[a-z][a-z0-9_]*`, dấu chấm bị từ chối lúc đăng ký.
5. **Phiếu khách trả, đổi nhận theo số lượng:** cho trộn tốt + hỏng (tiền hoàn đủ như M2); dòng vừa có hàng về vừa thiếu một phần → `ErrInvalidInput`, vì tiền hoàn một phần của món thiếu là việc của Task M3-24 (kế hoạch: "customer_return, exchange: Task 24"). Dòng thiếu cả dòng giữ luật M2 (tiền hoàn 0).
6. **Thân HTTP lẫn hai kiểu:** có `sellable`/`damaged`/`missing` ở bất kỳ dòng nào → cả thân đi đường số lượng, số không gửi là 0 (dòng chỉ có `condition` thành 0/0/0 → 422 `qty_mismatch`). Mô tả hợp đồng "có đủ ba số thì bỏ qua condition" vẫn đúng; không gửi số nào → đường M2.
7. **Mã lỗi mới `qty_mismatch`** (422) cho `ErrQtyMismatch` (kế hoạch nêu 422, không nêu mã): thêm vào `x-error-codes` và câu tiếng Việt ở `console/src/app/errors.ts`.
8. **`ReceiveReturnQty` là phương thức của `Writer` và nhận `sellerID`** như `ReceiveReturn` của M2 (chữ ký kế hoạch bỏ hai thứ này, nhưng mọi đường ghi phiếu của M2 đều có).
9. **Consumer đặt trong gói gốc `fulfillment`** (`return_received.go`), không ở `internal/receipt/consumer.go`: cần `openAnomaly`, `publishShipment`, `StatusChanged` của gói gốc; gói con nhập ngược gói gốc là vòng nhập.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): `ReceiveReturn` dựng dòng số lượng từ `Condition` rồi đi thân chung `receiveQty`; hành vi M2 không đổi (test M2 Task 22 xanh) | `TestReceiveReturn_TD08Restock`, `TestReceiveReturn_DamagedAndMissing`, `TestCarrierReturn_TD02Receive`, `TestCarrierReturn_MissingStaysInTransit`, `TestReturnAPI_Flow`, test chỉ số khách (`customer`) | `go test -race -count=1 -run 'Return\|Carrier\|Receive' ./internal/modules/order/ ./internal/modules/customer/ ./internal/httpapi/` | `ok order 42.879s`, `ok customer 3.261s`, `ok httpapi 3.297s`; `golangci-lint run ./internal/modules/order/` → `0 issues.` |
| TD04/AT12 ca a (phía đơn): V1 S1 D1 M0, V2 S1 → in_transit 0, damaged 1, có thể bán đúng; dòng `sellable_qty 1, damaged_qty 1, condition mixed`; đơn returned; `order.returned` có `kind = carrier_return`, `shipmentId`, `missingQty 0`, `returnId`; không sổ loss | `TestReceiveReturnQty_CarrierSplit` | `go test -race -count=1 -run 'Return\|Carrier\|Receive\|QtySplit' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 20.057s`; ci-task lần 2: `ok …/order 381.738s` |
| TD04 ca b (phía đơn): dòng thiếu → nhả in_transit, `RecordLoss` (doc_type adjust, doc_id = mã phiếu, tổng 2), on_hand −1 mỗi biến thể, `missingQty 2` | `TestReceiveReturnQty_CarrierMissingRecordsLoss` | như trên | như trên |
| Thiếu dòng, dòng lạ, trùng, S+D+M lệch, không dòng → `ErrQtyMismatch`; số âm → `ErrInvalidInput`; phiếu vẫn requested, tồn không đổi | `TestReceiveReturnQty_Mismatch` | như trên | như trên |
| Quy luật kiểm số lượng (rapid): nhận khi và chỉ khi đủ dòng và tổng đúng; tình trạng suy ra khớp số (mixed khi trộn) | `TestQtySplit_Property` | `go test -count=1 -run TestQtySplit_Property ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 0.511s` |
| Q-T91-8 phía đơn: phiếu hãng của vận đơn không còn hiện hành → `ErrInvalidTransition`, tồn không đổi | `TestReceiveReturnQty_NotCurrentShipment` | như dòng TD04 ca a | như trên |
| Phiếu khách trả theo số lượng (S1 D1): on_hand +2, damaged +1, tiền hoàn đủ, `mixed`, payload `kind customer_return`, `missingQty 0`, không `shipmentId`; thiếu một phần → `ErrInvalidInput` | `TestReceiveReturnQty_Customer` | như dòng TD04 ca a | như trên |
| Thân M2 ghi cả ba số (sellable → `sellable_qty`, missing → `missing_qty`) | `TestReceiveReturn_WritesQtyColumns` | như dòng TD04 ca a | như trên |
| Thân cũ M2 vẫn chạy (test M2 Task 22 xanh); dòng `missing` của phiếu hãng nay ghi thất lạc (tên mới của test M2) | `TestReceiveReturn_TD08Restock`, `TestReceiveReturn_DamagedAndMissing`, `TestCarrierReturn_TD02Receive`, `TestCarrierReturn_MissingRecordsLoss`, `TestReturnAPI_Flow` | ci-task lần 2 (cả bộ order, httpapi) | `ok …/order 381.738s`, `ok …/httpapi 216.415s` |
| TD02/AT11: hãng báo Returning rồi ReturnedToShop → V1 vẫn returning, tồn chưa tăng (vA 5/0/1, vB 10/0/2); kho nhận vA S1, vB S2 → D1 returned, in_transit 0; consumer → V1 returned, `returned_at`, +1 `shipment.status_changed`; chạy lại không đổi version; `customer.StatsConsumer` trên cùng sự kiện → `returned_count = 1` | `TestReturnReceived_TD02` | `go test -race -count=1 -run TestReturnReceived ./internal/modules/fulfillment/` | `ok elimaz.com.vn/crm/internal/modules/fulfillment 2.463s`; ci-task lần 2: `ok …/fulfillment 189.289s`, `ok …/customer 216.294s` |
| TD04/AT12 ca a đầu cuối: vB S1 D1 → on_hand 10, damaged 1, không `missing_items`, V1 returned | `TestReturnReceived_TD04Damaged` | như dòng TD02 | như trên |
| TD04/AT12 ca b đầu cuối (dữ liệu dựng lại): vB S0 D1 M1 → on_hand 9, sổ loss −1, damaged 1; V1 returned; `missing_items` `detail.missingQty = 1`, `needs_review` | `TestReturnReceived_TD04Missing` | như dòng TD02 | như trên |
| Consumer: phiếu khách/không shipmentId → bỏ qua; vận đơn chưa returning → không đổi, "Cần xem" `invalid_edge` reason `return_without_returning`, không phát status_changed; payload hỏng, vận đơn không có → `queue.ErrPermanent` | `TestReturnReceived_Guards` | như dòng TD02 | như trên |
| HTTP: thiếu dòng → 422 `qty_mismatch`; tổng lệch → 422; dòng chỉ có condition lẫn dòng có số → 422; đúng → 200 restocked, dòng trộn không có `condition`; tồn +2, hỏng 1; cùng khóa → cùng thân; khóa khác version cũ → 409 `version_conflict` | `TestReturnAPI_ReceiveQty` | `go test -race -count=1 -run 'TestReturnAPI\|ErrorCode' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 3.361s` |
| Mã lỗi `qty_mismatch` có trong `x-error-codes` và có câu tiếng Việt | `errorcodes_test.go`, `console/src/app/errors.test.ts` | `cd console && npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| Consumer được nối vào worker (danh sách consumer đầy đủ) | `TestWireM3_KeepsM1Parts`, `TestNewModulesRegistersCash` | `go test -race -count=1 ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 1.663s`; ci-task lần 2: `ok …/cmd/crm 1.825s` |
| Cổng kiểm của task, lần 1 | — | ci-task | ĐỎ: `gen-check` (chỉ vì mã chưa stage), `jscpd` (1 đoạn trùng trong `returns_qty_test.go`, đã tách `carrierCase`), `cmd/crm` (2 test liệt kê consumer, đã thêm consumer mới); mọi gói khác `ok` |
| Cổng kiểm của task, lần 2 (sau khi stage, đã gộp main fe31a7f) | — | ci-task | `CI-TASK XANH (so với điểm tách nhánh fe31a7f…)` |

## Test bị xóa hoặc tắt

- `TestCarrierReturn_MissingStaysInTransit` (order, M2 Task 22) đổi tên thành `TestCarrierReturn_MissingRecordsLoss` và đổi điều kiện: trước kiểm V2 thiếu giữ `in_transit 1`; nay kiểm `in_transit 0`, `on_hand −1`, có thể bán không đổi. Lý do: M2 cố ý để phần thất lạc cho M3; Interface Task 114 (carrier_return: nhả mọi dòng, `Missing` → `RecordLoss`) thay hành vi đó. Không nới: test kiểm chặt hơn (thêm on_hand, available). Quyết định thay chủ shop 1.
- `cmd/crm`: `TestWireM3_KeepsM1Parts` ("consumer phải đúng một cash") và `TestNewModulesRegistersCash` (danh sách handler sự kiện) thêm `fulfillment_return_received` vào danh sách mong đợi; vẫn so đúng cả danh sách.

## Khác kế hoạch

- Consumer ở `internal/modules/fulfillment/return_received.go` (+ `area_returns.go`), test ở `return_received_test.go`, không có `internal/receipt/` (Quyết định 9). Thêm `queries/fulfillment/receipt.sql`.
- Tên consumer `fulfillment_return_received` (Quyết định 4); bất thường `invalid_edge` + reason thay cho loại `return_without_returning` (Quyết định 3); `condition` trộn là `mixed` (Quyết định 2).
- `ReceiveReturnQty` có `sellerID`, là phương thức của `Writer` (Quyết định 8). Mã lỗi `qty_mismatch` (Quyết định 7).
- Kế hoạch ghi "nhận hai lần (khóa khác) → 409": với version cũ là 409 `version_conflict` (test HTTP); với version mới là 422 `return_not_allowed` như M2 (`TestReturnAPI_Flow`).

## Việc phát hiện thêm

- CHECK `shipment_anomalies.kind` chưa có `return_without_returning` (kế hoạch Q-T91-8 đặt tên này): cần một migration mở rộng nếu muốn loại riêng (đề xuất gộp vào Task 49 / 0308 hoặc số mới). Hiện dùng `invalid_edge` + `detail.reason`.
- Phản hồi `ReturnItem` chưa có `sellable`, `damaged`, `missing`; hộp `ReceiveReturnDialog.tsx` chưa có ô nhập số lượng — cần cho màn kho nhận hàng hoàn (mockup Task 131) ở task màn hình M3.
- Tiền hoàn một phần cho dòng phiếu khách có món thiếu một phần: Task M3-24.

## Reviewer

Commit 1 (cấu trúc), vòng 1: **ĐẠT**. Reviewer đối chiếu từng nhánh thân cũ/thân mới (khung nhận, `conditionLines` dồn cả số vào một ô nên `condition()` và điều kiện `Missing > 0` trùng cách cũ, `markReceived`, `receiveCarrier`, `receiveCustomer`); không phát hiện phải sửa. Ghi chú cho commit 2: `qtySplit` phải tự kiểm đủ dòng, dòng lạ, trùng, S+D+M; dòng trộn của phiếu hãng cần xử lý riêng.

Commit 2 (hành vi), vòng 1: **CẦN SỬA** — một phát hiện: bảng Bằng chứng chưa có dòng của commit 2 (lệnh chèn bị hụt). Vòng đó kiểm mã không thấy lỗi tính đúng: thứ tự khóa ADR-46 (nhả in_transit khóa trước mọi dòng số dư; consumer LockSnapshot → vận đơn FOR UPDATE), consumer chạy lại được (đã returned thì dừng, `MarkShipmentReturned` có điều kiện `status = 'returning'`), đường tồn phiếu hãng (nhả S+D+M rồi MoveToDamaged/RecordLoss luôn đủ), cash bỏ qua trạng thái returned. Đã sửa: thêm 17 dòng bằng chứng.

Commit 2, vòng 2: **ĐẠT**. Reviewer đối chiếu từng dòng bằng chứng với test thật; không phát hiện phải sửa. Ghi chú không bắt buộc: hai nhánh consumer chưa có test riêng (vận đơn của đơn khác → ErrPermanent; partial_delivery + delivered có partial → giữ) — nhánh partial_delivery thuộc Task M3-26.
