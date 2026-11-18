# Task 12 — Lệnh ra ngoài — không tự gửi lần hai

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Gói mới `internal/platform/commands` (0 chỗ dùng trước task này): `Outcome`, các hằng `StatusDone`,
    `StatusNotSent`, `StatusRejected`, `StatusUnknown`, `Handler`, `Looker`, `DoneHook`, `CommandKind`, `Runner`,
    `NewRunner`, `Runner.Run`, `ResolveInDoubt`.
  - `internal/platform/queue`: **thêm** `CommandRunner` (interface phía dùng, Worker giao việc `mode = 'command'`),
    trường `WorkerOptions.Commands`, và phần quản lý việc cho API: `JobSummary`, `ListAttention`, `RetryJob`,
    `AbandonJob`, `ErrJobNotFound`, `ErrJobState`. Không ký hiệu có sẵn nào đổi chữ ký. `WorkerOptions` có 1 chỗ dựng
    ngoài gói (`cmd/crm/main.go:155`, không đổi vì chưa có lệnh nào) và các test của `queue`.
  - Hành vi `Worker` đổi: `ClaimJobs` nhận cả việc `command` **chỉ khi** `WorkerOptions.Commands` khác nil (test cũ
    `TestCommandJobsNotClaimed` giữ nguyên ý: worker không có runner vẫn không nhận việc command).
  - `internal/platform/internal/store` (mã sinh): `ClaimJobs` thêm tham số `with_commands` và cột `mode` (1 chỗ gọi:
    `queue/worker.go`); thêm câu mới cho commands và quản lý việc.
  - `internal/httpapi`: `handler` (không xuất) thêm `ListJobs`, `RetryJob`, `AbandonJob` thay cho bản 501 của
    `gen.UnimplementedHandler`; `Deps` thêm `WriteLockTimeout` (rỗng: `db.TxAPI`) để test ca `retry_later`;
    `errors.go` thêm hai lỗi `queue.ErrJobNotFound` → 404 `not_found`, `queue.ErrJobState` → 409 `job_state_conflict`.
    `httpapi.Deps` dựng ở `cmd/crm/main.go` (không đổi) và trong test của gói.
  - `internal/testkit`: thêm `FakeExternal` (tệp mới).
  - Phát hiện khi viết (đã thêm vào phạm vi, xem Khác kế hoạch): `queue.errText` đổi tên thành `queue.ErrorText`
    (xuất ra để `commands` dùng cùng giới hạn `last_error`; 2 chỗ gọi và định nghĩa, đều trong `worker.go`; commit cấu trúc riêng);
    `queue.AttentionStatuses()`.
- Gói phụ thuộc ngược (go list): `queue` ← `cmd/crm`; `httpapi` ← `cmd/crm`, `tests/arch`; `testkit` ← mọi gói có
  test trên PostgreSQL (`cfaccess`, `httpapi`, `identity`, `db`, `idempotency`, `metrics`, `queue`, `tenancy`,
  `tests/arch`, `tests/e2e`) — chỉ thêm tệp, không đổi gì có sẵn; `platform/internal/store` ← `db`, `idempotency`,
  `metrics`, `queue`, `tenancy` (+ `commands` mới).
- Operation OpenAPI bị chạm, oasdiff: `listJobs`, `retryJob`, `abandonJob` đã có trong `openapi.yaml` từ Task 6
  (trước trả 501). Task này chỉ viết handler; hợp đồng không đổi, oasdiff: không phá vỡ (không sửa `openapi.yaml`).
- Chỗ dùng trên màn hình: chưa có màn hình (Task 17).
- Câu sqlc, bảng, pha migration: **không migration** (bảng `platform.jobs` của 0007 đủ cột). Sửa `ClaimJobs`; thêm
  câu trong `queries/platform/jobs.sql` (quản lý việc: liệt kê, chạy lại, bỏ) và tệp mới `queries/platform/commands.sql`
  (ghi kết quả lệnh, `in_doubt`, tra lại). Thứ tự khóa: route retry/abandon sửa dòng `platform.jobs` rồi chèn
  `identity.audit_logs` (chỉ chèn) rồi `Claim.Complete` (`platform.idempotency_keys`) — đúng ngoại lệ (c) đã ghi sẵn
  trong `docs/adr/0046-thu-tu-khoa.md`. Runner: hook nghiệp vụ trước, `platform.jobs` cuối (như Task 11).
- Test sẽ chạy: vùng rủi ro cao (`queue`, `commands`, `idempotency`, `httpapi/guard.go` cùng gói) nên chạy cả bộ:
  `go test -race ./internal/... ./cmd/... ./tests/...` trên dự án compose `crm-t12` (cổng 5512); `make ci` phần chạy
  được trên máy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `db.RetryTx`, `db.TxAPI`, `db.ErrRetryLater`,
  `tenancy.WithTenant`, `queue.NextDelay`, `queue.ErrPermanent`, `queue.TerminalHook`, `metrics.Registry`,
  `httpapi.WriteTx`, `identity.RecordAudit`, `testkit.OpenStaff`, `Staff.Tenant/User/Grant/Exec`, `AccessKeyServer`.
  Tìm `in_doubt`, `Lookup`, `fake`, `Execute` trong `internal/`: chỉ có phần quét giữ chỗ hết hạn của Task 11
  (`FinishLeaseExpired`, `on_lease_expired = 'in_doubt'`) và test thử bộ nối `cfaccess`.
- Dùng lại / viết mới vì:
  - Giữ chỗ hết hạn → `in_doubt` + `OnTerminal`, và `pending` quá hạn → `expired` + `OnTerminal`: **dùng lại** quét
    của `queue.Worker` (Task 11), không viết lại trong `commands`. Worker tìm `TerminalHook` của lệnh qua `CommandRunner`.
  - Thời gian chờ khi `not_sent`: `queue.NextDelay`. Ghi trạng thái: `db.RetryTx`/`tenancy.WithTenant`; câu đổi trạng
    thái của lệnh viết mới trong `queries/platform/commands.sql` vì điều kiện khác (`expires_at`, `finished_at` đã thấy),
    còn `CompleteJob`, `KillJob`, `ReleaseJob` của Task 11 được dùng lại nguyên.
  - Hàm nhỏ `inTx` (WithTenant khi có tenant, ngược lại RetryTx) lặp 3 dòng giữa `queue` và `commands`: giữ riêng,
    không xuất từ `queue` chỉ để gộp 3 dòng.
  - Route: `WriteTx` (Task 9) + `RecordAudit` (Task 8), lỗi đổi sang HTTP ở `errors.go`. Test HTTP dùng lại cách dựng
    của `idempotency_test.go` (`testkit.OpenStaff`, `AccessKeyServer`, `authEnv.verifier`).
  - `FakeExternal` viết mới trong `testkit` (kế hoạch ghi tên tệp), không thư viện.
  - Không thêm thư viện.

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: lệnh ra ngoài là của `platform` (04 mục 5.3); bảng `platform.jobs`; mã trong `internal/platform/commands`
   và `queue`. Bảng nghiệp vụ của lệnh do module sở hữu chạm qua `OnDone`/`OnTerminal`.
2. Không ghi bảng module khác: `commands` chỉ ghi `platform.jobs`; dòng audit qua public API `identity.RecordAudit`.
3. Đọc dữ liệu module khác: không áp dụng.
4. Việc nguyên tử: lệnh được ghi bằng `JobQueue.Enqueue` trong transaction nghiệp vụ (Task 11). `done` + `OnDone`
   cùng một transaction; dead/in_doubt + `OnTerminal` cùng một transaction. Đổi trạng thái việc qua API + audit + kết quả
   chống trùng cùng một transaction (`WriteTx`).
5. Việc không nguyên tử: chính là phần này — gọi ra ngoài chạy **ngoài** transaction, sau khi việc đã `running` và commit.
6. Gọi ra ngoài: chỉ qua `Handler.Execute`; không chắc → `in_doubt`, không tự gửi lại. Chỉ `Lookup` nói "chưa gửi"
   (hoặc nhân viên bấm chạy lại) mới đưa về `pending`.
7. Ngữ cảnh shop: `platform.jobs` không RLS (Global Constraints, 04 mục 11); vì vậy mọi câu của API lọc tường minh
   `tenant_id = $tenant` của người gọi và chạy trong `WithTenant` (cùng transaction với audit có RLS). Runner ghi kết
   quả trong `WithTenant(job.tenant_id)`.
8. Nghiệp vụ không biết HTTP: `commands`, `queue` không nhập `net/http`; handler HTTP mỏng gọi `queue.*`.
9. Mọi đường ghi có khóa chống trùng và quyền: `retryJob`, `abandonJob` khai `platform.jobs.manage` và `IdempotencyKey`
   (đã có), qua `WriteTx`.
10. PostgreSQL là nguồn sự thật: mọi trạng thái trong `platform.jobs`; không Valkey.
11. Không thêm công nghệ: không.
12. Module mẫu: route retry thành mẫu đường ghi (Step 4b, CLAUDE.md).

Mục 6 (đồng thời):

1. Hai bên cùng sửa một việc: mọi câu đổi trạng thái có điều kiện (`status = 'running' AND locked_by = $me`; API:
   `status IN (…)` và `tenant_id`; tra lại: `status = 'in_doubt' AND finished_at = <đã thấy>` để kết quả tra cũ không
   đưa về `pending` một việc đã chạy lại và lại `in_doubt`). 0 dòng → lùi, không ghi gì.
2. Gửi lại cùng yêu cầu: `Idempotency-Key` qua `WriteTx`; test 50 yêu cầu song song cùng khóa → 1 lần đổi, 1 dòng audit.
3. Thứ tự khóa: `jobs` → `audit_logs` (chèn) → `idempotency_keys`, ngoại lệ (c) đã ghi trong ADR-46; runner: hook → `jobs`.
4. Lỗi tạm: `WriteTx` (RetryTx ×3) → 503 `retry_later` không lưu (test có khóa dòng `jobs` giữ 2 s, `lock_timeout` 50 ms).
5. READ COMMITTED + câu có điều kiện; không khóa thêm.
6. Việc nền lỗi: `not_sent` thử lại theo `NextDelay`/`RetryAfter`, tối đa 8 lần, rồi `dead`; `rejected` → `dead` ngay.
7. 429: `Outcome.RetryAfter` của handler được dùng thay lịch.
8. Tiến trình chết: giữ chỗ hết hạn → `in_doubt` (quét của Task 11, `on_lease_expired = 'in_doubt'` của `CommandKind`).
   `Stop` giữa lúc gọi → `in_doubt`; `Stop` trước khi gọi → trả về hàng chờ (chưa gửi gì).
9. Treo lâu: `Execute` chạy với hạn `Policy.Timeout` (≤ 30 s < giữ chỗ 90 s); quá hạn → `in_doubt`.
10. Tiền: không áp dụng.
11. Quy luật: rapid 100 lượt "một `ref` không bao giờ được bên ngoài ghi nhận quá 1 lần khi `Lookup` nói thật".
12. Dữ liệu cá nhân: API không trả `payload`; audit chỉ có mã nhân viên (uuid), mã việc (uuid), mã yêu cầu; không IP.
    `last_error` là chuỗi lỗi của handler (luật của Task 11: chỉ mã bản ghi).
13. Quyền: `platform.jobs.manage`, guard mặc định từ chối; việc của tenant khác → 404 (không lộ là có).

## Quyết định thay chủ shop

1. **Từ chối (404 không có việc / 409 sai trạng thái) không lưu vào khóa chống trùng** (`final = false` trong
   `WriteTx`). Vì sao: không đổi gì trong database, lưu thân lỗi sẽ phát lại `request_id` cũ; gửi lại cùng khóa thì xét
   lại theo trạng thái lúc đó. Phương án khác: lưu như kết quả cuối (ADR-41 "kết quả cuối") — đảo được bằng đổi một dòng.
2. **`abandon` chỉ cho `dead`, `in_doubt`** (kế hoạch chỉ ghi "→ expired"). Vì sao: việc `running` có thể là lệnh đang
   ra ngoài (bỏ giữa chừng làm mất kết quả); việc `pending` chưa qua `OnTerminal` nên module nghiệp vụ không biết nó bị
   bỏ. `retry` cho `dead`, `in_doubt`, `expired` đúng kế hoạch. Phương án khác: cho `pending` (cần gọi `OnTerminal` từ
   tiến trình api, nơi không có handler).
3. **Audit không ghi IP**; `ActorID` = mã nhân viên (uuid), `EntityID` = mã việc, `RequestID` = mã yêu cầu đã kiểm mẫu;
   `Before/After` chỉ `status`, `attempts`. Vì sao: IP là dữ liệu cá nhân của nhân viên, chưa cần ở M0.
4. **`Runner.Run` nhận thêm `workerID` và trả trạng thái đã ghi** (kế hoạch: `Runner.Run(ctx, j)`). Vì sao: mọi câu
   ghi kết quả có điều kiện `locked_by = <worker giữ chỗ>`; trạng thái trả về để Worker đếm `jobs_dead`,
   `jobs_in_doubt` ở một chỗ. Worker nhận runner qua interface phía dùng `queue.CommandRunner` (queue không nhập
   commands, tránh vòng phụ thuộc).
5. **`ResolveInDoubt` chưa được nối vào lịch chạy** (kế hoạch không ghi ai gọi; việc định kỳ là Task 13). Ghi ở Việc
   phát hiện thêm.
6. **Không migration.** Danh sách `listJobs` lọc `tenant_id` + `status` chưa có index riêng (quét `jobs_order`/bảng);
   chấp nhận ở M0 vì dòng `done` sẽ được dọn (Task 13). Ghi ở Việc phát hiện thêm.

## Bằng chứng

Database: compose `crm-t12`, PostgreSQL cổng 5512 (`CRM_TEST_DATABASE_URL`). Test viết trước, chạy đỏ trước khi có mã:
`go test ./internal/platform/commands/ ./internal/httpapi/` → `FAIL … [build failed]` (chưa có gói `commands`,
`Deps.WriteLockTimeout`).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `ok` → done, Calls 1, OnDone 1 lần (trong ngữ cảnh tenant) | `TestOkDone` | `go test -race -count=1 ./internal/platform/commands/` | `ok … commands` |
| Execute ngoài transaction (running đã commit) | `TestExecuteAfterRunningCommitted` | như trên | ok |
| **`timeout_after_send` → in_doubt; thêm 3 lượt Calls vẫn 1** | `TestTimeoutAfterSendInDoubtNeverResent` | như trên | ok |
| **Worker chết (giữ chỗ qua) → in_doubt, Execute không gọi** | `TestLeaseExpiredInDoubtWithoutExecute` | như trên | ok |
| `not_sent`: tự gửi lại → pending tương lai (5 s ±20%, hoặc RetryAfter); không → dead | `TestNotSent/{auto,retry-after,no-auto}` | như trên | ok |
| `not_sent` ở lượt 8 → dead | `TestNotSentLastAttemptDead` | như trên | ok |
| `reject` → dead, OnTerminal("dead") | `TestRejectDead` | như trên | ok |
| unknown / trạng thái lạ / Execute lỗi → in_doubt | `TestUnknownOutcomesInDoubt` | như trên | ok |
| Quá Timeout → in_doubt, `last_error` có `deadline` | `TestExecuteTimeoutInDoubt` | như trên | ok |
| `CommandKind(…,100ms,…)` luôn not_sent → sau 150 ms: expired, OnTerminal("expired") 1 lần | `TestCommandExpires` | như trên | ok |
| `Stop(50ms)` lúc Execute đang chờ → in_doubt | `TestStopDuringExecuteInDoubt` | như trên | ok |
| Stop trước khi gửi → pending, attempts 0, không gọi | `TestStoppedBeforeExecuteReleased` | như trên | ok |
| Lệnh không handler → dead; worker không runner không nhận lệnh | `TestCommandWithoutHandler`, `queue.TestCommandJobsNotClaimed` | `go test -race -count=1 ./internal/platform/commands/ ./internal/platform/queue/` | ok, ok |
| Ghi done lỗi → in_doubt, không gửi lại | `TestDoneRecordFailsInDoubt` | như trên | ok |
| Hạn ghi kết quả tính sau Execute: Execute 300 ms, hạn ghi 50 ms → vẫn done + OnDone (phát hiện reviewer) | `TestRecordAfterLongExecute` | `go test -race -count=1 ./internal/platform/commands/` | đỏ trước khi sửa (build: chưa có `recordTimeout`), sau sửa ok |
| Timeout của lệnh bị chặn dưới nửa giữ chỗ: Lease 200 ms, Timeout 5 s → in_doubt do quá giờ trong < 2 s | `TestCommandTimeoutBelowLease` | như trên | ok |
| `ResolveInDoubt`: done → done, Calls không tăng; not_sent → pending rồi Calls 1; unknown → in_doubt; không Looker → in_doubt | `TestResolveDone`, `TestResolveNotSentRequeues`, `TestResolveUnknownAndNoLooker` | `go test -race -count=1 ./internal/platform/commands/` | ok |
| Kết quả tra cũ không áp khi việc đã đổi | `TestResolveStaleLookupIgnored` | như trên | ok |
| **Thuộc tính (rapid 100 lượt): một ref không bao giờ bị ghi nhận quá 1 lần; không lệnh nào kẹt running** | `TestPropertyNeverSentTwice` | như trên, chạy 5 lần liền | `[rapid] OK, passed 100 tests`, 5/5 ok (trước khi sửa `dispatch` thì đỏ: "2 lệnh kẹt running … w1-4", xem Khác kế hoạch) |
| API: không quyền 403; tenant khác 404; retry hợp lệ → pending, attempts 0, bỏ hạn, 1 dòng audit; body không `payload`; thiếu khóa 400; gửi lại cùng khóa phát lại | `TestRetryJob`, `TestListJobs` | `go test -race -count=1 ./internal/httpapi/` | `ok … httpapi` |
| retry/abandon theo trạng thái (200/409), abandon có audit, tenant khác 404 | `TestRetryJobStates`, `TestAbandonJob` | như trên | ok |
| 50 yêu cầu song song cùng khóa → 1 lần đổi, 1 dòng audit, chỉ 200 cùng thân hoặc 409 | `TestRetryJobFiftyParallel` | như trên | ok |
| **`retry_later` không lưu (ADR-46)**: khóa dòng 2 s, lock_timeout 50 ms → 503 + Retry-After, 0 dòng khóa, 0 audit; gửi lại cùng khóa → 200, pending, 1 audit, không Idempotent-Replayed | `TestRetryJobRetryLaterNotStored` | như trên | ok |
| Không vỡ phần khác (vùng rủi ro cao: cả bộ) | mọi test | `go test -race -count=1 ./internal/... ./cmd/... ./tests/...` | mọi gói `ok` (cfaccess, httpapi, identity, commands, config, db, idempotency, logging, metrics, observability, queue, telemetry, tenancy, tests/arch, tests/e2e) |
| Lint, sinh mã, sqlc vet, API, trùng mã, lỗ hổng | — | `golangci-lint run ./...`; `make gen` rồi so mã sinh; `make sqlc-vet`; `make oasdiff`; `make jscpd`; `make vuln`; `make catalog` | `0 issues`; mã sinh không đổi sau `make gen`; `ok tests/arch`; oasdiff không báo; `Found 0 clones`; `No vulnerabilities found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `queue.Worker.dispatch`: câu nhận việc chạy trên `context.WithoutCancel` của vòng. Trước đó, `Stop` hủy ctx đúng lúc
  câu nhận việc commit làm mất câu trả lời: việc kẹt `running` tới hết giữ chỗ 90 s (lệnh thành `in_doubt` dù chưa
  gửi). Test thuộc tính của task này bắt được (`… kẹt running: w1-4 attempts=1`); sửa trong task vì `worker.go` thuộc
  phạm vi (Modify) và lỗi chạm đúng tiêu chí "không kẹt running".
- `Worker` kiểm `hardStop` trước khi giao lệnh cho runner (Stop quá grace trước lúc gửi → trả về hàng chờ, như việc
  transactional); `context.AfterFunc` hủy ctx bất đồng bộ nên không dựa vào `ctx.Err()` lúc đó.
- Thêm `httpapi.Deps.WriteLockTimeout` (kế hoạch: "`TxAPI` có `LockTimeout` thử 50 ms") thay vì sửa biến gói
  `db.TxAPI` trong test (biến dùng chung, test song song sẽ đua).
- `queue` có thêm phần quản lý việc (`ListAttention`, `RetryJob`, `AbandonJob`): kế hoạch chỉ ghi `handlers_jobs.go`,
  nhưng chỉ mã `internal/platform/**` được viết SQL lên `platform.jobs` (Global Constraints), nên handler gọi `queue`.
- `FakeExternal` có thêm `SetMode` (đổi chế độ an toàn khi đang có lời gọi) và `Record` (giả lập worker đã gửi rồi
  chết) cho test thuộc tính; sentinel `ErrFakeNotSent`, `ErrFakeTimeout`, `ErrFakeRejected`.
- `queue.NewWorker`: `Timeout` của loại lệnh bị chặn ở `Lease/2` (ghi chú của reviewer: Timeout ≥ giữ chỗ thì lượt
  quét đặt `in_doubt` khi Execute còn chạy, rồi tra lại có thể đưa về hàng chờ trong lúc lần gửi cũ vẫn đang đi).
  Câu nhận việc của vòng nền có hạn `claimTimeout` 10 s (ghi chú của reviewer: tránh `Stop` chờ mãi khi database treo).
- Hai commit: (1) cấu trúc — đổi tên `errText` → `ErrorText`; (2) hành vi — phần còn lại.

## Việc phát hiện thêm

- Chưa ai gọi `commands.ResolveInDoubt` định kỳ: nên là một việc `task.*` của Task 13 (lịch) hoặc của mốc có lệnh
  thật đầu tiên. `cmd/crm` chưa nối `WorkerOptions.Commands` (chưa có lệnh nào ở M0).
- `listJobs` chưa có index `(tenant_id, status, created_at) WHERE status IN ('dead','in_doubt','expired')`; cần
  migration mới khi bảng lớn (Task 13 dọn dòng `done` giảm bớt).
- `not_sent` khi đã quá `expires_at` thành `dead` (đúng chữ kế hoạch); spec 5.3 phân biệt "hết hạn, chưa gửi được"
  với "bị từ chối" — module nghiệp vụ đầu tiên có lệnh thật nên xét dùng `expired` cho ca này (cần sửa kế hoạch).
- Handler bỏ qua `ctx` của `Execute` vẫn có thể chạy quá giữ chỗ; luật cho handler ("Execute phải theo ctx") nên vào
  skill/mẫu module khi có bộ nối thật đầu tiên.
- `Worker.RunOnce` gọi song song với `Stop` trên cùng Worker là đua `WaitGroup` (Add lúc Wait); đường thật (`Start` +
  `Stop`) không gặp vì `Stop` chờ vòng dừng trước. Chỉ ghi lại; test của task không dùng cách đó.

## Reviewer

Commit 1 (cấu trúc: `errText` → `queue.ErrorText`, dòng danh mục): **ĐẠT**. Không phát hiện ảnh hưởng tính đúng.
Ghi chú không bắt buộc: sửa "3 chỗ gọi" thành "2 chỗ gọi và định nghĩa" (đã sửa); `ErrorText(nil)` sẽ panic — để ý chỗ
`commands` gọi ở commit 2 (mọi chỗ gọi trong `runner.go` truyền lỗi khác nil).

Commit 2 (hành vi), lượt 1: **CẦN SỬA**, 1 phát hiện:
- `commands/runner.go`: hạn ghi kết quả (5 s) dựng trước khi gọi `Execute`; Execute lâu hơn 5 s thì mọi lần ghi sau đó
  (done, not_sent, in_doubt) hết hạn, lệnh kẹt `running` 90 s rồi thành `in_doubt` dù bên ngoài đã xác nhận done.
  **Đã sửa:** `recordCtx` dựng sau `Execute` (nhánh Stop trước khi gửi dựng riêng; lần ghi in_doubt sau khi ghi done lỗi
  dùng hạn mới); test `TestRecordAfterLongExecute` (đỏ trước, xanh sau).
Ghi chú không bắt buộc đã xử lý: chặn `Timeout` lệnh ≤ `Lease/2` (+ test `TestCommandTimeoutBelowLease`); hạn 10 s cho
câu nhận việc của vòng nền. Ghi chú để lại (Việc phát hiện thêm): `not_sent` quá hạn → `dead` hay `expired`.

Commit 2, lượt 2: **ĐẠT**. Phần sửa lượt 1 đúng và đủ (hai test mới đỏ nếu bỏ phần sửa); soát lại toàn bộ 28 tệp
không có phát hiện ảnh hưởng tính đúng. Ghi chú không bắt buộc: Lease rất ngắn (< ~10 s) thì Lease/2 + hạn ghi 5 s có
thể vượt giữ chỗ — vẫn an toàn (không gửi lại), chỉ mất kết quả done; `ResolveInDoubtRequeue` giữ `attempts` và
`expires_at` nên lệnh đã hết lượt hay quá hạn sẽ thành dead/expired thay vì được gửi (an toàn; module đầu tiên có lệnh
thật cần biết).

Kết luận: ĐẠT
