# Task 202 — Nhả suất, hoàn tất đơn nháp, đối chiếu đêm

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 202 / M5-19). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `order.Writer.Cancel` (chữ ký không đổi; 4 chỗ gọi ngoài test: `httpapi/handlers_order_cancel.go`, `order/returns.go`,
    `order/webview.go`, `order/duplicates.go`) — mọi đường hủy, cả `ExpireDrafts`, `OnShipmentCancelConfirmed`,
    `DetachShipment` (qua `finishCancel`/`cancelAll`), đi qua nhánh flash mới trong `cancelAll`.
  - `state.Check` (gói nội bộ `order/internal/state`, 2 chỗ gọi: `cancel.go` `prepareCancel`, `transition.go`
    `applyTransitionWith`): thêm trường `Conditions.WebChannel` và cạnh `draft → new` chỉ khi `WebChannel`. Test
    `TestCheck_AllPairs` (12 → 13 cặp) và bảng `TestCheck_Conditions` của M2 Task 08 cập nhật.
  - `order.ExpireDrafts`: lịch 60 s/200 đơn → 15 s/500 đơn; `TestExpireDraftsSchedule` đòi `Every == time.Minute` → 15 s.
  - `inventory.Reconcile`: công thức `held_vs_reservations` đổi; thêm `CheckFlashUnits`, trường `Mismatch.FlashItemID`;
    chỗ gọi: `reconcileHandler`, `promotion/flash_lifecycle_test.go` (checkBooks), `inventory/reconcile_test.go`.
  - Mới: `promotion.FlashSaleOpenForRelease`, `inventory.ReleaseFlashUnits`, `inventory.MarkFlashSold`,
    `inventory.FlashMismatchAlertKind`, `inventory.NewFlashMismatchCommand`, `order.FlashItemOf`,
    `order.CompleteFlashInput`, `order.Writer.CompleteFlashDraft`, `order.ErrHoldExpired`, `customer.HasPhone`.
  - `order.Writer.Edit` (chữ ký không đổi): đơn có dòng flash sửa dòng → `ErrInvalidTransition` (`checkFlashLinesKept`);
    chỗ gọi: `httpapi` sửa đơn, `CompleteFlashDraft` (chỉ phí ship, địa chỉ).
  - `store.LockOrderRow` (mã sinh) thêm cột `channel`; `lockedOrder` đặt `Order.Channel` (15 chỗ gọi `lockOrder`, chỉ đọc
    thêm một trường).
- Gói phụ thuộc ngược (go list): `order` ← httpapi, httpapi/web, conversation(+internal/live), cash(+internal/refunds),
  fulfillment, wiring/webcache, cmd/crm, testkit/codtest, testkit/shiptest, tools/loadkit. `inventory` ← promotion, order,
  httpapi, wiring/webcache, cmd/crm, invtest, tools/loadkit. `promotion`, `customer`: thêm hàm, không đổi ký hiệu cũ.
- Operation OpenAPI bị chạm, oasdiff: không (điểm cuối hoàn tất đơn là Task 204).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không migration. Câu mới: `queries/inventory/flash_release.sql`
  (`LockHeldReservationsOfItem`, `ReleaseReservationsKeepBalance`, `ReopenFlashUnits`, `ReleaseHeldFlashUnits`,
  `MarkFlashUnitsSold`), `queries/inventory/reconcile.sql` (`ReconcileBatch` thêm CTE đơn vị trống của đợt
  scheduled/open; `FlashAvailableInClosedSales`, `FlashUnitsVsOrderLines` — đọc `promotion.flash_sales_v`,
  `promotion.flash_sale_items_v`, `ord.order_items_v`), `queries/promotion/flash_claims.sql` (`FlashSaleStatusForShare`),
  `queries/ord/flash.sql` (`FlashLinesOfOrder`, `FlashItemOfOrder`), `queries/ord/orders.sql` (`LockOrder` + `channel`),
  dùng lại `GetIdentityOwner` (`queries/customer/identities.sql`), `GetRootOf` (`queries/customer/customers.sql`). Bảng chạm: `inventory.flash_sale_units`,
  `inventory.stock_reservations`, `inventory.stock_balances` (chỉ đường đợt đã đóng, qua `ReleaseHeldByItems` có sẵn),
  `promotion.flash_sale_claims`, `ord.orders`.
- Cấu hình nối: `cmd/crm/main.go` đăng ký `inventory.NewFlashMismatchCommand`; `wiring/webcache/consumer.go` đổi
  `order.cancelled` của đơn flash thành báo hiệu `flash_item` (chỗ đã chừa sẵn "trước M5-19 bỏ qua").
- Test kiến trúc (sau lần chạy `tests/arch` đầu): `tests/arch/sqlusage_test.go` `viewReadExceptions` thêm `inventory`
  (ba view), `tests/arch/sellerfilter_test.go` `sellerExempt` thêm `queries/ord/flash.sql#FlashItemOfOrder`;
  `docs/views.md` cột "Mốc đọc" của ba view (xem Quyết định thay chủ shop).
- Test sẽ chạy: `-run` các test mới của order (`TestFlashRelease_*`, `TestFlashComplete_*`, `TestFlashLifecycle_*`),
  `TestExpireDrafts*`, `TestCancel*` liên quan; `internal/modules/order/internal/state`; inventory `TestReconcile*`;
  customer `TestHasPhone`; promotion `TestFlash*`; webcache; `tests/arch`. Cả gói ở ci-task (order, inventory, customer,
  promotion là vùng rủi ro cao: chạy cả bộ, `-race`).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (db.Savepoint, db.NullUUID,
  commands.CommandKind), `inventory.ReleaseHeldByItems`, `inventory.LockFlashStock`, `inventory.ReservationStatesByItems`,
  `order.Writer.Edit`, `CheckShipAddress`, `shipProblem`/`shipError`/`shipChoiceJSON`, `SetOrderWebFields`, `AddNote`,
  `applyTransition`, `promotion.Unclaim`, `customer` `identityOwner`/`GetRootOf`, `inventory.notify`.
- Dùng lại / viết mới vì:
  - Đợt đã đóng: nhả dòng giữ hàng bằng `ReleaseHeldByItems` (khóa số dư theo biến thể tăng dần, sổ `order`/`release`,
    held −) — đúng nghĩa "đường cũ của M2"; chỉ thêm câu đổi đơn vị `held → released`.
  - Đợt còn mở: viết mới `ReleaseFlashUnits(reopen=true)` vì `transition` của inventory luôn đổi số dư (ở đây số dư
    không đổi, 05 mục 7.4).
  - Hoàn tất đơn: dùng lại `Writer.Edit` (địa chỉ, phí ship, tính tiền bằng `pricing`, `order.updated`) +
    `CheckShipAddress` (đủ tỉnh, xã, `customer.ValidateUnits`) + `applyTransition` (`draft → new`) + `SetOrderWebFields`
    (hãng khách chọn, như đơn web) + `AddNote`. Không viết đường tính tiền thứ hai.
  - Cảnh báo `reconcile:flash`: dùng lại `notify`, `commands.CommandKind` như `stock_mismatch_alert`; kind riêng vì
    khóa và nội dung khác (chỉ mã món).
  - `HasPhone`: dùng lại `GetIdentityOwner` (index `customer_identities_value_key`) + `GetRootOf`; không câu SQL mới.

## Soát thiết kế

5.1 Chủ dữ liệu: claims, đợt thuộc `promotion`; đơn vị, giữ hàng, số dư thuộc `inventory`; đơn thuộc `order`; danh tính
thuộc `customer`. Mỗi câu SQL nằm trong `queries/<module>` của chủ bảng. 5.2 Không ghi bảng module khác: `order` gọi
`promotion.FlashSaleOpenForRelease/Unclaim`, `inventory.ReleaseFlashUnits/MarkFlashSold/ReleaseHeldByItems`,
`customer.HasPhone`. 5.3 Đọc module khác: đối chiếu đêm của `inventory` đọc view `promotion.flash_sales_v`,
`flash_sale_items_v`, `ord.order_items_v` (docs/views.md, thêm `inventory` vào cột "ai dùng"). 5.4 Nguyên tử: hủy và
hoàn tất trong tx của bên gọi; `ExpireDrafts` một tx cho cả lô. 5.5 Sau commit: `order.cancelled`, `order.updated`,
`order.new` qua outbox; consumer `webcache` báo hiệu `flash_item`. 5.6 Không gọi ra ngoài (cảnh báo là lệnh `commands`).
5.7 Mọi câu trong tx tenant (RLS). 5.8 Module không nhập HTTP. 5.9 Không đường HTTP mới (Task 204). 5.10 Không Valkey.
5.11 Không thư viện mới. 5.12 Theo mẫu `cancel.go`/`create_m2.go` (điều phối nhiều module trong tx bên gọi, lỗi bọc `%w`).

6.1 Đồng thời: đổi đơn vị có điều kiện trạng thái (`status IN ('held','sold') AND order_item_id = $1`); số đơn vị
khớp `quantity` của dòng được giữ bằng cách chặn sửa dòng của đơn flash (`checkFlashLinesKept` trong `Edit`) và kiểm
ở đối chiếu đêm (`flash_units`), không kiểm lại lúc nhả; `Unclaim` có điều kiện `quantity >= q`. 6.2 Gửi lại: `CompleteFlashDraft` gọi lại khi đơn đã `new`
→ trả lại kết quả (Replayed), không ghi; đơn đã hủy vì hết hạn → `ErrHoldExpired` (kết quả cuối). 6.3 Thứ tự khóa
(ADR-46): đơn (7) → `coupons` (10, `Release`) → `flash_sales FOR SHARE` theo id tăng dần (10) → `flash_sale_claims`
(10) → `stock_balances` (11, chỉ đơn của đợt đã đóng + đơn thường, một lần theo biến thể tăng dần) →
`stock_reservations` → `flash_sale_units` (11). Cả lô `ExpireDrafts` giữ thứ tự này trên toàn transaction (không hủy
từng đơn một trong cùng tx vì sẽ khóa số dư đơn trước rồi mới khóa mã/đợt của đơn sau). Đóng đợt: `flash_sales FOR
UPDATE` → số dư → đơn vị: chờ `FOR SHARE` của đường hủy hoặc ngược lại, không vòng. Lượt mua: claims → đơn vị SKIP LOCKED,
không giữ `flash_sales`/số dư. 6.4 Lỗi tạm: RetryTx của bên gọi chạy lại 40P01/55P03. 6.5 READ COMMITTED: `FOR SHARE`
trên đợt chặn đóng đợt tới hết tx hủy, nên quyết định "mở/đã đóng" không đổi trong tx. 6.6 Việc nền: `order.expire_drafts`
15 s, `MaxAttempts 1`, lỗi thì kỳ sau; đơn không hủy được ở bước kiểm (hai dòng flash, hàng đã rời kho) bị bỏ qua (không
đếm riêng; kết quả chỉ có số đơn đã hủy); lô lỗi nghiệp vụ ở pha hủy → lùi savepoint, hủy từng đơn trong savepoint
riêng, bỏ qua đơn lỗi (`expireOneByOne`); lỗi tạm, ctx hủy, savepoint hỏng → trả lỗi, kỳ sau chạy lại. 6.7 Không gọi bên ngoài. 6.8 Tiến trình chết: tx lùi. 6.9 Timeout: theo tx của bên gọi. 6.10
Tiền: tổng tính lại bằng `pricing` qua `Edit`, trigger `check_order_totals`. 6.11 Quy luật: rapid 100 lượt chuỗi thao tác
mua, hoàn tất, hết hạn, hủy, thêm suất, đóng đợt với bốn bất biến. 6.12 Không log SĐT; cảnh báo chỉ mang mã món. 6.13
Quyền: điểm cuối Task 204; `HasPhone` sai → `ErrNotFound` (không lộ đơn).

## Bằng chứng

Lệnh chung (máy Mac, PostgreSQL crm-shared 5600, sau khi gộp main 9634b595): `go test -race -count=1 -v -run '<tên>' <gói>`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| [FS06] hết hạn khi đợt mở: `cancelled` `draft_expired`, đơn vị `available`, claims 0, `held` không đổi, đối chiếu không lệch | `TestFlashRelease_ExpireWhileOpen` | `-run '^(TestFlashRelease_.*)$' ./internal/modules/order/` | `--- PASS: TestFlashRelease_ExpireWhileOpen (1.76s)` |
| [FS17] đợt đóng trước hạn: đơn vị `released`, `held` −1, có thể bán +1, đối chiếu không lệch | `TestFlashRelease_ExpireAfterClose` | như trên | `--- PASS: TestFlashRelease_ExpireAfterClose (0.31s)` |
| [FS17] đợt đóng khi đang giữ, hoàn tất trong hạn → `new`, đơn vị `sold`, `held` giữ nguyên | `TestFlashComplete_AfterClose` | `-run '^(TestFlashComplete_.*)$' ./internal/modules/order/` | `--- PASS: TestFlashComplete_AfterClose (0.38s)` |
| [FS15] SĐT khác → `ErrNotFound`; đúng SĐT → `new`; gọi lại → cùng kết quả (Replayed) | `TestFlashComplete_Phone` | như trên | `--- PASS: TestFlashComplete_Phone (0.38s)` |
| [FS15] hoàn tất sau hạn → `ErrHoldExpired`, đơn `cancelled`, đơn vị `available` | `TestFlashComplete_Expired` | như trên | `--- PASS: TestFlashComplete_Expired (0.36s)` |
| Hoàn tất: đầu vào sai, thiếu xã, phí lệch → lỗi tương ứng, đơn không đổi | `TestFlashComplete_Rejects` | như trên | `--- PASS: TestFlashComplete_Rejects (0.34s)` |
| Hoàn tất đơn không phải nháp flash web (dòng thường, kênh manual) → `ErrNotFound`, không đổi | `TestFlashComplete_NotFlashOrder` | `-run '^(TestFlashComplete_NotFlashOrder\|TestFlashRelease_EditLinesBlocked\|TestFlashRelease_ExpireSkipsBrokenOrder\|…)$'` | `ok … order 78.853s` (sau review lần 1) |
| Sửa dòng đơn flash → `ErrInvalidTransition`, sửa phí ship vẫn được | `TestFlashRelease_EditLinesBlocked` | như trên | `ok … order 78.853s` |
| Lô hết hạn có đơn lệch claims → đơn tốt vẫn hủy, đơn lệch bỏ qua | `TestFlashRelease_ExpireSkipsBrokenOrder` | như trên | `ok … order 78.853s` |
| Hủy đơn `sold` khi đợt mở → đơn vị `available`, claims −1 | `TestFlashRelease_CancelWhileOpen` | `-run '^(TestFlashRelease_.*)$'` | `--- PASS: TestFlashRelease_CancelWhileOpen (1.80s)` |
| Đơn hai dòng flash → hủy lỗi, không đổi gì; hết hạn bỏ qua đơn đó | `TestFlashRelease_TwoFlashLines` | như trên | `--- PASS: TestFlashRelease_TwoFlashLines (2.06s)` |
| Tranh 50 lần: đóng đợt ∥ hết hạn → không deadlock, bất biến | `TestFlashRelease_CloseRacesExpire` | như trên | `--- PASS: TestFlashRelease_CloseRacesExpire (4.59s)` |
| Tranh 50 lần: mua ∥ đóng đợt | `TestFlashRelease_BuyRacesClose` | như trên | `--- PASS: TestFlashRelease_BuyRacesClose (5.43s)` |
| Rapid (200 lượt, TestMain của gói): mua, hoàn tất, hết hạn, hủy, thêm suất, đóng đợt → bốn bất biến sau mỗi bước, đối chiếu cuối chuỗi | `TestFlashLifecycle_Invariants` | `-run '^TestFlashLifecycle_Invariants$'` | `--- PASS: TestFlashLifecycle_Invariants (19.01s)` |
| `FlashItemOf`: đơn flash → mã món; đơn thường → ok=false | `TestFlashItemOf` | `-run '^TestFlashItemOf$'` | `--- PASS: TestFlashItemOf (1.72s)` |
| Lịch `order.expire_drafts` 15 s / 500 đơn; hết hạn, giới hạn, tranh xác nhận, nhiều đơn không deadlock | `TestExpireDraftsSchedule`, `TestExpireDrafts`, `TestExpireDrafts_Limit`, `TestExpireDrafts_RaceWithConfirm`, `TestExpireDrafts_ManyOrdersNoDeadlock` | `-run '^(TestExpireDrafts.*)$'` | 5 × `--- PASS`; `ok elimaz.com.vn/crm/internal/modules/order 23.062s` |
| `state.Check`: cạnh `draft → new` chỉ khi `WebChannel` (13 cặp hợp lệ) | `TestCheck_AllPairs`, `TestCheck_Conditions` | `go test -count=1 -v ./internal/modules/order/internal/state/` | `--- PASS: TestCheck_AllPairs`, `--- PASS: TestCheck_Conditions`, `ok … 0.410s` |
| Đối chiếu: `held_vs_reservations` cộng đơn vị `available` đợt mở; `flash_units` báo đúng mã món (sửa tay đơn vị bằng owner); cảnh báo `reconcile:flash` chỉ mã món | `TestReconcile_FlashUnits`, `TestReconcile_FlashAlert`, `TestReconcile_EachCheck` | `-run '^(TestReconcile.*)$' ./internal/modules/inventory/` | 6 × `--- PASS`; `ok … inventory 11.307s` |
| `customer.HasPhone` theo cả nhóm gộp | `TestHasPhone` | `-run '^TestHasPhone$' ./internal/modules/customer/` | `--- PASS: TestHasPhone (0.35s)` |
| Consumer webcache: `order.cancelled` đơn flash → báo hiệu `flash_item`; đơn thường → không | `TestConsumer_OrderCancelledFlash` | `-run '^TestConsumer_OrderCancelledFlash$' ./internal/wiring/webcache/` | `ok … webcache 2.160s` |
| Lượt mua của Task 201 không gãy | `TestFlash*` promotion | `-run '^TestFlash' ./internal/modules/promotion/` | `ok … promotion 14.921s` |
| Kiến trúc (luật view, ADR-29, lint) | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 71.598s` |
| Mã lỗi `hold_expired` có câu tiếng Việt | `errors.test.ts` | `cd console && npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| Cổng tĩnh + test gói đổi và gói phụ thuộc (`-race` vùng rủi ro cao) | ci-task | `scripts/agent/ci-task.sh` (từ xa, bể github) | `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 9634b595…)`, run 45964777956: static success, test (1)–(6) success; sau sửa review lần 1: run 45974987329 `CI-TASK XANH`, static + test (1)–(6) success; `go test -count=1 -v -timeout 20m ./tests/arch/` → `ok … tests/arch 337.124s` (máy tải cao; hai lần chạy mặc định 10 phút bị cắt vì quá giờ, không có test đỏ) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `inventory.ReleaseFlashUnits(ctx, tx, orderItemID, reason, reopen)`: hàm tự do, thêm `reason` (sổ ghi lý do nhả:
  `cancelled`/`draft_expired`). Đường đợt đã đóng nhả dòng giữ hàng bằng `ReleaseHeldByItems` của cả lô (một lần khóa
  số dư theo biến thể tăng dần), hàm mới chỉ đổi đơn vị `held → released`.
- `ExpireDrafts`: cả lô hủy chung một lần trong savepoint (như M2) thay vì "mỗi đơn một lần Cancel": hủy từng đơn
  trong cùng tx sẽ khóa số dư của đơn trước rồi mới khóa đợt của đơn sau (trái ADR-46). Đơn hỏng ở bước kiểm (hai dòng
  flash, hàng đã rời kho) bị bỏ qua; lô lỗi nghiệp vụ ở pha hủy → lùi savepoint rồi hủy từng đơn trong savepoint riêng,
  bỏ qua đơn lỗi (giữ ý "lỗi một đơn không chặn đơn khác"; ở đường dự phòng thứ tự khóa giữa các đơn không còn, deadlock
  hiếm → lỗi tạm, kỳ sau).
- Chặn sửa dòng của đơn flash trong `Writer.Edit` (sau review lần 1: sửa số lượng làm lệch claims/đơn vị và chặn đường
  nhả suất).
- `CompleteFlashInput.ShipAddress` dùng kiểu `ShipAddress`, hãng là `Shipping *WebShipChoice` của đường đơn web có sẵn; `CompleteFlashDraft` là phương thức của `Writer` và nhận thêm `Actor` (như mọi đường ghi của order).
- Thêm mã lỗi `hold_expired` (422) vào `errors_web.go`, `x-error-codes`, console (dùng ở Task 204).
- Nối consumer `webcache`: `order.cancelled` của đơn flash → báo hiệu `flash_item` (chỗ M5-02 chừa sẵn).

## Quyết định thay chủ shop

- Đối chiếu đêm của `inventory` đọc `promotion.flash_sales_v`, `promotion.flash_sale_items_v`, `ord.order_items_v`
  (kế hoạch bắt buộc), nhưng bảng "Được gọi" (04 mục 3) không cho inventory gọi promotion/order. Chọn: thêm inventory
  vào `viewReadExceptions` (tests/arch, cơ chế "chỉ đọc view, không nhập Go" đã có cho customer, fulfillment,
  reporting) và ghi cột "Mốc đọc" trong `docs/views.md`. Phương án khác: nhập Go gói promotion/order từ inventory (vòng
  nhập, không làm được), hoặc chuyển đối chiếu flash sang module promotion (trái kế hoạch). Đảo ngược: bỏ một dòng.
- `order.FlashItemOf(ctx, tx, orderID)` giữ đúng chữ ký kế hoạch (không `sellerID`): thêm miễn ADR-29 cho câu
  `FlashItemOfOrder` trong `sellerExempt` (việc hệ thống của consumer, chỉ trả mã món). Phương án khác: thêm `sellerID`
  (payload `order.cancelled` có sẵn) — đổi chữ ký kế hoạch; dễ làm sau nếu chủ shop muốn.

## Việc phát hiện thêm

- Task 204 nối `CompleteFlashDraft` vào `WriteTx` phải commit cả khi trả `ErrHoldExpired` (đơn đã hủy trong tx); nếu
  lùi thì lịch 15 s vẫn hủy, chỉ chậm.
- Màn sửa đơn (console) nên ẩn sửa dòng cho đơn flash (máy chủ đã chặn, trả 422).
- (Reviewer, không bắt buộc) `ExpireDrafts`: nếu lô lỗi nghiệp vụ ở pha `applyCancelled` (sau khi vài đơn đã đổi
  `Status`/`Version` trong bộ nhớ), `expireOneByOne` dùng bản bộ nhớ đã đổi và bỏ qua nhầm các đơn đó tới kỳ sau. Hiện
  chưa có đường lỗi nào ở pha đó; cách sửa rẻ: chép `Order` của từng plan trước lần chạy cả lô. Đơn bị bỏ qua chưa
  được đếm vào kết quả `{"expired": n}`.

## Reviewer

Kết luận: ĐẠT (lần 2, 27/11).

- Lần 1: CẦN SỬA — (1) dòng bằng chứng `TestFlashComplete_Rejects` nói ca không có trong test → thêm
  `TestFlashComplete_NotFlashOrder`, viết lại dòng; (2) lỗi nghiệp vụ của một đơn chặn cả lô hết hạn → savepoint cả lô,
  `expireOneByOne` bỏ qua đơn lỗi, chặn sửa dòng đơn flash trong `Edit`, thêm `TestFlashRelease_ExpireSkipsBrokenOrder`,
  `TestFlashRelease_EditLinesBlocked`; (3) tên câu SQL, mục 6.1, 6.6 lệch mã → sửa tệp task.
- Lần 2: ĐẠT, không phát hiện phải sửa. Hai quyết định thay chủ shop (ngoại lệ chỉ đọc view cho inventory, miễn ADR-29
  cho `FlashItemOfOrder`) được đánh giá an toàn (view `security_invoker`, RLS vẫn áp).
- Ghi chú không bắt buộc (chưa làm, ghi ở Việc phát hiện thêm): bản `Order` trong bộ nhớ không lùi theo savepoint ở
  đường dự phòng; đơn bị bỏ qua không được đếm.
