# Task 35 — Kiểm kho

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 35 (M1-35)). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (tồn kho).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn; chỉ thêm. Mới trong `inventory`:
  `Stocktake`, `StocktakeLine` (kèm xem trước: `Current`, `AvailableAfter`), `Shortage`, `ApplyResult`, `StartStocktake`, `RecordCount`,
  `(Writer).ApplyStocktake`, `CancelStocktake`, `GetStocktake`, `ErrStocktakeOpen`, `ErrStocktakeNotOpen`,
  `ShortageAlertKind`, `Notifier`, `NewShortageAlertCommand`, hằng trạng thái phiên. Thêm vào `httpapi`: 5 handler, ánh
  xạ 2 lỗi mới trong `inventoryError`. `cmd/crm`: đăng ký lệnh cảnh báo thiếu hàng vào Runner (cạnh lệnh cảnh báo).
- Gói phụ thuộc ngược (go list): `internal/modules/inventory` ← `internal/httpapi`, `cmd/crm`, `invtest`, `store`.
  Hook test các gói này.
- Operation OpenAPI bị chạm, oasdiff: thêm 5 operation (`startStocktake`, `recordStocktakeCount`, `applyStocktake`,
  `cancelStocktake`, `getStocktake`) và schema `Stocktake*`; không sửa operation cũ → oasdiff không breaking. Mã lỗi
  `stocktake_open`, `stocktake_not_open` đã khai sẵn trong `x-error-codes` (câu tiếng Việt ở `console/src/app/errors.ts`
  kiểm khi Task 48 có).
- Chỗ dùng trên màn hình: chưa có (màn kiểm kho điện thoại là Task 329 / M1-51d). `console/src/api/**` sinh lại.
- Câu sqlc, bảng, pha migration: migration `0121_stocktakes.sql` (pha mở rộng: chỉ CREATE, GRANT, trigger) — bảng
  `inventory.stocktakes`, `inventory.stocktake_lines`, sequence mã phiếu `KK-`; tệp câu mới `queries/inventory/stocktakes.sql`.
  Dùng lại `InsertLedger`, `EnsureBalance`, `ListAvailability` (không sửa) và `ApplyDelta` (thêm một nhánh điều kiện
  `on_shelf`, quyết định 4; nơi gọi cũ không đổi). Thứ tự khóa đã có ở ADR-46
  nhóm 11 (`stocktakes → stocktake_lines → stock_balances`), không sửa tệp ADR.
- Test sẽ chạy: cả bộ vùng cao `go test -race ./internal/modules/inventory/...` (gồm thuộc tính cũ và mới),
  `./internal/httpapi/...` (handler kiểm kho + `TestErrorCodes_DeclaredInContract`, route), `./tests/arch/...`,
  `./cmd/crm/...` build; `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands.CommandKind,
  commands.Handler, queue.JobQueue, tenancy.WithTenant, db.RetryTx), trong `inventory`: `applyDelta`, `Writer.changed`,
  `Availability`, `EnsureBalance`, `actorUUID`/`pgUUID`, `lockDraft`/`whyNotDraft` của chứng từ; trong `httpapi`:
  `inventoryWrite`/`inventoryRead`/`canAt`/`audit`/`staffActor`; `alertjobs` (mẫu lệnh gọi Alerter).
- Cảnh báo trùng của jscpd đã trả lời: `availability` (test inventory) giống helper `balance` của test order — hai gói
  test khác module, không dùng chung được, viết lại; `CancelStocktake` khóa rồi mới đổi (khác `CancelDocument`); cặp
  yêu cầu song song của test httpapi gộp thành `oneWinsOneConflict`.
- Dùng lại / viết mới vì:
  - Đổi số dư: dùng `applyDelta` (một câu UPDATE có điều kiện + dòng sổ `doc_type = 'stocktake'`) và `Writer.changed`
    (stock.changed + báo hiệu). Không viết câu đổi số dư mới.
  - Kiểm quyền theo kho, chống trùng, audit: dùng lại helper của `handlers_inventory.go`.
  - Lệnh cảnh báo: viết `shortageAlert` mới trong `inventory` (hiện thực `commands.Handler`, Kind từ `CommandKind`),
    không dùng `alertjobs.scheduled` vì đó là việc theo lịch, kiểu riêng không xuất ra; nghiệp vụ (văn bản, khóa chống
    lặp) thuộc module kho. Interface `Notifier` khai ở phía dùng (inventory), `observability.Alerter` thỏa nó.
  - Khóa/kiểm trạng thái phiên viết riêng (`lockOpen`) dù giống `lockDraft`: khác bảng, khác lỗi, khác lý
    do thay đổi (luật phiên kiểm kho vs luật phiếu) → giữ riêng.

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1 Chủ dữ liệu: phiên kiểm kho thuộc `inventory` (05 mục 2 "Kiểm kho"), bảng trong schema `inventory`.
- 5.2 Không ghi bảng module khác: chỉ ghi bảng `inventory.*`; không đụng đơn, không đụng `stock_reservations`.
- 5.3 Đọc module khác qua view: biến thể và người bán đọc qua `catalog.variants_v`.
- 5.4 Việc nguyên tử: áp dụng phiên (đổi trạng thái + mọi dòng số dư + sổ + sự kiện + ghi lệnh cảnh báo) trong một
  transaction của bên gọi.
- 5.5 Việc sau commit: cảnh báo là lệnh `inventory.shortage_alert` ghi bằng `Enqueue` trong cùng tx; gửi Telegram chạy
  ngoài transaction ở Runner.
- 5.6 Gọi ra ngoài: lệnh dựng bằng `commands.CommandKind` (giữ chỗ hết hạn → `in_doubt`). `AutoRetryNotSent = true`:
  Notify lỗi → `not_sent` (giống `alertjobs`); trùng tin ít hại và Alerter tự chống lặp theo khóa
  `stock-shortage:<id>`.
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; hai bảng mới có `enable_tenant_rls`, unique và khóa ngoại ghép
  `tenant_id`.
- 5.8 Nghiệp vụ không biết HTTP: module không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi: 4 đường ghi có `IdempotencyKey`, `x-permission` (`inventory.stocktake`, `inventory.stocktake.apply`);
  đích `warehouse` kiểm trong handler bằng `canAt` như chứng từ.
- 5.10 Không áp dụng: không dùng Valkey.
- 5.11 Không thêm thư viện.
- 5.12 Theo mẫu `catalog`/chứng từ: hàm đọc nhận tx, sửa có `version`, đường đổi tồn là phương thức của `Writer`.

Đúng khi chạy đồng thời (mục 6):
- 6.1 Áp dụng: `UPDATE stocktakes … WHERE status='open' AND version=$v` (hai yêu cầu → một thắng, bên kia
  `stocktake_not_open`). Ghi dòng đếm: khóa phiên `FOR UPDATE` (kiểm open), dòng có `version` riêng (`lineVersion`),
  phiên tăng `version` mỗi lần đếm để áp dụng theo bản xem trước đã thấy. Số dư: `on_hand += delta` qua `ApplyDelta`
  điều kiện `on_hand − in_transit >= |delta|` khi giảm (quyết định 4; CHECK `on_hand >= 0` là lớp sau). Một phiên mở mỗi kho: unique một phần.
- 6.2 `Idempotency-Key` trên mọi đường ghi (middleware sẵn có); khóa tự nhiên `UNIQUE (tenant_id, stocktake_id, variant_id)`.
- 6.3 Thứ tự khóa ADR-46 nhóm 11: `stocktakes` → `stocktake_lines` → `stock_balances` theo `variant_id` tăng dần (một kho
  mỗi phiên). `RecordCount` đọc số dư không khóa.
- 6.4 Lỗi tạm: đường HTTP qua `refusableWrite`/`WriteTx` (RetryTx sẵn có).
- 6.5 READ COMMITTED + câu có điều kiện + khóa dòng.
- 6.6 Lệnh lỗi: lịch thử lại của hàng đợi; 8 lượt rồi dead.
- 6.7 Telegram 429: Alerter trả `TelegramRetryError` → lệnh đặt `RetryAfter` đúng thời gian Telegram yêu cầu (như
  `alertjobs`); lỗi gửi log một lần ở mép lệnh (không dữ liệu khách).
- 6.8 Tiến trình chết: transaction tự lùi; lệnh thành `in_doubt` (cảnh báo, không gửi lại tự động).
- 6.9 Giới hạn thời gian mặc định của db và hàng đợi; lệnh 10 s.
- 6.10 Không áp dụng: không có tiền.
- 6.11 Rapid 200 lượt: xen kẽ giữ, gửi, giao, đếm, áp dụng → `on_hand − in_transit` = số đếm + thay đổi sau lúc đếm;
  `held` và dòng giữ hàng không bị phiên đổi; bộ bất biến cũ vẫn chạy.
- 6.12 Văn bản cảnh báo chỉ có mã phiếu và số mã hàng; payload lệnh chỉ mã; audit không có dữ liệu cá nhân.
- 6.13 Mặc định từ chối: guard kiểm có grant; handler kiểm đích kho của phiên (403 khi khác kho); áp dụng cần
  `inventory.stocktake.apply`.

## Quyết định thay chủ shop

1. **`ApplyStocktake` là phương thức của `Writer`** (kế hoạch ghi hàm rời). Chọn vì luật module mẫu: đường đổi tồn là
   phương thức của `Writer` (cần Events, Queue để phát stock.changed và ghi lệnh cảnh báo). Phương án khác: hàm rời nhận
   thêm tham số — trái mẫu. Đảo ngược dễ.
2. **Ghi lại số đếm khi đã có dòng phải gửi `lineVersion`; `lineVersion` rỗng khi dòng đã có → `version_conflict`.**
   Chọn vì hai người quét cùng mã không ghi đè nhau im lặng. Phương án khác: rỗng = ghi đè — dễ mất số đếm.
3. **Mỗi lần đếm tăng `version` của phiên**; áp dụng phải gửi đúng version vừa xem trước. Chọn vì "xem trước chênh lệch,
   áp dụng" phải áp đúng bản đã xem. Phương án khác: version chỉ đổi khi đổi trạng thái — có thể áp số chưa xem.
4. **Điều kiện giảm khi áp dụng là `on_hand − in_transit ≥ |delta|`** (điều kiện mới `on_shelf` của câu `ApplyDelta`),
   chặt hơn "`on_hand + delta >= 0`" của kế hoạch. Lý do: nếu để `on_hand < in_transit`, lúc giao xong (`Consume`: on_hand
   −q) sẽ vi phạm CHECK và đơn không đóng được. Không đủ → cả phiên không áp, `insufficient_stock` kèm `variantId` và
   `available` = phần trên kệ hiện tại (đếm lại dòng đó). Phương án khác: theo đúng chữ kế hoạch — để lỗi lộ ra ở bước
   giao hàng.
5. **Kho đã đóng (`status = 'closed'`) không mở được phiên kiểm** → `invalid_input` (400). Thuộc phạm vi kiểm kho; dùng
   mã có sẵn thay vì thêm mã `warehouse_closed` (thêm mã cần câu ở màn hình). Phiếu kho ở kho đóng: ngoài phạm vi, ghi ở
   "Việc phát hiện thêm".
6. **Ghi dòng đếm cũng ghi audit** (`inventory.stocktake.count`, chỉ số lượng) như mọi đường ghi của kho.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| KH05: on_hand 10, in_transit 3, đếm 7 → expected 7, delta 0, không chỉnh, 0 dòng sổ | `TestStocktake_InTransitNotAdjusted` | `go test -race ./internal/modules/inventory/...` (trong `make ci`) | `ok elimaz.com.vn/crm/internal/modules/inventory 539.208s` |
| Đếm 7 rồi 3 cái giao xong → áp dụng: on_hand 7, không chỉnh (bản cũ ra 4) | `TestStocktake_DeliveredAfterCount` | như trên | ok |
| KH07: on_hand 5, held 4 (2+2), đếm 2 → on_hand 2, held 4, 2 giữ hàng vẫn held, Shortages available −2, 1 lệnh `inventory.shortage_alert`, sổ −3, stock.changed | `TestStocktake_ShortageKeepsHolds` | như trên | ok |
| Cảnh báo: khóa `stock-shortage:<id>`, normal, văn bản "Kiểm kho KK-…: 1 mã hàng thiếu so với phần đang giữ", không tên mẫu; Notify lỗi → not_sent; Telegram 429 → not_sent kèm RetryAfter 7 s | `TestShortageAlertCommand` | như trên | ok |
| Đếm thừa: on_hand 5 → đếm 6 → on_hand 6, sổ stocktake +1, không cảnh báo | `TestStocktake_Overcount` | như trên | ok |
| Biến thể chưa có số dư: đếm 3 → on_hand 3 | `TestStocktake_NewBalanceRow` | như trên | ok |
| Hàng xuất sau lúc đếm làm phần trên kệ âm → không áp gì, InsufficientError (có 2) | `TestStocktake_ApplyWouldGoNegative` | như trên | ok |
| Áp dụng hai lần song song → một thành công, một `ErrStocktakeNotOpen`; sổ 1 dòng | `TestStocktake_ApplyTwiceConcurrent` | như trên | ok |
| Hủy phiên open → cancelled, số dư không đổi; hủy phiên applied → `stocktake_not_open`; mở phiên mới sau hủy | `TestStocktake_Cancel`, `TestStocktakeAPI_WarehouseScope` | như trên + `go test -race ./internal/httpapi/...` | `ok elimaz.com.vn/crm/internal/httpapi 492.672s` |
| Phiên thứ hai cùng kho → `stocktake_open` 409; kho đóng → invalid_input; kho tenant khác → not found | `TestStocktake_OneOpenPerWarehouse`, `TestStocktake_WarehouseChecks`, `TestStocktakeAPI_Flow` | như trên | ok |
| Ghi lại số đếm cần lineVersion; chụp lại số dư; đếm tăng version phiên | `TestRecordCount_Rules`, `TestStocktakeAPI_Flow` | như trên | ok |
| Quyền: phạm vi `warehouse:w1` mở/đếm/đọc/hủy ở w2 → 403; chỉ có `inventory.stocktake` áp dụng → 403; tenant B → 404 | `TestStocktakeAPI_WarehouseScope`, `TestStocktakeAPI_Flow` | như trên | ok |
| Áp dụng hai lần qua API, khóa khác nhau → một 200, một 409 `stocktake_not_open`; sổ 1 dòng | `TestStocktakeAPI_ApplyTwice` | như trên | ok |
| Thuộc tính (rapid, 300 lượt của TestMain ≥ 200): on_hand − in_transit = số đếm + thay đổi sau lúc đếm; held, giữ hàng không đổi; sổ = số dư; in_transit ≤ on_hand | `TestStocktakeInvariants` | như trên | ok |
| Bất biến cũ của kho vẫn đúng (bộ vùng cao inventory) | `TestInventoryInvariants`, toàn bộ gói | như trên | ok |
| Luật schema (RLS, unique có seller_id sau tenant_id, ADR-29) | `TestSchemaRulesStillClean`, `TestCheckSchemaRules*` | `make ci` | `ok …/modules/identity`, `ok …/platform/db` |
| Cổng kiểm đầy đủ sau sửa reviewer, trên bản đã gộp main tới Task 396/38 | build, vet, golangci-lint, gen-check, sqlc vet, govulncheck, oasdiff, catalog, jscpd, test-guard, gitleaks, console; `go test -race ./...` | `make build vet lint gen-check sqlc-vet vuln oasdiff catalog-check jscpd test-guard gitleaks console-check` rồi `go test -race -count=1 -p 2 -timeout 30m ./...` | `gates=0`, `test=0` (38 gói ok; `ok …/modules/inventory 145.888s`, `ok …/internal/httpapi 220.078s`) |
| Sau khi gộp thêm Task 39 rồi Task 31 (main; openapi giữ cả vận đơn và kiểm kho) | cổng như trên (trừ vuln) + test các gói bị chạm | `make build vet lint gen-check sqlc-vet oasdiff catalog-check jscpd test-guard gitleaks console-check`; `go test -race -p 2 ./internal/httpapi/... ./internal/modules/{inventory,order,identity}/... ./tests/{arch,e2e}/... ./cmd/...` | `gates=0`, `test=0` (`ok …/internal/httpapi`, `ok …/modules/inventory`, `ok …/modules/catalog`, `ok …/modules/identity`, `ok …/tests/arch`) |

Ghi chú chạy kiểm: `make ci` đầy đủ đã qua một lần (exit 0) trên bản gộp Task 30/397 trước review. Sau review, máy dùng
chung bị tải nặng (load ~20, 33 container của các phiên song song): một lần PostgreSQL thử của task tự khởi động lại giữa
chừng (log: "untracked child process exited with exit code 2"), làm mất thống kê `pg_stat_archiver` nên `TestDeepHealth`
hỏng ở lần sau; vài lần khác DROP DATABASE và gói quá 10 phút. Đã dựng lại container crm-t35 và chạy test với `-p 2`,
timeout 30 phút; không test nào bị sửa hay nới.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `ApplyStocktake` là phương thức `Writer` (quyết định 1).
- Điều kiện giảm chặt hơn (quyết định 4): thêm nhánh `on_shelf` vào câu `ApplyDelta` (`queries/inventory/balances.sql`).
- Unique của dòng đếm là `(tenant_id, seller_id, stocktake_id, variant_id)` (luật ADR-29: bảng có `seller_id` thì unique
  có `seller_id` ngay sau `tenant_id`), kèm index tra `(tenant_id, stocktake_id, variant_id)`.
- Ca KH07 "2 đơn vẫn confirmed": kiểm ở mức giữ hàng (dòng giữ vẫn `held`) và mã không gọi gì của module đơn; chưa có test
  dựng đơn thật (Task 30 vừa gộp, chưa có helper đơn cho test kho).
- Handler kiểm kho ở tệp riêng `internal/httpapi/handlers_stocktake.go` (kế hoạch ghi sửa `handlers_inventory.go`) để
  tệp không quá dài; dùng chung helper của `handlers_inventory.go`.
- Lệnh cảnh báo ở `internal/modules/inventory/shortagealert.go`, nối ở `cmd/crm/main.go` (cạnh lệnh cảnh báo, cùng Alerter).
- `TestStocktakeInvariants` chạy tuần tự trong mỗi lượt (xen kẽ thứ tự thao tác ngẫu nhiên) để ảnh chụp "phần trên kệ
  lúc đếm" là chính xác; chạy song song đã có ở `TestInventoryInvariants` và `TestStocktake_ApplyTwiceConcurrent`.
- Dọn trùng (jscpd): thêm helper test `oneWinsOneConflict` và dùng lại ở `TestInventoryAPI_PostIdempotent`.

## Việc phát hiện thêm

- Migration `0121` (số kế hoạch giao) nằm trước `0123`, `0124` đã có trên main: database dev đã chạy tới `0124` sẽ bị
  goose báo thiếu migration khi nâng (dựng lại database dev, hoặc chạy goose cho phép migration thiếu). Chưa deploy nên
  không ảnh hưởng production; cần lưu ý khi gộp vào máy dev đang chạy.
- Máy dùng chung quá tải khi nhiều phiên chạy `make ci` cùng lúc (load ~20): PostgreSQL thử tự khởi động lại một lần, gói
  test chạm 10 phút. Nên cân nhắc giới hạn `-p` hoặc timeout của `make test` khi chạy song song nhiều phiên.

- (từ Task 29) Kho đã đóng vẫn tạo và ghi sổ phiếu kho được — thuộc chứng từ kho, không làm ở task này.
- (từ Task 29) Lỗi xuất hàng lỗi (`damage_export`) báo `available` = `damaged`, thấp hơn thực tế khi kiểm kho đã làm
  `on_hand < damaged` (đường kiểm kho được phép làm `damaged > on_hand`) — thuộc chứng từ kho, không làm ở task này.

## Reviewer

Lần 1 — Kết luận: CẦN SỬA. Phát hiện (trung bình): lệnh cảnh báo bỏ qua `RetryAfter` khi Telegram trả 429 và nuốt lỗi
không log. Đã sửa: đọc `*observability.TelegramRetryError` → `Outcome.RetryAfter`, log một lần ở mép (logger truyền vào
`NewShortageAlertCommand`), thêm ca 429 vào `TestShortageAlertCommand`; mục Ảnh hưởng, 6.7 sửa cho khớp.

Lần 2 (sau sửa và gộp main tới Task 31) — Kết luận: ĐẠT. Không còn phát hiện ảnh hưởng tính đúng. Đã kiểm bản gộp
openapi: 48 operation của main còn đủ, thêm đúng 5 operation kiểm kho; mã sinh chỉ thêm 5 operation đó. Ghi chú không bắt
buộc đã xử lý: sửa chữ tệp task (bỏ `StocktakePreview`, `whyNotOpen`; 6.1 ghi đúng điều kiện `on_shelf`); ghi lưu ý thứ tự
migration 0121 vào "Việc phát hiện thêm".
