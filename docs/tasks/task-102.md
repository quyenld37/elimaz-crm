# Task 102 — M3-11: Lệnh tra trạng thái và tra bù theo lịch

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 102, M3-11). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `fulfillment.NewRefreshCommand(o CommandOptions)` → `NewRefreshCommand(s *Service)`: lệnh tra cần tài khoản đã
    giải mã (`ResolveAccount`), `ApplyTracking`, `AttachTrackingCode`, đồng hồ của Service. 3 chỗ gọi:
    `internal/modules/fulfillment/wire.go:25` (dời sang `trackHandlers()` của `area_track.go`),
    `internal/modules/fulfillment/env_test.go:64`, `tests/e2e/stack_test.go:186`. Kiểu `RefreshCommand` giữ tên, thân
    đổi (Execute dùng `Track`, OnDone dùng `ApplyTracking`); bỏ kiểu riêng `refreshResult` của M1.
  - `RefreshCommandKind()` (giữ tên, là `TrackKind` của kế hoạch — Quyết định 3): chính sách đổi thành
    `CommandKind("fulfillment.refresh_status", 30m, 20s)` + `OrderBestEffort`. Chỗ dùng: `create_shipment.go`
    (`enqueueRefresh`), `webhook_source.go` (qua `enqueueRefresh`).
  - Mới: `Service.SyncShipment`, `NewTrackAlertCommand`, `TrackAlertKind`, `PollScheduleName`, hằng mã lỗi đồng bộ.
  - `trackHandlers()` trả lệnh tra (khi có adapter) và lệnh tra bù `fulfillment.poll` (khi có database);
    `trackSchedules()` trả lịch `fulfillment.poll` (600 s, theo tenant).
  - `cmd/crm/main.go` (`alertSchedules`): thêm `fulfillment.NewTrackAlertCommand(d, alerter, logger)` cạnh
    `NewAuthAlertCommand`. `cmd/crm/wire_m3_test.go` không đổi: test dựng `m3Deps{}` không database nên lệnh, lịch
    tra bù (chỉ có khi có database) không xuất hiện; lệnh `fulfillment.refresh_status` vẫn có khi có hãng giả.
  - `httpapi`: `SyncShipment` (handlers_shipment_sync.go) thay thân 501.
  - Xóa câu sqlc `InsertShipmentEvent` (`queries/fulfillment/events.sql`, chỉ lệnh tra M1 dùng; nay `ApplyTracking` ghi
    qua `InsertTrackedEvent`). Test: `ownerExec` dời từ `accounts_test.go` (accEnv) lên `shipEnv` (`env_test.go`) dùng
    chung với `trackEnv` (jscpd); `shipEnv.useCommands`; `export_test.go` mở hai hàm phân loại lỗi cho test bảng.
- Gói phụ thuộc ngược (go list): `fulfillment` ← `cmd/crm`, `internal/httpapi`, `internal/modules/cash`,
  `internal/adapters/carrier/{,best,spx,vtp,fake,manual,carriertest}`, `internal/testkit/shiptest`,
  `internal/platform/files` (test), `tests/e2e`, `tools/fakecarrier/server`. Chỉ `cmd/crm`, `tests/e2e`, `httpapi` gọi
  ký hiệu đổi chữ ký.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng (`syncShipment` đã khai ở Task 95: 202 `JobAccepted`,
  `fulfillment.read`, Idempotency-Key bắt buộc). Hiện thực `syncShipment`; xóa dòng của nó khỏi
  `tests/arch/testdata/m3_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (nút "Tra lại" và dòng "cập nhật lần cuối" là task console sau; `lastSyncedAt`,
  `lastSyncError` có ở view `fulfillment.shipments_v`, `getShipment` mở rộng ở Task 103).
- Câu sqlc, bảng, pha migration: `queries/fulfillment/poll.sql` mới — `TrackTarget` (vận đơn + tài khoản cho lệnh
  tra), `PollCandidates` (chọn vận đơn tra bù, tối đa N mỗi tài khoản), `CountUnsynced` (gauge), `SetSyncError`,
  `CountSyncErrors` (nội dung cảnh báo). Bảng chạm: `fulfillment.shipments` (`last_sync_error`, đọc
  `last_synced_at`…), `fulfillment.carrier_accounts` (đọc; `MarkAuthFailed`), `fulfillment.shipment_anomalies`,
  `platform.jobs`. Migration mới `0312_fulfillment_anomaly_stock_short.sql` (pha mở rộng: nới CHECK `kind` thêm
  `stock_short`, Quyết định 5).
- Test sẽ chạy: `go test -race ./internal/modules/fulfillment/...`, `./internal/httpapi/ -run 'Shipment|M3'`,
  `./cmd/crm/ -run WireM3`, `./tests/arch/...`; rồi `ci-task.sh` (migration mới → cổng chạy toàn bộ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` — `db.Savepoint`
  (bước "tất cả hoặc không" trong transaction lớn, đúng mục "khi nào dùng"), `db.TransientCode`, `logging.ScrubText`,
  `queue.ErrorText`, `tenancy.WithTenant`, `commands.CommandKind`/`Handler`/`Looker`/`DoneHook`,
  `queue.NewQueue().Enqueue` (chống trùng theo DedupeKey), `schedules.Schedule`, `metrics.Registry.Gauge`; trong
  fulfillment: `ApplyTracking`, `AttachTrackingCode`, `ResolveAccount`, `MarkAuthFailed`, `openAnomaly`,
  `LoadSettings` (`PollMinutes`), `safeCode`, `enqueueRefresh`, `shipmentOrderKey`, `Notifier`; lớp HTTP:
  `refusableWrite`, `canAt`, `h.audit`, `fulfillmentService`. Máy chủ hãng giả `tools/fakecarrier/server` +
  `adapters/carrier/fake.Client` như test của Task 100.
- Dùng lại / viết mới vì: áp kết quả chỉ qua `ApplyTracking`/`AttachTrackingCode` (Task 99), tài khoản chỉ qua
  `ResolveAccount` (Task 100). Viết mới: lệnh tra M3 (thay thân lệnh M1, giữ tên loại việc), lệnh tra bù, lệnh cảnh
  báo đồng bộ (theo mẫu `authAlert`, không gộp: payload và nội dung khác, đếm từ database). Không thêm thư viện.

## Soát thiết kế

- 5.1, 5.2: dữ liệu ghi đều của fulfillment (`shipments.last_sync_error`, `shipment_anomalies`); đơn, tồn chỉ đổi qua
  `ApplyTracking` → `order.Writer` (public API).
- 5.3: không đọc bảng module khác (chỉ `inventory.warehouses_v` có sẵn trong `ResolveAccount`).
- 5.4: `OnDone` áp kết quả trong transaction của Runner (cùng lúc đổi việc sang done); `AttachTrackingCode` +
  `ApplyTracking` trong một savepoint: lỗi nghiệp vụ lặp lại thì lùi savepoint, ghi `last_sync_error`/anomaly trong
  chính transaction đó, việc vẫn done (Quyết định 6).
- 5.5, 5.6: gọi hãng chỉ trong `Execute`, ngoài transaction (đọc vận đơn, tài khoản trong transaction ngắn trước đó);
  tra là đọc nên `AutoRetryNotSent = true`, `Lookup` luôn `not_sent`. Cảnh báo là lệnh riêng
  `fulfillment.track_alert` (Notify ngoài transaction).
- 5.7: mọi câu trong `tenancy.WithTenant`; SQL tĩnh trong `queries/fulfillment/poll.sql`.
- 5.8: fulfillment không nhập `net/http`; handler `SyncShipment` mỏng (kiểm quyền theo kho, gọi Service, audit).
- 5.9: `syncShipment` có Idempotency-Key (hợp đồng) và khóa chống trùng của việc theo phút.
- 5.10, 5.11: không Valkey, không thư viện mới.
- 5.12: theo mẫu lệnh `commands` (Task 12), lệnh kiểm của Task 100, `order.expire_drafts` (lịch theo tenant).
- 6.1 khóa: `ApplyTracking`/`AttachTrackingCode` giữ thứ tự ADR-46 (đơn nhóm 7 → vận đơn nhóm 8 → số dư nhóm 11);
  `SetSyncError`, `openAnomaly` chỉ chạm dòng vận đơn đã khóa (hoặc một câu UPDATE một dòng); `platform.jobs` cuối.
  Lệnh tra bù chỉ đọc vận đơn không khóa rồi ghi việc.
- 6.2 chống trùng: việc tra `DedupeKey` `poll:<id>:<kỳ>`, `manual:<id>:<phút>`, `evt:<mã sự kiện>`; sự kiện hãng chống
  trùng bằng khóa sự kiện (`ApplyTracking`); cảnh báo `<loại>:<tenant>:<ngày>`.
- 6.3: không thêm khóa nhiều bảng mới; không cần test deadlock mới (đường khóa là của Task 99 đã có test).
- 6.4 lỗi tạm của database trong `OnDone`: trả lỗi lên (Runner: in_doubt → resolver tra lại `not_sent` → chạy lại);
  lỗi nghiệp vụ lặp lại (tồn thiếu, kết quả sai dạng, ràng buộc) không trả lên để khỏi tra mãi.
- 6.6, 6.7: lỗi của lệnh: 429 → `not_sent` theo `Retry-After`; 401/403 → `rejected` + `MarkAuthFailed`; quá giờ, lỗi
  mạng, 5xx → `not_sent` (8 lượt, hết hạn 30 phút → expired); 4xx khác → `rejected`. Mọi nhánh ghi `last_sync_error`
  (mã ngắn), `last_synced_at` giữ giờ cũ.
- 6.8: tiến trình chết giữa lúc gọi hãng → in_doubt → `Lookup` `not_sent` → chạy lại (đọc, không hại).
- 6.9: `Execute` 20 s (Timeout của loại); lệnh tra bù chỉ một transaction ngắn mỗi tenant.
- 6.10: tiền COD, phí thật do `ApplyTracking` ghi (int64 đồng); lệnh tra chỉ chuyển nguyên số hãng báo.
- 6.11: quy luật "trạng thái chỉ lấy từ API, không từ webhook" (VC17), "áp đúng một lần" do khóa sự kiện của Task 99;
  test ca theo kế hoạch.
- 6.12: kết quả lệnh (`platform.jobs.result`) chỉ có mã hãng, mã trạng thái, khóa sự kiện, giờ, số tiền; chuỗi tự do
  của hãng qua `logging.ScrubText`; `last_sync_error`, `last_error`, nội dung cảnh báo chỉ mã và số lượng.
- 6.13: `syncShipment` cần `fulfillment.read` ở kho của tài khoản hãng của vận đơn (canAt), tenant khác → 404.

## Quyết định thay chủ shop

1. **Mã nằm ở gói `fulfillment` (`refresh_status.go`, `track_poll.go`), không ở `fulfillment/internal/track`.** Lệnh tra
   cần `Service` (`ApplyTracking`, `AttachTrackingCode`, `ResolveAccount`), kiểu công khai ở gói gốc; gói `internal` con
   không nhập được gói cha (vòng nhập). Cùng cách Task 99, 100. Test ở `fulfillment/track_{command,poll}_test.go`.
2. **Thay thân lệnh M1 chứ không thêm lệnh mới**: `RefreshCommand` giữ tên, `NewRefreshCommand` nhận `*Service`; lệnh
   dời khỏi danh sách M1 trong `wire.go` sang `trackHandlers()` (một dòng ở `wire.go`). Cách cũ (`refreshResult`,
   `Advance` của M1 trong lệnh tra) xóa hẳn. Đảo: quay lại commit trước.
3. **`TrackKind` của kế hoạch là `RefreshCommandKind()` (giữ tên có sẵn)**, chính sách mới đúng kế hoạch (30 phút,
   20 s) và **giữ `OrderBestEffort` của M1**: một lần tra dead không chặn lần tra sau của cùng vận đơn (cùng OrderKey
   `shipment:<id>` với lệnh tạo). Phương án khác: đổi tên thành `TrackKind` (thêm một commit đổi tên, không thêm giá trị).
4. **Lỗi lặp lại khi áp (Q-T91-11, ghi chú Task 99) → việc `done`, không `dead`.** `OnDone` chạy `AttachTrackingCode` +
   `ApplyTracking` trong `db.Savepoint`; lỗi nghiệp vụ (tồn thiếu, kết quả sai dạng, ràng buộc) → lùi savepoint, ghi
   `last_sync_error` (`stock_short` thêm anomaly `stock_short`), ghi lệnh cảnh báo trong ngày, việc done. Lý do: `OnDone`
   trả lỗi thì Runner đưa việc vào `in_doubt`, resolver tra lại (`Lookup` của lệnh đọc luôn `not_sent`) rồi chạy lại —
   tra mãi; muốn `dead` từ `OnDone` phải sửa `platform/commands` (vùng rủi ro cao, ngoài phạm vi). Done + mã lỗi + cảnh
   báo dừng vòng lặp, người xử lý vẫn thấy (Cần xem, cảnh báo), tra bù theo lịch thử lại khi đã sửa tồn. Kế hoạch ghi
   "transaction riêng (RetryTx mới)"; savepoint trong transaction của Runner cho cùng kết quả mà nguyên tử với việc
   đổi sang done. Lỗi tạm của database (40P01, 55P03, 40001, nhóm 08/53/57, lỗi nối, ctx) vẫn trả lên để chạy lại.
5. **Migration `0312_fulfillment_anomaly_stock_short.sql`**: CHECK `kind` của `shipment_anomalies` (0300) chưa có
   `stock_short`. Pha mở rộng (nới CHECK). Số 0312 để chừa 0310, 0311 cho Task 114, 122 chạy song song.
6. **Mã lỗi đồng bộ ghi ngay trong `Execute`** (transaction ngắn sau lời gọi hãng, hạn riêng 5 s): nhánh `not_sent`
   không có hook của Runner nên kế hoạch "OnTerminal ghi last_sync_error" không phủ lần thử chưa kết thúc (VC18 cần
   `timeout` ngay lần đầu). Không ghi được → `not_sent` (đọc lại sau, giữ `Retry-After`), để `MarkAuthFailed` không bị
   mất. Vì vậy lệnh tra không cần `OnTerminal`.
7. **Lệnh của lịch `fulfillment.poll` là lệnh `commands`** (module không có chỗ đăng ký handler transactional; khung
   Task 95 chỉ có lệnh, handler vào, consumer, lịch, dọn): `Execute` tự mở một transaction tenant để chọn và ghi việc
   (chống trùng theo kỳ, chạy lại an toàn), không gọi hãng; hết hạn sau một kỳ.
8. **Gauge `shipments_unsynced_over_threshold`** (nhãn: mã tenant) = số vận đơn đủ điều kiện tra bù mà chưa đồng bộ hay
   cũ hơn **2 × PollMinutes** của trạng thái (đã lỡ ít nhất một lượt). Kế hoạch không nêu ngưỡng.
9. **Cảnh báo đồng bộ**: lệnh mới `fulfillment.track_alert` (cmd/crm nối cùng Alerter), khóa `stock-short:<tenant>` (và
   `track-apply-failed:<tenant>` cho lỗi áp khác), một lệnh mỗi (mã, tenant, ngày giờ Việt Nam); nội dung đếm lúc gửi
   số vận đơn chưa kết thúc mang mã đó — chỉ số lượng.
10. **`syncShipment` cần `fulfillment.read` ở kho của tài khoản hãng (403 khi khác kho)**, như `getShipment`; ghi audit
    `fulfillment.shipment.sync` (jobId) mỗi lần bấm. Vận đơn đã kết thúc vẫn 202 (lệnh tra done không gọi hãng).
11. **Vận đơn chưa có mã hãng**: adapter có `LookupByRef` (hay không có `Track`) → tra theo mã vận đơn của CRM trước để
    lấy mã hãng rồi `Track`; không tìm thấy → `not_sent` `carrier_not_found` (như M1). Vận đơn `delivered` không có
    `delivered_at` (dòng M1) lấy `last_status_at`, rồi `created_at` làm mốc 7 ngày.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, đỏ trước khi có mã | mọi test dưới | `go vet ./internal/modules/fulfillment/` lúc chỉ có test | FAIL biên dịch: `undefined: fulfillment.NewTrackAlertCommand`, `SyncShipment undefined`, `PollScheduleName` |
| VC05: PickedUp, `last_synced_at` cũ 3 giờ → lịch tra bù ghi 1 việc → V1 shipping, tồn in_transit, `last_synced_at` = giờ chạy; lượt sau 10 phút → 0 việc | `TestPoll_PickedUp` | A | PASS |
| VC17: API InTransit, webhook "Delivered" → V1 shipping, đơn confirmed, không sự kiện delivered | `TestTrack_WebhookOnlySignals` | A | PASS |
| VC18: hãng treo → việc pending lượt sau (attempts 1), `last_sync_error = timeout`, `last_synced_at` trong `shipments_v` giữ giờ cũ | `TestTrack_CarrierHangs` | A | PASS |
| 429 Retry-After 7 → pending, `run_at` cách 6–8 s (đồng hồ database), `rate_limited` | `TestTrack_RateLimited` | A | PASS |
| 401 → rejected auth, tài khoản auth_failed, lệnh cảnh báo carrier-auth | `TestTrack_AuthRevoked` | A | PASS |
| Tài khoản paused → dead account_unavailable, không gọi hãng; delivered 8 ngày → done không gọi hãng | `TestTrack_SkipsWithoutCalling` | A | PASS |
| Lịch: 450 vận đơn cũ → 200 việc (chưa đồng bộ trước, rồi cũ nhất); cùng kỳ → 0; delivered 8 ngày, vừa đồng bộ → không chọn; DedupeKey `poll:<id>:<kỳ>`; gauge = 450 | `TestPoll_BudgetPerAccount` | A | PASS |
| Lịch bỏ tài khoản paused, mode manual | `TestPoll_SkipsUnavailableAccounts` | A | PASS |
| VC10: V4 creating (lệnh tạo in_doubt), hãng có kiện theo mã V4 và PickedUp → V4 có mã hãng, shipping; lệnh tạo sau đó done → version, sự kiện không đổi, 0 anomaly | `TestTrack_InDoubtCreateFoundByCode` | A | PASS |
| `syncShipment` 5 lần trong một phút → 1 việc, phút sau việc mới; vận đơn lạ ErrNotFound | `TestTrack_ManualSyncOncePerMinute`, `TestSyncShipmentAPI` (HTTP: 5 × 202 cùng jobId, 1 việc, 5 audit, 403 khác kho, 403 chỉ order.read, 404 vận đơn lạ) | A, B | PASS |
| Q-T91-11: ErrStockShort → việc done, vận đơn giữ nguyên, `stock_short`, 1 anomaly mở, 1 cảnh báo `stock-short:<tenant>` trong ngày | `TestTrack_StockShort` | A | PASS |
| Kết quả lệnh tra không dữ liệu cá nhân | `TestTrack_ResultHasNoPersonalData` | A | PASS |
| Phân loại lỗi tạm của database (40P01, 55P03, 40001, 57014, 57P01, 57P03, 53300, 08006, lỗi nối, ctx) và lỗi lặp lại | `TestTrack_TransientDBError` (16 ca) | A | PASS |
| Phân loại lỗi gọi hãng | `TestTrack_ClassifyTrackError` (11 ca) | A | PASS |
| Lệnh tra M1 giữ hành vi (trùng sự kiện, tín hiệu giả, tin muộn) qua đường mới | `TestRefresh_SameEventThreeTimes`, `TestRefresh_ForgedSignal`, `TestRefresh_LateShippingAfterDelivered` | A | PASS |
| Cổng của task | toàn bộ cổng tĩnh + test (migration mới → toàn bộ) | C | xem dưới |

Lệnh A: `go test -race -count=1 ./internal/modules/fulfillment/ -run 'TestTrack_|TestPoll_|TestRefresh_' -v` → 17 `--- PASS`,
`ok elimaz.com.vn/crm/internal/modules/fulfillment 7.311s`; cả gói `go test -race -count=1 ./internal/modules/fulfillment/...`
→ `ok … 31.682s`. Lệnh B: `go test -race -count=1 ./internal/httpapi/ -run 'TestSyncShipmentAPI|TestShipmentAPI|TestM3' -v`
→ `--- PASS: TestSyncShipmentAPI`, `TestM3Routes_PendingList`, `ok … 3.841s`. Lệnh C: ci-task của task (lần 1: mọi gói
test `ok` kể cả `tests/e2e`, `tests/arch`; đỏ `gen-check` vì chưa stage và `jscpd` 3 đoạn trùng mới → đã sửa; lần 2
sau khi stage: build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard, test gói đổi
96 giây → `CI-TASK XANH (so với điểm tách nhánh fe31a7f…)`).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Tệp: `fulfillment/{refresh_status.go (viết lại), track_poll.go}` thay cho `internal/track/{command.go,poll.go}`; test
  `fulfillment/track_{command,poll}_test.go`, `httpapi/shipment_sync_test.go` (Quyết định 1).
- Thêm: migration `0312` (Quyết định 5), `cmd/crm/main.go` (nối lệnh cảnh báo), `wire.go` (một dòng, Quyết định 2),
  `tests/e2e/stack_test.go`, `env_test.go` (chữ ký `NewRefreshCommand`), `tests/arch/testdata/m3_pending.txt`.
- ErrStockShort: việc done trong savepoint thay cho "transaction riêng (RetryTx mới)" (Quyết định 4); lỗi áp khác
  cũng done + `apply_failed` + cảnh báo (ghi chú Task 99: không tra mãi).
- `last_sync_error` ghi ở `Execute`, không ở `OnTerminal` (Quyết định 6).
- VC10: mã hãng của máy chủ giả là `FC…` (máy chủ tự sinh), test so với mã hãng giả trả khi tra theo mã vận đơn, không
  phải chuỗi `FK0004`.
- VC18: client HTTP của test bỏ sau 1 s (máy chủ treo 30 s) để test không phải chờ hết 20 s của lệnh; cùng nhánh lỗi
  (quá giờ) với hạn 20 s của Runner.

## Việc phát hiện thêm

- Chưa có index cho câu chọn tra bù (`shipments` theo `carrier_account_id`, `status`, `last_synced_at`); quy mô hiện
  tại (vài nghìn vận đơn mở) đủ nhanh, nên thêm index `CONCURRENTLY` (tệp riêng) khi số vận đơn mở lớn.
- `fulfillment.Deps` không có Logger: lỗi áp bị nuốt thành `apply_failed` (có cảnh báo) mà không có dòng log chi tiết
  của lỗi gốc; nên thêm Logger vào Deps (sửa `module.go`, `wire_m3.go`) ở task chung.
- `cmd/crm/wire_m3_test.go` chưa có ca có database kiểm lệnh, lịch `fulfillment.poll` được nối (reviewer Task 102).
- `getShipment` (Task 103) cần trả `lastSyncedAt`, `lastCarrierEventAt`, `lastSyncError` (đã có ở view).

## Reviewer

Kết luận: ĐẠT (lần 1, agent `reviewer` trên `git diff --cached`, 21 tệp).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp thực tế; ghi chú: mục Ảnh hưởng từng ghi sửa `cmd/crm/wire_m3_test.go` mà tệp không đổi → đã sửa dòng
  đó trong tệp task (test không có ca có database kiểm lịch tra bù được nối: ghi vào Việc phát hiện thêm).
- Bằng chứng: 16 dòng đều trỏ test có thật và kiểm đúng tiêu chí; không test nào bị xóa, tắt, nới.
- Quyết định 4 (savepoint, việc done khi lỗi lặp lại) được xác nhận không trái ADR-46 (khóa trong savepoint nhả khi lùi).
- Ghi chú không bắt buộc (không sửa): production chưa có adapter nên nút "Tra lại" ghi việc không handler (như lệnh tạo
  M1); `Execute` đọc `*j.TenantID` sau `parsePayload` (đã kiểm nil ở đó).
