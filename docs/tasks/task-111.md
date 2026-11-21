# Task 111 — M3-20: Đơn kẹt, Cần xem, cập nhật lần cuối

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 111, M3-20). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ thêm, không đổi ký hiệu có sẵn.
  - `fulfillment`: thêm `StuckScheduleName`, `StuckAlertKind`, `NewStuckAlertCommand`, `(*Service).ListStuck`,
    `(*Service).ListForReview`, `(*Service).ResolveAnomaly`, kiểu `StuckFilter`, `StuckShipment`, `ReviewItem`,
    `ReviewCounts`, `ReviewFilter`, `ReviewPage`, `ReviewItem`, `AnomalyResolveInput` (tên `ResolveInput` đã thuộc vùng create), `Anomaly`, sentinel `ErrAnomalyNotFound`, `ErrAnomalyNotResolvable`. Thân
    `listsHandlers`, `listsSchedules` (area_lists.go, khung của Task M3-04, 0 chỗ gọi ngoài wire.go) đổi từ nil.
  - `commands`: thêm `Status(ctx, tx, jobID)` (đọc trạng thái một lệnh trong ngữ cảnh tenant, dùng câu sqlc
    `CommandStatus` có sẵn; 0 chỗ dùng cũ). Lý do ở Quyết định 1.
  - `httpapi/errors.go` (`fulfillmentError`): thêm hai dòng ánh xạ (`ErrAnomalyNotFound` → 404 not_found,
    `ErrAnomalyNotResolvable` → 409 invalid_transition). `cmd/crm/main.go` (`alertSchedules`): thêm
    `fulfillment.NewStuckAlertCommand` cuối danh sách lệnh cảnh báo. `docs/danh-muc-dung-chung.md`: sinh lại
    (`make catalog`, có `commands.Status`).
  - `httpapi`: thân ba hàm `ListStuckShipments`, `ListShipmentsForReview`, `ResolveShipmentAnomaly`
    (handlers_shipment_lists.go, đang trả 501) — chữ ký do ogen sinh, không đổi.
- Gói phụ thuộc ngược (go list): `fulfillment` ← cmd/crm, internal/httpapi, internal/modules/cash (+ formats,
  statement), internal/adapters/carrier/** , testkit/codtest, testkit/shiptest, tools/fakecarrier/server.
  `commands` ← 10 gói (cmd/crm, httpapi, các module có lệnh). Chỉ thêm hàm nên không gói nào phải sửa.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng `listStuckShipments`,
  `listShipmentsForReview`, `resolveShipmentAnomaly` đã có từ Task M3-04); oasdiff không đổi. Xóa ba dòng khỏi
  `tests/arch/testdata/m3_pending.txt` (đã hiện thực).
- Chỗ dùng trên màn hình: chưa có (màn Đơn kẹt, Cần xem là Task 135, M3-44). `getShipment` đã trả
  `lastCarrierEventAt`, `lastSyncedAt` (Task 102/103); task này chỉ thêm test.
- Câu sqlc, bảng, pha migration: tệp mới `queries/fulfillment/lists.sql` (khối sqlc fulfillment có sẵn, sinh vào
  `internal/modules/fulfillment/internal/store`): `StuckCreatingJobs`, `SyncStuckShipments` (một câu đánh dấu/bỏ dấu
  `stuck_since`), `CountStuckByStatus`, `ListStuckShipments`, `ListOpenAnomalies`, `ReviewCounts`,
  `AnomalyForResolve`, `LockShipmentForAnomaly`, `AnomalyKindState`, `ResolveAnomaly`. Bảng: đọc/ghi `fulfillment.shipments`
  (`stuck_since`, `needs_review`, `updated_at` — đã GRANT), `fulfillment.shipment_anomalies` (status, resolution,
  resolved_by, resolved_at — đã GRANT), đọc `fulfillment.callback_tasks`, `fulfillment.carrier_accounts`,
  `ord.orders_v`. Migration `0328_fulfillment_anomalies_open_created.sql`: pha mở rộng, chỉ thêm index
  `(tenant_id, created_at, id) WHERE status = 'open'` cho danh sách Cần xem (keyset).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/...` (vùng rủi ro cao: không — trong fulfillment chỉ
  `status.go` thuộc danh sách), `./internal/platform/commands/...` (vùng rủi ro cao: cả bộ), `./internal/httpapi/...`,
  `./cmd/crm/...`, `./tests/arch/...`; rồi `scripts/agent/ci-task.sh` (chạm migration → toàn bộ test).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (con trỏ, giờ Việt Nam,
  phạm vi kho), `track_poll.go` (lịch theo tenant là lệnh, cảnh báo một lần mỗi ngày bằng `DedupeKey`),
  `handlers_inventory_aged.go` (`readableWarehouses`, `canAt`, `inventoryRead`), `refusableWrite` + `WriteTx`,
  `ResolveCreateAnomaly`/`ResolveCancelAnomaly` (đóng anomaly rồi tính lại `needs_review`), `inventory.AgedCursor`,
  `order.DuplicateCursor`, `testkit.ExplainAsApp` + cách đọc hằng SQL của `queue/besteffort_test.go`.
- Dùng lại / viết mới vì:
  - Dùng lại: `commands.CommandKind`, `queue.Enqueue` với `DedupeKey` theo ngày (như `enqueueTrackAlert`),
    `Notifier`, `severityNormal`, `vietnam`, `LoadSettings().StuckHours`, `shipstate.Display` (nhãn trạng thái),
    `readableWarehouses`, `canAt`, `refusableWrite`, `h.audit`, `fulfillmentError`.
  - Viết mới con trỏ `lists.Cursor` (mốc + mã): mỗi module có con trỏ riêng theo luật "logic nghiệp vụ ở lại trong
    module" (AgedCursor của inventory là kiểu của module khác, không nhập được từ fulfillment).
  - Viết mới `commands.Status`: không có hàm công khai nào đọc trạng thái một lệnh; module không được đọc thẳng
    `platform.jobs`.
  - Câu đóng anomaly: viết `ResolveAnomaly` theo mã anomaly (hai câu có sẵn đóng theo (vận đơn, loại) của vùng khác,
    khác lý do thay đổi), cùng khuôn "đóng rồi tính lại needs_review".
  - jscpd (lần ci-task đầu báo 9 đoạn trùng mới, đã xử lý hết, không gộp sang module khác): kiểm payload của lệnh
    lịch, lệnh cảnh báo (giống track_poll) viết lại bằng `rejected(…)` và bỏ đọc slot (giờ lấy từ đồng hồ);
    `nonNilIDs` (giống `catalog.nonNil`) bỏ hẳn — mảng nil thành NULL, `= ANY (NULL)` không khớp kho nào (test "không
    kho nào"); `pageLimit` (giống `customer.clampLimit`) viết một dòng; con trỏ (giống `inventory.AgedCursor`) giữ
    riêng vì luật module, viết bằng `AppendUint64`; test: dựng server không chép khuôn của shipment_sync_test (đồng hồ
    thật), đọc hằng SQL bằng `strings.Cut` thay cho AST như queue, kiểm `commands.Status` gộp vào `TestAbandon`.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `stuck_since`, `shipment_anomalies` thuộc fulfillment; migration chỉ thêm index ở schema fulfillment.
- 5.2 Không ghi bảng module khác: chỉ ghi `fulfillment.*`. 5.3 Đọc mã đơn qua `ord.orders_v`; trạng thái lệnh qua
  `commands.Status` (public API), không đọc `platform.jobs`.
- 5.4 Nguyên tử: đóng anomaly + tính lại `needs_review` + audit + lưu khóa chống trùng trong một `WriteTx`.
  Đánh dấu kẹt: một câu UPDATE trong transaction của lịch.
- 5.5 Không cần nguyên tử: cảnh báo ngày là lệnh (`commands`) ghi trong tx của lịch, gửi Telegram ở `Execute`.
- 5.6 Gọi ra ngoài: chỉ Notify (Alerter) trong lệnh; không gọi hãng.
- 5.7 Mọi câu trong `tenancy.WithTenant` (lịch: `db.TxWorker`; API: `TxAPI`, `WriteTx`). Không bảng mới.
- 5.8 Module không nhập HTTP; handler mỏng. 5.9 `resolveShipmentAnomaly` có `IdempotencyKey`, `x-permission:
  fulfillment.correct` (có sẵn). 5.10 không áp dụng (không Valkey). 5.11 không thư viện mới. 5.12 theo mẫu
  track_poll (lịch), cancel (đường ghi có Authorize dưới khóa), aged (danh sách theo phạm vi kho).

Mục 6:
- 6.1 Đồng thời: câu đánh dấu kẹt khóa dòng `ORDER BY id FOR UPDATE SKIP LOCKED` (dòng đang bị đường khác sửa thì
  để lượt sau), điều kiện kẹt nằm trong WHERE của chính câu khóa nên được kiểm lại trên bản mới nhất; SET dựa trên giá
  trị hiện tại (`stuck_since IS NULL` → đánh dấu, ngược lại bỏ dấu). Đóng anomaly: khóa vận đơn (`FOR UPDATE`) rồi
  `UPDATE … WHERE status = 'open'`; người thứ hai → 0 dòng → 409.
- 6.2 Gửi lại: `IdempotencyKey`; cảnh báo ngày chống trùng bằng `DedupeKey stuck-shipments:<ngày>` (unique của jobs).
- 6.3 Thứ tự khóa: chỉ `fulfillment.shipments` → `fulfillment.shipment_anomalies` (ADR-46 nhóm 8). Không khóa đơn
  (không đổi đơn). Nhiều dòng vận đơn: theo id tăng dần, SKIP LOCKED.
- 6.4 Lỗi tạm: `WriteTx`/`RetryTx` có sẵn; thân hàm gán lại biến mỗi lần chạy.
- 6.5 READ COMMITTED + câu có điều kiện. 6.6 Lệnh lịch lỗi database → `not_sent`, chạy lại trong kỳ (như poll);
  lệnh cảnh báo Notify lỗi → `not_sent`, tự gửi lại. 6.7 không áp dụng (không gọi hãng).
- 6.8 Chết giữa chừng: lịch chạy lại kỳ sau (đánh dấu tính lại từ dữ liệu, không cộng dồn).
- 6.9 Câu đánh dấu, danh sách dùng index (test ExplainAsApp 50.000 vận đơn kết thúc + 500 mở).
- 6.10 không áp dụng (không tiền). 6.11 Quy luật: test bảng VC18 đủ nhánh; rapid cho nhãn mọi loại anomaly và con trỏ.
- 6.12 Cảnh báo chỉ số lượng theo trạng thái, không mã đơn, mã vận đơn, tên khách. Audit đóng anomaly chỉ có
  loại, trạng thái; không chép `resolution` (chữ tự do có thể có SĐT) vào audit.
- 6.13 Quyền: danh sách lọc theo kho có `fulfillment.read` (không kho nào → rỗng); đóng anomaly cần
  `fulfillment.correct` (phạm vi none, guard kiểm toàn shop; thiếu → 403) và phải thấy kho của tài khoản hãng của vận
  đơn (`fulfillment.read` ở đó; không → 404).

## Quyết định thay chủ shop

1. **"Lệnh tạo expired/dead" đọc qua `commands.Status`** (hàm mới, chỉ đọc, câu sqlc có sẵn). Lý do: lệnh bị "bỏ"
   bằng `queue.AbandonJob` không gọi `OnTerminal`, nên dữ liệu của fulfillment không biết lệnh đã kết thúc (vận đơn
   giữ `create_state` pending/unknown). Phương án khác: join thẳng `platform.jobs` (trái luật đọc bảng module khác);
   chỉ dựa vào `create_state = 'not_sent'` (bỏ sót đúng ca kế hoạch đòi). Vận đơn `creating` có `not_sent` (lệnh đã
   kết thúc chắc chưa gửi, chờ "Gửi lại") cũng là kẹt.
2. **Khóa cảnh báo** `stuck-shipments:<ngày>:<tenant>` (thêm tenant sau ngày như `stock-short:<tenant>`): kế hoạch ghi
   `stuck-shipments:<ngày>`; thêm tenant để hai shop không chặn cảnh báo của nhau. Tiền tố giữ nguyên cho RUNBOOK
   (`stuck-shipments:*`). Không có vận đơn kẹt → không gửi tin (lệnh `done`).
3. **"Giờ đầu tiên sau 08:00"**: lượt lịch đầu tiên của ngày (giờ Việt Nam) có giờ ≥ 08:00 gửi cảnh báo; lỡ lượt
   08:00 (máy tắt) thì lượt sau trong ngày gửi. Phương án khác: chỉ gửi trong 08:00–08:59 (mất cảnh báo cả ngày khi
   lỡ lượt).
4. **`counts`**: `review` = anomaly mở; `stuck` = vận đơn chưa kết thúc có `stuck_since`; `callbacksOpen` = việc gọi
   lại `open` + `in_progress`; `callbacksOverdue` = trong số đó `due_at` < bây giờ; `inDoubt` = anomaly mở loại
   `in_doubt_create`, `in_doubt_cancel`. Mọi số đếm theo phạm vi kho như danh sách.
5. **Thứ tự**: Đơn kẹt sắp theo `stuck_since` tăng dần (kẹt lâu nhất trước), Cần xem theo `created_at` tăng dần (việc
   cũ nhất trước). `hoursSinceUpdate` = giờ từ `coalesce(last_carrier_event_at, last_status_at)`.
6. **Lịch cũng bỏ dấu** khi vận đơn không còn kẹt (đã kết thúc, ngưỡng nâng, đã gửi lại, anomaly stock_short đã đóng)
   — kế hoạch chỉ nói "có sự kiện mới thì về rỗng" (FinishShipmentSync); không có bước này thì vận đơn đã giao/hủy
   có thể mang dấu kẹt mãi.
7. **Mã lỗi đóng anomaly**: loại có đường riêng hay anomaly đã đóng → 409 `invalid_transition` (mã có sẵn); không có
   / tenant khác / ngoài phạm vi đọc → 404.
8. **Không đóng ở đây cả `manual_cancel`, `in_doubt_cancel`** (phát hiện reviewer vòng 1; kế hoạch chỉ nêu
   `in_doubt_create`): hai loại này có đường riêng `setShipmentManualStatus` (Hãng đã hủy / Hãng không cho hủy, Task
   106) — đường đó chỉ mở khi việc hủy tay còn mở và tự đóng việc sau khi áp. Đóng chung trước thì vận đơn kẹt mãi ở
   `cancel_requested`, không còn nút sửa. Câu hướng dẫn của hai loại chỉ tới "Ghi tay trạng thái". Phương án khác: cho
   đóng và chỉ sửa câu (vẫn để lại bẫy). `summary` của `resolveShipmentAnomaly` trong openapi.yaml chỉ nhắc
   `in_doubt_create`; không sửa ở task này (chỉ là mô tả, tránh sinh lại hợp đồng) — ghi ở Việc phát hiện thêm.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| VC18: V1 shipping 73 giờ → kẹt; V2 71 giờ → không; V3 awaiting_pickup 49 giờ → kẹt; mode manual → không; stuck_since = giờ lịch | `TestStuck_ThresholdsAlertAndClear` (`expectStuck`) | `go test -race -run 'TestStuck_\|TestReview_' ./internal/modules/fulfillment/` | `ok … fulfillment 27.598s` (sau gộp main) |
| Danh sách: V1, V3 đủ trường (hoursSinceUpdate 73/49, mã đơn, mã hãng, nhãn, lastCarrierEventAt), lọc trạng thái, tài khoản, kho (ngoài phạm vi, không kho nào → rỗng) | `TestStuck_ThresholdsAlertAndClear` (`checkFirstList`) | như trên | ok |
| Cảnh báo ngày: một tin khóa `stuck-shipments:<ngày>:<tenant>` chứa `awaiting_pickup: 1, shipping: 1`, không mã vận đơn, mã hãng, mã đơn, id; lượt hai cùng ngày không gửi thêm, không đổi stuck_since | `TestStuck_ThresholdsAlertAndClear` (`checkAlertOnce`, lượt hai) | như trên | ok |
| Sự kiện mới cho V1 → stuck_since rỗng, rời danh sách, lượt sau không đánh dấu lại (V2 qua 72 giờ thì kẹt) | `TestStuck_ThresholdsAlertAndClear` | như trên | ok |
| getShipment/ShipmentView có lastCarrierEventAt, lastSyncedAt | `TestStuck_ThresholdsAlertAndClear`, `TestShipmentListsAPI` (`checkStuckAPI`) | như trên; `go test -race -run TestShipmentListsAPI ./internal/httpapi/` | ok; `ok … httpapi 2.862s` |
| Vận đơn đã giao còn dấu → lịch bỏ dấu | `TestStuck_ThresholdsAlertAndClear` (cuối) | như trên | ok |
| Bổ sung Task 91: creating có lệnh tạo bị bỏ (AbandonJob in_doubt → expired) → kẹt; not_sent → kẹt; lệnh còn chờ → không; stock_short mở → kẹt; đóng stock_short → bỏ dấu; số đếm vào cảnh báo (`creating: 2, shipping: 1`) | `TestStuck_CreatingAndStockShort` | như trên | ok |
| Trước 08:00 giờ VN: đánh dấu, không gửi; 08:00 gửi; không có vận đơn kẹt → không gửi | `TestStuck_AlertWindow`; `TestAlertDay`, `TestAlertText` | như trên; `go test -race ./internal/modules/fulfillment/internal/lists/` | ok; `ok … lists 1.424s` |
| Phân trang đơn kẹt theo con trỏ, lâu nhất trước, không lặp; con trỏ hỏng → ErrInvalidPage (HTTP 400) | `TestStuck_ListPages`, `TestCursor_RoundTrip` (rapid), `checkStuckAPI` | như trên | ok |
| ExplainAsApp với 50.000 vận đơn đã kết thúc + 500 mở (kèm anomaly, việc gọi lại đã đóng): danh sách, đếm, câu đánh dấu đơn kẹt, danh sách và số đếm Cần xem không Seq Scan | `TestStuck_PlanUsesIndexes` | `go test -race -run TestStuck_PlanUsesIndexes ./internal/modules/fulfillment/` | `ok … fulfillment 5.879s` |
| VC16: in_doubt_create nằm trong Cần xem với câu "Không rõ hãng đã tạo vận đơn chưa — kiểm trên trang hãng rồi bấm Đã tạo / Chưa tạo"; đóng → ErrAnomalyNotResolvable / HTTP 409, vẫn mở | `TestReview_ListCountsAndInDoubt`, `checkReviewAPI`, `checkResolveAPI` | như trên | ok |
| counts: 2 cần xem, 2 kẹt, 3 gọi lại mở, 1 quá hạn, 1 không rõ (done/closed, anomaly đã đóng không đếm); kho ngoài phạm vi → 0 | `TestReview_ListCountsAndInDoubt` | như trên | ok |
| manual_cancel, in_doubt_cancel: đóng ở đây → ErrAnomalyNotResolvable, vẫn mở (đường riêng: Ghi tay trạng thái) | `TestReview_ListCountsAndInDoubt` (cuối) | như trên | ok (sau vòng 1: `ok … fulfillment 19.951s`) |
| Mọi loại anomaly của CHECK có câu riêng kèm hướng xử lý; loại lạ có câu chung; in_doubt_create, manual_cancel, in_doubt_cancel không đóng được, câu hai loại hủy chỉ tới Ghi tay trạng thái | `TestLabel_EveryKind`, `TestLabel_UnknownAndResolvable` (rapid) | `go test -race ./internal/modules/fulfillment/internal/lists/` | ok |
| resolveShipmentAnomaly: resolution bắt buộc; đóng anomaly cuối → needs_review false, còn anomaly khác → true; lần hai → 409; lạ, tenant khác → 404; Authorize nhận kho đúng, từ chối → không đổi; resolved_by | `TestReview_Resolve` | `go test -race -run 'TestStuck_\|TestReview_' ./internal/modules/fulfillment/` | ok |
| 8 lần đóng song song → đúng một thành công | `TestReview_ResolveConcurrent` | như trên | ok |
| HTTP: thiếu fulfillment.correct → 403; không thấy kho → 404; thiếu resolution → 400; cùng khóa trả lại 200; khóa mới → 409; audit không chép lý do (SĐT) | `TestShipmentListsAPI` (`checkResolveAPI`) | `go test -race -run TestShipmentListsAPI ./internal/httpapi/` | ok |
| commands.Status: pending, in_doubt, expired sau Abandon; tenant khác, mã lạ → ErrCommandNotFound | `TestAbandon` (`expectStatus`) | `go test -race -run 'TestAbandon$' ./internal/platform/commands/` | `ok … commands 1.741s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Văn bản cảnh báo ngày xếp theo thứ tự trạng thái cố định (creating, awaiting_pickup, shipping, …): "awaiting_pickup:
  1, shipping: 1" (kế hoạch viết "shipping: 1, awaiting_pickup: 1"; cùng nội dung).
- Test có database đặt ở gói `fulfillment_test` (`lists_stuck_test.go`, `lists_review_test.go`) vì dựng vận đơn cần
  `shipEnv`; `internal/lists/{stuck_test.go,review_test.go}` chỉ test phần thuần (nhãn, giờ gửi, văn bản cảnh báo,
  con trỏ).
- `internal/httpapi/handlers_lists.go` của kế hoạch là tệp danh sách mẫu (catalog); khung M3-04 đặt ba handler ở
  `handlers_shipment_lists.go` nên sửa tệp đó.

## Việc phát hiện thêm

- `summary` của `resolveShipmentAnomaly` (openapi.yaml) chỉ nói `in_doubt_create` → 409; thực tế cả `manual_cancel`,
  `in_doubt_cancel` (Quyết định 8). Sửa mô tả ở task chạm hợp đồng sau (Task 135 màn Cần xem).
- Đóng anomaly không phát `shipment.changed` lên realtime (needs_review đổi); màn Cần xem (Task 135) nên tải lại danh
  sách sau khi đóng.

## Reviewer

- Vòng 1: CẦN SỬA — câu hướng dẫn `manual_cancel` bảo "đóng việc" và `resolveShipmentAnomaly` cho đóng
  `manual_cancel`/`in_doubt_cancel`, làm mất đường `setShipmentManualStatus` (vận đơn kẹt `cancel_requested`). Đã sửa
  (Quyết định 8, test). Ghi chú tùy chọn đã làm: `ReviewCounts` không còn CTE quét mọi vận đơn, mỗi số đếm đi qua index;
  thêm `listOpenAnomalies`, `reviewCounts` vào test ExplainAsApp.
- Vòng 2: **ĐẠT**, không phát hiện phải sửa. Reviewer kiểm lại `Resolvable` (ba loại có đường riêng), câu hướng dẫn hai
  loại hủy, đọc lại loại/trạng thái sau khóa vận đơn, `ReviewCounts` năm câu con qua index, đủ bảng bằng chứng.
  Ghi chú không bắt buộc: câu 6.13 lệch mã (đã sửa câu chữ ở trên); văn bản cảnh báo xếp theo thứ tự trạng thái
  ("awaiting_pickup: 1, shipping: 1", kế hoạch ghi ngược thứ tự, cùng nghĩa); `checkReviewAPI` lấy phần tử đầu trước
  khi kiểm độ dài (test vẫn đỏ đúng chỗ).
