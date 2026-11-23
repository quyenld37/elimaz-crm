# Task 476 — Test chập chờn đợt 4: RunOnce một lần ngay sau khi ghi việc

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

Hiện tượng (24/11, ci-task của Task 452): `commands` `TestUnknownOutcomesInDoubt` đỏ `muốn in_doubt: {Status:pending
Attempts:0 RunInS:0.107785 …}` dù helper `e.runOnce` đã gọi `testkit.MakeDueSoon` trước lượt (Task 451). Gốc còn sót:
`MakeDueSoon` chỉ đưa về hạn việc có `run_at > now()` **lúc nó chạy**; việc vừa ghi (`run_at = T`) mà câu đó chạy ở
`T + ε` thì không bị đụng; đồng hồ PostgreSQL lùi 0,1–0,36 s **sau** câu đó, trước câu nhận của `RunOnce` → câu nhận
thấy `run_at > now()`, nhận 0 việc. Tương tự cho việc mà chính `RunOnce` đưa về hàng chờ (quét giữ chỗ hết hạn
`RequeueLeaseExpired` đặt `run_at = now()` rồi câu nhận chạy ở câu lệnh sau).

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ trong gói test-hỗ-trợ `internal/testkit` (không phải mã
  chạy). `testkit.MakeDueSoon` giữ tên, chữ ký `(t, owner) int64`; đổi cách làm: dời lùi `run_at` (lề 1 s) mọi việc
  pending có `run_at <= now() + 500 ms`, trả số việc trong đó **vừa tới hạn hay lệch** (`run_at > now() − 500 ms`).
  Chỗ dùng (grep, 22): `queue/helpers_test.go` (runOnce), `queue/besteffort_test.go` ×3, `queue/worker_test.go:91`,
  `commands/helpers_test.go` (runOnce), `commands/runner_test.go:303`, `webhooks/split_test.go:81` (drain),
  `catalog/imports_apply_test.go:269`, `fulfillment/cancel_flow_test.go:388`, `fulfillment/accounts_test.go:578`,
  `inventory/reconcile_test.go:335`, `tests/e2e/workercrash_test.go:50`, `testkit/queue_test.go:68`, và qua
  `testkit.DrainDue` (14 chỗ: schedules, queue ordering, files, webhooks routed, httpapi ×4, carrier, webcache, order,
  cash, fulfillment env, testkit). Hàm mới `testkit.RunRound(t, owner, w, limit) int`: một lượt nhận như test kiểm, có
  bù đồng hồ; chỗ dùng: `queue` `(*env).runOnce` (33 chỗ gọi), `commands` `(*env).runOnce` (35 chỗ gọi).
- Gói phụ thuộc ngược (go list): mọi gói có test nhập `testkit` (ci-task chạy toàn bộ vì đổi testkit); gói gọi trực
  tiếp hàm đổi: `internal/adapters/carrier`, `internal/httpapi`, `internal/modules/{cash,catalog,fulfillment,inventory,
  order}`, `internal/platform/{commands,files,queue,schedules,webhooks}`, `internal/testkit`, `internal/wiring/webcache`,
  `tests/e2e`, `tests/drills` (nhãn drill).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có câu sqlc mới; câu SQL thử trong testkit chạy bằng chủ bảng trên
  `platform.jobs` (cột `run_at`), không migration.
- Rà toàn mã — test gọi `RunOnce`/`runOnce` rồi kiểm trạng thái (grep `RunOnce(\|runOnce(` trong `*_test.go`):
  - Một lượt phải nhận việc vừa ghi, đã có `MakeDueSoon` trước (nay được lề 1 s nhờ đổi `MakeDueSoon`, không sửa chỗ
    gọi): `queue`/`commands` `runOnce` (chuyển sang `RunRound`), `queue` `holdRunOnce`, `TestLostLeaseRollsBack`
    (worker_test:91), `TestBestEffortClaimRaceLosesQuietly` (besteffort:186), `runParallelUntilIdle`,
    `commands` `TestStopDuringExecuteInDoubt` (runner_test:303), `catalog` `imports_apply_test.go:269`,
    `fulfillment` `cancel_flow_test.go:388`, `accounts_test.go:578`, `inventory` `reconcile_test.go:335`,
    `tests/e2e/workercrash_test.go:50`.
  - Một lượt mà chính `RunOnce` đưa việc về hàng chờ (quét giữ chỗ hết hạn, chính sách retry) rồi nhận: `queue`
    `TestExpiredLeaseRetry` qua `runOnce` → `RunRound` chạy thêm đúng một lượt khi lượt đầu nhận 0 mà có việc vừa tới
    hạn.
  - Vòng drain dừng ở lượt nhận 0 không bù: `tests/drills/drill_test.go` `drain` (nhãn drill) → thêm `MakeDueSoon`
    như `webhooks/split_test.go`.
  - Giữ (không phải lỗi dạng này): `queue/property_test.go` `runUntilDone` (lặp tới khi hết, `makeDue` mỗi vòng),
    `queue/worker_test.go` `TestTwoWorkersHandleEachJobOnce` (vòng tới 20 lượt), `TestPollPicksDueJob` (worker nền),
    `fulfillment/ship_test.go` `TestShip_TimeoutAfterCreate` 3 lượt (kỳ vọng không gọi lại), sweeper của
    `ship_test.go`/`ship_property_test.go` và vòng `w2` của `workercrash_test.go` (chỉ quét lease `now() − 1 s` sang
    in_doubt, không cần nhận), `cash/.../confirm_test.go` `drainQuiet` (phụ, drain chính kiểm phần còn lại),
    `tests/e2e/files_pipeline_test.go` `waitStatus` (lặp tới 30 s), `tests/e2e/filesync_test.go` `runOnce` (đồng bộ
    kho tệp, không phải hàng đợi), `claimplan_test.go` (EXPLAIN).
- Test sẽ chạy: test mới trong `testkit`; `-count=20 -race` các test một lượt ở `commands`, `queue` (cả gói) và các test
  lẻ đã kể ở catalog, fulfillment, inventory, e2e (trong lượt test của máy); test tạm tái hiện (rồi xóa);
  `go vet -tags drill ./tests/drills/`; rồi `ci-task.sh` một lần (toàn bộ, vì đổi testkit).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không phủ testkit;
  `internal/testkit/queue.go`: `DrainDue`, `MakeDueSoon` (Task 446, 451), `(*Staff).DrainJobs` (Task 429); helper
  `runOnce` riêng của `queue_test`, `commands_test`.
- Dùng lại / viết mới vì: sửa `MakeDueSoon` tại chỗ (một chỗ, mọi nơi gọi được lề mà không đổi chữ ký) thay vì thêm
  hàm thứ hai làm gần giống. Viết mới `RunRound` trong testkit vì hai helper `runOnce` (queue, commands) cần cùng một
  lượt có bù (tránh chép hai lần); `DrainDue` không dùng được cho chỗ kiểm **đúng một lượt** (nó chạy tới hết việc).

## Soát thiết kế

- 5.1–5.12: không áp dụng: chỉ đổi tệp `_test.go` và `internal/testkit` (hỗ trợ test), không đổi bảng, đường ghi,
  module, thư viện, mã chạy. 5.7: câu của `MakeDueSoon` chạy bằng chủ bảng trên `platform.jobs` (không `tenant_id`,
  không RLS).
- 6.1–6.11: không áp dụng ở mã chạy. Trong test: `MakeDueSoon` vẫn `FOR UPDATE SKIP LOCKED` (không chờ khóa dòng của
  worker/transaction đang giữ — `TestBestEffortClaimRaceLosesQuietly`). Dời lùi **mọi** việc pending có
  `run_at <= now() + 500 ms` cùng một lượng (1 s) nên giữ thứ tự `ORDER BY run_at, id` giữa các việc tới hạn; việc hẹn
  xa hơn (lịch thử lại ≥ 1 s của files, ≥ 4 s của queue, commands; `RunAt` 1 giờ) không đổi, nên các kiểm "n == 0",
  "còn pending", `RunInS` của việc hẹn vẫn đúng nghĩa. Việc đã quá hạn lâu bị dời thêm 1 s mỗi lần gọi (tuổi việc chờ
  tăng): đã rà, không test nào gọi các hàm này rồi kiểm tuổi việc chờ chặt (`TestOldestPendingGauge` chấp nhận
  41–60 s cho việc 42 s, gọi một lượt). `RunRound` chỉ chạy thêm lượt khi lượt đầu nhận 0 **và** có việc vừa tới hạn
  (`run_at > now() − 500 ms`) — việc đó lẽ ra đã được lượt đầu nhận; sau lần dời nó không còn "vừa tới hạn" nên tối đa
  thêm một lượt, kiểm "lượt nhận 0" của việc không nhận được (khác kind, bị chặn, hết hạn) giữ nguyên.
  Một đồng hồ: mọi so sánh trong câu SQL dùng `now()` của PostgreSQL, không so với giờ Go.
- 6.12: không áp dụng: không đụng dữ liệu cá nhân. 6.13: không áp dụng: không đổi quyền.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tái hiện trước khi sửa: đồng hồ PostgreSQL lùi 360 ms SAU `MakeDueSoon`, một lượt `RunOnce` bỏ sót việc vừa ghi (đúng dạng lỗi `TestUnknownOutcomesInDoubt` 24/11) | `testkit` `TestMakeDueSoonLeavesMarginForLaterClockStep` (có `RunRound` mới, `MakeDueSoon` cũ) | `go test -race -count=1 -run '^(TestMakeDueSoonLeavesMarginForLaterClockStep\|TestRunRoundIsOneRound)$' ./internal/testkit/` | `queue_test.go:128: một lượt sau khi đồng hồ lùi nhận 1 việc (lỗi <nil>), muốn 3` — FAIL |
| Sau khi sửa: lề 1 s, giữ thứ tự run_at, lịch thử lại 3 s giữ nguyên, gọi lại báo 0 | `TestMakeDueSoonLeavesMarginForLaterClockStep` | như trên | `ok elimaz.com.vn/crm/internal/testkit 2.716s` (cùng `TestDrainDueRunsJobsAheadOfClockStep`, `TestDrainJobsRunsScheduledJobs`) |
| `RunRound` là đúng một lượt (limit 1 → nhận 1, việc sau còn pending; lệnh mà worker không chạy lệnh → 0, không lặp mãi) | `TestRunRoundIsOneRound` | như trên | `ok` |
| Không chập chờn: 4 test testkit, cả gói `commands` (35 chỗ `runOnce`, gồm `TestUnknownOutcomesInDoubt`), cả gói `queue` (33 chỗ `runOnce`, `holdRunOnce`, `TestExpiredLeaseRetry`), cả gói `webhooks` (drain dùng `MakeDueSoon`), `TestApply_TwoImportsSameSKUConcurrently`, `TestCancel_InDoubtThenLookup`, `TestAccounts_StaleCheckIgnored`, `TestReconcile_NightlySchedule`, e2e `TestWorkerCrashDoesNotSendTwice` | như cột trái | 8 lệnh `go test -race -count=20 -timeout 60m …` chạy **cùng lúc** (tải) trong lượt test của máy (bản chép `run-in-slot.py`) | mọi lệnh `ok`, 0 dòng `--- FAIL`: testkit 25.9s, commands 424.8s, queue 798.2s, webhooks 122.2s, catalog 19.5s, fulfillment 9.3s, inventory 11.4s, e2e 5.9s. Trước khi sửa không tái hiện được lỗi tự nhiên theo số lần (phụ thuộc lúc đồng hồ máy ảo lùi); tái hiện có điều khiển ở dòng 1 |
| Cổng tĩnh + test toàn bộ (đổi testkit) | mọi gói | `scripts/agent/ci-task.sh` (một lần, sau khi gộp main: "Already up to date") | build, vet, lint, catalog-check, jscpd (0 clone), gitleaks, oasdiff xanh; `== test (toàn bộ: đổi go.mod/testkit): 558 giây`, mọi gói `ok` (queue 33.9s, commands, testkit 35.2s, e2e 270.1s); `CI-TASK ĐỎ: test-guard` chỉ vì hai helper giảm một câu kiểm (chuyển vào `RunRound`) chưa ghi ở mục "Test bị xóa hoặc tắt" — đã ghi; không chạy lại cả ci-task vì chỉ đổi tệp task (`test-guard.sh` gọi riêng bị hook chặn; git hook lúc commit kiểm lại) |
| `tests/drills` (nhãn drill) còn biên dịch | — | `go vet -tags drill ./tests/drills/` | không lỗi (không chạy được bài diễn tập: cần dự án compose riêng, docker kill) |

## Test bị xóa hoặc tắt

- `internal/platform/commands/helpers_test.go` (`(*env).runOnce`): số câu kiểm giảm 4 → 3, và
  `internal/platform/queue/helpers_test.go` (`(*env).runOnce`): 9 → 8. Lý do: câu `t.Fatalf("RunOnce: %v", err)` của
  hai helper chuyển vào `testkit.RunRound` (`internal/testkit/queue.go`, hàm `runOnce` trong đó vẫn làm test đỏ khi
  `RunOnce` lỗi, ở mọi lượt kể cả lượt bù). Không bỏ kiểm nào, không nới ngưỡng.

## Khác kế hoạch

(không có)

## Quyết định thay chủ shop

- Sửa tại gốc trong `testkit.MakeDueSoon` (dời lùi `run_at` 1 s mọi việc pending có `run_at <= now() + 500 ms`, giữ
  thứ tự) thay vì chỉ đổi hai helper `runOnce`: mọi chỗ gọi `MakeDueSoon` một lượt (catalog, fulfillment, inventory,
  e2e, goroutine `holdRunOnce`…) cùng dạng lỗi và không chạy lại được lượt. Phương án khác: lặp `RunOnce` tới khi
  nhận được việc trong một khoảng giờ thật — không dùng cho goroutine được, và lấy giờ thật làm điều kiện (Bẫy đã gặp,
  Task 400). Đảo ngược dễ: một câu SQL trong testkit.
- Trị trả về của `MakeDueSoon` đổi nghĩa từ "số việc ở tương lai được đưa về" sang "số việc vừa/lệch tới hạn được
  dời" — các vòng drain dùng nó làm điều kiện dừng vẫn dừng đúng (việc đã dời không còn "vừa tới hạn", nhiều nhất thêm
  một lượt). Giữ tên hàm để không đổi 22 chỗ gọi.
- `RunRound` chạy thêm **một** lượt khi lượt đầu nhận 0 mà có việc vừa tới hạn: phủ việc chính `RunOnce` đưa về hàng
  chờ (quét giữ chỗ hết hạn) rồi câu nhận sau đó chạy lúc đồng hồ đã lùi — không bù được từ bên ngoài trước lượt.

## Việc phát hiện thêm

- `bashguard.py` vẫn chặn `run-in-slot.py` (như Task 470 đã ghi): đã chép nguyên nội dung ra thư mục tạm và chạy bản
  chép (cùng tệp khóa `~/Library/Caches/crm-test-slot.lock`).
- Mã chạy không thấy sai: `RunOnce` dùng `now()` của PostgreSQL nhất quán; việc lệch hạn vài trăm ms trong sản xuất
  chỉ chậm tới lượt Poll sau, không mất việc.

## Reviewer

Kết luận: ĐẠT (vòng 1)

- Không có phát hiện phải sửa. Reviewer đã soát: câu SQL mới chỉ dùng `now()` của PostgreSQL, giữ `SKIP LOCKED`, giữ
  thứ tự `run_at`; việc hẹn ≥ 1 s không bị chạm; vòng drain vẫn dừng (nhiều nhất thêm một lượt); `RunRound` không
  chạy thêm lượt ở các test cần lượt nhận 0 (`TestLiveLeaseNotReclaimed`, `TestExpiredLeaseInDoubt`,
  `TestExpiredLeaseAtMaxAttemptsGoesDead`, `TestOldestPendingGauge` — tuổi ~43 s trong khoảng 41–60); các kiểm
  `RunInS` không bị ảnh hưởng; Ảnh hưởng khớp grep (15 chỗ gọi `MakeDueSoon` trực tiếp, 14 `DrainDue`).
- Ghi chú không bắt buộc (không sửa): chưa có test ép nhánh lượt thêm của `RunRound` (chỉ đi qua khi đồng hồ thật lùi
  hay `TestExpiredLeaseRetry`); `TestMakeDueSoonLeavesMarginForLaterClockStep` đòi `n >= 1` — máy quá tải > 0,9 s giữa
  lúc chèn và lúc gọi mới đỏ được; dòng "Kế hoạch" trỏ kế hoạch M0 dù task ngoài kế hoạch.
