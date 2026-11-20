# Task 97 — M3-06: Hàm đơn hàng còn thiếu cho vận chuyển

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 97 = M3-06; luật đọc tên Q-T91-1). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký nào đang dùng.
  - Mới trong `order`: `Snapshot`, `LockSnapshot`, `Writer.ReverseDelivery`, `Writer.MarkDeliveredPartial`,
    `DeliveredLine`, `Writer.SetPackingStatus`, `PackingResult` + hằng `Packing*`, `Packing{Updated,AlreadyFurther,
    NotAllowed}`, `Writer.ApplyAddressChange`, `TopicOrderDeliveryReversed`, hằng `ReturnPartialDelivery`,
    `ReturnStatusReceived` (Q-T91-4; `ReturnReceived` đã là kiểu payload M2), trường `Changed.Partial` (`json:"partial,omitempty"`: payload cũ không đổi byte nào).
  - `order.Lock` giữ chữ ký (3 chỗ gọi ở `fulfillment`: `fulfillment.go:149`, `refresh_status.go:121`,
    `create_shipment.go:305`; 3 chỗ trong `shipment_progress_test.go`); thân gọi lõi chung với `LockSnapshot`.
  - `state.Conditions` thêm trường `CarrierCorrection` (bool, mặc định false). Chỗ dựng `Conditions`: 1 chỗ trong mã
    (`transition.go`, applyTransition) + `machine_test.go`; gói `internal` nên ngoài `order` không ai thấy.
  - Không xuất ra ngoài: `applyTransition` tách thành `applyTransitionWith(…, transitionOpt)`, `emit` tách
    `emitChanged` (commit cấu trúc riêng, không đổi hành vi); mọi chỗ gọi `applyTransition`, `emit` giữ nguyên.
  - `store.LockOrderRow` (mã sinh, nội bộ) thêm 3 cột `packing_status, order_type, warehouse_id`; chỗ dùng đọc theo
    tên trường nên không gãy.
- Gói phụ thuộc ngược (go list): `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`,
  `internal/testkit/shiptest` nhập `order`. Không gói nào phải sửa (chỉ thêm ký hiệu).
- Operation OpenAPI bị chạm, oasdiff: không operation nào (hàm mới do task sau nối vào HTTP: Task 17 đổi địa chỉ,
  Task 26 giao một phần, task in phiếu/đóng gói). Việc riêng điều phối viên giao: xóa hai dòng duyệt của Task 95
  (`createShipment`, `getShipment`, 2026-11-20) và chú thích đi kèm trong `openapi/breaking-approved.yaml` (thay đổi đã
  gộp vào main) → `approved: []`.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: `queries/ord/orders.sql` `LockOrder` đọc thêm 3 cột; tệp mới `queries/ord/shipping.sql`:
  `LockOrdersForPacking` (`WHERE id = ANY($1) ORDER BY id FOR UPDATE`), `SetPackingStatus`, `SetShipAddress` (cột đã
  GRANT UPDATE cho `crm_app` ở 0204). Không gán `status` ngoài `transition.sql` (`TestOrderStatusSQL_OnlyInTransition`).
  Không migration. Bảng: `ord.orders`, `ord.order_status_history` (ghi thêm), `ord.order_items` (đọc); giữ hàng và sổ
  tồn chỉ qua public API `inventory.Writer.ConsumePartial`, `ReverseConsumption`, `StartTransitByItems` (Task 96).
- Test sẽ chạy: `go test -race ./internal/modules/order/...` (gồm `internal/state`), `./internal/modules/inventory/...`
  (vùng rủi ro cao order, inventory), gói phụ thuộc qua cổng kiểm của task (một lần trước review).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm khóa nhiều
  dòng chung — mỗi module tự viết câu `ORDER BY id FOR UPDATE`); trong `order`: `lockOrder`, `lockForShipment`,
  `lockCurrentShipment`, `lockedOrder`, `applyTransition`, `insertHistory`, `emitUpdated`, `rejectRequestedCancel`,
  `cleanAddress`, `normalizedShipPhone`; `customer.ValidateUnits`; trong `inventory`: `ConsumePartial`,
  `ReverseConsumption`, `StartTransitByItems`, `ReservationStatesByItems`.
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên (không viết bản thứ hai của móc nối vận đơn, A11).
  `MarkDeliveredPartial` cùng khung với `OnShipmentEvent(delivered)` nhưng bước trừ tồn khác (`ConsumePartial` theo
  dòng) và sự kiện có `partial`: phần chung (kiểm trạng thái, đọc dòng, held → in_transit, hủy chờ → confirmed rồi
  delivered) tách thành hàm nội bộ cho cả hai, không chép. Đổi địa chỉ dùng `cleanAddress`, `normalizedShipPhone`,
  `ValidateUnits` của sửa đơn; câu SQL riêng vì `UpdateOrderEdit` ghi cả tiền và bị `Editable` chặn (đường này cố ý
  chỉ ghi địa chỉ). Viết mới: câu khóa nhiều đơn cho đóng gói (chưa có).

## Soát thiết kế

- 5.1 chủ dữ liệu: trạng thái đơn, đóng gói, địa chỉ giao thuộc `order` (`ord.orders`); tồn thuộc `inventory`.
- 5.2, 5.3: không ghi bảng module khác: tồn qua `inventory.Writer` (public API); không đọc bảng `fulfillment` (chỉ nhận
  mã vận đơn và so với `current_shipment_id` của chính đơn).
- 5.4: mỗi hàm chạy trong tx của bên gọi (fulfillment), đơn + tồn + lịch sử + sự kiện nguyên tử; lỗi nào bên gọi lùi cả.
- 5.5: sự kiện qua `events.Emit` trong tx (outbox), báo hiệu realtime; không gọi ra ngoài.
- 5.6: không áp dụng: không lệnh ra ngoài.
- 5.7: mọi câu trên bảng có `tenant_id` chạy trong tx tenant của bên gọi (test bằng `crm_app` trong `WithTenant`); tenant
  khác/người bán khác → `ErrNotFound` (ADR-29: mọi câu có `seller_id`).
- 5.8: module không nhập `net/http`.
- 5.9: không áp dụng: không đường HTTP mới (task sau thêm, khi đó có khóa chống trùng); hàm tự an toàn khi gọi lại
  (applied=false, `already_further`).
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: theo mẫu lệnh nhiều module (Task 73): đổi trạng thái chỉ qua `applyTransition*`, SQL trạng thái chỉ ở
  `transition.sql`; sentinel lỗi trong `order/errors.go`.
- 6.1: khóa dòng đơn `FOR UPDATE` trước khi đọc trạng thái; câu ghi so `version` đọc dưới khóa.
- 6.2: gọi lại an toàn: đảo/giao một phần lần hai → applied=false (đơn đã rời trạng thái nguồn); cùng `ref` lần hai ở
  inventory → không đổi; đóng gói lùi hay lặp → `already_further`, không lỗi; đổi địa chỉ trùng → không ghi.
- 6.3: ADR-46: đơn (nhóm 7) trước số dư/giữ hàng (nhóm 11); nhiều đơn khóa một câu `ORDER BY id` (test hai goroutine
  `[D1,D2]` và `[D2,D1]` không deadlock).
- 6.4: bên gọi (fulfillment) chạy trong `db.RetryTx`/việc nền; hàm không giữ biến cộng dồn ngoài thân (chạy lại được).
- 6.5–6.9: không áp dụng: không việc nền, không gọi ra ngoài; transaction ngắn của bên gọi.
- 6.10: không đổi tiền: `grand_total`, `cod_amount` giữ nguyên khi giao một phần, đảo giao (tiền do `cash` xử lý theo
  sự kiện, Q-1).
- 6.11: rapid 200 lượt: chuỗi ngẫu nhiên móc nối M2 xen hàm mới → không `delivered` hai lần không có đảo ở giữa; `Seq`
  sự kiện tăng ngặt; tồn không âm.
- 6.12: địa chỉ, SĐT không vào log, sự kiện, lịch sử (lịch sử chỉ lý do mã máy); `ShipAddress` tự che khi log.
- 6.13: không áp dụng: quyền kiểm ở lớp HTTP của task sau; hàm nhận `sellerID` của bên gọi (ADR-29).

## Quyết định thay chủ shop

1. **`LockSnapshot` mới, giữ `Lock` chỉ khóa.** Kế hoạch đổi `Lock` thành trả `Snapshot`; làm vậy phải sửa 3 chỗ gọi
   trong `fulfillment` (`fulfillment.go`, `refresh_status.go`, `create_shipment.go`) — vùng của Task 98 đang chạy song
   song, phiên này không được sửa tệp fulfillment. Chọn: hàm `LockSnapshot(ctx, tx, sellerID, id) (Snapshot, error)` cùng
   lõi `lockOrder`; `Lock` giữ chữ ký. Phương án khác: đổi `Lock` (đúng chữ kế hoạch). Đảo: một task sau đổi `Lock`
   thành bản trả `Snapshot` và xóa `LockSnapshot` (3 chỗ gọi).
2. **Tên hằng `ReturnStatusReceived`** (không phải `ReturnReceived`): `ReturnReceived` đã là kiểu payload của M2.
3. **Payload `order.delivery_reversed` là `Changed{orderId, sellerId, version, customerId, status}`** theo bổ sung Task 91
   (consumer `customer_stats` đọc `Changed`), không phải `{orderId, shipmentId, customerId, reason}` của dòng Interface
   cũ; lý do nằm ở lịch sử trạng thái (`reason`), vận đơn là `current_shipment_id` của đơn. Một version một sự kiện: lần
   đảo chỉ phát `order.delivery_reversed`, không phát thêm `order.confirmed` (`transitionOpt.topic`).
4. **Thiếu hay sai mẫu `reason` của `ReverseDelivery` → `order.ErrInvalidInput`** (kiểm trước khi khóa; mẫu như CHECK
   của lịch sử). Phương án khác: trả nguyên `inventory.ErrReasonRequired` (lỗi của module khác lọt ra ngoài order).
5. **`ApplyAddressChange` trả `(changed bool, err error)`** (kế hoạch: `error`): cùng địa chỉ → `false`, không ghi, để
   bên gọi chỉ audit khi có đổi (tên trường `EditFieldShipAddress`; order không nhập identity nên audit ở bên gọi như
   `Edit`). `shipmentID` không phải vận đơn hiện hành → `ErrInvalidTransition` (lời người dùng, không lặng im như móc
   nối hãng); chỉ đơn `confirmed`/`cancel_requested`; địa chỉ phải đủ tỉnh, xã (đơn đã chốt). Người làm `a` không ghi
   vào đâu (`_`), như `AttachShipmentBy`.
6. **Lịch sử đóng gói:** `order_status_history` không có cột đóng gói; ghi dòng `confirmed → confirmed` lý do
   `packing_picking`/`packing_packed` với người đóng gói (cùng kiểu dòng `new → new` đổi lý do chờ của M2), version + 1,
   `order.updated`. Một mã không có trong lệnh nhiều đơn → cả lệnh `ErrNotFound` (không đổi đơn nào). Phương án khác:
   cột mới `packed_by` (cần migration, ngoài phạm vi).
7. **`partial` trong `order.delivered`:** trường `Changed.Partial` có `omitempty` — payload M2 không đổi byte nào
   (`TestOnShipmentEvent_DeliveredNoPartialKey`).
8. **Hai commit cấu trúc trước commit hành vi** (luật CLAUDE.md mục 3): b86607a (tách `applyTransitionWith`,
   `emitChanged`, hằng Q-T91-4) và 6787ba5 (tách `beginShipmentStep`, `finishDelivered`), mỗi commit reviewer ĐẠT.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc: tách `applyTransitionWith`, `emitChanged` không đổi hành vi | cả bộ `order` (sự kiện, lịch sử, móc nối M2) | `go test -race -count=1 ./internal/modules/order/...` | `ok order 209.768s`, `ok state`, `ok pricing` |
| Commit cấu trúc 2: tách `beginShipmentStep`, `finishDelivered` khỏi `OnShipmentEvent`, không đổi hành vi | `TestOnShipmentEvent*`, `TestShipmentProgress*`, `TestApplyShipmentProgress*`, `TestCancel*`, `TestDetach*`, `TestAttach*`, `TestReturn*`, `TestPay*` (trên bản sao HEAD + `shiphooks.go`) | `go test -race -count=1 -run '<các mẫu trên>' ./internal/modules/order/` | `ok order 48.860s`; `go build ./... && go vet` OK |
| VC04/AT08: packed rồi picking → `already_further`, vẫn packed; đơn new → `not_allowed`; lịch sử người đóng gói | `TestSetPackingStatus`, `TestSetPackingStatus_Rejects` | `go test -race -count=1 -run 'TestSetPackingStatus' ./internal/modules/order/` | `--- PASS` cả 3 |
| `[D1,D2]` và `[D2,D1]` song song không deadlock (10 vòng) | `TestSetPackingStatus_NoDeadlock` | như trên | `--- PASS (0.51s)` |
| TD10: đảo → confirmed, vA on_hand 5 in_transit 1, một `order.delivery_reversed` seq = version; R1 lại → false; S2 → false; thiếu lý do → lỗi | `TestReverseDelivery`, `TestReverseDelivery_NotDelivered`, `TestReverseDelivery_ThenReturning` | `go test -race -count=1 -run 'TestReverseDelivery' ./internal/modules/order/` | `--- PASS` cả 3 |
| Máy trạng thái: delivered → confirmed chỉ khi `CarrierCorrection`; 49 cặp không đổi | `TestCheck_CarrierCorrection`, `TestCheck_AllPairs` | `go test -race -count=1 ./internal/modules/order/internal/state/` | `ok` |
| TD09: [vA×1, vB×1] → delivered, vA 4, vB 9 / in_transit 1, `partial: true`; gọi lại → false; từ held; chờ hủy; lỗi | `TestMarkDeliveredPartial`, `_FromHeld`, `_Rejects`, `_CancelRequested`, `TestOnShipmentEvent_DeliveredNoPartialKey` | `go test -race -count=1 -run 'TestMarkDeliveredPartial\|TestOnShipmentEvent_DeliveredNoPartialKey' ./internal/modules/order/` | `--- PASS` cả 5 |
| DH15: đổi địa chỉ qua S1; `Edit` vẫn `ErrLockedByShipment`; vận đơn khác, thiếu xã, SĐT sai bị từ chối; đơn delivered không đổi | `TestApplyAddressChange`, `TestApplyAddressChange_Delivered` | `go test -race -count=1 -run TestApplyAddressChange ./internal/modules/order/` | `--- PASS` cả 2 |
| `LockSnapshot` đúng trường, tenant/người bán khác → `ErrNotFound` | `TestLockSnapshot` | `go test -race -count=1 -run TestLockSnapshot ./internal/modules/order/` | `--- PASS` |
| Thuộc tính 200 lượt: không delivered hai lần không đảo ở giữa; seq tăng ngặt = version; tồn không âm | `TestShipping_Invariants` | `go test -race -count=1 -v -run TestShipping_Invariants ./internal/modules/order/` | `[rapid] OK, passed 200 tests` |
| Cả bộ order (M2 Task 08, 18, 20, 21, 22), gói phụ thuộc, cổng tĩnh (build, vet, lint, gen-check, sqlc vet, jscpd, oasdiff, test-guard, gitleaks) | gói đổi + phụ thuộc + vùng cao | cổng kiểm của task (lần 2, sau khi sửa trùng mã jscpd ở test) | `CI-TASK XANH`; `ok order 194.159s`, `ok fulfillment 27.177s`, `ok httpapi 76.306s`, `ok customer`, `ok cash`, `ok tests/arch`, `ok tests/e2e` |
| Cả bộ inventory (không đổi, kế hoạch Step 4) | toàn bộ `inventory` | `go test -race -count=1 ./internal/modules/inventory/...` | `ok inventory 75.396s` |
| Hằng `ReturnPartialDelivery`, `ReturnStatusReceived` khớp CHECK `ord.returns` | `TestSchemaReturnM3Constants` | `go test -race -count=1 -run TestSchemaReturnM3Constants ./internal/modules/order/` | `ok` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký theo Q-T91-1: mọi hàm mới là phương thức `order.Writer` nhận `sellerID` sau `tx`; `Update` của kế hoạch là
  `Edit`. `Lock` → `LockSnapshot`, `ApplyAddressChange` trả `(bool, error)` (Quyết định 1, 5).
- Payload `order.delivery_reversed` là `Changed` (Quyết định 3).
- Việc thêm theo lời điều phối viên: `openapi/breaking-approved.yaml` bỏ hai dòng duyệt Task 95 (`createShipment`,
  `getShipment`, 2026-11-20) và chú thích đi kèm → `approved: []` (đã gộp vào main).
- `docs/views.md`: dòng `order.delivery_reversed` đổi từ giữ chỗ thành dòng thật; `order.delivered` thêm `partial?`;
  `order.updated` thêm đóng gói, đổi địa chỉ.

## Việc phát hiện thêm

- `OnShipmentEvent(delivered)` (M2) gọi sau `ReverseDelivery` trả lỗi thô 23505 (`stock_ledger_step_key`) từ
  `inventory.ConsumeByItems` trên giữ hàng đã đảo (inventory ghi "Consume lại giữ hàng đã đảo không được hỗ trợ"). Bên gọi
  lùi transaction nên không hỏng dữ liệu, nhưng nên là `inventory.ErrStateConflict`; fulfillment (Task 98) không nên gửi
  "delivered" lần hai sau đảo (đơn đi đường hoàn). Đề xuất task inventory nhỏ.
- `inventory.ConsumePartial` với Qty < số đang giữ trên giữ hàng đã đảo không bị chặn (chú thích nói ErrStateConflict);
  tồn vẫn cân (đảo đã cộng lại), chỉ lệch chú thích. Task 96 (inventory) xem lại.
- Test chập chờn ngoài vùng: `TestCustomerWriteAPI` (`internal/httpapi`) kiểm thân 409 không chứa "912" nhưng `ownerId`
  ngẫu nhiên có thể chứa "912" (đỏ một lần trong cổng kiểm và một lần chạy lẻ, xanh 3/3 lần sau và trên main);
  `TestStreamBatchDedupe` (`internal/platform/realtime`) đỏ khi LISTEN rớt do máy bận, chạy lẻ xanh.
- `ReverseDelivery` sau một lần giao một phần chỉ đảo giữ hàng `consumed` (dòng giao đủ); phần đã giao của dòng giao
  một phần (giữ hàng vẫn `in_transit`, bước `consume_partial`) không được `inventory.ReverseConsumption` cộng lại; nếu
  mọi dòng đều giao một phần thì đảo trả `inventory.ErrStateConflict`. Đúng chú thích của Task 96 nhưng cần quyết định
  nghiệp vụ khi M3 nối TD09 + TD10 (Task 26/28).
- `Lock` và `LockSnapshot` cùng tồn tại (Quyết định 1): gộp khi task fulfillment kế tiếp chạm 3 chỗ gọi.

## Reviewer

Kết luận: ĐẠT (3 lượt, mỗi commit một lượt, không phát hiện phải sửa).

- b86607a (cấu trúc 1): ĐẠT; ghi chú tên hằng trong tệp task → đã sửa thành `ReturnStatusReceived`.
- 6787ba5 (cấu trúc 2): ĐẠT; ghi chú thêm dòng bằng chứng → đã thêm.
- Commit hành vi: ĐẠT. Ghi chú không bắt buộc: đảo sau giao một phần và delivered sau đảo (đã ở Việc phát hiện thêm);
  Task 17 phải ghi audit `EditFieldShipAddress` khi `ApplyAddressChange` trả changed=true.
