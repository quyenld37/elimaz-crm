# Task 11 — Lõi hàng đợi jobs sau interface JobQueue

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Không ký hiệu xuất ra ngoài nào **có sẵn** bị đổi định nghĩa. Gói mới `internal/platform/queue` (0 chỗ dùng trước
    task này): `Policy`, `DefaultPolicy`, `Kind`, `NewJob`, `JobQueue`, `NewQueue`, `Job`, `Handler`, `TerminalHook`,
    `ErrPermanent`, `RetrySchedule`, `NextDelay`, `WorkerOptions`, `Worker`, `NewWorker`, `Worker.Start/Stop/RunOnce`,
    và `ErrInvalidJob` (kiểm đầu vào của `Enqueue`, xem Khác kế hoạch).
  - `internal/platform/internal/store` (mã sinh): **thêm** các câu của `queries/platform/jobs.sql`; không câu cũ nào đổi.
  - `internal/modules/identity/internal/store/models.go` (mã sinh): sqlc thêm struct `PlatformJob` vì hai khối sqlc
    dùng chung thư mục `migrations`; không mã nào của identity dùng nó.
  - `cmd/crm` (`serve`, nhánh worker): thêm `queue.NewWorker(...).Start`, SIGTERM → `Stop(25 s)`; hàm không xuất.
- Gói phụ thuộc ngược (go list): `internal/platform/internal/store` được `db`, `idempotency`, `metrics`, `tenancy`
  nhập (chỉ thêm hàm, không đổi). `queue` mới chỉ được `cmd/crm` nhập. `cmd/crm` không gói nào nhập (test e2e chạy
  tệp chạy).
- Operation OpenAPI bị chạm, oasdiff: không (route quản lý việc là Task 12).
- Chỗ dùng trên màn hình: không (Task 12, 17).
- Câu sqlc, bảng, pha migration: **mở rộng**. `migrations/0007_jobs.sql` tạo `platform.jobs` đúng Interfaces của kế
  hoạch, hai index một phần, trigger `AFTER INSERT` → `pg_notify('jobs', NEW.kind)`; `crm_app` được `SELECT, INSERT,
  UPDATE`. Bảng nền tảng **không RLS** (Global Constraints liệt kê `jobs`; `tenant_id` có thể NULL với việc toàn hệ
  thống), nên `platform.jobs` được thêm vào danh sách miễn `platformTables` của `schemarules_test.go` (xem Quyết định 1).
  Thứ tự khóa: `platform.jobs` đã có trong `docs/adr/0046-thu-tu-khoa.md` nhóm 13 (sau `idempotency_keys`, luôn cuối
  transaction); task này chỉ thêm ghi chú mốc ở cột "Mốc thêm" (chèn, không viết lại dòng). Câu mới:
  `queries/platform/jobs.sql` (khối sqlc `queries/platform` có sẵn).
- Test sẽ chạy: vùng rủi ro cao (`internal/platform/queue`) nên chạy cả bộ của vùng:
  `go test -race ./internal/platform/... ./cmd/... ./tests/arch/... ./tests/e2e/...` trên dự án compose `crm-t11`
  (cổng 5511); cuối cùng `make ci` (phần chạy được trên máy).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có `db.RetryTx`,
  `db.TxWorker`, `db.ErrRetryLater`, `tenancy.WithTenant`, `metrics.Registry` (`Observe`, `Inc`, `Gauge`),
  `telemetry.Setup`/`WithSyncExporter`, `testkit.OpenStaff`, `Staff.Tenant`, `Staff.ProbeCounter`. Tìm `queue`,
  `jobs`, `LISTEN`, `NOTIFY`, `SKIP LOCKED`, `traceparent`, `jitter` trong `internal/`, `cmd/`: chỉ có chú thích hẹn
  Task 11 (`cmd/crm/main.go`, `db/tx.go`) và `db.Backoff` (lùi 20 ms × 2^n của RetryTx).
- Dùng lại / viết mới vì:
  - Mọi transaction qua `db.RetryTx` hoặc `tenancy.WithTenant` (handler có tenant), không tự mở transaction.
    Kết nối `LISTEN` lấy bằng `Pool.Acquire` (không phải transaction).
  - `NextDelay` viết mới, không dùng `db.Backoff`: khác mục đích (lịch thử lại việc nền 5 s → 6 giờ của spec 04 mục
    5.1, ±20%) và khác lý do thay đổi (Backoff là chờ ngắn giữa các lần chạy lại một transaction, ADR-46).
  - Chỉ số: `metrics.Registry` có sẵn (`job_ms`, `job_pickup_ms` bằng `Observe`; `jobs_dead`, `jobs_in_doubt` bằng
    `Inc`; `jobs_oldest_pending_age_s` bằng `Gauge`).
  - Dấu vết: `propagation.TraceContext` của OpenTelemetry (đã có trong ADR-44); chỉ ghi `traceparent`, `tracestate`
    (không ghi baggage, có thể chứa dữ liệu tùy ý). Test dùng `tracetest.NewInMemoryExporter` của otel SDK có sẵn.
  - Test: `testkit.OpenStaff` (hai pool), bảng thử tạo trong test; rapid (ADR-44).
  - Không thêm thư viện.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: hàng đợi là của `platform` (04 mục 3, 5.1): bảng `platform.jobs`, mã trong `internal/platform/queue`.
2. Không ghi bảng module khác: gói chỉ ghi `platform.jobs`; thay đổi nghiệp vụ do handler của module làm trong
   transaction được giao. Bảng thử của test nằm trong schema `probe` của database thử.
3. Đọc dữ liệu module khác: không áp dụng.
4. Việc nguyên tử: `Enqueue` nhận `pgx.Tx` của bên gọi (việc ghi cùng transaction với dữ liệu); lệnh đánh dấu `done`
   nằm trong cùng transaction với thay đổi của handler (04 mục 5.1 "đúng một lần").
5. Việc sau commit: đây là cơ chế (hàng đợi). Hook `OnTerminal` chạy trong cùng transaction với lệnh đổi trạng thái.
6. Gọi ra bên ngoài: việc `command` không được nhận ở task này (Task 12 làm `commands.Runner`); handler
   `transactional` chạy trong transaction nên không được gọi mạng (ghi ở doc `Handler`).
7. Ngữ cảnh shop: `jobs` là bảng nền tảng không RLS (spec 04 mục 11: worker nhận việc của mọi tenant); bù lại chỉ
   `internal/platform/**` nhắc tên bảng, payload chỉ mang mã bản ghi (doc `NewJob`), handler luôn chạy trong
   `tenancy.WithTenant(job.tenant_id)`; việc không tenant chạy ngoài ngữ cảnh nên bảng RLS trả 0 dòng (test).
8. Nghiệp vụ không biết HTTP: gói không nhập `net/http`.
9. Đường ghi: không áp dụng (không có route mới).
10. PostgreSQL là nguồn sự thật: hàng đợi là bảng PostgreSQL; `NOTIFY` chỉ để đánh thức, vẫn hỏi mỗi 2 s.
11. Thư viện: không thêm.
12. Module mẫu: gói nền tảng theo mẫu `idempotency`: câu SQL trong `queries/platform`, sqlc, test gói `_test` trên
    PostgreSQL thật, rapid cho quy luật.

Mục 6 (đúng khi chạy đồng thời):

1. Hai worker cùng nhận: một câu `UPDATE … FROM (SELECT … FOR UPDATE OF j SKIP LOCKED)`; mọi lần đổi trạng thái sau đó
   có điều kiện `status = 'running' AND locked_by = <mình>` (worker mất giữ chỗ không ghi được). Thứ tự theo đối tượng:
   điều kiện `NOT EXISTS` việc cùng `(tenant_id, order_key)` có `(order_seq, id)` nhỏ hơn đang `pending`, `running`,
   `in_doubt`, hoặc `dead` có `block_on_dead`; việc chặn luôn được thấy (ảnh chụp cũ thấy `pending`, mới thấy
   `running`), không trạng thái nào chuyển ngược về chỗ không chặn trong task này.
2. Gửi lại cùng việc: `UNIQUE NULLS NOT DISTINCT (tenant_id, kind, dedupe_key)` + `ON CONFLICT DO NOTHING`, rồi câu
   **thứ hai** đọc id cũ (ảnh chụp mới thấy dòng bên kia vừa commit).
3. Thứ tự khóa: `platform.jobs` nhóm 13, luôn cuối. Transaction của handler: dòng nghiệp vụ trước, `UPDATE jobs … done`
   là câu cuối. Đường `dead`/`in_doubt`/`expired`: gọi `OnTerminal` (có thể chạm bảng nghiệp vụ) **trước**, rồi mới
   `UPDATE jobs` có điều kiện; 0 dòng → lùi cả hai. Bước nhận việc và bước trả về hàng chờ là transaction riêng chỉ
   chạm `jobs`.
4. Lỗi tạm: `RetryTx` chạy lại transaction tới 3 lần (`Handle` có thể bị gọi lại, đúng luật "fn chạy lại được"); hết
   lượt → coi là một lần thử lỗi theo lịch (ADR-46 điểm 2).
5. Mức cô lập: READ COMMITTED (mặc định của RetryTx), câu có điều kiện, `SKIP LOCKED`.
6. Việc nền lỗi: đây là cơ chế: `NextDelay` (5 s, 30 s, 2 phút, 10 phút, 1 giờ, 6 giờ, ±20%), `attempts >=
   max_attempts` hoặc `ErrPermanent` → `dead` + `OnTerminal` + chỉ số `jobs_dead` (cảnh báo Telegram là Task 19).
7. Bên ngoài 429: không áp dụng (Task 12).
8. Tiến trình chết: dòng `running` hết `lease_until` → `retry`: về `pending` (hết lượt thì `dead`, xem Quyết định 3);
   `in_doubt`: thành `in_doubt` + `OnTerminal`. Transaction của handler tự lùi.
9. Treo lâu: `Handle` có `ctx` hết hạn sau `Policy.Timeout` (mặc định 30 s); cả transaction chạy trên `ctx` đó nên quá
   giờ thì lùi; giữ chỗ 90 s > 30 s. Tắt êm: `Stop(grace)` ngừng nhận, chờ, hết `grace` thì hủy, việc dở về `pending`
   với `attempts - 1` (ghi trên `ctx` tách khỏi việc hủy, có hạn).
10. Tiền: không áp dụng.
11. Quy luật: rapid 100 lượt: mỗi việc hoàn tất đúng một lần; trong mỗi `OrderKey` thứ tự hoàn tất tăng theo
    `(OrderSeq, ID)`; không việc nào kẹt `running`.
12. Dữ liệu cá nhân: payload chỉ mang mã bản ghi (doc); log và thuộc tính dấu vết chỉ có `job.id`, `job.kind`,
    `attempts`; `last_error` cắt tối đa 1000 ký tự (handler chịu trách nhiệm không để dữ liệu cá nhân trong lỗi).
13. Quyền: không áp dụng (không route).

## Bằng chứng

Máy: dự án compose `crm-t11`, PostgreSQL 18 cổng 5511 (`CRM_TEST_DATABASE_URL`). Test viết trước, chạy đỏ trước khi
có mã (`go test ./internal/platform/queue/` → `[build failed]`; `TestCheckSchemaRulesCleanSchema` → "muốn 2 dòng, nhận
1"; `TestWorkerRoleRunsQueue` → việc vẫn `pending` sau 15 s).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `NextDelay(1, .5)=5s`, `(6, .5)=6h`, `(9, .5)=6h`; rnd 0 và 1 đúng ±20% | `TestNextDelaySchedule`, `TestNextDelayJitterBounds`, `TestNextDelayAttemptZero` | `go test -race -count=1 ./internal/platform/queue/` | `ok … queue 22.8s` |
| `Enqueue` trong tx lùi → 0 dòng | `TestEnqueueRolledBackLeavesNoRow` | như trên | PASS |
| Cùng khóa → `created=false`, 1 dòng, trả id cũ; hai tenant → 2 dòng; `TenantID nil` hai lần → 1 dòng | `TestEnqueueDedupe` | như trên | PASS |
| Ghi đúng chính sách, payload; Policy rỗng lấy DefaultPolicy; đầu vào sai → `ErrInvalidJob`; NOTIFY `jobs` mang tên loại | `TestEnqueueStoresPolicyAndPayload`, `TestEnqueueDefaultsAndValidation`, `TestEnqueueNotifies` | như trên | PASS |
| Handler thành công → `done`, có `result`, chỉ số `job_ms`, `job_pickup_ms` | `TestHandlerSuccessMarksDoneWithResult` | như trên | PASS |
| Handler chèn dòng rồi lỗi → bảng thử 0 dòng, `pending`, `attempts=1`, `run_at` sau 4–6 s | `TestHandlerErrorRollsBackAndReschedules` | như trên | PASS |
| **Đổi `locked_by` lúc handler chạy → bảng thử 0 dòng, việc không `done`** | `TestLostLeaseRollsBack` | như trên | PASS (việc giữ `running` của w2) |
| `ErrPermanent` → `dead`, `OnTerminal("dead")` một lần, `jobs_dead`; lỗi lần thứ 8 → `dead` | `TestPermanentErrorGoesDeadOnce`, `TestEighthFailureGoesDead` | như trên | PASS |
| `running` hết `lease_until`, `retry` → chạy lại tới `done`; giữ chỗ còn hạn không bị nhận; hết lượt → `dead`; `in_doubt` → `in_doubt` + `OnTerminal` + `jobs_in_doubt` | `TestExpiredLeaseRetry`, `TestLiveLeaseNotReclaimed`, `TestExpiredLeaseAtMaxAttemptsGoesDead`, `TestExpiredLeaseInDoubt` | như trên | PASS |
| Hai worker `RunOnce` song song trên 20 việc → mỗi việc `Handle` đúng 1 lần | `TestTwoWorkersHandleEachJobOnce` | như trên | PASS |
| Việc của A thấy đúng dòng RLS của A; việc không tenant thấy 0 dòng | `TestHandlerRunsInTenantContext` | như trên | PASS (`{"seen": 2}`, `{"seen": 0}`) |
| `Timeout: 50ms` → `ctx.Err()` bên trong handler, `pending`, `last_error` chứa `deadline` | `TestHandlerTimeout` | như trên | PASS |
| Thứ tự: OrderSeq 3, 2, 1 với Concurrency 3 → Handle 1, 2, 3; `dead` + `BlockOnDead` chặn, `false` không chặn; việc đầu chờ thử lại vẫn chặn; khác tenant không chặn | `TestOrderingBySeq`, `TestDeadBlocksWhenBlockOnDead`, `TestDeadDoesNotBlockWithoutBlockOnDead`, `TestRetryingHeadBlocks`, `TestSameOrderKeyOtherTenantDoesNotBlock` | như trên | PASS |
| `Stop(100ms)` khi handler chờ 5 s → trả dưới 1 s, `pending`, `attempts=0`, thay đổi lùi; Stop chờ việc xong trong grace rồi thôi nhận | `TestStopReturnsUnfinishedJob`, `TestStopWaitsThenStopsClaiming` | `go test -race -count=1 -v -run 'TestStop…'` | PASS (0.33 s) |
| `pending` quá `expires_at` → `expired`, không `Handle`, `OnTerminal("expired")` một lần | `TestPendingPastExpiryExpires` | như trên | PASS |
| Không handler → `dead`, `last_error='no handler'`; việc `command` không bị nhận (Task 12); `RunAt` tương lai không chạy sớm | `TestNoHandlerGoesDead`, `TestCommandJobsNotClaimed`, `TestRunAtInFutureNotClaimed` | như trên | PASS |
| Dấu vết: Enqueue trong span → handler ở span con `job <kind>` (consumer) cùng TraceID; không span → `trace_context` NULL | `TestTraceFlowsThroughQueue`, `TestNoTraceStoresNull` | như trên | PASS |
| Enqueue khi worker đang chờ → nhận dưới 1 s nhờ NOTIFY (Poll 1 phút); Poll vẫn nhận việc tới hạn | `TestNotifyWakesWorker`, `TestPollPicksDueJob` | như trên | PASS (0.39 s) |
| Hạn ghi kết quả tính từ lúc handler xong: handler chạy lâu hơn hạn ghi vẫn được hẹn lại theo lịch; Stop hủy handler chạy lâu vẫn trả việc về `pending`, `attempts=0` (phát hiện của reviewer, đỏ trước khi sửa: "context deadline exceeded", việc kẹt `running`) | `TestRecordAfterLongHandler`, `TestStopAfterLongHandlerReleases` | `go test -race -count=1 -run 'TestRecordAfterLongHandler\|TestStopAfterLongHandlerReleases' ./internal/platform/queue/` | trước sửa: FAIL cả hai; sau sửa: `ok` |
| Gauge `jobs_oldest_pending_age_s` | `TestOldestPendingGauge` | như trên | PASS |
| Thuộc tính rapid 100 lượt: mỗi việc hoàn tất đúng 1 lần, thứ tự hoàn tất tăng theo `(OrderSeq, ID)` trong mỗi OrderKey, không kẹt `running` | `TestPropertyQueue` | `go test -race -count=1 -v -run TestPropertyQueue ./internal/platform/queue/` | `[rapid] OK, passed 100 tests (11.0s)` |
| Không chập chờn | cả gói | `go test -race -count=3 ./internal/platform/queue/` | `ok … 57.8s` |
| Vai trò worker của tệp chạy thật chạy hàng đợi (NOTIFY), SIGTERM → thoát 0; ba vai trò vẫn khởi động và tắt êm | `TestWorkerRoleRunsQueue`, `TestBootEachRoleThenGracefulStop` | `go test -race -count=1 ./tests/e2e/` | `ok … e2e 15.8s` |
| `platform.jobs` là bảng nền tảng được miễn RLS; bỏ danh sách miễn thì cả `tenant_routes` và `jobs` bị báo, không bảng nào khác | `TestCheckSchemaRulesCleanSchema`, `TestSchemaRulesStillClean` (identity) | `go test -race -count=1 ./internal/platform/tenancy/ ./internal/modules/identity/` | `ok` cả hai |
| Mọi gói, test kiến trúc (SQL chỉ trong queries, không Begin ngoài db, ranh giới) | toàn bộ | `go test -race -count=1 ./internal/... ./cmd/... ./tests/...` | mọi gói `ok` |
| Cổng tĩnh | `make ci` (build, vet, lint, gen-check, sqlc vet, test -race, govulncheck, oasdiff, catalog-check, jscpd, test-guard, gitleaks) | `make ci` sau khi stage; `make catalog-check jscpd test-guard gitleaks` lại sau khi thêm ghi chú danh mục | `0 issues.`; mọi gói `ok`; `No vulnerabilities found.`; `Found 0 clones.`; `no leaks found`; catalog-check không chênh |

## Test bị xóa hoặc tắt

(không có). Một assertion ở vùng rủi ro cao được **đổi** (không bớt): `TestCheckSchemaRulesCleanSchema` (tenancy) trước
đòi "bỏ danh sách miễn → đúng 1 dòng về `platform.tenant_routes`"; nay đòi mỗi bảng trong danh sách miễn
(`tenant_routes`, `jobs`) đều bị báo và không có dòng nào của bảng khác. Lý do: `platform.jobs` là bảng nền tảng thứ
hai có `tenant_id` (bị báo 2 dòng: RLS và `UNIQUE (tenant_id, id)`).

## Khác kế hoạch

1. `WorkerOptions` có thêm `Logger *slog.Logger` (nil → `slog.Default()`): vòng chạy nền của `Start` là mép (worker)
   nên phải tự log lỗi một lần (luật "Lỗi và log"). `RunOnce` trả lỗi, không log.
2. Thêm `ErrInvalidJob` (Enqueue từ chối tên loại rỗng, `DedupeKey` rỗng, `TenantID` trỏ `uuid.Nil`, payload không mã
   hóa được, Policy lạ), `ErrAlreadyStarted`, `ErrStopGrace` (Stop phải hủy việc vì quá grace), và các hằng
   `ModeTransactional`, `ModeCommand`, `LeaseRetry`, `LeaseInDoubt`, `StatusDead`, `StatusInDoubt`, `StatusExpired`.
3. Migration thêm ràng buộc ngoài Interfaces (chỉ chặt hơn): `CHECK` cho `kind`, `dedupe_key` khác rỗng,
   `max_attempts > 0`, `attempts >= 0`, `(order_key IS NULL) = (order_seq IS NULL)`, `running` ⇔ có `lease_until` và
   `locked_by`; thêm hai index một phần (`lease_until` của `running`, `expires_at` của `pending`) cho hai bước quét.
4. `ClaimJobs` chỉ nhận `mode = 'transactional'` (Quyết định 2). Task 12 bỏ điều kiện này khi thêm `commands.Runner`.
5. Tên tệp test: thêm `helpers_test.go` (môi trường, handler thử dùng chung của gói test) và `export_test.go`
   (`SetRecordTimeout`: test đổi hạn ghi 5 s để thử handler chạy lâu hơn hạn đó mà không chờ 5 s).
6. `tests/e2e/boot_test.go`: tách hàm `startRole` (commit cấu trúc riêng) để test mới `tests/e2e/worker_test.go` không
   chép đoạn khởi chạy tiến trình (jscpd).

## Quyết định thay chủ shop

1. **`platform.jobs` được thêm vào danh sách miễn RLS `platformTables` (cả `tenancy` và `identity`).** Ghi chú giao
   việc nhắc "bảng có `tenant_id` phải có RLS, không thêm vào danh sách miễn", nhưng kế hoạch (Global Constraints) và
   spec 04 mục 11 ghi rõ `jobs` là bảng nền tảng **không RLS** (worker nhận việc của mọi tenant; `tenant_id` NULL với
   việc toàn hệ thống nên policy chuẩn không áp được). Bù lại: chỉ `internal/platform/**` nhắc tên bảng, handler luôn
   chạy trong `WithTenant`. Phương án khác: bật RLS với policy riêng cho worker (trái spec, cần role BYPASSRLS cho
   worker). Đảo ngược: một dòng trong hai test + migration mới.
2. **Việc `mode = 'command'` chưa được worker nhận** (giữ `pending`) cho tới Task 12. Kế hoạch ghi "việc `command` giao
   cho Task 12" mà không nói Task 11 làm gì với chúng; nhận rồi chạy như transactional là sai (lệnh ra ngoài không được
   chạy trong transaction), đặt `dead` thì mất việc. Không nhận là an toàn và dễ đảo ngược (Task 12 sửa một điều kiện).
3. **Giữ chỗ hết hạn với chính sách `retry` mà đã hết lượt (`attempts >= max_attempts`) → `dead` + `OnTerminal`**, thay
   vì về hàng chờ mãi. Kế hoạch chỉ ghi "`retry` → `pending`"; một việc làm chết worker mỗi lần chạy (hết bộ nhớ) sẽ
   lặp vô hạn. Phương án khác: luôn về `pending` (đúng chữ kế hoạch, nhưng không có điểm dừng).
4. **Thứ tự trong transaction kết thúc: `OnTerminal` trước, câu đổi trạng thái `jobs` có điều kiện sau** (0 dòng →
   lùi cả hai), để giữ luật ADR-46 nhóm 13 (`jobs` luôn khóa cuối) khi hook chạm bảng nghiệp vụ.
5. **Mã giữ chỗ (`locked_by`) của tiến trình worker là `worker@<máy>/<uuid>`**, mỗi lần chạy một mã: tiến trình chạy
   lại trên cùng máy không bị coi là chủ của việc mà lần chạy trước đang giữ.
6. **Tắt êm:** `cmd/crm` gọi `Stop(25 s)` rồi mới dừng vòng ghi chỉ số, nhịp sống (ghi được cả chỉ số lúc tắt). Quá
   grace thì hủy handler và chờ thêm tối đa 5 s để việc dở ghi về hàng chờ.

## Việc phát hiện thêm

1. Hai bản sao danh sách bảng nền tảng được miễn RLS (`platformTables` trong `tenancy/schemarules_test.go` và
   `modules/identity/users_test.go`): nên gom về một chỗ (ví dụ hàm xuất trong `tenancy`) ở một task cấu trúc riêng.
2. Container dừng với `stop_grace_period` mặc định của Docker (10 s) sẽ giết worker trước 25 s của `Stop`: cấu hình
   compose/máy chủ (Task 21) cần `stop_grace_period` ≥ 30 s cho vai trò worker.
4. `NewWorker` chưa chặn `Policy.Timeout >= WorkerOptions.Lease` (reviewer, không bắt buộc): việc có thể bị nhận lại
   khi handler còn chạy; kết quả vẫn chỉ ghi một lần, chỉ tốn công chạy hai lần.
3. M0 chưa có handler nào: worker hiện đặt `dead` mọi việc transactional (không handler). Task 13–15 đăng ký handler.

## Reviewer

Commit 1 (cấu trúc, `tests/e2e/boot_test.go`: tách `startRole`): **ĐẠT**, không có phát hiện phải sửa. Reviewer so
từng điểm (env, stdout/stderr, Wait, Kill khi dọn) và xác nhận hành vi của `bootAndStop` giữ nguyên.

Commit 2 (hành vi), vòng 1: **CẦN SỬA**, một phát hiện về tính đúng:

- `runJob` tạo ctx có hạn 5 s cho các lần ghi trạng thái **trước** khi handler chạy, nên handler chạy quá 5 s (mặc
  định cho tới 30 s) thì không ghi được hẹn lại, `dead`, hay trả về hàng chờ khi `Stop`: việc kẹt `running` tới hết giữ
  chỗ, mất lịch lùi hoặc thành `in_doubt`. Đã sửa: hạn ghi tạo ngay trước lần ghi (`recordCtx`, sau handler). Test đỏ
  trước khi sửa: `TestRecordAfterLongHandler`, `TestStopAfterLongHandlerReleases` (hạn ghi 50 ms qua
  `SetRecordTimeout`).
- Ghi chú không bắt buộc đã xử lý: thêm dòng `identity/internal/store/models.go` vào Ảnh hưởng. Chưa làm: chặn
  `Policy.Timeout >= Lease` ở `NewWorker` (kết quả vẫn ghi đúng một lần nhờ điều kiện `locked_by`; ghi vào Việc phát
  hiện thêm).

Commit 2, vòng 2: **ĐẠT**, không còn phát hiện phải sửa. Reviewer xác nhận mọi lần ghi sau handler dùng
`recordCtx` (không còn hạn tạo sớm), hạn ghi vẫn tách khỏi việc hủy của `Stop` và `Timeout`, hai test mới kiểm đúng ca
lỗi. Ghi chú không bắt buộc còn lại: `Timeout >= Lease` (Việc phát hiện thêm 4).

Sau review chỉ thêm tài liệu (không đổi mã): dòng "khi nào không dùng" của `internal/platform/queue` trong
`docs/danh-muc-ghi-chu.yaml` (cổng `make catalog-check` đòi) và `docs/danh-muc-dung-chung.md` sinh lại.
