# Task 431 — Móc nối vận đơn khi đơn cancel_requested (M2-r2)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (bù review độc lập M2, Task 89; dòng 431 của
`danh-so-task-toan-cuc.md`). Phạm vi: B-01, B-02 của `docs/review-doc-lap-m2.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào; đổi **hành vi** của hai hàm:
  - `order.Writer.OnShipmentEvent` (chỗ gọi: lớp bọc M1 `Writer.ApplyShipmentProgress` trong
    `internal/modules/order/shipment_progress.go:49`, qua đó `internal/modules/fulfillment/refresh_status.go:151`; test
    `internal/httpapi/handlers_order_cancel_test.go:57`, `internal/modules/customer/stats_test.go:115`, test gói
    `order`). Đơn `cancel_requested` nay nhận được `picked_up` (held → in_transit, đơn giữ `cancel_requested`) và
    `delivered` (cancel_requested → confirmed → delivered). Trước đây cả hai trả `ErrStatusConflict`.
  - `order.Writer.DetachShipment` (chỗ gọi: `internal/modules/fulfillment/create_shipment.go:320` (`OnTerminal`),
    `Writer.OnShipmentCancelConfirmed` (`cancel.go`, nhánh đơn confirmed), test gói `order`). Nay: `confirmed` → gỡ như
    cũ; `cancel_requested` → hủy đơn như hãng xác nhận hủy (cancelled, nhả held, trả lượt mã, gỡ vận đơn, giữ lý do);
    trạng thái khác (delivered, returned) mà vận đơn còn hiện hành → `ErrInvalidTransition`.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/cash`, `internal/modules/customer`,
  `internal/modules/fulfillment`, `internal/testkit/shiptest`. Không gói nào đổi mã; `fulfillment` đổi hành vi theo
  (OnTerminal của lệnh tạo vận đơn chết với đơn đang `cancel_requested` nay hủy đơn thay vì để đơn kẹt).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không (màn hình đọc trạng thái đơn; không có trạng thái mới).
- Câu sqlc, bảng, pha migration: không đổi câu SQL, không migration. Dùng lại `DetachCurrentShipment`,
  `SetCancelReason`, `ClearShipmentOfCancelled`, câu chuyển trạng thái của `transition.sql`.
- Tài liệu: `docs/views.md` dòng `order.updated` (DetachShipment trên đơn cancel_requested phát `order.cancelled`).
- Test mới: `internal/modules/order/shiphooks_cancel_test.go` (7 test, hàm phụ `requestedDH01`,
  `requestedAfterPickup`, `moves`); sửa `cancel_race_test.go` (`TestCancel_Invariants` thêm thao tác `deliver`,
  `detach`; tách `shipmentStep`, `orderInvariant` khỏi `cancelStep`, `checkCancelInvariants` cho gocognit).
- Test sẽ chạy: `go test -race ./internal/modules/order/...` (vùng có tồn: chạy cả gói), `./internal/modules/fulfillment/...`,
  `./internal/modules/customer/...`, `./internal/httpapi/...`, `./internal/modules/cash/...` qua `scripts/agent/ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về trạng
  thái đơn — đúng, logic nghiệp vụ ở lại module); trong `order`: `finishCancel`, `applyTransition`, `setCancelReason`,
  `reservationsOf`, `lockCurrentShipment`, `OnShipmentCancelConfirmed`/`Rejected`.
- Dùng lại / viết mới vì: tách nhánh `cancel_requested` của `OnShipmentCancelConfirmed` thành hàm riêng
  `confirmRequestedCancel` để `DetachShipment` dùng chung (cùng mục đích "hãng chắc chắn không còn kiện → hủy đơn",
  cùng lý do đổi); tách thân `OnShipmentCancelRejected` thành `rejectRequestedCancel` để `OnShipmentEvent(delivered)`
  dùng chung (cùng nghĩa "hãng không hủy được"). Không viết hàm hủy/đổi trạng thái thứ hai.
- Test: dùng lại `attachedDH01`, `pickedUpDH01`, `mustCancel`, `carrierCancel`, `detachBy`, `history` có sẵn (jscpd báo
  3 đoạn lặp ở bản đầu, đã gộp vào các hàm này; lần chạy sau 0 đoạn lặp mới).

## Soát thiết kế

Mục 5:
- 5.1–5.3: không đổi dữ liệu, không bảng mới; `order` đổi tồn chỉ qua public API `inventory.Writer` như cũ.
- 5.4: mọi việc trong transaction của bên gọi (fulfillment), như hợp đồng M2-18.
- 5.5, 5.6: sự kiện `order.*` qua `w.emit` trong transaction như cũ; không gọi ra ngoài.
- 5.7: các câu dùng lại chạy trong tx tenant của bên gọi.
- 5.8–5.11: không áp dụng (không HTTP, không đường ghi mới, không Valkey, không thư viện).
- 5.12: theo mẫu Task 73 (`applyTransition` là đường đổi trạng thái duy nhất).

Mục 6:
- 6.1: đơn bị khóa `FOR UPDATE` trước (lockOrder) trong mọi nhánh; version đọc dưới khóa.
- 6.2: gọi lại an toàn: picked_up lần hai trên cancel_requested (không còn held) → applied=false; delivered lần hai →
  đơn delivered → false; DetachShipment lần hai → vận đơn không còn hiện hành → false.
- 6.3: thứ tự khóa ADR-46 giữ: đơn (7) → lượt mã (10, trong finishCancel) → số dư (11). OnTerminal khóa đơn rồi vận
  đơn (8) rồi gọi DetachShipment → hủy đơn khóa nhóm 10, 11: vẫn tăng dần.
- 6.4–6.9: không đổi (bên gọi chạy lại transaction; không treo mới).
- 6.10: không chạm tiền.
- 6.11: mở rộng test rapid `TestCancel_Invariants` thêm thao tác "detach", "deliver" và đòi picked_up không lỗi.
- 6.12, 6.13: không áp dụng.

## Quyết định thay chủ shop

1. **B-01, ca `picked_up` khi đơn `cancel_requested`: chọn cách (a) của review — giữ hợp đồng M2-18 mà kế hoạch M3 dựa
   vào.** `OnShipmentEvent(picked_up)` chuyển held → in_transit, đơn **giữ** `cancel_requested`; sau đó M3 gọi
   `OnShipmentCancelRejected` đưa đơn về `confirmed` (VC12 `[EffPickedUp, EffCancelRejected]`, kế hoạch M3 dòng 690,
   707, 959, 965 dùng nguyên, **không phải sửa kế hoạch M3**). Vì sao: đúng sơ đồ 05 mục 2 (đơn chỉ rời
   `cancel_requested` qua lời hãng), không sửa kế hoạch M3, trạng thái đơn phản ánh đúng "hãng chưa trả lời hủy".
   Phương án khác: (b) sửa kế hoạch M3 gọi `EffCancelRejected` trước — bỏ vì phải sửa kế hoạch và vẫn hỏng khi hãng
   lấy hàng lúc vận đơn còn `awaiting_pickup` (sự kiện tiêu thụ `order.cancel_requested` chưa chạy). Cách đảo ngược:
   bỏ `StatusCancelRequested` khỏi nhánh nhận của `OnShipmentEvent` (một dòng trong `shiphooks.go`).
2. **B-01, ca `delivered` khi đơn `cancel_requested`:** hàng đã tới tay khách nên việc hủy không còn ý nghĩa: đơn đi
   `cancel_requested → confirmed` (lý do `carrier_rejected_cancel`, bỏ lý do hủy, như hãng từ chối) rồi
   `confirmed → delivered` (hai cạnh có sẵn của sơ đồ), trong một transaction. Phương án khác: trả lỗi rõ — bỏ vì M3
   gọi `EffDelivered` cho vận đơn `awaiting_pickup` (bảng M3 dòng 686) mà đơn có thể đang `cancel_requested`; lỗi làm
   sự kiện của hãng xử lý lại mãi. Đảo ngược: đổi nhánh thành trả `ErrStatusConflict`.
3. **B-02, `DetachShipment`:** `confirmed` → gỡ như cũ; `cancel_requested` → hủy đơn (cùng đường
   `OnShipmentCancelConfirmed`: cancelled, nhả held, trả lượt mã; hàng đã in_transit → `ErrInvalidTransition`);
   trạng thái khác còn gắn vận đơn → `ErrInvalidTransition`. Vì sao không chọn "chỉ nhận confirmed": `fulfillment`
   (M1, `create_shipment.go` `OnTerminal`) đã gọi `DetachShipment` khi lệnh tạo chết, và kế hoạch M3 dòng 880 gọi
   `DetachShipment` cho vận đơn `rejected`; nếu đơn đang `cancel_requested` mà trả lỗi thì hook kết thúc lệnh lùi
   mãi. Gỡ vận đơn mà hãng chắc chắn không tạo chính là "hãng xác nhận hủy" (05 mục 2). M3 gọi `DetachShipment` hay
   `OnShipmentCancelConfirmed` trước đều ra cùng kết quả, hàm gọi sau trả false. Đảo ngược: nhánh `cancel_requested`
   trả `ErrInvalidTransition`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| B-01/VC12: đơn cancel_requested + picked_up → held→in_transit, đơn giữ cancel_requested; lặp lại → false; hãng từ chối → confirmed, vẫn in_transit; giao → delivered | `TestOnShipmentEvent_PickedUpWhileCancelRequested` (shiphooks_cancel_test.go) | `go test -race -run 'WhileCancelRequested|CancelRequestedAfterPickup|TestDetachShipment|TestCancel_Invariants' ./internal/modules/order/` | trước sửa: FAIL `áp picked_up lên đơn cancel_requested`; sau sửa: `ok … order 28.487s` |
| Đã lấy hàng khi cancel_requested mà hãng lại xác nhận hủy → ErrInvalidTransition, không nhả | `TestOnShipmentEvent_PickedUpWhileCancelRequestedThenCarrierConfirms` | như trên | trước: FAIL; sau: ok |
| B-01: delivered khi cancel_requested → cancel_requested→confirmed (carrier_rejected_cancel, seq v+1) → delivered (seq v+2), trừ tồn một lần, gọi lại false | `TestOnShipmentEvent_DeliveredWhileCancelRequested` | như trên | trước: FAIL `áp delivered lên đơn cancel_requested`; sau: ok |
| Yêu cầu hủy sau khi đã lấy hàng: tin lấy hàng lặp → false, đơn giữ cancel_requested; giao → delivered | `TestOnShipmentEvent_CancelRequestedAfterPickup` | như trên | trước: FAIL; sau: ok |
| B-02: DetachShipment trên cancel_requested → cancelled, nhả held, giữ lý do, order.cancelled; gọi lại và OnShipmentCancelConfirmed sau → false | `TestDetachShipment_CancelRequestedCancelsOrder` | như trên | trước: FAIL (đơn cancel_requested, mất vận đơn); sau: ok |
| Thứ tự ngược (OnShipmentCancelConfirmed rồi DetachShipment) → DetachShipment false | `TestDetachShipment_AfterCarrierConfirmsCancel` | như trên | ok (đã qua cả trước sửa) |
| B-02: DetachShipment trên cancel_requested đã in_transit, trên delivered → ErrInvalidTransition, không đổi | `TestDetachShipment_RefusedStates` | như trên | trước: FAIL (`<nil>`); sau: ok |
| Quy luật (rapid): thêm thao tác deliver, detach; lấy hàng/giao bằng vận đơn hiện hành không bao giờ lỗi; không đơn cancel_requested nào mất vận đơn; tồn khớp mô hình | `TestCancel_Invariants` (cancel_race_test.go) | như trên | ok (với mã cũ, bước lấy hàng trên cancel_requested rơi vào `rt.Fatalf`) |
| Không vỡ hành vi cũ (confirmed, gỡ, lớp bọc M1, hủy) | `TestOnShipment*`, `TestWriterDetachShipment`, `TestDetachShipment`, `TestApplyShipment*`, `TestShipmentWrappers_SameAsNew`, `TestCancel*` | `go test -race -run '…' ./internal/modules/order/` | ok (75 s) |
| Cổng kiểm của task | cổng tĩnh + test gói đổi và gói phụ thuộc | ci-task.sh (lần 2, sau khi sửa lint gocognit/gci và 3 đoạn lặp jscpd của lần 1) | build, vet, lint, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; `ok …/modules/order 361.477s`, `ok …/fulfillment`, `ok …/cash`, `ok tests/arch`. Ba test đỏ do máy quá tải (load ~20, bước test 6647 s): `TestOrderRead_EditCancelTimeline` (mốc giờ lệch), `TestStatsConcurrentWithMerge` (57014 statement timeout), `TestMotDonDiTron` (401 ở bước tạo sản phẩm, trước mọi đơn) — chạy lại riêng: `ok …/httpapi 2.124s`, `ok …/customer 2.067s`, `ok …/tests/e2e 10.786s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Không phải sửa kế hoạch M3: VC12 (dòng 690, 707, 959, 965), dòng 880 (`OnTerminal` → `DetachShipment`), dòng 951
  đều chạy đúng với hành vi mới. Hai thứ tự gọi (`DetachShipment` trước hay `OnShipmentCancelConfirmed` trước) cho
  cùng kết quả.

## Việc phát hiện thêm

- Kế hoạch M3 Task 8 (gợi ý, không sửa): khi vận đơn còn `awaiting_pickup` mà đơn đã `cancel_requested` (consumer
  `order.cancel_requested` chưa kịp chạy) và hãng báo `PickedUp`, bảng cạnh M3 chỉ xuất `EffPickedUp` (không
  `EffCancelRejected`): đơn giữ `cancel_requested` với hàng in_transit cho tới khi lệnh hủy của Task 14 nhận
  `CancelRejectedPickedUp`. Đúng spec nhưng M3 nên có test cho ca này (dòng 685 + 956).
- B-03, B-04 (cùng vùng B) thuộc task khác theo bảng của review, không làm ở đây.
- (Reviewer, không bắt buộc) Chú thích `OnTerminal` ở `internal/modules/fulfillment/create_shipment.go` dòng 275, 279
  ("phát order.updated", "giữ hàng không đổi") chưa nói ca đơn `cancel_requested` (nay đơn bị hủy, nhả hàng).
- (Reviewer) `docs/views.md` dòng `order.<trạng thái>` chưa ghi hai nguồn phát mới: `DetachShipment` → `order.cancelled`,
  `OnShipmentEvent(delivered)` khi đơn `cancel_requested` → `order.confirmed`.
- (Reviewer) Chưa có test ở gói `fulfillment` cho `OnTerminal` với đơn `cancel_requested`; M3 Task 12 nên thêm.

## Reviewer

Kết luận: ĐẠT (lần 1). Không có phát hiện phải sửa. Reviewer đã kiểm thứ tự khóa ADR-46 ở nhánh giao xong khi
`cancel_requested` và ở `OnTerminal` → `DetachShipment` → `finishCancel` (7 → 8 → 10 → 11); nhánh `default` mới của
`DetachShipment` chỉ chạm `delivered`/`returned`; sự kiện `order.confirmed` thêm chỉ có `customer.StatsConsumer` nghe
(tính lại từ dữ liệu, vẫn đúng); khớp VC12 và dòng 880, 951 của kế hoạch M3, khớp sơ đồ 05 mục 2. Không test nào bị
xóa, tắt, nới. Ba ghi chú không bắt buộc chép ở "Việc phát hiện thêm".
