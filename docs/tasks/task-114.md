# Task 114 — M3-23: Nhận hàng hoàn của hãng về kho theo số lượng tốt, hỏng, thiếu

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 114, M3-23). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới trong `order`: `ReceiveQtyLine`, `Writer.ReceiveReturnQty`, `ErrQtyMismatch`, hằng `ConditionMixed`. `Writer.ReceiveReturn` giữ chữ ký; chỗ gọi: `internal/httpapi/handlers_order_returns.go:174`, `internal/modules/order/returns_test.go:86`, `internal/modules/customer/stats_test.go:194` (không phải sửa).
  - `order.ReturnReceived` thêm trường `ShipmentID` (`shipmentId`, bỏ khi rỗng), `MissingQty` (`missingQty`): chỉ thêm trường; chỗ dùng chỉ trong `order/returns.go` (grep `ReturnReceived`). Consumer đang nghe `order.returned`: `customer.StatsConsumer` chỉ đọc `customerId` (không đổi). `cash` hiện không nghe `order.returned`/`order.partially_returned` (grep trong `internal/modules/cash`: 0 chỗ).
  - Phiếu `carrier_return`: `order.returned` đổi payload từ `Changed` sang `ReturnReceived` (nhúng `Changed`, giữ mọi khóa cũ; thêm `returnId`, `kind`, `refundAmount` = 0, `shipmentId`, `missingQty`).
  - Mới trong `fulfillment`: `ReturnReceivedConsumer` (tên `fulfillment_return_received`; mẫu tên consumer `[a-z][a-z0-9_]*` không cho dấu chấm như kế hoạch viết `fulfillment.return_received`), đăng ký qua `returnsConsumers()` của `area_returns.go` → `Module.Consumers()` (wire.go không đổi).
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

(điền khi làm)

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): `ReceiveReturn` dựng dòng số lượng từ `Condition` rồi đi thân chung `receiveQty`; hành vi M2 không đổi (test M2 Task 22 xanh) | `TestReceiveReturn_TD08Restock`, `TestReceiveReturn_DamagedAndMissing`, `TestCarrierReturn_TD02Receive`, `TestCarrierReturn_MissingStaysInTransit`, `TestReturnAPI_Flow`, test chỉ số khách (`customer`) | `go test -race -count=1 -run 'Return\|Carrier\|Receive' ./internal/modules/order/ ./internal/modules/customer/ ./internal/httpapi/` | `ok order 42.879s`, `ok customer 3.261s`, `ok httpapi 3.297s`; `golangci-lint run ./internal/modules/order/` → `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Commit 1 (cấu trúc), vòng 1: **ĐẠT**. Reviewer đối chiếu từng nhánh thân cũ/thân mới (khung nhận, `conditionLines` dồn cả số vào một ô nên `condition()` và điều kiện `Missing > 0` trùng cách cũ, `markReceived`, `receiveCarrier`, `receiveCustomer`); không phát hiện phải sửa. Ghi chú cho commit 2: `qtySplit` phải tự kiểm đủ dòng, dòng lạ, trùng, S+D+M; dòng trộn của phiếu hãng cần xử lý riêng.
