# Task 78 — M2-21: Hủy đơn, yêu cầu hủy, đơn nháp hết hạn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (Task 78, M2-21). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (order điều phối inventory, promotion).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Mới trong `order`: `Writer.Cancel`, `Writer.OnShipmentCancelConfirmed`, `Writer.OnShipmentCancelRejected`,
    `Writer.ExpireDrafts`, hằng lý do hủy `Cancel*` (`CancelCustomerChangedMind` … `CancelDuplicate`), tên lịch `ExpireDraftsSchedule`. `order.Background` (1 chỗ
    gọi: `cmd/crm/modules.go`) nay trả handler và lịch `order.expire_drafts`; `cmd/crm/main_test.go` đổi kỳ vọng
    (trước: 0 việc nền, 0 lịch).
  - `promotion.ActiveCouponCode` (1 chỗ gọi: `order/create_m2.go` `replay`) được thay bằng `promotion.OrderCouponCode`
    (mã của lượt đang hiệu lực, hoặc của lượt đã trả khi đơn hủy; lượt bỏ khi sửa đơn không tính); xóa hàm, câu SQL cũ.
  - Không đổi chữ ký nào đang có (`applyTransition`, `DetachShipment`, `lockForShipment`, `changeLocked` của Task 76
    giữ nguyên; `Cancel` dùng lại `changeLocked`). Hàm riêng mới `Writer.lockCurrentShipment` (shiphooks.go) dùng ở
    `DetachShipment` và hai móc nối mới. `tests/arch/sellerfilter_test.go`: thêm một ngoại lệ có lý do.
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`,
  `internal/testkit/shiptest`; `promotion` ← `order`, `httpapi`, `cmd/crm`.
- Operation OpenAPI bị chạm, oasdiff: `cancelOrder` (POST `/api/v1/orders/{id}/cancel`, `order.cancel`, có sẵn từ Task
  M2-02, đang trả 501) nay có handler. Không sửa `openapi.yaml` → oasdiff không đổi.
- Chỗ dùng trên màn hình: chưa có màn đơn (Task M2-28); không sửa console.
- Câu sqlc, bảng, pha migration: mới `queries/ord/cancel.sql` (ghi `cancel_reason`, `cancel_note`, gỡ
  `current_shipment_id` khi đơn sang `cancelled`; không gán `status`), `queries/ord/drafts.sql` (đơn nháp quá hạn `FOR
  UPDATE SKIP LOCKED`); câu promotion `ActiveCouponCodeByOrder` thay bằng `OrderCouponCode`. Bảng `ord.orders` (cột đã
  có, quyền UPDATE đã cấp ở 0117/0204), `promotion.coupon_redemptions` (chỉ đọc). Không migration.
- Test sẽ chạy: `go test -race ./internal/modules/order/...` (vùng cao: cả gói), `./internal/modules/promotion/...`,
  `./internal/httpapi/ -run 'OrderAPI'`, `./cmd/crm/ -run TestNewModules`, `./tests/arch/`; trước review chạy ci-task
  một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (idempotency, schedules,
  queue), `order` (applyTransition, lockOrder, lockForShipment, lockedOrder, wired, emitUpdated, DetachShipment),
  `inventory` (`ReleaseHeldByItems`, `ReservationStatesByItems`), `promotion` (`Release`), `schedules.Schedule`,
  `queue.Handler`, `httpapi.refusableWrite`, `handler.audit`, `orderError`.
- Dùng lại / viết mới vì:
  - Đổi trạng thái chỉ qua `applyTransition` (lịch sử, sự kiện `order.cancelled`/`order.cancel_requested`, báo hiệu).
  - Khóa đơn và so vận đơn hiện hành: `lockOrder`, `lockForShipment` của Task 75; nhánh đơn `confirmed` của
    `OnShipmentCancelConfirmed` gọi thẳng `Writer.DetachShipment`.
  - Nhả hàng `inventory.Writer.ReleaseHeldByItems`, trả lượt `promotion.Release` (có sẵn).
  - Handler HTTP: `refusableWrite` (lỗi nghiệp vụ không lưu theo khóa) + `orderError` + `handler.audit`.
  - Viết mới: `cancel.go` (lõi hủy dùng chung cho `Cancel`, `ExpireDrafts`, `OnShipmentCancelConfirmed`), câu SQL ghi
    lý do hủy (chưa câu nào ghi `cancel_reason`), handler việc nền `order.expire_drafts` (chưa module nghiệp vụ nào có
    lịch theo tenant; theo mẫu `schedules.cleanupHandler`).
  - Bản hủy tạm `cancelWithCoupon` của `deadlock_test.go` (Task 74) để nguyên: tệp test chung, Task 76 chạy song song
    (ghi ở Việc phát hiện thêm).

## Soát thiết kế

5.1 Chủ dữ liệu: đơn, lý do hủy thuộc `ord`; lượt mã thuộc `promotion`, tồn thuộc `inventory`. 5.2 Không ghi bảng
module khác: gọi `promotion.Release`, `inventory.Writer.ReleaseHeldByItems`; câu đọc mã cho lần gửi lại nằm trong
promotion. 5.3 Đọc module khác: qua hàm công khai, không đọc bảng. 5.4 Nguyên tử: hủy + nhả hàng + trả lượt trong một
transaction của bên gọi. 5.5 Sau commit: sự kiện `order.cancelled`, `order.cancel_requested` qua `events.Emit` trong tx
(outbox); M3 nghe `order.cancel_requested` để gửi lệnh hủy. 5.6 Gọi ra ngoài: không có (hủy ở hãng là việc M3). 5.7
Tenant: mọi câu trong `WithTenant`/`WriteTx`; việc nền theo tenant chạy trong `WithTenant` của worker. 5.8 Không HTTP
trong module. 5.9 `cancelOrder` có `x-permission: order.cancel` và `IdempotencyKey` (sẵn có). 5.10 Không Valkey. 5.11
Không thư viện mới. 5.12 Theo mẫu `order` (Writer, sentinel, test PostgreSQL thật).

6.1 Đồng thời: khóa đơn `FOR UPDATE` rồi so `version` (`ErrVersionConflict`); `ExpireDrafts` lấy đơn bằng `FOR UPDATE
SKIP LOCKED` (đơn đang được chốt/sửa bị bỏ qua tới kỳ sau). 6.2 Gửi lại: `Idempotency-Key` của route; gửi lại tạo đơn
sau khi đơn đã hủy trả lại đơn cũ (việc để lại của Task 74). 6.3 Thứ tự khóa ADR-46: đơn (nhóm 7) → mã (nhóm 10,
`promotion.Release`) → số dư tồn (nhóm 11, `ReleaseHeldByItems`), giống `Create`. `ExpireDrafts` hủy nhiều đơn trong
một tx giữ đúng thứ tự cho cả lần (`cancelAll`): khóa mọi đơn → trả lượt mã từng đơn (theo mã giảm giá) → nhả giữ hàng
mọi dòng một lần (inventory khóa số dư theo biến thể tăng dần) → đổi trạng thái; test `TestExpireDrafts_ManyOrdersNoDeadlock`. 6.4 Lỗi tạm: `WriteTx`/`db.RetryTx`
chạy lại; hàm gán lại mọi biến kết quả. 6.5 READ COMMITTED + khóa dòng. 6.6 Việc nền: `MaxAttempts 1`, hết hạn sau một
kỳ (lỗi → `dead`, kỳ sau có việc mới). 6.7 không áp dụng: không gọi bên ngoài. 6.8 Chết giữa chừng: transaction lùi.
6.9 Handler 30 s; mỗi kỳ tối đa 200 đơn. 6.10 Tiền: không đổi tiền đơn. 6.11 Quy luật: test rapid (giữ hàng = tổng số
lượng đơn còn mở, `used_count` = số lượt hiệu lực, đơn `cancelled` không còn vận đơn hiện hành). 6.12 Dữ liệu cá nhân:
ghi chú hủy không vào sự kiện, audit (chỉ có hay không). 6.13 Quyền: `order.cancel` ở bảng route; lý do hệ thống
(`draft_expired`, `duplicate`) không nhận từ API (enum hợp đồng).

## Quyết định thay chủ shop

1. **Hàm nhận thêm `sellerID`** (`Cancel`, `OnShipmentCancelConfirmed`, `OnShipmentCancelRejected`): kế hoạch ghi chữ
   ký không có người bán; chọn thêm như mọi hàm đơn của M2 (ADR-29, `docs/m2/doi-chieu-m1.md` mục 3, `Confirm` của
   Task 76, móc nối của Task 75). Phương án khác: tự đọc người bán từ đơn — bỏ lớp chặn người bán ở tầng dưới.
2. **Hủy đơn đang `cancel_requested` bằng `Cancel` → `ErrInvalidTransition`** (máy trạng thái cho phép
   `cancel_requested → cancelled`, nhưng cạnh đó chỉ đi qua `OnShipmentCancelConfirmed`): nếu nhân viên hủy hẳn khi hãng
   chưa xác nhận thì hàng bị nhả trong khi hãng có thể vẫn lấy hàng. Dễ đảo: bỏ một nhánh `case`.
3. **Lý do hủy lưu ngay lúc yêu cầu hủy** (`cancel_reason`, `cancel_note` khi sang `cancel_requested`), giữ khi hãng
   xác nhận, xóa khi hãng từ chối (đơn về `confirmed`). Phương án khác: chỉ ghi lúc `cancelled` — mất lý do nhân viên
   chọn vì hãng trả lời sau.
4. **Hàng đã `in_transit` → `ErrInvalidTransition` cho cả hai nhánh của `OnShipmentCancelConfirmed`** (đơn
   `cancel_requested` theo kế hoạch, và đơn `confirmed` chỉ hủy vận đơn): hàng đã rời kho thì gỡ vận đơn cũng để M3
   xử lý đường hoàn. Phương án khác: nhánh `confirmed` gỡ vận đơn như `DetachShipment` dù hàng đang đi.
5. **WB12 khi gắn vận đơn thắng:** `Cancel` mang version cũ nhận `ErrVersionConflict` (đúng luật version); test tải lại
   rồi hủy lần nữa → `cancel_requested`. Phương án khác: bỏ kiểm version cho hủy — trái luật sửa có version.
6. **`ExpireDrafts` lỗi ở một đơn → lùi cả lần** (việc nền `MaxAttempts 1`, kỳ sau 60 giây có việc mới; lỗi hiện ở
   việc `dead`). Phương án khác: savepoint từng đơn và bỏ qua đơn lỗi — thêm đường lỗi im lặng; chỉ cần khi gặp đơn
   nháp hỏng bất biến thật.
7. **`ExpireDrafts` quét mọi người bán của tenant** (việc hệ thống); từng đơn vẫn khóa lại theo `seller_id` của dòng.
8. **Lý do nhả hàng, trả lượt:** `order_cancelled`; đơn nháp hết hạn dùng `draft_expired` (báo cáo phân biệt được).
9. **Gửi lại tạo đơn sau khi đơn đã hủy** (việc của Task 74): dấu vân tay dùng `promotion.OrderCouponCode` (lượt đang
   hiệu lực, hoặc lượt đã trả gần nhất trừ lượt bỏ khi sửa đơn) → trả lại đơn đã hủy (spec 04 mục 5.5: "lần hai trả
   về đơn đã tạo"), không dùng mã lần hai. Phương án khác: so mã theo `coupon_discount` của đơn — không phân biệt được
   hai mã.
10. **Thứ tự trả lượt mã khi `ExpireDrafts` hủy nhiều đơn: theo mã giảm giá (chữ)**, cùng thứ tự ở mọi lần chạy; các
   đường khác chỉ khóa một mã mỗi transaction nên không tạo vòng chờ. Phương án khác: theo `coupon_id` — cần thêm hàm
   công khai ở promotion chỉ để sắp.

## Bằng chứng

Test viết trước mã: lần biên dịch đầu của `cancel_test.go`, `drafts_test.go` báo `Writer.Cancel`, `OnShipmentCancelConfirmed`,
`ExpireDrafts`, `order.CancelOutOfStock`, `order.ExpireDraftsSchedule` undefined (FAIL), rồi mới viết `cancel.go`, `drafts.go`.

Lệnh chung (A): `go test -race -count=1 -timeout 580s -v -run 'TestCancel_|TestExpireDrafts|TestConfirm|TestCreateCoupon_Replay' ./internal/modules/order/`
→ mọi test dưới `--- PASS`, `ok … order 25.118s`. (B): `go test -race -count=1 -run 'TestOrderAPI_Cancel' -v ./internal/httpapi/` → `ok … httpapi 2.975s`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TD01: hủy confirmed V2 ×1 (tồn 1) → Available 1 ngay, đơn khác V2 ×1 tạo được; lý do, ghi chú, giờ hủy, lịch sử, `order.cancelled` seq = version; ghi chú không vào sự kiện | `TestCancel_TD01ReleasesStock` | A | PASS |
| Hủy draft, new, đơn chờ duyệt giảm giá → cancelled, nhả hàng, bỏ hạn nháp | `TestCancel_DraftAndNew` | A | PASS |
| KM03: SALE10 used 1 → hủy → 0, lượt `released` (`order_cancelled`); mã tắt `release_on_cancel` → giữ 1, lượt active | `TestCancel_KM03ReleasesCoupon` | A | PASS |
| TD03: có vận đơn S1 → `cancel_requested`, nhãn `cancel_requested`, giữ hàng, lượt không đổi, `order.cancel_requested`; hãng xác nhận → cancelled, gỡ vận đơn, nhả hàng, trả lượt, lịch sử của hãng; báo lần hai → false | `TestCancel_TD03RequestThenCarrierConfirms` | A | PASS |
| TD03 khác: hãng từ chối → confirmed, vẫn S1, giữ hàng, bỏ lý do; lần hai → false | `TestCancel_CarrierRejects` | A | PASS |
| Vận đơn cũ S0 khi hiện hành S1 → applied false (xác nhận và từ chối) | `TestCancel_StaleShipment` | A | PASS |
| Hàng đã `in_transit`, hãng báo hủy → `ErrInvalidTransition`, tồn và đơn không đổi | `TestCancel_InTransitCannotBeCancelled` | A | PASS |
| Đơn confirmed chỉ hủy vận đơn → như DetachShipment (`order.updated`) | `TestCancel_CarrierConfirmsOnConfirmedOrderDetaches` | A | PASS |
| Hủy delivered, cancelled, cancel_requested → `ErrInvalidTransition`; version cũ → `ErrVersionConflict`; lý do sai, ghi chú 501 ký tự → `ErrInvalidInput`; người bán khác, không có → `ErrNotFound` | `TestCancel_Refusals` | A | PASS |
| Hủy `delivered` qua API → 422 `invalid_transition`; version cũ 409; lý do hệ thống 400; không quyền 403; không có 404; audit không có nội dung ghi chú | `TestOrderAPI_Cancel` | B | PASS |
| 50 lần hủy song song cùng khóa → 1 dòng lịch sử, `used_count` giảm đúng 1 | `TestOrderAPI_CancelSameKey50` (HTTP, cùng `Idempotency-Key`); `TestCancel_Parallel50SameVersion` (module, cùng version) | B; A | PASS; PASS |
| Đơn nháp T, hạn 24 giờ: `ExpireDrafts(T+23h)` 0; `ExpireDrafts(T+24h+1m)` → cancelled `draft_expired`, người làm system, hàng và lượt trả; đơn đã chốt không bị đụng | `TestExpireDrafts` | A | PASS |
| Giới hạn mỗi lần, limit 0 → `ErrInvalidInput` | `TestExpireDrafts_Limit` | A | PASS |
| Lịch `order.expire_drafts` 60 giây theo tenant, handler cùng Kind chạy được | `TestExpireDraftsSchedule`; `TestNewModulesRegistersCash` | A; `go test -race -count=1 -run TestNewModules ./cmd/crm/` | PASS; `ok … cmd/crm 1.548s` |
| Đua hết hạn và chốt (`Writer.Confirm` của Task 76), 100 lần → đúng một trạng thái, tồn và lượt khớp | `TestExpireDrafts_RaceWithConfirm` | A | PASS, log `chốt thắng 77/100, hết hạn thắng 23/100` |
| WB12: `Cancel` và `AttachShipmentBy` đồng thời 100 lần → không đơn cancelled còn vận đơn | `TestCancel_RaceWithAttach` | `go test -race -count=1 -v -run TestCancel_RaceWithAttach ./internal/modules/order/` | PASS, log `hủy trước 45/100, gắn vận đơn trước 55/100` |
| Không deadlock khi `ExpireDrafts` hủy nhiều đơn (đơn 1 giữ biến thể mã lớn, đơn 2 mã nhỏ) song song với tạo đơn cả hai biến thể, 50 lần | `TestExpireDrafts_ManyOrdersNoDeadlock` | `go test -race -count=1 -run TestExpireDrafts_ManyOrdersNoDeadlock ./internal/modules/order/` | Bản trước sửa (hủy từng đơn trọn vẹn): FAIL `pg_stat_database.deadlocks 0 → 45`; bản sửa (`cancelAll`): `ok … order 6.863s` |
| Quy luật (rapid, 200 lượt): held/in_transit = tổng đơn còn mở, `used_count` = lượt active, đơn cancelled không vận đơn, không giữ hàng | `TestCancel_Invariants` | A | `[rapid] OK, passed 200 tests` |
| Gửi lại tạo đơn sau khi hủy → trả đơn cũ (việc của Task 74); khác mã → `ErrDuplicateOrder`; test cũ vẫn đạt | `TestCancel_ReplayAfterCancel`, `TestCreateCoupon_Replay` | A | PASS |
| Luật kiến trúc: status chỉ ở transition.sql; seller_id ở câu ord (ngoại lệ có lý do cho `LockExpiredDrafts`) | `tests/arch` | `go test -count=1 -run 'TestSellerFilter\|TestLintCleanOnRealCode\|TestOrderStatusSQL' ./tests/arch/` | `ok … tests/arch 4.400s` |
| ci-task trên bản đã gộp main (Task 76), đã stage | gói đổi, phụ thuộc, vùng cao; arch; e2e | ci-task (lần 2; lần 1 đỏ: jscpd 9 clone mới, `TestSellerFilterProductCode` thiếu seller_id ở `LockExpiredDrafts`, gen-check đỏ giả do tệp chưa stage — đã sửa) | `CI-TASK XANH`; `ok …/modules/order 210.312s`, `ok …/promotion 117.553s`, `ok …/httpapi 56.172s`, `ok …/tests/arch 17.942s`, `ok …/tests/e2e 118.971s`, `ok …/cmd/crm 1.588s` |
| ci-task lần 3 sau sửa của reviewer (đã stage) | gói đổi, phụ thuộc, vùng cao; arch; e2e | ci-task | `CI-TASK XANH`; `ok …/modules/order 307.938s`, `ok …/promotion 196.228s`, `ok …/httpapi 142.646s`, `ok …/tests/arch 32.588s`, `ok …/tests/e2e 108.725s` |
| Cổng tĩnh | lint, jscpd | `golangci-lint run ./internal/modules/order/... ./internal/httpapi/...`; `jscpd … --fail-on-new-clones=0 .` | `0 issues.`; không còn clone mới |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký có thêm `sellerID` (Quyết định 1).
- Sửa thêm ngoài danh sách tệp của kế hoạch: `internal/modules/promotion/redeem.go`, `queries/promotion/redeem.sql`
  (`ActiveCouponCode` → `OrderCouponCode`, việc để lại của Task 74), `internal/modules/order/create_m2.go` (một dòng
  gọi), `internal/modules/order/drafts.go` (handler, lịch), `cmd/crm/main_test.go` (kỳ vọng việc nền, lịch),
  `docs/views.md` (nguồn của `order.cancelled`, `order.cancel_requested`), test HTTP
  `internal/httpapi/handlers_order_cancel_test.go`.
- Thêm test rapid `TestCancel_Invariants` (mục 6.11, vùng rủi ro cao).
- `tests/arch/sellerfilter_test.go`: thêm một dòng ngoại lệ có lý do cho `queries/ord/drafts.sql#LockExpiredDrafts` (cơ
  chế ngoại lệ có sẵn, như `VariantExists`): việc hệ thống quét mọi người bán trong tenant; từng đơn khóa lại theo
  `seller_id` của dòng. Không đổi cách kiểm của test.
- `shiphooks.go`: gom "kiểm wired + lockForShipment" thành `Writer.lockCurrentShipment`, `DetachShipment` dùng lại (jscpd
  báo trùng với hai móc nối mới; hành vi không đổi, `TestWriterDetachShipment` vẫn đạt).

## Việc phát hiện thêm

- `deadlock_test.go` (Task 74) còn bản hủy tạm `cancelWithCoupon` bằng SQL trực tiếp; nay đã có `Writer.Cancel`. Không
  sửa ở task này (tệp test chung, Task 76/77 chạy song song); task sau có thể đổi sang `Writer.Cancel`.
- Mô tả của `cancelOrder` (và các operation M2 đã làm khác như `listCustomers`) trong `openapi.yaml` vẫn ghi "Chưa làm
  ở Task M2-02 (trả 501)"; không sửa ở đây để không đổi mã sinh và đụng tệp hợp đồng của task song song.
- (Cho Task M2-20, sửa đơn) Đường đổi mã giảm giá (`Detach` mã cũ rồi `Redeem` mã mới trong một transaction) khóa hai
  dòng mã; `ExpireDrafts` trả lượt nhiều đơn theo thứ tự mã (chữ) — nên thống nhất một thứ tự khóa hai mã.
- Thân `Order` của API chưa có `cancelReason`, `cancelNote` (đọc chi tiết đơn là Task M2-24).

## Reviewer

Lượt 1 (agent `reviewer`, 21 tệp đã stage): CẦN SỬA. Phát hiện phải sửa: `ExpireDrafts` hủy từng đơn trọn vẹn
nên sang đơn thứ hai đã giữ số dư tồn (nhóm 11) mà còn xin khóa mã (nhóm 10) và số dư biến thể khác không theo thứ tự
tăng dần → deadlock với tạo đơn (ADR-46). Đã sửa: tách `prepareCancel` / `cancelAll` / `applyCancelled`; `ExpireDrafts`
khóa mọi đơn, trả lượt theo mã giảm giá, nhả tồn một lần, rồi đổi trạng thái; thêm
`TestExpireDrafts_ManyOrdersNoDeadlock` (bản cũ: 45 deadlock; bản sửa: 0). Ghi chú không bắt buộc: tên hằng ở mục Ảnh
hưởng (đã sửa); audit "trước" đọc trước khi khóa (lần ghi lệch version vẫn bị từ chối; giữ nguyên).

Kết luận: ĐẠT (lượt 2, agent `reviewer`, 21 tệp đã stage). Phát hiện phải sửa: không có. Reviewer xác nhận `ExpireDrafts`
khóa đúng thứ tự ADR-46 cho cả lần (đơn → lượt mã → số dư một lần → đổi trạng thái), đường một đơn (`Cancel`,
`OnShipmentCancelConfirmed`) không đổi hành vi, `TestExpireDrafts_ManyOrdersNoDeadlock` phân biệt được lỗi thật (bộ đếm
deadlock). Ghi chú không bắt buộc: test đọc bộ đếm deadlock của cả database thử (mỗi test có database riêng từ
testkit); khi có đường sửa đơn đổi mã (Task M2-20) cần thống nhất thứ tự khóa hai mã với cách sắp theo chữ (ghi ở Việc
phát hiện thêm).
