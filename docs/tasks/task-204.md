# Task 204 — Trạng thái flash sale cho website; hoàn tất đơn; vòng dự phòng (M5-21)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 204 / M5-21). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào đã có. Thêm mới: `promotion.FlashRemaining`
  + kiểu `promotion.FlashItemRemaining` (0 chỗ dùng cũ); `webcatalog.FlashCurrent`, `webcatalog.FlashAvailable`, `webcatalog.FlashStatusAt`,
  hằng `FlashScheduled`, `FlashOpen`, kiểu `WebFlashSale`, `WebFlashItem` (mới); `web.Catalog.FlashCurrent`, `web.Catalog.WithFlashGate` (mới; `NewCatalog` giữ
  chữ ký, 1 chỗ gọi ở `server.go` prepare thêm `.WithFlashGate(d.FlashGate)`); gói mới `internal/wiring/flashstate`
  (`New`, `Loop.Start`, `Loop.Stop`); `testkit.Valkey.FlushAll` (mới, thay hàm `flushAll` cục bộ của
  `internal/httpapi/web/flash_test.go` — 2 chỗ gọi trong tệp đó). Handler mới `WebFlashCurrent`, `WebFlashComplete`
  (gen.Handler). `flashRefusal` (không xuất) thêm `order.ErrDuplicateOrder` (1 chỗ gọi: `WebFlashBuy`).
- Tệp thực sự đổi ngoài ký hiệu mới ở trên: `internal/httpapi/handlers_web_flash.go` (hai handler mới, `flashRefusal`,
  sửa chú thích 422→400), `internal/httpapi/server.go` (prepare: `.WithFlashGate(d.FlashGate)`), `cmd/crm/main.go` (vai trò
  api chạy `flashstate.Loop`, dừng trước khi đóng Valkey), `internal/httpapi/web/catalog.go` (trường `gate`),
  `internal/modules/promotion/flashtest` (`Sale.Items`, hàm mới `ScheduleItems`; `Schedule` giữ chữ ký, gọi
  `ScheduleItems` — chỗ gọi cũ trong `internal/httpapi/web`, `tests/e2e` không đổi), `internal/httpapi/web/flash_test.go`
  (env flash: `flashOpts.extra`, đồng hồ tua được `shift`/`now`, `items`; `flushAll` → `f.vk.FlushAll`), `docs/views.md`
  (dòng `promotion.flash_sale_items_v` thêm bên dùng `webcatalog`, Task 204).
- Gói phụ thuộc ngược (go list): `internal/httpapi/web` ← 1 gói (`internal/httpapi`); `internal/queries/webcatalog` ← 3
  gói; `internal/modules/promotion` ← 7 gói (chỉ thêm hàm, không gãy); `internal/testkit` ← 61 gói (chỉ thêm phương thức);
  `internal/wiring/flashstate` ← `cmd/crm` (luật kiến trúc (m): chỉ cmd nhập wiring).
- Operation OpenAPI bị chạm, oasdiff: thêm `webFlashCurrent` (GET `/api/v1/web/flash-sales/current`, service_key,
  `web.catalog.read`) và `webFlashComplete` (POST `/api/v1/web/flash-orders/{externalRef}/complete`, service_key,
  `web.flash.buy`, Idempotency-Key); schema mới `WebFlashCurrent`, `WebFlashSaleState`, `WebFlashItemState`,
  `WebFlashCompleteRequest`, `WebFlashCompleted`. Chỉ thêm → oasdiff không có thay đổi phá vỡ. Mã lỗi dùng mã đã có
  (`hold_expired`, `phone_required`, `not_found`…), không thêm vào `x-error-codes`.
- Chỗ dùng trên màn hình: không có (điểm cuối của website); `console/src/api/**` sinh lại theo hợp đồng, không màn nào gọi.
- Câu sqlc, bảng, pha migration: không migration. Câu mới `queries/promotion/flash_state.sql` (`FlashActiveRemaining`:
  đọc `promotion.flash_sales`, `promotion.flash_sale_items` của chính module + view `inventory.flash_units_summary_v`) và
  `queries/webcatalog/flash_current.sql` (`FlashCurrentItems`, `FlashAvailableOf`: chỉ view `promotion.flash_sales_v`,
  `promotion.flash_sale_items_v`, `catalog.variants_v`, `inventory.flash_units_summary_v`). Không ghi bảng nào.
- Test sẽ chạy: `internal/wiring/flashstate` (mới, -race), `internal/httpapi/web` (`-run` các test flash), `internal/queries/webcatalog`,
  `internal/modules/promotion` (chạy ở ci-task: vùng rủi ro cao), `internal/httpapi` (bảng route, errors snapshot), `cmd/crm`
  build; còn lại do ci-task chọn theo gói phụ thuộc.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `cache.AdmissionGate` (SetState/Remaining/SoldOut, Task
  187), `cache.ReadCache.GetOrLoad` + `SpaceFlash`, `tenancy.ListTenantIDs`, `realtime.Bus.SubscribeSystem`,
  `webcache.Invalidator` (mẫu vòng nền nghe báo hiệu, hàng chờ + resync), `webcache.TopicInvalidate`/`EntityFlashItem`/
  `EntityFlashSale`, `order.Writer.CompleteFlashDraft` (Task 202), `auditWebOrder`, `shipChoice`, `WriteTx`,
  `errorBody`, `flashtest.Schedule`, `testkit.StartValkey`, mẫu `Catalog.Sold30`.
- Dùng lại / viết mới vì: dùng lại tất cả các thứ trên. Viết mới: `flashstate.Loop` (Invalidator xóa khóa bộ đệm, Loop
  đọc PostgreSQL rồi ghi trạng thái cổng — khác việc, khác lý do thay đổi; chỉ giống khung hàng chờ, giữ riêng);
  `promotion.FlashRemaining` (FlashLive chỉ một đợt và đếm cả khách, không lọc trạng thái đợt); hai câu webcatalog (gói
  đọc của website chỉ đọc view, không gọi module). `flushAll` của test web dời vào `testkit.Valkey.FlushAll` để test
  flashstate dùng chung (tránh jscpd).

## Soát thiết kế

- 5.1–5.3: không bảng mới. promotion đọc bảng của mình + view `inventory.flash_units_summary_v`; webcatalog chỉ đọc view
  `_v` đã công bố (luật sqlusage).
- 5.4: hoàn tất đơn: một transaction `WriteTx` (TxAPI): `CompleteFlashDraft` (khóa đơn, sửa, chuyển trạng thái, đơn vị
  sold) + audit `order.create` + lưu kết quả theo khóa chống trùng — nguyên tử.
- 5.5: vòng dựng lại đọc trong tx rồi ghi Valkey **sau** commit (ngoài tx); không phát sự kiện.
- 5.6: không gọi ra ngoài (Valkey là hạ tầng nội bộ, lỗi bỏ qua).
- 5.7: mọi câu chạy trong `tenancy.WithTenant` (vòng: từng tenant của `ListTenantIDs`).
- 5.8: handler mỏng ở `internal/httpapi`; module không nhập net/http.
- 5.9: `webFlashComplete` có `x-permission: web.flash.buy` + Idempotency-Key; `webFlashCurrent` là đọc.
- 5.10: Valkey chỉ là cờ/số gợi ý: `remaining` của website lấy từ cổng, thiếu thì đếm ở PostgreSQL; lượt mua vẫn chốt
  trong PostgreSQL (Task 201); trạng thái tự hết hạn 10 s khi vòng ngừng.
- 5.11: không thư viện mới. 5.12: theo mẫu Invalidator (vòng nền), Sold30 (đọc có đệm), WebCreateOrder (lưu từ chối).
- 6.1: hai lần hoàn tất cùng đơn: `CompleteFlashDraft` khóa đơn `FOR UPDATE`; lần sau thấy đã `new` → trả kết quả cũ
  (Replayed, không ghi audit lần hai); cùng khóa chống trùng → bản đã lưu.
- 6.3: khóa: chỉ đường của Task 202 (đơn → … theo ADR-46); task này không thêm khóa. Vòng dựng lại chỉ đọc (không FOR).
- 6.4: lỗi tạm của database trong hoàn tất → `WriteTx` 503 không lưu. Vòng: lỗi một tenant ghi log, vòng sau làm lại.
- 6.5: READ COMMITTED; số `available` đọc ở vòng có thể cũ ≤ 2 s — chỉ là gợi ý cho website và cờ hết suất; khi cờ sai
  "còn" lượt mua xuống database; khi cờ sai "hết" (đơn vị vừa trả lại): vòng hay báo hiệu `flash_item` sửa trong ≤ 2 s,
  website gửi lại với X-Attempt ≥ 2 bỏ qua cờ (QĐ21).
- 6.6: không có việc nền mới trong hàng chờ; vòng chạy trong tiến trình api.
- 6.7: không áp dụng: không gọi dịch vụ ngoài.
- 6.8: tiến trình api chết → không ai ghi cổng → trạng thái hết hạn 10 s → `SoldOut.known = false` → lượt mua xuống
  database (test `TestLoop_StopExpiresState`). Hai tiến trình api cùng chạy vòng: cùng ghi một giá trị đọc từ database,
  ghi sau thắng, vô hại.
- 6.9–6.13: hàm trong RetryTx gán lại biến mỗi lần (out, refused, map kết quả); không gửi ra ngoài trong tx; không log
  SĐT/địa chỉ (lỗi chỉ mang mã); không cộng dồn.

## Quyết định thay chủ shop

- QĐ-204-1 `status` trả website là trạng thái **hiệu lực**: đợt `scheduled`/`open` mà giờ máy chủ ≥ `startsAt` → `open`
  (lượt mua của Task 201 cũng nhận đợt `scheduled` trong khung giờ); trước giờ mở → `scheduled`; quá `endsAt` → không
  trả. Lý do: website đếm ngược theo `serverTime`, `startsAt`; nhịp mở đợt chạy mỗi phút nên trạng thái ghi trong
  database có thể trễ. Phương án khác: trả nguyên cột `status` (website thấy `scheduled` dù đã mua được).
- QĐ-204-2 Thân hoàn tất dùng `shippingChoice` (cùng schema `WebShippingChoice` của đơn web, đối chiếu bằng `shipChoice`)
  thay cho trường `carrier` của kế hoạch: `CompleteFlashInput.Shipping` của Task 202 đã là lựa chọn giao hàng đầy đủ;
  website dùng một dạng cho cả hai đường. Phương án khác: trường `carrier` riêng (phải dịch sang cùng kiểu).
- QĐ-204-3 Hoàn tất ghi audit `order.create` (người làm: khóa dịch vụ) khi đơn nháp thành `new` (lượt mua không ghi,
  Task 203 Q8): đơn nháp chưa phải đơn thật cho tới khi hoàn tất; lần gọi lại (đã `new`) không ghi lần hai.
- QĐ-204-4 `hold_expired` (422) là kết quả cuối **được lưu** theo khóa (transaction đã hủy đơn); 404, 400 không lưu.
- QĐ-204-5 `order.ErrDuplicateOrder` (mã ngoài đã dùng cho đơn khác) ở lượt mua flash trả lời 422 và **trả khóa ngay**
  (đưa vào `flashRefusal`), không giữ khóa 60 s (Task 203 việc phát hiện thêm).
- QĐ-204-6 Món của đợt vừa đóng: vòng không ghi gì, trạng thái cũ tự hết hạn ≤ 10 s (cổng không có lệnh xóa; lượt mua
  vẫn trả `not_open` từ database).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| [FS07] vòng chạy đều: số còn ban đầu = số suất; mua hết → cờ bật, số còn 0; lượt sau sold_out | `TestLoop_SoldOutFlag` | `go test -race -count=1 ./internal/wiring/flashstate/` | trước khi viết `loop.go`: không biên dịch (gói chưa có); sau: `--- PASS: TestLoop_SoldOutFlag` |
| [FS06] đơn nháp hết hạn → order.cancelled → worker (consumer webcache) → báo hiệu flash_item → cờ tắt, còn 1; thêm suất → flash_sale → còn 3 (nhịp 1 giờ: chỉ báo hiệu làm đổi) | `TestLoop_SignalsRecompute` | như trên | `--- PASS: TestLoop_SignalsRecompute` |
| [HT27] FLUSHALL → vòng dựng lại cờ và số còn | `TestLoop_FlushAllRebuilds` | như trên | `--- PASS: TestLoop_FlushAllRebuilds` |
| Dừng vòng: quá 10 s (đồng hồ cổng) → `SoldOut.known = false`, không có số còn; lượt mua xuống database, đúng sold_out; Stop hai lần | `TestLoop_StopExpiresState` | như trên | `--- PASS: TestLoop_StopExpiresState`; `ok … flashstate 5.476s` |
| [FS02] webFlashCurrent: đợt open 2 món đủ trường; đợt chưa tới giờ `scheduled`; `serverTime`; remaining giảm ngay sau 3 lượt mua (đếm PostgreSQL); số của cổng được dùng; quá endsAt bỏ đợt; tới giờ → open; khóa thiếu quyền 403 | `TestWebFlashCurrent_Shape` | `go test -race -count=1 -run '^(TestWebFlashCurrent_Shape\|TestWebFlashCurrent_Empty\|TestWebFlashComplete\|TestWebFlashBuy_ExternalRefTakenReleasesKey)$' ./internal/httpapi/web/` | trước khi viết handler: không biên dịch; sau: `ok … internal/httpapi/web 3.245s` |
| Không có đợt → `sales: []` | `TestWebFlashCurrent_Empty` | như trên | PASS |
| [FS15] từ chối không lưu: SĐT khác → 404 not_found (không có dòng khóa chống trùng); SĐT sai dạng 400; mã lạ 404; đơn vẫn draft | `TestWebFlashComplete_Refusals` | `go test -race -count=1 -run '^(TestWebFlashCurrent_Shape\|TestWebFlashCurrent_Empty\|TestWebFlashComplete\|TestWebFlashComplete_Refusals\|TestWebFlashComplete_HoldExpired\|TestWebFlashBuy_ExternalRefTakenReleasesKey\|TestWebFlashBuy_FlushAll)$' ./internal/httpapi/web/` | `ok … internal/httpapi/web 6.656s` |
| [FS15] hoàn tất trong hạn → new, tổng 229.000, đơn vị sold, 1 audit order.create; cùng khóa → bản lưu (Idempotent-Replayed); khóa khác → cùng kết quả, không audit lần hai | `TestWebFlashComplete` | như trên | PASS |
| [FS15] sau hạn → 422 hold_expired lưu theo khóa (lần hai Idempotent-Replayed), đơn cancelled draft_expired, 3 đơn vị trả về available | `TestWebFlashComplete_HoldExpired` | như trên | PASS |
| QĐ-204-5: mã ngoài đã dùng cho đơn món khác → 422 idempotency_key_reused, khóa trả lại ngay | `TestWebFlashBuy_ExternalRefTakenReleasesKey` | như trên; bỏ `ErrDuplicateOrder` khỏi `flashRefusal` rồi chạy lại | khi bỏ: `flash_state_test.go:276 (nay dòng 306 sau khi tách test): khóa chống trùng còn giữ (1 dòng)` FAIL; khi có: PASS |
| FLUSHALL test cũ vẫn đúng sau khi dời hàm sang testkit | `TestWebFlashBuy_FlushAll` | `go test -race -count=1 -run '^TestWebFlashBuy_FlushAll$' ./internal/httpapi/web/` | PASS |
| Cổng tĩnh + test theo gói (đổi testkit → toàn bộ) | ci-task | `ci-task.sh` (từ xa, github) | lần 2: `CI-TASK XANH (từ xa trên github …)`, run 46055756581: static success, test (1)–(6) success (toàn bộ gói vì đổi testkit) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thân hoàn tất dùng `shippingChoice` thay `carrier` (QĐ-204-2); trả lời 200 `{code, status, grandTotal}` như kế hoạch.
- Thêm `promotion.FlashRemaining` (vòng ở `internal/wiring` không được nhập `internal/queries`, luật (m); đếm theo món
  bằng câu con để không gộp cả bảng đơn vị). `webcatalog.FlashAvailable` là đường "thiếu thì đếm ở PostgreSQL".
- Thêm `testkit.Valkey.FlushAll` (dời `flushAll` của test web; cùng commit vì chỉ là hàm test 30 dòng, tránh jscpd) và
  `flashtest.ScheduleItems` (đợt nhiều món cho ca "đợt open 2 món").
- Ca "≤ 2 s", "≤ 1 s" của kế hoạch kiểm theo sự kiện (chờ tới khi trạng thái đúng, chốt chống treo 30 s; luật Task
  400): nhịp nhanh 50 ms ở test; ca báo hiệu dùng nhịp 1 giờ để chứng minh chính báo hiệu làm đổi.
- Ca "dừng vòng: sau 10 s" tua đồng hồ của cổng (GateOptions.Now) thay vì chờ 10 s thật.
- Vòng ghi log lỗi một lần mỗi chuỗi lỗi (không mỗi 2 s), ghi lại khi chạy lại được.
- ci-task lần 1 (run 46054086210): mọi test xanh (6 phần), cổng tĩnh đỏ: gci (`testkit/valkey.go`), gocognit (hai test
  web → tách `checkInitial`/`checkItem`, tách ca hoàn tất thành `_Refusals`, `_HoldExpired`), wrapcheck (`MarshalJSON`),
  jscpd 5 đoạn (dùng `testkit.OpenStaff`, `must`; bỏ `nilUUID`, `markFull`; viết lại `Stop`), `TestMoneyFieldsContract`
  (giá, tổng → `$ref Money`). Lần đầu ci-task chạy nền bị dừng ở giới hạn 10 phút của lệnh nền (không phải lỗi của
  task), chạy lại với giới hạn dài.

## Việc phát hiện thêm

- (Task 201, không sửa ở đây) `order.BuyFlash` với `ExternalRef` đã có đơn flash của cùng món trả `bought` với đơn đó
  cho **mọi khách** (kể cả SĐT khác): website gửi sai mã ngoài sẽ thấy mã đơn của người khác (không thấy SĐT, địa chỉ;
  hoàn tất vẫn đòi đúng SĐT). Cân nhắc so khách của đơn và trả `ErrDuplicateOrder` khi khác khách.
- Món của đợt vừa đóng: trạng thái trên cổng còn tới 10 s (QĐ-204-6); nếu muốn tắt ngay cần thêm lệnh xóa trạng thái
  vào `cache.AdmissionGate`.

## Reviewer

Kết luận: ĐẠT (lượt 1, trên phần đã stage sau khi gộp main b07c801d và ci-task XANH run 46055756581).

- Phát hiện phải sửa: không có.
- Ảnh hưởng: khớp; thiếu ghi `webcatalog.FlashStatusAt`, `FlashScheduled`, `FlashOpen` (chỉ thêm) — đã bổ sung vào mục
  Ảnh hưởng.
- Bằng chứng: 11 dòng đều trỏ test có thật và kiểm đúng tiêu chí (Shape dùng số của cổng cho món 2, đếm PostgreSQL cho
  món 1; hold_expired lưu theo khóa khớp WriteTx).
- Ghi chú không bắt buộc: số dòng trích ở dòng QĐ-204-5 đã lệch sau khi tách test (đã sửa chữ); khi không cấu hình
  Valkey gate vòng vẫn đọc PostgreSQL mỗi 2 s và log một cảnh báo (không sai; để nguyên — production luôn có gate).
