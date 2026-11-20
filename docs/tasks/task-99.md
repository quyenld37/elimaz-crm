# Task 99 — M3-08: Áp kết quả tra lên vận đơn, đơn, tồn (ApplyTracking)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 99, M3-08). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Thêm (gói `fulfillment`): `ApplyInput`, `ApplyReport`, `(*Service).ApplyTracking`, `(*Service).AttachTrackingCode`,
    `AttachResult`, `ShipmentEvents` (hàm gói, đọc lịch sử), `EventPage`, `EventView`, `ErrStockShort`,
    `ErrInvalidTracking`, `TopicShipmentCreated`, `ShipmentCreated`, bí danh `Source`/`Src*` (nguồn của sự kiện, để
    gói ngoài như `httpapi`, Task 11–14 gọi được mà không nhập `internal/shipstate`).
  - Đổi: `fulfillment.StatusChanged` chỉ **thêm** trường có `omitempty` (`orderId`, `from`, `effects`, `codCollected`,
    `partial`, `occurredAt`); 3 chỗ dựng giá trị (`create_shipment.go:145`, `cash/env_test.go:117`,
    `cash/cod_test.go:335`) và 1 chỗ đọc (`cash/consumer.go`) không phải sửa (trường cũ giữ tên, kiểu, nghĩa).
  - Không đổi chữ ký nào của `order`, `inventory`, `shipstate` (chỉ gọi: `order.LockSnapshot`, `order.Lock`,
    `Writer.OnShipmentEvent`, `OnShipmentReturning`, `OnShipmentCancelConfirmed`, `OnShipmentCancelRejected`,
    `ReverseDelivery`). Thêm một ký hiệu ở `order`: `order.ErrStockShort` (= `inventory.ErrStockShort`, cùng giá trị) —
    fulfillment không được nhập inventory (luật (a) của `TestBoundariesRealCode`), cần nó để bọc `ErrStockShort`
    (Q-T91-11); có dòng ở `internal/httpapi/errors.go` (`TestErrorsMappedRealCode`).
  - `internal/httpapi/errors.go`: thêm `fulfillment.ErrInvalidPage`, `ErrInvalidTracking` → 400 `invalid_input`,
    `fulfillment.ErrStockShort`, `order.ErrStockShort` → 409 `stock_short`.
  - `docs/views.md`: dòng `shipment.status_changed` (trường mới, mỗi bước một sự kiện), dòng mới `shipment.created`.
- Gói phụ thuộc ngược (go list) của `internal/modules/fulfillment`: `cmd/crm`, `internal/adapters/carrier` (+ `best`,
  `carriertest`, `fake`, `manual`, `spx`, `vtp`), `internal/httpapi`, `internal/modules/cash`,
  `internal/testkit/shiptest`, `tools/fakecarrier/server`. Không ai dùng ký hiệu mới ngoài `httpapi` (handler lịch sử).
- Operation OpenAPI bị chạm, oasdiff: `listShipmentEvents` (GET `/api/v1/shipments/{id}/events`) — hợp đồng đã có từ
  Task 93 (M3-04), task này chỉ thay thân handler 501 → 200; không sửa `openapi.yaml` nên oasdiff không đổi. Xóa dòng
  `listShipmentEvents` khỏi `tests/arch/testdata/m3_pending.txt`.
- Chỗ dùng trên màn hình: không (màn vận đơn là Task M3 sau; client sinh lại không đổi vì hợp đồng không đổi).
- Câu sqlc, bảng, pha migration: không migration (cột M3 đã có từ 0300). Câu mới:
  - `queries/fulfillment/shipments.sql`: `ShipmentOrderRef` (đọc đơn, người bán không khóa), `LockShipmentForApply`
    (FOR UPDATE OF s), `ApplyShipmentStatus`, `AdvanceShipmentLastEvent`, `FinishShipmentSync`, `TrackingCodeTaken`,
    `AttachShipmentTracking`, `MarkShipmentNeedsReview`.
  - `queries/fulfillment/events.sql`: `InsertTrackedEvent` (ON CONFLICT DO NOTHING RETURNING id),
    `ListShipmentEvents` (mới nhất trước, con trỏ (occurred_at, id)).
  - `queries/fulfillment/callbacks_core.sql` (mới): `NextCallbackAttempt`, `InsertCallbackTask`, `CloseOpenCallbacks`.
  - `queries/fulfillment/anomalies.sql` (mới): `OpenShipmentAnomaly` (ON CONFLICT theo unique mở DO NOTHING).
  - Bảng chạm (crm_app, cột đã GRANT ở 0118/0300): `fulfillment.shipments` (UPDATE cột trạng thái, mốc, đồng bộ,
    `needs_review`, `cancel_kind`, `create_state`), `shipment_events` (INSERT), `callback_tasks` (INSERT, UPDATE status,
    outcome), `shipment_anomalies` (INSERT). Bảng của module khác chỉ qua public API của `order` (và `order` gọi
    `inventory`).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/...` (vùng rủi ro cao), `./internal/modules/order/...`,
  `./internal/modules/inventory/...` (vùng rủi ro cao — kế hoạch Step 4), `./internal/modules/cash/...` (đọc
  `StatusChanged`), `./internal/httpapi/... -run 'ShipmentEvents|M3Routes|Shipment'`, `./tests/arch/...`; cuối cùng
  `scripts/agent/ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `logging`
  (`ScrubText`), `db` (`Savepoint`), `realtime` (`WarehouseScope`), `events` (`Emit`), `settings`; trong module:
  `fulfillment.LoadSettings` (`CallbackDeadlineHours`), `emitStatus`/`StatusChanged` (`create_shipment.go`),
  `shipstate.Decide`/`Target`, `order.LockSnapshot`, móc nối vận đơn của M2 (`shiphooks.go`, `cancel.go`,
  `returns.go`, `shipping.go`); test: `shipEnv` (`env_test.go`), `shiptest.Shop`, `invtest`.
- Dùng lại / viết mới vì:
  - Dùng lại `shipstate.Decide` (gọi thẳng, không qua `Advance` — ghi chú Task 98), `order.*` (mọi thay đổi đơn, tồn),
    `logging.ScrubText` (che SĐT, địa chỉ trong `sub_status`, `reason` trước khi lưu), `db.Savepoint` (gắn mã hãng vi
    phạm unique, tác động hủy gặp `ErrInvalidTransition`), `LoadSettings`, `events.Emit`, `realtime.WarehouseScope`,
    payload `StatusChanged` (thêm trường, không định nghĩa payload thứ hai). Test dùng lại `shipEnv`, `shiptest`.
  - Viết mới: vòng áp (`apply_run.go`), tác động (`apply_effects.go`), câu SQL ở trên (chưa có câu nào ghi các cột M3
    này). `emitStatus` của M1 không đủ (không có trường mới) nên ApplyTracking dựng `StatusChanged` đầy đủ qua một hàm
    phát riêng dùng chung `TopicShipmentStatusChanged`, `aggregateShipment`.

## Soát thiết kế

- 5.1 chủ dữ liệu: mọi bảng ghi là `fulfillment.*` (chủ là module fulfillment). Không dữ liệu mới.
- 5.2: không ghi bảng `ord.*`, `inventory.*`: đổi đơn và tồn chỉ qua public API của `order` (`order` gọi `inventory`).
- 5.3: không đọc bảng module khác; trạng thái đơn đọc qua `order.LockSnapshot`.
- 5.4: một transaction của bên gọi cho cả lô sự kiện: vận đơn, lịch sử, đơn, tồn, việc gọi lại, cần xem, sự kiện nội
  bộ (nguyên tử: không bao giờ vận đơn `delivered` mà tồn chưa trừ).
- 5.5: tiền COD không ghi ở đây: `cash` nghe `shipment.status_changed` (Emit trong cùng tx, consumer chạy sau commit).
- 5.6: không gọi mạng: `ApplyTracking` nhận `Tracking` đã đọc (lệnh tra chạy ngoài tx ở Task 11).
- 5.7: mọi câu chạy trong tx tenant của bên gọi; bảng không mới. Tenant khác → RLS ẩn → `ErrNotFound` (có test).
- 5.8: không nhập `net/http`, ogen; handler `ListShipmentEvents` mỏng (đọc, kiểm quyền kho, đổi kiểu).
- 5.9: không đường ghi HTTP mới (`listShipmentEvents` chỉ đọc, `x-permission: fulfillment.read` sẵn có).
- 5.10, 5.11: không áp dụng: không Valkey, không thư viện mới.
- 5.12: theo module mẫu `order` Task 73 (lệnh điều phối nhiều module, lỗi module khác bọc `%w`), test gói `_test` trên
  PostgreSQL thật, quy luật bằng rapid.
- 6.1: hai lần áp cùng vận đơn: cả hai khóa đơn (`order.LockSnapshot`, nhóm 7) rồi `SELECT … FOR UPDATE` vận đơn →
  chạy lần lượt; lần sau thấy trạng thái mới (Decide trả `duplicate_state`/`older`). Test 10 goroutine.
- 6.2: chống lặp bằng khóa tự nhiên `shipment_events_key (tenant_id, shipment_id, event_key)` ON CONFLICT DO NOTHING:
  sự kiện đã có thì bỏ qua cả quyết định và tác động (VC08). Việc gọi lại: unique `(shipment_id, attempt_no)`;
  anomaly: unique mở `(shipment_id, kind) WHERE open`. Gắn mã hãng: vận đơn đã có đúng mã → `Already`.
- 6.3 thứ tự khóa (ADR-46): đọc `order_id` không khóa → `ord.orders` (nhóm 7) → `fulfillment.shipments` →
  `shipment_events` → (tác động: `order` khóa lại đơn đã giữ, `inventory` nhóm 11) → `callback_tasks` →
  `shipment_anomalies`. Ghi `callback_tasks` sau số dư tồn (nhóm 11) trái thứ nhóm, nhưng mọi đường chạm việc gọi lại
  của một vận đơn đều khóa đơn của nó trước (nhóm 7) nên không thể chờ vòng; ghi ở Quyết định 5.
- 6.4: lỗi tạm (40P01, 55P03) trả nguyên lên bên gọi (RetryTx/worker chạy lại cả tx); không bắt trong savepoint.
- 6.5: READ COMMITTED + khóa dòng đơn, vận đơn; mọi đọc quyết định nằm sau khóa.
- 6.6–6.8: không áp dụng: hàm trong tx của bên gọi, không việc nền mới (lệnh tra là Task 11).
- 6.9: số câu theo số sự kiện của lô (vài chục); không chờ ngoài.
- 6.10: không ghi tiền; `fee_actual`, `cod_collected_amount` là `int64` đồng, chỉ ghi khi hãng báo.
- 6.11: rapid 200 lượt trên PostgreSQL thật: hành trình xáo trộn chia lô → tồn, đơn khớp trạng thái cuối; xuất kho ≤ 1
  bộ; sổ tồn khớp số dư.
- 6.12: `raw` lưu chỉ các trường đã chuẩn hóa (`key`, `rawStatus`, `canonical`, `subStatus`, `reason`), chuỗi tự do đi
  qua `logging.ScrubText`; lịch sử API không trả `raw`. Payload sự kiện, báo hiệu chỉ có mã, trạng thái, số tiền.
- 6.13: `listShipmentEvents` cần `fulfillment.read` ở kho của tài khoản hãng; thiếu quyền ở kho → 404 (không lộ có vận
  đơn), có test.

## Quyết định thay chủ shop

1. **Không tạo gói `internal/apply`; mã đặt trong gói `fulfillment`** (`apply_public.go`, `apply_run.go`,
   `apply_effects.go`, `apply_attach.go`). Vì sao: gói con phải dùng `fulfillment.Tracking`, `StatusChanged`,
   `LoadSettings`, `ErrNotFound` mà `fulfillment` lại nhập gói con → vòng nhập (Go cấm). Phương án khác: dời các kiểu đó
   vào gói con và để bí danh (như Task 98 làm với `Canonical`) — dời nhiều kiểu M1 đang dùng ở 6 adapter, rủi ro cao hơn
   lợi ích. Đảo: tách sau khi kiểu được dời. Test đặt trong `fulfillment_test` để dùng lại `shipEnv`.
2. **`Events` là hàm gói `fulfillment.ShipmentEvents`, không phải phương thức của `Service`**: `httpapi` chưa được nối
   `*fulfillment.Service` (Deps của httpapi không có), hàm chỉ đọc không cần phụ thuộc nào (như `Get` của M1). Nối
   Service vào httpapi sẽ đụng `cmd/crm` và Task 100 đang chạy song song. Đảo: thêm phương thức bọc khi có Service.
3. **Mỗi bước đổi trạng thái phát một `shipment.status_changed`** (Seq = version sau bước đó, `from`, `effects` của
   đúng bước), thay vì một sự kiện cho cả lô. Vì sao: Task 29 (`cash`) ghi tiền theo `effects` với khóa
   `collected:<shipment>:<seq>`; gộp cả lô (vd. giao rồi đảo trong cùng lần tra) làm mất thứ tự. VC05 (một tin) vẫn đúng
   một sự kiện. Bước không đổi trạng thái (`returning` + `ReturnedToShop`) tăng version nhưng không phát (Seq có lỗ là
   hợp lệ). `Bus.Publish shipment.changed` một lần mỗi lần áp có đổi.
4. **`AttachTrackingCode` tăng `version` 2**: phát `shipment.created` (Seq v+1) và `shipment.status_changed`
   (`awaiting_pickup`, Seq v+2) — hai sự kiện của một đối tượng không được cùng Seq (unique của `domain_events`).
   Phương án khác: một sự kiện (trái kế hoạch), hay aggregate khác cho `created` (mất thứ tự với status_changed).
5. **Ghi `callback_tasks` sau tác động tồn** (theo đúng thứ tự `Decision.Effects`), dù ADR-46 xếp `callback_tasks`
   (nhóm 8) trước tồn (nhóm 11): mọi đường chạm việc gọi lại đều khóa đơn trước nên không chờ vòng. Đổi thứ tự tác động
   sẽ làm khác thứ tự của máy trạng thái.
6. **Mở bất kỳ anomaly nào cũng đặt `needs_review = true`** (kế hoạch ghi rõ cho `tracking_conflict`; Task M3 sau
   `resolveShipmentAnomaly` đặt về `false` khi không còn anomaly mở — nên cờ phải bật cho mọi loại).
7. **`EffReturning` khi đơn còn `cancel_requested`** (vận đơn `awaiting_pickup` mà đơn đã yêu cầu hủy, hãng báo hoàn):
   gọi `OnShipmentCancelRejected` trước rồi `OnShipmentReturning` (Q-T91-2: "đơn về confirmed trước rồi mới tạo phiếu
   hoàn"); không thì `OnShipmentReturning` trả `ErrInvalidTransition` và lần tra kẹt mãi.
8. **`EffDeliveryReversed` chỉ gọi `ReverseDelivery`** (ref = mã dòng `shipment_events`, lý do `carrier_corrected`);
   `OnShipmentReturning` do `EffReturning` đi ngay sau trong cùng cạnh (gọi hai lần cũng không sao, hàm bỏ qua lần hai).
9. **Tác động hủy gặp `order.ErrInvalidTransition` chạy trong savepoint**: lùi phần `order` có thể đã ghi, mở anomaly
   `cancel_conflict`, vận đơn vẫn `cancelled` theo hãng. Gắn mã hãng cũng trong savepoint để bắt vi phạm unique
   `shipments_tracking_key` (hai vận đơn cùng lúc gắn một mã) mà tx vẫn dùng tiếp được.
10. **Người làm mặc định**: `in.Actor.Type` rỗng thì lấy theo nguồn (`carrier_api` → `carrier`, `system` → `system`;
    `staff` giữ `staff`). `raw_status` rỗng → mã chuẩn; dài hơn 64 ký tự → cắt (CHECK của M1); khóa sự kiện rỗng hay
    dài hơn 200 → `ErrInvalidTracking`.
11. **Mốc thời gian**: `picked_up_at`, `delivered_at`, `carrier_reported_returned_at` = `OccurredAt` của sự kiện (giữ
    mốc đầu); `last_status_at` = `now()` của database (như M1, dùng cho "Đơn kẹt"); `last_carrier_event_at` dời theo
    `Decision.AdvanceLast` (ghi chú Task 98), không lùi. Giờ ghi lịch sử đơn = `Deps.Now`.

## Bằng chứng

Lệnh A: `go test -race -count=1 -v ./internal/modules/fulfillment/ -run 'TestApply|TestAttach'` → 23 `--- PASS`, `ok`
(rapid 200 lượt). Lệnh B: `go test -race -count=1 ./internal/httpapi/ -run 'TestShipmentEventsAPI|TestShipmentAPI|M3Routes'`
→ `ok`. Lệnh C: `scripts/agent/ci-task.sh` (fulfillment, order, inventory, cash, httpapi, arch, e2e…).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| VC05: PickedUp → shipping, vA in_transit 1, vB in_transit 2, đúng 1 `shipment.status_changed` from awaiting_pickup to shipping, version + 1 | `TestApply_PickupDeliverDuplicateLate` | A | PASS |
| VC06: Delivered → V1, D1 delivered, vA on_hand 4, vB 8, đúng 2 dòng phiếu xuất; mốc picked_up_at, delivered_at, last_carrier_event_at | `TestApply_PickupDeliverDuplicateLate` | A | PASS |
| VC08: gửi lại e1, e2 hai lần → Inserted 0, phiếu xuất vẫn 2, không thêm sự kiện nội bộ | `TestApply_PickupDeliverDuplicateLate` | A | PASS |
| VC09: InTransit 12:00 (e9) sau thành công → applied false, `older`, đơn delivered | `TestApply_PickupDeliverDuplicateLate` | A | PASS |
| VC07: hai lần thất bại → việc gọi lại attempt 1 (hạn 10:00 hôm sau), attempt 2 (14:00 hôm sau); giao xong → cả hai closed `delivered`; phiếu xuất 2 | `TestApply_FailedTwiceThenDelivered` | A | PASS |
| Thiếu tin lấy hàng: awaiting_pickup → Delivered: [EffPickedUp, EffDelivered], 1 phiếu xuất mỗi biến thể | `TestApply_DeliveredWithoutPickup` | A | PASS |
| Lô lộn thứ tự áp theo OccurredAt; mỗi bước đổi trạng thái 1 sự kiện (Quyết định 3); báo hiệu `shipment.changed` fulfillment.read ở kho; last_synced_at | `TestApply_BatchSortedByTime` | A | PASS |
| fee_actual, cod_collected_amount ghi; last_sync_error, stuck_since xóa; payload `codCollected` | `TestApply_FeeAndCODCollected` | A | PASS |
| VC11: V1 hủy để thay (`cancel_kind replace`), D1 gắn V3; tin muộn của V1 chỉ vào lịch sử V1; PickedUp của V1 → `not_current` + anomaly `not_current_moved`; tồn không đổi | `TestApply_LateEventsOfReplacedShipment` | A | PASS |
| Hãng xác nhận hủy, đơn cancel_requested → đơn cancelled, nhả hàng, `cancel_kind order_cancel` | `TestApply_CancelConfirmedReleasesStock` | A | PASS |
| Hãng tự hủy → `carrier_cancelled`, đơn confirmed gỡ vận đơn, giữ hàng | `TestApply_CarrierCancelledWithoutRequest` | A | PASS |
| `ErrInvalidTransition` khi hủy (hàng in_transit) → không nhả, anomaly `cancel_conflict`, needs_review, vận đơn cancelled | `TestApply_CancelConflictWhenInTransit` | A | PASS |
| Task 431: awaiting_pickup + đơn cancel_requested + PickedUp → chỉ EffPickedUp, đơn giữ cancel_requested, hàng in_transit | `TestApply_PickedUpWhileOrderCancelRequested` | A | PASS |
| Q-T91-2: cancel_requested + Returning → [EffPickedUp, EffCancelRejected, EffReturning], đơn confirmed, phiếu hoàn requested | `TestApply_ReturningWhileCancelRequested` | A | PASS |
| Quyết định 7: awaiting_pickup + đơn cancel_requested + Returning → đơn confirmed, phiếu hoàn | `TestApply_ReturningAwaitingWhileOrderCancelRequested` | A | PASS |
| Hoàn đóng việc gọi lại `carrier_returning`; ReturnedToShop → `carrier_reported_returned_at`, vẫn returning | `TestApply_ReturningClosesCallbacks` | A | PASS |
| TD10: delivered + Returning (API) → đảo phiếu xuất, đơn confirmed, phiếu hoàn | `TestApply_DeliveryReversed` | A | PASS |
| Mã lạ ×2, giao một phần → không áp, mỗi loại anomaly 1 dòng, status sự kiện rỗng | `TestApply_AnomaliesOpenOnce` | A | PASS |
| Tenant B với vận đơn của A → `ErrNotFound`, không ghi | `TestApply_OtherTenantNotFound` | A | PASS |
| SĐT (receiver_phone) trong trạng thái con, lý do → không có trong raw, sub_status, failed_reason | `TestApply_RawHasNoPhone` | A | PASS |
| Khóa sự kiện rỗng → `ErrInvalidTracking` | `TestApply_InvalidEventKey` | A | PASS |
| Song song 10 goroutine Delivered → đúng 1 EffDelivered, không lỗi/deadlock, 2 dòng phiếu xuất | `TestApply_ConcurrentDelivered` | A | PASS |
| Thuộc tính (rapid 200, PostgreSQL thật): hành trình xáo trộn + bản trùng, 1–5 lô → vận đơn = đích sự kiện cuối, đơn, tồn, phiếu xuất ≤ 1 (=1 khi giao), phiếu hoàn, sổ tồn = số dư | `TestApply_PropertyShuffledBatches` | A | PASS |
| AttachTrackingCode: creating → awaiting_pickup, created, phí, 1 `shipment.created` + 1 status_changed; lại cùng mã → Already; mã khác → `tracking_conflict` | `TestAttachTrackingCode` | A | PASS |
| Mã thuộc V6 → anomaly ở V5, V5 creating, needs_review (savepoint) | `TestAttachTrackingCode_TakenByOther` | A | PASS |
| Hai goroutine gắn cùng mã → 1 `shipment.created` | `TestAttachTrackingCode_Concurrent` | A | PASS |
| API: không quyền ở kho → 404; chỉ order.read → 403; vận đơn lạ → 404; mới nhất trước, không `raw`, applied/ignoredReason/label; trang 2 theo con trỏ; con trỏ sai → 400 | `TestShipmentEventsAPI` | B | PASS |
| `listShipmentEvents` đã làm (bỏ khỏi m3_pending) | `TestM3Routes_PendingList`, `TestM3Contract_PendingList` | B, C | PASS |
| Không nhập inventory, sentinel đã ánh xạ, lint | `TestBoundariesRealCode`, `TestErrorsMappedRealCode`, `TestLintCleanOnRealCode` | C | PASS |

Đột biến tay (kiểm test bắt lỗi): `IsCurrent: true` → `TestApply_LateEvents…` FAIL; bỏ dời `last_carrier_event_at` khi
`AdvanceLast` → `TestApply_PropertyShuffledBatches` FAIL; `PickedUp: false` → sống (tương đương: `order.OnShipmentEvent`
tự bỏ qua lần lấy hàng thứ hai khi không còn `held`).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tệp: không có `internal/apply/*` (Quyết định 1): `fulfillment/{apply_public.go,apply_run.go,apply_effects.go,
  apply_attach.go}`; test `fulfillment/{apply_test.go,apply_property_test.go,apply_attach_test.go}`.
- `Events` → hàm gói `ShipmentEvents` (Quyết định 2).
- `queries/fulfillment/shipments.sql`, `events.sql` đã có từ M1: thêm câu vào, không tạo lại. Thêm `ShipmentEventExists`
  (kiểm con trỏ trang).
- VC11 "tin `Cancelled` muộn của V1 → `not_current`": `shipstate.Decide` (Task 98) xét "trùng trạng thái" trước "không
  hiện hành", nên V1 đã `cancelled` nhận thêm `Cancelled` ra `duplicate_state`. Test kiểm điều kế hoạch cần (chỉ thêm lịch
  sử V1, không áp, D1 vẫn gắn V3, tồn không đổi); tin `PickedUp` của V1 đúng `not_current` + `not_current_moved`.
- Thêm `order.ErrStockShort` (Ảnh hưởng) — kế hoạch đòi bọc `inventory.ErrStockShort` mà fulfillment không được nhập
  inventory.
- `ApplyTracking` mặc định `Actor.Type` theo nguồn (Quyết định 10); `rawStatus` cũng qua `ScrubText` (thêm an toàn).

## Việc phát hiện thêm

- Task M3-11 (lệnh tra): `ApplyTracking` trả lỗi của `order` (vd. `ErrStatusConflict` khi đơn ở trạng thái lạ mà vận đơn
  vẫn hiện hành) thì cả lần áp lùi và lần tra sau gặp lại đúng lỗi đó; lệnh tra nên đưa ca này tới `dead` có cảnh báo
  (ghi chú reviewer).
- Chưa có test cho nhánh `order.ErrStockShort` → `fulfillment.ErrStockShort` (cần kiểm kho hạ tồn dưới phần giữ rồi áp
  lấy hàng); nên thêm ở task kiểm kho/M3 kế tiếp (ghi chú reviewer).
- Task 98 đã ghi: tin `older` là tin kết thúc (`delivered`, `returning`) có thể nên mở "Cần xem" — chưa làm ở đây
  (Decide không trả anomaly cho ca này; đổi luật là việc của máy trạng thái).
- `cancel_requested + Delivered/DeliveryFailed` vẫn là `invalid_edge` (ghi chú Task 98) — áp đúng luật hiện có.

## Reviewer

- Lần 1 (bản đã gộp main `fc3b605`, ci-task XANH): **ĐẠT**, không có phát hiện phải sửa. Reviewer kiểm thứ tự khóa
  (đơn → vận đơn), chống áp hai lần (ON CONFLICT bỏ cả quyết định), Seq có lỗ không làm kẹt hàng đợi (theo OrderSeq),
  consumer `cash` M1 không ghi lặp với sự kiện mới, Quyết định 7–9 khớp mã `order` và CHECK của 0300, dữ liệu cá nhân qua
  `ScrubText`, handler 404 ngoài kho; đối chiếu từng dòng bảng bằng chứng. Ghi chú không bắt buộc: chưa test nhánh
  `ErrStockShort`, chú thích `TestApply_AnomaliesOpenOnce` nói "cạnh sai, needs_review" mà thân test chỉ kiểm hai loại
  anomaly, nên thêm `ShipmentEventExists` vào mục Ảnh hưởng, lỗi `order` lặp ở lệnh tra (đã chép vào Việc phát hiện thêm).

Kết luận: ĐẠT
