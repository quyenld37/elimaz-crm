# Task 439 — M2-r10: Sửa kế hoạch M3, M5, Interfaces Task 79 theo chữ ký thật

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (bảng bù review 19/11, M2-r10; phát hiện E-02 của `docs/review-doc-lap-m2.md`). Loại việc (quy trình mục 3): A — **chỉ tài liệu kế hoạch** (thư mục `../../docs/superpowers/plans/` nằm ngoài git của CRM), không sửa mã.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có — task không sửa mã Go. Ký hiệu chỉ được **đọc** để sửa kế hoạch: phương thức `order.Writer` (`AttachShipmentBy`, `DetachShipment`, `OnShipmentEvent`, `ApplyPaymentTotals`, `MarkRefunded`, `Cancel`, `CreateReturn`, `OnShipmentReturning`, `ReceiveReturn`, `ReceiveReturnQty`, `ReverseDelivery`, `CloseReturn`, `CancelReturn`), hàm gói `order.CarrierReturnFor`, `order.Lock`, `order.GetReturn`; `customer.Writer` (`FindOrCreateForOrder`, `AddIdentity`); `inventory.Writer` (`ReleaseInTransitByItems`, `ReceiveCustomerReturn`, `MoveToDamaged`); `fulfillment.CarrierAdapter` và cờ khả năng; `cash` (phiếu thu, nghĩa vụ hoàn).
- Gói phụ thuộc ngược (go list): không áp dụng (không đổi gói nào).
- Operation OpenAPI bị chạm, oasdiff: không (chỉ đọc `openapi/openapi.yaml` để ghi đúng operationId vào kế hoạch).
- Chỗ dùng trên màn hình: không (chỉ đọc `console/src/screens/` để ghi đúng thư mục màn vào kế hoạch M3 Task 132–138).
- Câu sqlc, bảng, pha migration: không (chỉ đọc `migrations/03xx` để ghi đúng tên bảng, cột).
- Tệp bị sửa: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 110, 116, 118, 127–140, bảng Quyết định, Việc cần chủ shop), `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Giả định về mốc trước và các task đọc mã M1–M3), `docs/tasks/task-439.md`. **Không** sửa kế hoạch M1, M2 (Task 415 chạy song song) — xem Quyết định thay chủ shop Q1.
- Test sẽ chạy: không có test mã mới (task tài liệu). Cổng: `ci-task.sh` một lần; bằng chứng là bảng đối chiếu kế hoạch ↔ `tệp:dòng` của mã thật ở mục Bằng chứng.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: không viết hàm hay kiểu nào. Dùng lại luật đọc tên Q-T91-1 và bảng `docs/m3/doi-chieu-m2.md` của Task 91 (không chép lại chữ ký đã có ở đó, chỉ trỏ tới).
- Dùng lại / viết mới vì: dùng lại; chỉ thêm vào kế hoạch những chỗ Task 91 chưa sửa (thân Task 110, 116, 118, 127–140 và M5).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng cho mã (task không sửa mã). Điều kiểm tương ứng trên tài liệu: chữ ký ghi vào kế hoạch giữ ranh giới module (`fulfillment` chỉ gọi public API của `order`; `cash` gọi `order`; ADR-29 `sellerID` ngay sau `tx`) và không đề xuất lớp bọc bỏ `sellerID` (Q-T91-1).
- 6.1–6.13: không áp dụng (không có đường ghi, khóa, hàng đợi, tiền nào được viết). Chỗ kế hoạch nói tới tiền (Task 116 cấn trừ) và khóa (Task 118) chỉ đổi tên theo mã thật, không đổi luật.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| E-02: G-M2-3 của M3 dùng chữ ký thật | (tài liệu) luật Q-T91-1, dòng G-M2-3 (Task 91 đã sửa); thân Task 110, 116, 118 nay dùng `w.X(ctx, tx, sellerID, …)` | grep `func (w Writer) (CreateReturn|OnShipmentReturning|ReceiveReturn|ApplyPaymentTotals|Cancel|ReverseDelivery)(` trong `internal/modules/order` | `returns.go:131,348,463`, `payments.go:28`, `cancel.go:55`, `shipping.go:63` — mọi hàm nhận `sellerID` sau `tx` |
| E-02: M5 B1 là phương thức `customer.Writer` | dòng B1 của bảng "Đối chiếu với mã thật" (M5) | grep `func (w Writer) (FindOrCreateForOrder|AddIdentity)` trong `internal/modules/customer` | `fororder.go:49`, `identity.go:52` |
| M5: chữ ký khác (order, promotion, idempotency, catalog) | 24 dòng bảng "Đối chiếu với mã thật" của M5 | grep `Redeem`, `CreateBatch`, `Create`, `Confirm`, `ExpireDrafts`, `Acquire`, `OutOfStockError`, `ArticleHTML`, `GetBatch`, `LockSnapshot` trong `internal` | `redeem.go:73`, `batch.go:75,218`, `create_m2.go:122`, `confirm.go:33`, `drafts.go:33`, `idempotency.go:80`, `order/errors.go:67`, `article_html.go:56`, `shipping.go:41`; `customer.Actor`, `promotion.Actor`, `fulfillment.Actor` đọc ở `write.go:83`, `manage.go:48`, `fulfillment.go:47` |
| Task 110 theo mã thật | khối "Theo mã thật" của Task 110 | grep `RequestAddressChange`, `DeliveryStatsFor`, `CallbackDeadlineHours`; đọc `area_callbacks.go`, `handlers_callbacks.go` | `area_address.go:120`, `delivery_stats.go:49`, `apply_effects.go:113-121`; vùng callbacks còn khung (nil, 501) |
| Task 116 theo mã thật | khối "Theo mã thật" của Task 116 | grep `OrderTypeExchange`, `ReturnExchange`, `RefundsConsumerName`, `MethodExchangeCredit`, `ErrCODLockedByShipment`, `LockSnapshot` | `create_m2.go:41`, `returns.go:34`, `cash/refunds.go:37`, `cash/payments.go:28,46`, `order/shipping.go:41` |
| Task 118 theo mã thật | khối "Theo mã thật" của Task 118 | grep `shipment_cod_v`, `cod_transfers_v` trong `docs/views.md`; grep `codePreviewStale` trong `internal/httpapi`; đọc `area_corrections.go`, `handlers_corrections.go` | `views.md:55,56`; `errors_cash.go:18` (hằng của cash, cần dời sang `errors.go`); vùng corrections còn khung |
| Task 127–130 theo mã thật | ghi chú và Interfaces của Task 127–130 | đọc `internal/adapters/carrier/registry.go:20-32`, `cmd/crm/wire_m3.go:54-58`, `ls internal/adapters/carrier/{best,spx}` | `All` = `manual.New(SelfKey), manual.New(vtp.Key), best.New(cfg), spx.New(cfg)` (+ fake khi `FakeOn`); `best`, `spx` chỉ có `adapter.go` khung |
| Task 132–140 theo mã thật | khối "Theo mã thật" của Phần E và từng task | grep hằng chủ đề realtime trong `internal/modules/fulfillment`; grep chuỗi quyền trong `identity/permissions.go`; `ls console/src/screens`; đọc `docs/mockups/m3/README.md:106-112` | chủ đề `fulfillment.carrier_account`, `shipment.changed`, `callback_task.changed`, `label_batch.ready`, `shipping_quote.ready`; 19 quyền `fulfillment.*`/`cash.*`; 13 thư mục màn, chưa có màn M3 trên `main`; mockup "Chờ điều phối viên duyệt" |
| Quyết định chủ shop 21/11 ghi vào kế hoạch | bảng "Chủ shop quyết ngày 21/11/2026" (CS-21/11-1..3) + ghi chú ở "Việc cần chủ shop", "Phụ thuộc", Task 127–130, 137, 139, 140 | `grep -c "CS-21/11"` trên kế hoạch M3 | `15` |
| Chỉ sửa M3, M5 (không M1, M2) | — | `diff -u` bản sao lúc bắt đầu (scratchpad của phiên) với tệp hiện tại | M3: 336 dòng diff, M5: 53 dòng; mọi khối là của task này; M1, M2 không bị sửa |
| Cổng kiểm | — | ci-task (một lần, sau khi gộp `main` 90eeee6) | `CI-TASK XANH (so với điểm tách nhánh 90eeee6…)`; test kiến trúc `ok … 25.348s`; gitleaks "no leaks found" |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tiêu đề task có "Interfaces Task 79" (kế hoạch M2) nhưng lệnh giao việc cấm sửa kế hoạch M1, M2 (Task 415 song song): đoạn sửa đã soạn sẵn ở "Việc phát hiện thêm" để điều phối viên chép vào sau khi gộp Task 415 (Quyết định Q1).
- Kế hoạch M5 không sửa từng dòng trong thân task: thêm bảng "Đối chiếu với mã thật" (24 dòng) cuối phần "Giả định về mốc trước" và một dòng đầu tệp nói bảng này thắng thân task (Quyết định Q3). Kế hoạch M3 sửa thẳng trong thân Task 110, 116, 118, 127–140 (khối "Theo mã thật" ngay dưới Files) vì các task đó sắp chạy.

## Quyết định thay chủ shop

- **Q1 — Không sửa kế hoạch M2 (Interfaces Task 79).** Chọn: soạn đoạn sửa trong tệp task, điều phối viên chép sau khi gộp Task 415. Vì: lệnh giao việc cấm sửa M1/M2; hai phiên cùng ghi một tệp ngoài git có thể đè nhau. Phương án khác: sửa thẳng (bỏ vì trái lệnh). Đảo: chép đoạn vào kế hoạch M2 bất kỳ lúc nào.
- **Q2 — Nghĩa của "đối soát COD tạm dừng".** Chọn: tạm dừng phần cần hãng thật (định dạng bảng kê theo tệp thật của từng hãng, nhập tệp thật trên staging, tiêu chí "Xong M3" (2)); giữ sổ COD, khớp/chốt bảng kê bằng định dạng chung `elimaz_v1` và màn đối soát (Task 137 đã làm trên nhánh của nó; tệp task 137 ghi HV30–32 chủ shop OK 21/11). Vì: phần nội bộ đã có trên `main`, gỡ đi tốn mà không an toàn hơn; phần cần hãng không làm được khi tích hợp dừng. Phương án khác: dừng cả màn đối soát — chỉ cần sửa dòng CS-21/11-1. Đảo: sửa một dòng bảng.
- **Q3 — M5 sửa bằng bảng đối chiếu, không sửa từng dòng thân task.** Vì: M5 chưa bắt đầu, Task M5-00 (Task 183) vốn đối chiếu lại toàn bộ; một bảng có `tệp:dòng` dễ kiểm hơn 30 chỗ sửa rải rác và ít rủi ro làm hỏng văn bản khác. Phương án khác: sửa từng chỗ trong thân task.
- **Q4 — CS-21/11-2 (a) cần ADR ngoại lệ B10 trước khi code**, làm ở task mới sau khi mở lại tích hợp hãng; trong lúc chờ, (b) cập nhật tay là đường chạy được. Vì: (a) đi ngược luật spec "webhook chỉ là tín hiệu" (quy trình mục 3 loại C). Phương án khác: viết (a) ngay — bỏ vì tích hợp đang dừng và cần ADR.
- **Q5 — `createCarrierAccount` trả 422 cả với tài khoản `manual` khi tắt hãng giả** (Task 100, 106; `carrier.All` chỉ được nối khi `carrier.FakeOn`): ghi vào Task 140 là việc phải sửa trước khi chạy thật **dù** tích hợp hãng dừng (SPX nhập tay cần nó). Không tự sửa mã (task tài liệu).

## Việc phát hiện thêm

- **Interfaces Task 79 của kế hoạch M2 (`2026-11-18-crm-m2-khach-don.md`, mục "Interfaces" của Task 79) — chưa sửa
  trong tệp kế hoạch M2** (Quyết định Q1: Task 415 đang sửa kế hoạch M1, M2 song song). Điều phối viên chép đoạn dưới
  vào đầu mục Interfaces của Task 79 sau khi gộp Task 415 (đối chiếu từ mã `main` ngày 21/11):
  > **Chữ ký thật (Task 439, review M2 E-02; thêm vào Q-T57-3):** mọi hàm ghi dưới là phương thức của
  > `order.Writer{Events, Queue, Realtime}` và nhận `sellerID` ngay sau `tx` (ADR-29); `a Actor` là
  > `order.Actor{TenantID, UserID uuid.UUID; Type string; CanApproveDiscount bool}`:
  > `(w Writer) CreateReturn(ctx, tx, sellerID, orderID uuid.UUID, in CreateReturnInput, a Actor, now time.Time) (Return, error)` (`returns.go:131`);
  > `(w Writer) OnShipmentReturning(ctx, tx, sellerID, orderID, shipmentID uuid.UUID, a Actor, now time.Time) (applied bool, err error)` (`returns.go:348`);
  > hàm gói `CarrierReturnFor(ctx, tx, sellerID, shipmentID uuid.UUID) (Return, error)` (`returns.go:400`);
  > `(w Writer) ReceiveReturn(ctx, tx, sellerID, returnID uuid.UUID, version int64, lines []ReceiveLine, a Actor, now time.Time) (Return, error)` (`returns.go:463`, gọi đường nhận theo số lượng `ReceiveReturnQty` của M3, `returns_qty.go:84`);
  > `(w Writer) CloseReturn(ctx, tx, sellerID, returnID uuid.UUID, version int64, a Actor, now time.Time) (Return, error)` (`returns.go:562`);
  > `(w Writer) CancelReturn(ctx, tx, sellerID, returnID uuid.UUID, version int64, a Actor, now time.Time) (Return, error)` (`returns.go:577`; phiếu `partial_delivery`, `carrier_return` không hủy được, Task 442);
  > đọc: `GetReturn(ctx, tx, sellerID, id)` (`returns.go:650`), `ListReturns(ctx, tx, sellerID, f ReturnFilter)` (`returns.go:695`).
  > Hàm của `inventory` là phương thức `inventory.Writer`: `ReleaseInTransitByItems(ctx, tx, orderItemIDs, a)`,
  > `ReceiveCustomerReturn(ctx, tx, docID, lines, a)`, `MoveToDamaged(ctx, tx, docID, variantID, warehouseID, qty, a)`
  > (`inventory/returns.go:45,52,89`).
- **Quyết định CS-21/11-2 (a) là ngoại lệ của spec B10** (tin webhook VTP khi khớp bí mật): theo quy trình mục 3 loại C
  cần ADR ngoại lệ do chủ shop duyệt trước khi code; ghi ở kế hoạch M3 (bảng "Chủ shop quyết ngày 21/11"), làm sau khi
  tích hợp hãng mở lại.

## Reviewer

Lần 1 (21/11/2026): CẦN SỬA, hai phát hiện, đã sửa trong kế hoạch M3 (tệp ngoài git):
- Task 110: `publishCallback` là phương thức chưa xuất của `*applyRun` (`apply_effects.go:158`), không gọi được từ đường ghi `Service` → sửa câu: tách hàm báo hiệu nhận `tx`, `tenantID` (commit cấu trúc) rồi dùng cho cả hai đường.
- CS-21/11-1: phần "vẫn làm đối soát `elimaz_v1`" là cách hiểu của Q2, không phải lời chủ shop → ghi rõ ngay trong dòng "cách hiểu của Task 439, chờ chủ shop xác nhận", cách đảo.
- Ghi chú không bắt buộc cũng đã sửa: dòng `adapter.go:83-106`; `carriertest/testdata/best/webhook.json` là Create; SPX ghi rõ `statuses.json` có, `webhook.json` tạo mới.

Lần 2 (21/11/2026): **ĐẠT** — không còn phát hiện; đã kiểm lại dòng 84, 1141, 1695, 1713, 1728 của kế hoạch M3 với mã thật.
