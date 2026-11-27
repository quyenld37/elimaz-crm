# Task 200 — Lên lịch (chứng từ giữ cho đợt), thêm suất, đóng đợt, nhịp mở/đóng tự động

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 200, M5-17). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn, chỉ thêm. `inventory`: `FlashItemRef`,
  `FlashShortage`, `LockFlashStock`, `Writer.ReserveForFlash`, `Writer.AddFlashUnits`, `Writer.CloseFlashItem`.
  `promotion`: `Writer.ScheduleFlashSale`, `Writer.AddSlots`, `Writer.CloseFlashSale`, `Writer.Tick`, `Live`,
  `FlashLive`, `FlashLiveItem`, `FlashShortageError`, `FlashShortItem`, sentinel `ErrFlashOverlap`, `ErrFlashEmpty`,
  `ErrFlashNotActive`, `ErrFlashEnded`, `ErrFlashInsufficient`, hằng `TopicFlashSaleOpened/Closed/Changed`,
  `FlashTickSchedule`, kiểu payload `FlashSaleEvent`. Hằng chưa xuất `topicFlash*` của `internal/wiring/webcache/consumer.go`
  (3 chỗ dùng trong tệp đó và test của nó) thay bằng hằng của promotion.
- Gói phụ thuộc ngược (go list): `inventory` ← cmd/crm, httpapi, invtest, order, promotion, wiring/webcache, tools/loadkit;
  `promotion` ← cmd/crm, httpapi, httpapi/web, order, wiring/webcache. Chỉ thêm hàm nên không gãy chỗ gọi.
- Operation OpenAPI bị chạm, oasdiff: thêm 4 operation (chỉ thêm, oasdiff không báo phá vỡ): `scheduleFlashSale`
  `POST /api/v1/flash-sales/{flashSaleId}/schedule`, `addFlashSlots` `POST …/items/{flashItemId}/add-slots`,
  `closeFlashSale` `POST …/close` (ghi: `promotion.flash.manage`, `IdempotencyKey`), `flashSaleLive` `GET …/live`
  (`promotion.flash.read`). Schema mới `FlashScheduleRequest`, `FlashAddSlotsRequest`, `FlashLive`, `FlashLiveItem`,
  `FlashShortage`; `Error` thêm thuộc tính `shortages`; `x-error-codes` thêm `flash_overlap`, `flash_sale_empty`,
  `flash_not_active`, `flash_ended`.
- Chỗ dùng trên màn hình: chưa có màn đợt flash sale (màn ở task sau); `console/src/api/**` sinh lại, `errors.ts` thêm
  câu tiếng Việt cho 4 mã mới.
- Câu sqlc, bảng, pha migration: migration `0533_flash_ledger_doc_types.sql` (số trống kế tiếp sau 0532 trên main; kế
  hoạch ghi 0508) — pha mở rộng: nới CHECK `doc_type` của `inventory.stock_ledger` thêm `flash_hold`, `flash_release`
  (DROP rồi ADD NOT VALID + VALIDATE, giữ đủ 7 giá trị cũ; không đụng `stock_documents`). Ràng buộc
  `stock_ledger_step_key UNIQUE (tenant_id, doc_type, doc_id, variant_id, warehouse_id, bucket, step)` của M1 vẫn
  chặn áp hai lần cùng bước. Câu mới: `queries/inventory/flash_docs.sql` (`LockFlashBalances`, `InsertFlashUnits`,
  `ReleaseAvailableFlashUnits`), `queries/promotion/flash_lifecycle.sql` (`SetFlashSaleStatus`, `BumpFlashSaleVersion`,
  `FlashOverlaps`, `DueFlashSales`, `AddFlashItemQuantity`, `FlashLiveItems`). Bảng chạm: `promotion.flash_sales`, `flash_sale_items`
  (ghi), `flash_sale_claims` (đọc), `inventory.stock_balances`, `stock_ledger`, `flash_sale_units` (ghi qua inventory),
  view `inventory.flash_units_summary_v` (promotion đọc).
- Thứ tự khóa (ADR-46): lên lịch: `flash_sales` (đợt, FOR UPDATE) → [nhóm 11] `stock_balances` của mọi món theo
  (variant_id, warehouse_id) tăng dần (FOR UPDATE) → kiểm chồng giờ, kiểm đủ hàng → đổi số dư theo cùng thứ tự → chèn
  `flash_sale_units` (dòng mới) → đổi trạng thái đợt. Thêm suất: `flash_sales` → `flash_sale_items` (món) →
  `stock_balances` (một dòng) → chèn đơn vị. Đóng: `flash_sales` → `stock_balances` mọi món theo (variant, warehouse)
  tăng dần → `flash_sale_units` (UPDATE … status='available') → đổi `held`. Nhịp: khóa các đợt đến hạn theo id tăng dần
  trong một câu, rồi khóa số dư của mọi món của mọi đợt cần đóng trong **một** câu theo (variant, warehouse)
  tăng dần, rồi đóng từng đợt (sửa sau review vòng 1), rồi mở các đợt đến giờ.
- Tệp khác bị chạm: `internal/modules/promotion/flash.go` (`lockDraftSale` gọi hàm chung mới `lockSale`, dùng cho cả
  `lockActiveSale`; jscpd báo hai hàm khóa đợt trùng), `module.go` (lịch + handler), `export_test.go`
  (`FlashTickHandler`); `internal/httpapi/errors.go` (`errorBody.Shortages`, `detailedError` gọi `flashShortageDetail`),
  `errors_promotion.go`, `errors_snapshot_test.go`; `cmd/crm/main_test.go` (danh sách lịch theo tenant thêm
  `promotion.flash_tick`); `docs/views.md` (dòng sự kiện `flash_sale.*`).
- Test sẽ chạy: `go test -race -run '^(TestFlash…)$' ./internal/modules/inventory/ ./internal/modules/promotion/`, test
  handler flash sale và bảng lỗi ở `internal/httpapi`, test consumer `internal/wiring/webcache`, `cmd/crm` (lịch),
  `tests/arch`, console `errors.test.ts`; cả gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về flash);
  `inventory/movement.go` (`applyDelta`, `Writer.changed`), `inventory.InsufficientError`, `inventory.Availability`,
  `promotion.Writer` (Events, Queue, Realtime), `promotion.lockDraftSale`, `FlashItemsBySale`, mẫu lịch theo tenant
  `birthday_run.go` (`schedules.Schedule` + `queue.Handler` trong `promotion.Background`), `webcache.Consumer`.
- Dùng lại / viết mới vì: mọi đổi số dư đi qua `applyDelta` (một câu UPDATE có điều kiện + dòng sổ) và `Writer.changed`
  (stock.changed); lịch theo đúng mẫu birthday_run; thân lỗi theo `detailedError`. Viết mới `LockFlashBalances` vì câu
  khóa có sẵn (`LockBalancesOfReservations`) đi từ dòng giữ hàng, không từ danh sách (biến thể, kho).

## Soát thiết kế

5.1 Đợt, món thuộc promotion; số dư, sổ, đơn vị thuộc inventory (05 ERD flash sale). 5.2 promotion chỉ gọi public API
của inventory (`LockFlashStock`, `Writer.*Flash*`), không ghi bảng inventory. 5.3 promotion đọc đơn vị qua view
`inventory.flash_units_summary_v`, biến thể qua `catalog.variants_v`. 5.4 Lên lịch + giữ suất + trải đơn vị là một
transaction (đủ hết hay không gì). 5.5 Báo cho bộ đệm web và màn hình đi sau commit qua sự kiện (consumer webcache).
5.6 không áp dụng: không gọi ra ngoài. 5.7 mọi câu trong tx tenant của bên gọi (handler: WriteTx; lịch: tx việc theo
tenant của hàng đợi). 5.8 module không nhập net/http. 5.9 ba operation ghi có `IdempotencyKey` và `x-permission`.
5.10 không dùng Valkey. 5.11 không thêm thư viện. 5.12 theo mẫu catalog/inventory (Writer, sentinel, test _test trên
PostgreSQL thật).

6.1 Số dư: câu UPDATE có điều kiện (`available ≥ q`, `held ≥ q`) dưới khóa dòng; đợt: khóa dòng + `version` ở lên lịch.
6.2 Idempotency-Key ở HTTP; sổ có unique (doc_type, doc_id, …, step) — bước `schedule`, `add_<version món>`, `close`
không áp hai lần. 6.3 Thứ tự khóa như mục Ảnh hưởng; số dư nhiều món khóa theo (variant, warehouse) tăng dần (ADR-46
nhóm 11), không theo id món (xem Quyết định); test rapid chạy đồng thời đếm `pg_stat_database.deadlocks`. Chồng giờ
kiểm **sau** khi khóa số dư: hai đợt dùng chung biến thể cùng lên lịch thì đợt sau chờ khóa số dư, câu kiểm của nó
thấy đợt trước đã commit (READ COMMITTED, mỗi câu một snapshot mới). 6.4 handler dùng WriteTx (RetryTx); hàm gán lại
mọi biến kết quả, không cộng dồn ngoài tx. 6.5 READ COMMITTED + câu có điều kiện. 6.6 việc lịch: MaxAttempts 3, hết hạn
sau một kỳ (15 s), kỳ sau chạy lại; lượt idempotent (đợt đã mở/đóng không khớp điều kiện nữa). 6.7 không áp dụng.
6.8 tx tự lùi; nhịp chạy lại. 6.9 lên lịch 20.000 suất: một câu INSERT … generate_series, dưới statement_timeout.
6.10 không có tiền đổi (giá flash đã lưu ở Task 199). 6.11 test rapid (held ≥ 0, sau đóng không còn đơn vị available,
không deadlock). 6.12 sự kiện, lỗi chỉ mang mã; không dữ liệu khách. 6.13 ghi: `promotion.flash.manage`; đọc live:
`promotion.flash.read`; test 403.

## Quyết định thay chủ shop

- QĐ-200-1: Số dư của nhiều món khóa và đổi theo (variant_id, warehouse_id) tăng dần, không theo `id` món như câu chữ
  kế hoạch. Vì: ADR-46 nhóm 11 và `Reserve` của đơn thường khóa số dư theo khóa này; khóa theo id món có thể deadlock
  với một đơn nhiều dòng. Phương án khác: theo id món (dễ deadlock). Một đợt mỗi biến thể một món nên thứ tự là duy nhất.
- QĐ-200-2: Bước sổ của thêm suất là `add_<version món mới>` (gạch dưới), không `add-<version>`: CHECK `step` của
  `stock_ledger` (M1) chỉ nhận `^[a-z][a-z0-9_]{0,31}$`. Đổi được dễ (chỉ là chuỗi).
- QĐ-200-3: Hàm đổi tồn của inventory là phương thức của `inventory.Writer` (cần Events, Queue để phát stock.changed),
  nhận `inventory.Actor`; hàm của promotion là phương thức của `promotion.Writer` nhận `promotion.Actor` (promotion không
  nhập identity) thay cho `identity.AuthUser` của kế hoạch; audit ghi ở handler như Task 199.
- QĐ-200-4: Kiểm đủ hàng bằng `inventory.LockFlashStock` (khóa số dư FOR UPDATE rồi đọc số có thể bán trong cùng câu)
  trước khi ghi, để báo **mọi** món thiếu mà không ghi gì; kiểm chồng giờ chạy sau câu khóa này (6.3).
- QĐ-200-5: Thêm suất cũng tăng `version` của đợt và phát `flash_sale.changed` (bộ đệm website, màn hình thấy số suất
  mới). Kế hoạch không ghi sự kiện cho thêm suất; không có thì bộ đệm website giữ số suất cũ tới hạn.
- QĐ-200-6: Mã lỗi mới: `flash_overlap` (422, biến thể đã ở đợt khác chồng giờ), `flash_sale_empty` (422, đợt không
  món), `flash_not_active` (409, thêm suất/đóng đợt khi đợt nháp hay đã đóng; lên lịch đợt không nháp vẫn
  `not_editable`), `flash_ended` (422, lên lịch khi giờ kết thúc đã qua). Thiếu hàng dùng `insufficient_stock` (409 như
  kho) kèm mảng `shortages: [{variantId, sku, need, sellable}]`.
- QĐ-200-7: Lịch `promotion.flash_tick` khai trong `promotion.Background` (mẫu birthday_run, cmd/crm đã định nghĩa mọi
  lịch của module qua `mods.Schedules`) thay vì sửa `cmd/crm/main.go`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tồn 10 → giữ 6 suất: held 6, 6 đơn vị trống, có thể bán 4; đơn thường 5 hết hàng, 4 được [FS01][FS09]; một stock.changed; áp lại bước schedule bị unique sổ chặn | `inventory.TestFlashReserve_HoldsForSale` | `go test -race -count=1 -run '^(TestFlashReserve_HoldsForSale\|TestFlashReserve_Insufficient\|TestFlashAddAndClose)$' ./internal/modules/inventory/` | `ok … inventory 2.285s` |
| Tồn 5, 6 suất → thiếu {need 6, sellable 5}; biến thể chưa có số dư → sellable 0; ReserveForFlash → InsufficientError; không đơn vị, held không đổi | `inventory.TestFlashReserve_Insufficient` | như trên | `ok` |
| Thêm 3 suất (bước add_2) → held 6; thêm vượt → ErrInsufficient; đóng món 4 trống + 2 giữ → 4 released, held 2, sổ −4; đóng lại → 0, không thêm dòng sổ | `inventory.TestFlashAddAndClose` | như trên | `ok` |
| Lên lịch → scheduled, held 6, live 6 trống, 1 flash_sale.changed; lên lịch lại not_editable; nhịp đúng starts_at → open 1 lần, lần hai không gì; thêm 3 suất khi open → 9, held 9; quá ends_at → closed `ended`, held 0, sổ +9 −9 [FS08]; đối chiếu sổ + công thức held sau mỗi bước | `promotion.TestFlashLifecycle_ScheduleOpenAddClose` | `go test -race -count=1 -run '^(TestFlash.*)$' ./internal/modules/promotion/` | `ok … promotion 12.976s` |
| Đóng tay → closed `manual`, held 0; đóng lại, thêm suất sau đóng, đợt nháp → ErrFlashNotActive; version cũ, quá giờ, không món, add 0, vượt 20.000, lý do lạ | `promotion.TestFlashLifecycle_CloseAndRejections` | như trên | `ok` |
| Hai món, một thiếu → lỗi liệt kê đúng món thiếu (sku, need 6, sellable 5), món đủ không bị giữ, đợt draft, 0 đơn vị; thêm suất thiếu → {need 8, sellable 7} | `promotion.TestFlashSchedule_Insufficient` | như trên | `ok` |
| Cùng biến thể ở đợt scheduled chồng giờ → overlap, không giữ gì; nối tiếp → được; đợt kia đã đóng → được | `promotion.TestFlashSchedule_Overlap` | như trên | `ok` |
| Live đếm khách quantity > 0; đợt không có → ErrNotFound | `promotion.TestFlashLive_Buyers` | như trên | `ok` |
| Lịch promotion.flash_tick 15 s theo tenant; handler trả {"closed":0,"opened":1} | `promotion.TestFlashTickSchedule`, `cmd/crm.TestNewModulesRegistersCash` | như trên; `go test -count=1 -run TestNewModules ./cmd/crm/` | `ok` |
| Nhịp đóng hai đợt (A biến thể X lớn, B biến thể W nhỏ) trong khi transaction khác khóa W rồi X → không 40P01 (trước khi sửa: `deadlock detected (SQLSTATE 40P01)`), cả hai đóng, sổ khớp | `promotion.TestFlashTick_CloseManyNoDeadlock` | `go test -race -count=3 -run '^TestFlashTick_CloseManyNoDeadlock$' ./internal/modules/promotion/` | trước sửa `FAIL … 40P01`; sau sửa `ok` (3 lần) |
| Rapid: lên lịch, thêm suất, đóng đồng thời → không lỗi database/40P01, deadlocks không tăng, held ≥ 0 = giữ hàng + đơn vị trống, sổ khớp, sau đóng 0 đơn vị trống | `promotion.TestFlashLifecycle_ConcurrentInvariants` | như trên | `ok` (200 lượt, ~12 s) |
| HTTP: 409 insufficient_stock kèm shortages; lên lịch 200; not_editable; live; add-slots; close; flash_not_active; flash_sale_empty; audit mỗi lần ghi | `httpapi.TestFlashSaleAPI_Lifecycle` | `go test -race -count=1 -run '^(TestFlashSaleAPI_.*\|TestErrorSnapshot.*\|TestDomainError.*)$' ./internal/httpapi/` | `ok … httpapi 2.479s` |
| Quyền: chỉ promotion.flash.read → ghi 403, live 200; sale → live 403; live đợt lạ 404 | `httpapi.TestFlashSaleAPI_LifecyclePermissions` | như trên | `ok` |
| Bảng mã lỗi mới (5 dòng) | `internal/httpapi/errors_snapshot_test.go` | như trên | `ok` |
| Câu tiếng Việt cho 4 mã mới | `console/src/app/errors.test.ts` | `cd console && npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| Consumer webcache: sự kiện flash_sale.* → báo hiệu cache + đúng một báo hiệu flash_sale.changed (promotion.flash.read); chủ đề khác không có | `webcache.TestConsumer_Payloads` | `go test -race -count=1 -run TestConsumer ./internal/wiring/webcache/` | `ok … 1.951s` |
| Kiến trúc, lint, mã lặp | `tests/arch`, golangci-lint, jscpd | `go test -count=1 ./tests/arch/`; `golangci-lint run <các gói sửa>`; `make jscpd` | `ok … arch 100.4s`; `0 issues`; `Found 0 clones` |
| Cổng kiểm của task (toàn bộ test vì migration có DROP) | ci-task | `scripts/agent/ci-task.sh` | `CI-TASK XANH (từ xa trên blacksmith …)`, run 45938805986 (vòng 1) và 45940144943 (sau sửa review): static, test (1), test (2) success |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có test e2e cho task này)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Migration số 0533 (kế hoạch 0508): main đã tới 0532.
- Test rapid chạy theo số lượt của gói promotion (200, `TestMain`) thay vì 50: đổi cờ `rapid.checks` cho riêng một
  test không an toàn khi các test của gói chạy song song; 200 lượt mất ~12 s.
- Ca "đóng đợt có 4 trống và 2 giữ" nằm ở test inventory (`TestFlashAddAndClose`), nơi dựng được dòng đơn thật cho
  đơn vị held; test promotion kiểm đóng đợt toàn trống.
- Thêm `BumpFlashSaleVersion` (QĐ-200-5) và gộp `lockDraftSale`/`lockActiveSale` vào `lockSale`.
- Xem QĐ-200-1 … QĐ-200-7.

## Việc phát hiện thêm

- Phép kiểm `held_vs_reservations` của đối chiếu đêm (M1) sẽ báo lệch cho biến thể đang có đợt lên lịch (held gồm cả
  đơn vị flash `available`, không có dòng giữ hàng) tới khi M5-19 (Task 202) mở rộng công thức. `FLASH_SALE_ENABLED`
  còn tắt cho website, nhưng bốn operation nhân viên (Task 199, 200) không bị cờ chặn: nên làm Task 202 trước khi nhân
  viên lên lịch đợt thật.
- (Reviewer vòng 1) Cùng biến thể ở hai kho khác nhau (kho mặc định đổi giữa hai bản nháp) thì hai lần lên lịch song
  song khóa hai dòng số dư khác nhau, kiểm chồng giờ có thể cùng không thấy nhau. Hiếm (shop một kho MAIN); muốn chặn hẳn
  thì khóa theo biến thể hay ràng buộc ở database.
- (Reviewer vòng 1) Thêm suất vẫn nhận khi đợt đã quá giờ kết thúc mà nhịp chưa chạy (tối đa ~15 s); nhịp sau đóng và
  trả suất, sổ không sai.

## Reviewer

Vòng 1: CẦN SỬA — `Tick` đóng nhiều đợt khóa số dư theo từng đợt (mỗi đợt tăng dần nhưng cả transaction không), có
thể deadlock với đơn nhiều dòng hay lên lịch đợt khác (ADR-46 nhóm 11). Đã sửa: `Tick` khóa số dư của mọi đợt cần đóng
trong một câu trước khi đóng; test tất định `TestFlashTick_CloseManyNoDeadlock` (đỏ 40P01 trước khi sửa, xanh sau).
Ghi chú không bắt buộc của reviewer chép vào "Việc phát hiện thêm".

Vòng 2: ĐẠT — lỗi deadlock vòng 1 đã sửa đúng (`LockFlashStock` một lần cho mọi đợt cần đóng, ORDER BY variant_id,
warehouse_id FOR UPDATE); không phát hiện mới. Ghi chú không bắt buộc: test mới dựa vào id đợt A < B (uuidv7 theo thứ tự
tạo nên luôn đúng) và một `time.Sleep` 300 ms để dựng tình huống (máy chậm thì có thể bỏ sót, không đỏ giả).

Kết luận: ĐẠT
