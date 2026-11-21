# Task 446 — Test chập chờn đợt 3 và rà trộn đồng hồ Go, PostgreSQL

Kế hoạch: task chung của điều phối viên (21/11), không có trong kế hoạch M1; số toàn cục `x-flaky3` (đã thêm dòng
`| chung (21/11) | x-flaky3 | 446 | Test chập chờn đợt 3, rà trộn đồng hồ |` vào
`../../docs/superpowers/plans/danh-so-task-toan-cuc.md`, tệp ngoài repo crm). Loại việc (quy trình mục 3): A

Tên task trong lệnh giao có dấu `/` ("Go/PostgreSQL") làm `new-task.sh` lỗi sed (tệp rỗng); tạo lại với tên "Go,
PostgreSQL" (Việc phát hiện thêm).

## Gốc đã đo (trước khi viết test)

- **Đồng hồ PostgreSQL lùi.** Vòng `clock_timestamp()` 60 s trong một phiên PostgreSQL của crm-shared (máy ảo colima):
  `backward 4 worst 00:00:00.355016` — đồng hồ máy chủ database lùi 4 lần/phút, có lần 0,36 s (máy ảo chỉnh giờ).
  Việc ghi `run_at = now()` (giờ bắt đầu transaction ghi); lần nhận việc ngay sau (`ClaimJobs`: `run_at <= now()`) có
  thể lấy `now()` **nhỏ hơn** → việc chưa tới hạn, `RunOnce` trả 0, các `drain()` riêng dừng → lệnh còn `pending`.
  Đúng ba lỗi (1), (2): `TestShipment_CreateRejectedCancels` ("lệnh tạo = pending"), `TestShipment_InDoubtRetryLooksUpFirst`
  ("lệnh = pending"), `TestCancel_EventRedelivered` ("lệnh hủy = 0": `shipped()` dừng drain khi lệnh tạo còn pending →
  vận đơn chưa awaiting_pickup → consumer không ghi lệnh hủy). Cùng gốc với queue `TestBestEffortRetryingHeadDoesNotBlock`
  (Task 445 ghi: "tin 1 chưa ở trạng thái chờ thử lại").
- **Mốc tệp theo giây.** `pg_stat_file(...PG_VERSION).modification` cắt xuống giây (đo 40 lần tạo database: mtime −
  now() từ −1027 ms tới −63 ms). `TestCleanupStaleDropsOnlyIdleOldDatabases` gọi dọn với ngưỡng 0 (`modification <
  now()`); một lần lùi 0,36 s ngay sau lúc tạo ở đầu giây → database chưa "đủ cũ" → mẫu `tpl` không bị dọn → `DROP` lúc
  dọn test báo 42809 "cannot drop a template database".
- **Luật cảnh báo trộn đồng hồ** (Task 445 Việc phát hiện thêm): `rules.go` so `finished_at`, `run_at` (PostgreSQL ghi),
  `backup_runs.finished_at` (`now()`), thống kê `pg_stat_activity`/`pg_stat_archiver`, `error_events.first_seen_at`
  (`now()`) với `RuleOptions.Now` (đồng hồ Go); `alert_events.sent_at` của luật `dead:`/`in-doubt:` ghi bằng `o.Now`
  (Go) nhưng chống lặp (`AlertSentWithin`) so với `now()` của PostgreSQL. Test `TestRuleDeadNoPayload` ghi `finished_at`
  bằng Go, `sent_at` bằng `now()`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): **testkit** thêm `DrainDue(t, owner *db.DB, w *queue.Worker)`
  và `MakeDueSoon(t, owner *db.DB) int64` (mới); `(*Staff).DrainJobs` giữ chữ ký (11 chỗ gọi), thân dùng chung vòng
  lặp với `DrainDue`. **observability**: `RuleOptions` thêm trường không xuất `dbNow` (không đổi chỗ dựng nào: mọi chỗ
  dựng dùng tên trường — `cmd/crm/main.go:344`, `alertjobs`, test); `CheckRules`, `ErrorRules` giữ chữ ký (`ErrorRules`
  trước có 1 chỗ gọi `rules.go:271`, nay `rules.go` gọi `errorRulesAt` không xuất với đồng hồ đã đọc; còn
  `summary_test.go` gọi `ErrorRules`). Mã sinh sqlc thêm `DBNow` (gói `internal/platform/internal/store`).
  `export_test.go` `ReadPGStats` (chỉ test) tự đọc đồng hồ database.
- Gói phụ thuộc ngược (go list): testkit được mọi gói test có database nhập — chỉ thêm hàm, không đổi hàm cũ; nhập
  `observability`: `cmd/crm`, `internal/httpapi`, `internal/modules/{cash,fulfillment,inventory}`,
  `internal/platform/{metrics,queue,webhooks}`, `observability/alertjobs` — API không đổi, chỉ cần build.
- Hàm drain riêng của test sẽ đổi sang dùng chung (chỉ tệp `_test.go`): `fulfillment/env_test.go` `drain`,
  `platform/queue/ordering_test.go` `drain`, `platform/files/process_helpers_test.go` `drain`,
  `platform/events/emit_test.go` `drain`, `platform/webhooks/split_test.go` `drain`,
  `httpapi/handlers_files_verify_test.go` `drain`, `adapters/carrier/webhook_test.go` `drain`,
  `modules/order/duplicates_test.go` `drain`; vòng không mang tên drain (rà `RunOnce … n == 0`):
  `platform/schedules/cleanup_test.go` `runCleanup`, `platform/webhooks/routed_test.go` (vòng trong test),
  `httpapi/handlers_customer_import_test.go` `runImporter`, `httpapi/carrier_accounts_test.go` `caEnv.run`. Giữ
  nguyên: `cash/internal/statement/confirm_test.go` `drainQuiet` (chạy trong goroutine nên không được gọi `t.Fatal`;
  sau nó test luôn gọi `DrainJobs` ở goroutine chính, dừng sớm vô hại). Đã dùng `DrainJobs` sẵn:
  `catalog/imports_test.go`, `customer/importer_test.go`, `httpapi/create_shipment_test.go`,
  `httpapi/handlers_cod_statements_test.go`, `cash/internal/statement/{match,import}_test.go`.
- Test của observability đổi theo: `helpers_test.go` `e.job` (ghi mốc theo `now()`; chỗ gọi ở `rules_test.go`,
  `digest_test.go`, `deephealth_test.go`), `rules_eval_test.go` (đặt `dbNow`), `export_test.go` (`CheckRulesAt`).
  testkit: `pgtemplate_test.go`, `pgseed_test.go` (ngưỡng dọn).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: thêm câu `DBNow` (`SELECT now()`) trong `queries/platform/ops.sql`. Không đổi bảng,
  không migration.
- Mã chạy đổi: chỉ `internal/platform/observability/{rules.go,errorrules.go,pgwatch.go,pgwatch_dynsql.go}` (mỗi mốc so
  với đồng hồ của bên ghi). Hàng đợi (`ClaimJobs`) **không đổi** (xem Quyết định).
- Test sẽ chạy: `go test -race` các gói trên (fulfillment, queue, files, events, webhooks, httpapi -run liên quan,
  carrier, order -run TestDuplicates, cash statement, testkit, observability); lặp ≥ 30 lần dưới tải mỗi test sửa; rồi
  ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không phủ testkit
  (`make catalog` không đổi gì); tìm theo tên thấy `(*Staff).DrainJobs` (`internal/testkit/queue.go`, Task 429: đưa
  **mọi** việc hẹn về hạn). grep `func .*drain` thấy 10 vòng riêng (liệt kê ở Ảnh hưởng) cùng một việc
  "chạy worker tới khi hết việc tới hạn", mỗi vòng dừng ở `RunOnce == 0`; `files` `makeDue` (SKIP LOCKED) và `events`
  drain (đưa mọi việc về hạn trước mỗi lượt) là hai biến thể. Đồng hồ database: `queries/ord/create.sql`,
  `queries/customer/write.sql` có `SELECT now()` riêng của module (sqlc theo khối, platform không dùng được store của
  module) → viết `DBNow` trong khối platform.
- Dùng lại / viết mới vì: `DrainJobs` không dùng được cho mọi test: test queue, files, fulfillment cố ý giữ việc hẹn
  thử lại (vd. `TestBestEffortRetryingHeadDoesNotBlock` muốn tin 1 còn pending 6 giờ; lệnh hủy AutoRetryNotSent). Viết
  `DrainDue` cùng vòng lặp với `DrainJobs` (một hàm nội bộ `drain`, hai cách đưa về hạn): chỉ việc hẹn trong 500 ms
  tới (lùi của đồng hồ, không phải lịch thử lại — lịch nhỏ nhất trong mã là 1 s của `files.retryDelay`) được đưa về
  hạn, bằng `FOR UPDATE SKIP LOCKED` như `files` `makeDue` (không chờ khóa của worker chạy song song). `MakeDueSoon`
  xuất ra cho vòng phải giữ cách xử lý lỗi riêng (`webhooks` split: lỗi việc cố ý, ghi log, không dừng vòng). Vòng
  `events` vốn đưa mọi việc về hạn → dùng `DrainJobs`. Đồng hồ database của luật: một hàm `dbClock` dùng cho
  `CheckRules`, `ErrorRules` và `ReadPGStats` (test), không viết lại ở từng luật.

## Soát thiết kế

- 5.1–5.3: chỉ đọc bảng của platform (`jobs`, `alert_events`, `backup_runs`, `error_events`) và view thống kê như cũ.
  5.4–5.6, 5.9: không áp dụng: không đổi đường ghi, không gọi ra ngoài. 5.7: không áp dụng: bảng platform không có
  `tenant_id` (`platform.jobs` không RLS; câu đưa về hạn của testkit chạy bằng chủ bảng như `makeJobsDue`). 5.8: không
  nhập HTTP. 5.10–5.11: không Valkey, không thư viện mới. 5.12: câu `DBNow` tĩnh qua sqlc; câu dynsql của pgwatch giữ
  nguyên, chỉ đổi tham số.
- 6.1–6.11: không áp dụng ở mã chạy: luật chỉ đọc, không khóa, không tiền. Câu testkit `UPDATE … WHERE id IN (SELECT …
  FOR UPDATE SKIP LOCKED)` không chờ khóa dòng của worker (bẫy 55P03 Task 421). Đúng về thời gian (bẫy "một đồng hồ"):
  mỗi mốc so với đồng hồ của bên ghi — mốc PostgreSQL ghi (`run_at`, `finished_at` của hàng đợi, sao lưu,
  `first_seen_at` lỗi, `alert_events.sent_at`, `pg_stat_*`) so với `now()` của PostgreSQL đọc một lần mỗi lượt; mốc Go
  ghi (nhịp sống, `metrics_minute`, lần chép tệp) giữ so với `RuleOptions.Now`. Đồng hồ giả của test (`RuleOptions.Now`
  lệch d so với đồng hồ máy) dời đồng hồ database đúng d.
- 6.12: văn bản cảnh báo không đổi (chỉ số, mã việc). 6.13: không đổi quyền (crm_app chạy `SELECT now()`).

## Đã làm

- `internal/testkit/queue.go`: `DrainDue`, `MakeDueSoon` (bù đồng hồ PostgreSQL lùi ≤ 500 ms, `SKIP LOCKED`);
  `DrainJobs` và `DrainDue` dùng chung vòng `drain`. Mọi vòng drain riêng ở Ảnh hưởng đổi sang `DrainDue` (giữ ngữ
  nghĩa cũ: việc hẹn thử lại giữ lịch), vòng `events` sang `DrainJobs` (vốn đưa mọi việc về hạn), `webhooks` split thêm
  `MakeDueSoon` vào điều kiện dừng (giữ cách xử lý lỗi riêng); vòng trong `TestRoutedSplitUsesRouteKey` (trước bỏ qua
  lỗi `RunOnce`, test không cố ý làm lỗi) sang `DrainDue` (cũng hạ độ phức tạp gocognit 21 → dưới 20).
- testkit `pgtemplate_test.go`, `pgseed_test.go`: lượt dọn "mọi database đã đủ cũ" dùng ngưỡng `anyAge = −1 phút` thay 0.
- `observability`: `RuleOptions` có `dbNow` (không xuất) do `checkRules` đặt bằng `dbClock` (câu sqlc `DBNow`, dời cùng
  độ lệch giả của `Now` so với đồng hồ máy); `queue-lag`, `dead:`/`in-doubt:` (cả mốc `alert_events.sent_at` ghi qua
  `notifyAsOf`), `backup-age`, `new-error` (`errorRulesAt`), `wal-archive`, `long-tx`, `lock-wait` so với `dbNow`; chỉ
  số theo phút, nhịp sống, lần chép tệp, `error-spike` giữ `Now`. Không đọc được đồng hồ database thì `CheckRules` trả
  lỗi, không chạy luật nào (database hỏng thì mọi luật cũng không đọc được).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Gốc (1)(2): đồng hồ PostgreSQL lùi | vòng `clock_timestamp()` 60 s | `psql … DO $$ … $$` (Gốc đã đo) | `backward 4 worst 00:00:00.355016` |
| Tái hiện (1)(2) trước khi sửa: giả lùi 300 ms trước `drain()` cũ | `TestShipment_CreateRejectedCancels`, `TestShipment_InDoubtRetryLooksUpFirst`, `TestCancel_EventRedelivered` (bản tạm chèn `run_at = now() + 300 ms` đầu `drain()`) | `go test -race -count=1 -run '…' ./internal/modules/fulfillment/` | `terminal_test.go:77: lệnh tạo = pending, muốn dead` (seller-0, seller-1); `terminal_test.go:193: lệnh = pending, dấu in_doubt false, create_state pending`; `cancel_flow_test.go:358: lệnh hủy = 0, muốn 1` — đúng ba thông báo của main |
| Sau khi sửa, vẫn giả lùi 300 ms ở **mọi** lần drain: cả gói fulfillment qua | cả gói (bản tạm) | `go test -race -count=1 -timeout 30m ./internal/modules/fulfillment/` | `ok … fulfillment 100.622s`; rồi bỏ phần giả |
| `DrainDue` chạy việc lệch 300 ms và việc tới hạn, giữ lịch việc hẹn 3 s, 6 giờ; `MakeDueSoon` không chạm việc hẹn xa; `DrainJobs` vẫn chạy cả việc 6 giờ | `TestDrainDueRunsJobsAheadOfClockStep`, `TestDrainJobsRunsScheduledJobs` (mới) | `go test -race -count=1 -run TestDrain ./internal/testkit/` | trước khi có mã: `undefined: testkit.DrainDue`, `undefined: testkit.MakeDueSoon`; sau: `ok` |
| Ngưỡng bù không nuốt lịch thử lại 1 s của files | cả gói files | `go test -race -count=1 ./internal/platform/files/` | ngưỡng 1 s: `TestFileProcess_RetryCapNotResetByRolledBackProcessing` đỏ (`RunOnce: … hook dead`) → hạ 500 ms: `ok … files 83.936s` |
| Tái hiện (3) trước khi sửa: database "chưa đủ cũ" khi `now()` chậm hơn mốc tệp | `TestCleanupStaleDropsOnlyIdleOldDatabases` (bản tạm ngưỡng 2 s ≈ đồng hồ lùi) | `go test -race -count=1 -run TestCleanupStaleDropsOnlyIdleOldDatabases ./internal/testkit/` | `…_idle còn tồn tại = true`, `…_tpl còn tồn tại = true`, `pg.go:125: … cannot drop a template database (SQLSTATE 42809)` — đúng lỗi của main |
| Mốc `PG_VERSION` theo giây | 40 lần tạo database | `psql` (Gốc đã đo) | mtime − now() từ −1027 ms tới −63 ms |
| Tái hiện (4) trước khi sửa: luật so mốc PostgreSQL với đồng hồ Go | `TestRulesPostgresClockSkew` (4 ca), `TestRuleDeadAlertMarkPostgresClock` (2 ca) (mới; `CheckRulesAt` lúc đó gọi `CheckRules` với `Now` = đồng hồ Go lệch) | `go test -race -count=1 -run 'ClockSkew\|AlertMark' ./internal/platform/observability/` | `-5m0s_over=true`: `queue-lag`, `in-doubt:cmd.x`, `dead:msg.d` không báo, `new-error` báo sai; `5m0s_over=false`: `queue-lag`, `in-doubt` báo sai, `new-error` không báo; `-5m0s`: `lượt 1 không báo việc dead mới` |
| Sau khi sửa (4): đồng hồ Go lệch ±5 phút không đổi kết quả các luật mốc PostgreSQL; mốc `sent_at` theo PostgreSQL (lượt 2 không báo lại) | như trên + mọi test cũ của gói (`TestRuleDeadNoPayload` đã ghi mốc theo `now()`) | `go test -race -count=1 ./internal/platform/observability/...` | `ok … observability 8.322s`, `ok … alertjobs`, `ok … liveprobe` |
| Các gói đổi drain qua | queue, events, files, webhooks, schedules, carrier, testkit, order (`TestDuplicates`), httpapi (`TestVerify`, customer import, carrier account) | `go test -race -count=1 ./internal/platform/{queue,events,files,webhooks,schedules}/ ./internal/adapters/carrier/ ./internal/testkit/`; `-run 'TestDuplicates\|Dup' ./internal/modules/order/`; `-run 'CustomerImport\|CarrierAccount\|TestVerify' ./internal/httpapi/` | tất cả `ok` |
| Mỗi test sửa ≥ 30 lần dưới tải đều qua | 3 test fulfillment; queue `TestBestEffort*`, `TestOrderingBySeq`; testkit `TestCleanupStale*`, `TestDrain*`; observability `TestRule*`, `*ClockSkew`, `TestDeepHealth*`, `TestDailyDigest*` | 8 tiến trình song song (2 mỗi gói, `nice -n 10`), mỗi tiến trình `-test.count=15 -test.v`; tải máy lên 35,5 | mỗi test `30 PASS`, `--- FAIL` 0 ở cả 8 log (vd. `30 PASS: TestShipment_CreateRejectedCancels`, `30 PASS: TestShipment_InDoubtRetryLooksUpFirst`, `30 PASS: TestCancel_EventRedelivered`, `30 PASS: TestCleanupStaleDropsOnlyIdleOldDatabases`, `30 PASS: TestRuleDeadNoPayload`, `30 PASS: TestBestEffortRetryingHeadDoesNotBlock`) |
| Cổng của task: build, vet, lint, gen-check, sqlc-vet, danh mục, jscpd, gitleaks, oasdiff, test-guard, **toàn bộ test** (đổi testkit) | — | ci-task (lần 2, sau khi stage) | `CI-TASK XANH (so với điểm tách nhánh dd70e9a…)`, test toàn bộ 493 giây. Lần 1 (chưa stage): ĐỎ lint (`gci` `rules_test.go:276`; `gocognit` 21 `TestRoutedSplitUsesRouteKey`) và gen-check (chưa stage) — đã sửa; mọi gói test khác `ok` |

## Quyết định thay chủ shop

- **Không sửa hàng đợi (`ClaimJobs` `run_at <= now()`)**: đồng hồ lùi chỉ làm việc chạy trễ tới lượt nhận sau (worker
  thật có Poll và NOTIFY); máy chủ thật (Linux, không máy ảo) hiếm khi lùi; nới điều kiện nhận việc ở vùng rủi ro cao
  là đổi hành vi cho mọi việc. Sửa ở test: drain bù độ lùi. Phương án khác: nhận việc có dung sai (`run_at <= now() +
  0,5 s`). Dễ đảo: chỉ tệp test và testkit.
- **`DrainDue` (chỉ bù đồng hồ) thay vì `DrainJobs` (đưa mọi việc về hạn) cho các drain riêng**: lệnh giao gợi ý dùng
  `DrainJobs`, nhưng nhiều test cố ý giữ việc hẹn thử lại (queue best-effort 6 giờ, files lịch trần, lệnh hủy tự thử
  lại); `DrainJobs` sẽ đổi điều test kiểm. `DrainDue` giữ đúng ngữ nghĩa cũ của từng drain, chỉ bỏ lỗi đồng hồ.
- **Ngưỡng bù 500 ms**: lớn hơn lần lùi lớn nhất đã đo (0,36 s), nhỏ hơn một nửa lịch thử lại nhỏ nhất trong mã (1 s
  của files). Lùi lớn hơn 0,5 s thì drain có thể dừng sớm như cũ (Việc phát hiện thêm).
- **Luật cảnh báo đổi ở mã chạy** (Task 445 đã để lại): nhỏ, cùng gói, không đổi chữ ký công khai — `RuleOptions.Now`
  vẫn là đồng hồ giả của test; đồng hồ database dời cùng độ lệch nên mọi test cũ giữ nguyên ý. Phương án khác: thêm
  trường `DBNow` công khai cho bên gọi đặt (bên gọi phải tự đọc đồng hồ database, dễ trộn lại).
- **Không đổi Summary, digest, dọn lịch, fulfillment kẹt**: cửa sổ giờ, ngày nên lệch giây không đổi kết quả thực tế;
  ghi Việc phát hiện thêm.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `scripts/agent/new-task.sh` lỗi khi tên task có `/` (sed `s/…/…/`), để lại tệp task rỗng.
- Đồng hồ máy ảo PostgreSQL của crm-shared lùi tới 0,36 s vài lần mỗi phút: test gọi `RunOnce` **một lần** ngay sau khi
  ghi việc rồi đòi đã nhận (không qua drain) vẫn có thể đỏ thất thường; nên chỉnh đồng bộ giờ của máy ảo colima (hạ
  tầng, ngoài phạm vi) hoặc rà các test đó.
- Mốc trộn đồng hồ còn lại ở cửa sổ giờ, ngày (lệch giây không đổi kết quả thực tế): `Summary`/`SendDailyDigest`
  (`sent_at`, `last_seen_at` so với `now − 24 giờ` của Go), `schedules` dọn (`finished_at`; `quotes.created_at` so với
  `now − N ngày`), fulfillment `SetSyncError` (`last_status_at` ghi bằng `now()` ở `shipments.sql`, `create.sql`,
  `cancel.sql` nhưng bằng `@now` của Go ở `partial.sql`, `receipt.sql`; so với `@now − giờ`), `cash/lists.sql`
  `delivered_at < @cutoff`, `LatestGaugeSum`/`ListLatestSpoolFiles` (Task 445 ghi).
- Task 442 chạy song song: không trùng tệp (442 đã gộp vào main trước lần gộp main của task này, gộp fast-forward, không
  xung đột).

## Reviewer

Kết luận: ĐẠT (lượt 1, phần đã stage sau khi gộp main dd70e9a và ci-task XANH). Phát hiện phải sửa: không có. Ảnh hưởng
khớp thực tế (12 vòng drain đổi hết; grep `RunOnce … n == 0` chỉ còn `split_test.go` đã thêm `MakeDueSoon`, testkit và
`drainQuiet` cố ý giữ). Bảng bằng chứng: test mới kiểm đúng điều tiêu chí nói (worker, database thật; `TelegramAlerter`
thật cho mốc `sent_at`); không nới tiêu chí — vòng cũ thoát im lặng nay đỏ khi quá 50 vòng hay `RunOnce` lỗi (chặt
hơn). Ghi chú không bắt buộc: ngưỡng 500 ms chỉ đụng lịch thử lại 1 s của files khi việc sắp chạm trần `RetryMax` và
lượt chạy chậm quá 0,5 s (rủi ro nhỏ, đã ghi ở Quyết định); `ErrorRules` thêm một câu `DBNow` mỗi lần gọi; tham số
`ago` trùng tên hàm `ago` (không lỗi). Không sửa thêm.
