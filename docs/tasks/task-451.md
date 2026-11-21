# Task 451 — Test chập chờn đợt 4: mẩu số trong mã ngẫu nhiên, RunOnce một lần

Kế hoạch: task chung của điều phối viên (21/11), không có trong kế hoạch M1; số toàn cục `x-flaky4` (đã thêm dòng
`| chung (21/11) | x-flaky4 | 451 | Test chập chờn đợt 4 |` vào `../../docs/superpowers/plans/danh-so-task-toan-cuc.md`,
tệp ngoài repo crm). Loại việc (quy trình mục 3): A. Chỉ đổi tệp `_test.go`; không đổi mã chạy.

## Gốc (đo trước khi sửa)

- **(1) `TestListEntityAudit`** (`identity/audit_list_test.go:64` trên main 19ef392): kiểm "không mang giá trị
  before/after" bằng tìm mẩu 4 chữ số `0911`, `0922`, `0933` trong JSON của 3 dòng `AuditRecord` — mỗi dòng có `ID`
  là UUID ngẫu nhiên (32 ký tự hex). Xác suất một UUID chứa một mẩu 4 chữ số cho trước ≈ 29 × 16⁻⁴ ≈ 0,04 %; 3 mẩu × 3
  dòng ≈ 0,4 % mỗi lần chạy, và chuỗi `CreatedAt` thì không chứa được (có dấu `-`, `:`). Tái hiện bằng một test tạm có
  UUID `6f1c0933-…-0911…`: kiểm cũ đỏ (Bằng chứng).
- **(2) `TestHandlerErrorPersonalDataNotLeaked`** (`queue/personaldata_test.go:40`): ghi hai việc "chạy ngay" (`run_at
  = now()` của transaction ghi) rồi gọi `RunOnce` đúng **một** lần; đồng hồ PostgreSQL của máy ảo lùi (Task 446 đo: tới
  0,36 s, vài lần mỗi phút) → `now()` của câu nhận nhỏ hơn `run_at` → nhận 0 việc → "việc … thiếu last_error". Tái hiện
  bằng test tạm đặt `run_at = now() + 300 ms` sau khi ghi: đúng thông báo của main (Bằng chứng).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi helper không xuất trong gói `_test`:
  `queue_test` `runOnce(t, w, limit)` → phương thức `(*env).runOnce` (33 chỗ gọi trong `besteffort_test.go`,
  `errors_test.go`, `personaldata_test.go`, `trace_test.go`, `worker_test.go`), `holdRunOnce` → `(*env).holdRunOnce`
  (2 chỗ gọi, `besteffort_test.go`); `commands_test` `runOnce(t, w)` → `(*env).runOnce` (35 chỗ gọi trong
  `indoubt_test.go`, `runner_test.go`, `property_test.go`). Thêm `assertNoAuditValues` (`identity/audit_list_test.go`,
  1 chỗ gọi).
- Gói phụ thuộc ngược (go list): không áp dụng: chỉ đổi tệp `_test.go` (không gói nào nhập gói test).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không.
- Rà toàn mã (3a) — kiểm "không chứa mẩu số" trên chuỗi có mã ngẫu nhiên: grep `*_test.go` các mẫu
  `(Contains|Count|Index)(… "…[0-9]{3,9}…")`, `regexp` có `[0-9]{n}`/`\d{n}`, vòng `for _, leak := range []string{…}`,
  và chữ số 3–6 ký tự gần `leak|lộ|phone|SĐT`. Chỗ sửa: chỉ (1). Chỗ đã xem và giữ (văn bản không có mã ngẫu nhiên,
  hoặc mẩu dài ≥ 9 chữ số, xác suất trùng ≈ 16⁻⁹): `identity/audit_test.go:279` (JSON của một map cố định),
  `logging/dailyfile_test.go:178`, `logging/redact_test.go:73,137`, `customer/pii_test.go:33` (log JSON: chỉ có `time`
  dạng `2026-11-21T10:20:30.123+07:00` — không có 4 chữ số liền ngoài năm — và trường cố định; `trace_id` chỉ có khi ctx
  có span, ở đây không), `httpapi/handlers_customer_merge_test.go:95` (`977000002`, 9 chữ số),
  `promotion/redeem_test.go:348` (`912345678`), `vtp/webhook_test.go:66` (chuỗi cố định từ tệp mẫu),
  `cash/postings_test.go:258` (đã bỏ mã vận đơn ngẫu nhiên trước khi tìm, Task 440), `logging/scrub_test.go:112`,
  `observability/errors_test.go:61` (quy luật rapid trên đầu ra bộ lọc, không có mã ngẫu nhiên), `testkit`
  `AssertNoPersonalData` (mẩu `FakePhone` 10 chữ số, địa chỉ, token).
- Rà toàn mã (3b) — `RunOnce`/`Claim` một lần ngay sau khi ghi việc rồi kỳ vọng đã chạy (grep `RunOnce(`, `.Claim(`,
  `ClaimJobs` trong `*_test.go`; vòng drain riêng đã đổi ở Task 446). Sửa:
  - `queue`: `runOnce` (mọi chỗ gọi), `holdRunOnce` (trước: lượt nhận 0 → `waitClosed(started)` treo tới chốt),
    `TestBestEffortClaimRaceLosesQuietly` (tin 2 phải tới hạn để lần nhận chờ khóa của tin 1, không thì
    `waitForLockWait` treo), `runParallelUntilIdle` (lượt đầu nhận 0 bị tính là hết việc).
  - `queue/worker_test.go` `TestLostLeaseRollsBack` (RunOnce trong goroutine, `<-started` không chốt chống treo → nay
    `MakeDueSoon` trước và `waitClosed`; reviewer vòng 1 phát hiện).
  - `commands`: `runOnce` (mọi chỗ gọi), `TestStopDuringExecuteInDoubt` (RunOnce trong goroutine).
  - `tests/e2e/workercrash_test.go` `TestWorkerCrashDoesNotSendTwice` (một lượt `w1.RunOnce` rồi chờ bên ngoài nhận
    lệnh 5 s; reviewer vòng 1 phát hiện — lần rà đầu chỉ grep `internal`, `cmd`; đã rà lại `tests/`, `tools/`,
    `spikes/`).
  - `fulfillment/cancel_flow_test.go` `TestCancel_InDoubtThenLookup` (3 lượt ngay sau khi ghi lệnh hủy, kỳ vọng gọi hủy
    1 lần), `fulfillment/accounts_test.go` `TestAccounts_StaleCheckIgnored` (một lượt, kỳ vọng hook chạy — trước có
    thể qua mà không kiểm gì), `inventory/reconcile_test.go` `TestReconcile_NightlySchedule` (một lượt, kỳ vọng 2 việc
    done), `catalog/imports_apply_test.go` `TestApply_TwoImportsSameSKUConcurrently` (hai worker song song một lượt —
    trước, lượt nhận 0 làm test mất ý "song song" nhưng không đỏ vì `e.drain()` sau đó), `cash/cod_test.go`
    `TestConsumerThroughRegistryAndWorker` (2 lượt → `testkit.DrainDue`), `httpapi/shipping_quotes_test.go`
    `TestShippingQuotesAPI` (5 lượt → `testkit.DrainDue`).
  - Giữ: `queue/property_test.go` `runUntilDone` (lặp tới khi hết việc, `makeDue` mỗi vòng), `fulfillment/ship_test.go`
    `TestShip_TimeoutAfterCreate` 3 lượt (kỳ vọng **không** gọi tạo lại; lệnh đã in_doubt, lượt nhận 0 không đổi kết
    luận), `fulfillment/ship_property_test.go:98`, `ship_test.go:435` (worker quét lease hết hạn: `lease_until = now() −
    1 s`, lề 1 s lớn hơn lần lùi đo được 0,36 s), `cash/internal/statement/confirm_test.go` `drainQuiet` (Task 446 đã
    xét), `claimplan_test.go` (EXPLAIN, không chạy việc), `queue/worker_test.go` `TestPollPicksDueJob` (worker chạy
    nền có Poll), `TestTwoWorkersHandleEachJobOnce` (vòng tới 20 lượt, dừng khi mọi việc done),
    `tests/e2e/workercrash_test.go` vòng `w2` (lease `now() − 1 s`), `tests/e2e/files_pipeline_test.go` `waitStatus`
    (nhãn `imager`, lặp tới 30 s).
- Test sẽ chạy: `go test -race` cả gói `queue`, `commands`, `identity`; `-run` các test đã sửa ở fulfillment,
  inventory, catalog, cash, httpapi; mỗi test sửa lặp 30 lần, các gói chạy cùng lúc (tải); rồi `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không phủ testkit; tìm
  theo tên trong `internal/testkit/queue.go`: `testkit.DrainDue`, `testkit.MakeDueSoon` (Task 446: bù đồng hồ
  PostgreSQL lùi ≤ 500 ms, `SKIP LOCKED`, giữ lịch thử lại), `(*Staff).DrainJobs` (đưa mọi việc hẹn về hạn).
- Dùng lại / viết mới vì: dùng lại `MakeDueSoon` trước mỗi lượt ở chỗ test kiểm **đúng một lượt** (số việc nhận,
  việc còn pending, việc đang chạy) — `DrainDue` sẽ chạy thêm lượt và đổi điều test kiểm; dùng `DrainDue` ở chỗ vòng
  cũ chỉ để "chạy hết" (cash, httpapi). Không viết hàm testkit mới. `assertNoAuditValues` viết mới trong test identity
  (một chỗ dùng; `testkit.AssertNoPersonalData` quét mẩu cố định của testkit, không phải SĐT riêng của test này).

## Soát thiết kế

- 5.1–5.12: không áp dụng: chỉ đổi tệp `_test.go`, không đổi bảng, đường ghi, module, thư viện. 5.7: câu
  `MakeDueSoon` chạy bằng chủ bảng trên `platform.jobs` (không có `tenant_id`, không RLS), như Task 446.
- 6.1–6.11: không áp dụng ở mã chạy. Trong test: `MakeDueSoon` dùng `FOR UPDATE SKIP LOCKED` nên không chờ khóa dòng
  của transaction khác — `TestBestEffortClaimRaceLosesQuietly` giữ tin 1 bằng một transaction chưa commit, câu đưa về
  hạn bỏ qua dòng đó (không treo, không đổi tin 1); chỉ dời việc hẹn trong 500 ms tới, nên lịch thử lại (≥ 4 s ở
  queue, commands), `RunAt` 1 giờ (`TestRunAtInFutureNotClaimed`, besteffort) giữ nguyên, các kiểm "n == 0", "còn
  pending" vẫn đúng nghĩa. Một đồng hồ: không so giờ Go với giờ database.
- 6.12: kiểm dữ liệu cá nhân của audit mạnh hơn (thêm: không có trường `before`/`after`; SĐT đủ 10 chữ số của cả hai
  phía), không nới. 6.13: không áp dụng: không đổi quyền.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tái hiện (1) trước khi sửa: UUID có chữ số trùng mẩu | test tạm `TestTmp451UUIDDigits` (dòng có `ID` `6f1c0933-8a2b-4c1d-9e0f-0911aa22bb33`; chạy kiểm cũ rồi `assertNoAuditValues`) | `go test -race -count=1 -run TestTmp451 ./internal/modules/identity/` | kiểm cũ: `kiểm cũ: AuditRecord mang giá trị before/after: [{"ID":"6f1c0933-…`; `assertNoAuditValues` không báo; rồi xóa test tạm |
| Tái hiện (2) trước khi sửa: đồng hồ PostgreSQL lùi 300 ms sau khi ghi việc | test tạm `TestTmp451ClockStep` (đặt `run_at = now() + 300 ms`; ca cũ `w.RunOnce` trực tiếp, ca mới `e.runOnce`) | `go test -race -count=1 -run TestTmp451 ./internal/platform/queue/` | ca cũ: `zz_tmp451_test.go:30: việc 01a1fdf9-… thiếu last_error` (đúng thông báo của main); ca mới qua; rồi xóa test tạm |
| (1) kiểm đúng: không có trường before/after, không SĐT đủ 10 chữ số của cả hai phía | `TestListEntityAudit` | `go test -race -count=30 -run 'TestListEntityAudit$' ./internal/modules/identity/` (cùng lúc 7 lệnh lặp khác) | `ok … identity 7.174s` |
| (2) và (3b) ở queue: mọi test qua `e.runOnce`, `holdRunOnce`, `runParallelUntilIdle`, `TestBestEffortClaimRaceLosesQuietly` | cả gói `queue` | `go test -race -count=30 -timeout 60m ./internal/platform/queue/` (dưới tải) | `ok … queue 803.410s` |
| (3b) sửa sau reviewer vòng 1 | `TestLostLeaseRollsBack`, `TestWorkerCrashDoesNotSendTwice` | `go test -race -count=30 -run TestLostLeaseRollsBack ./internal/platform/queue/` và `… -run TestWorkerCrashDoesNotSendTwice ./tests/e2e/` chạy cùng lúc, kèm `go test -race -count=3 ./internal/platform/commands/` làm tải | `ok … queue 4.256s`, `ok … tests/e2e 4.799s` |
| (3b) commands: `e.runOnce` mọi chỗ gọi, `TestStopDuringExecuteInDoubt` | cả gói `commands` | `go test -race -count=30 -timeout 60m ./internal/platform/commands/` (dưới tải) | `ok … commands 320.454s` |
| (3b) fulfillment | `TestCancel_InDoubtThenLookup`, `TestAccounts_StaleCheckIgnored` | `go test -race -count=30 -run '…' ./internal/modules/fulfillment/` (dưới tải) | `ok … fulfillment 20.907s` |
| (3b) inventory | `TestReconcile_NightlySchedule` | `go test -race -count=30 -run … ./internal/modules/inventory/` (dưới tải) | `ok … inventory 15.875s` |
| (3b) catalog | `TestApply_TwoImportsSameSKUConcurrently` | `go test -race -count=30 -run … ./internal/modules/catalog/` (dưới tải) | `ok … catalog 17.145s` |
| (3b) cash | `TestConsumerThroughRegistryAndWorker` | `go test -race -count=30 -run … ./internal/modules/cash/` (dưới tải) | `ok … cash 12.262s` |
| (3b) httpapi | `TestShippingQuotesAPI` | `go test -race -count=30 -run … ./internal/httpapi/` (dưới tải) | `ok … httpapi 15.058s` |
| Cổng của task (sau khi gộp main 352eb56) | ci-task | `scripts/agent/ci-task.sh` | test mọi gói đổi và vùng cao `ok` (httpapi, cash/…, catalog, fulfillment, identity, inventory, commands, queue, tests/arch); `CI-TASK ĐỎ: test-guard` vì test-guard lấy tệp task mới sửa nhất (task-183.md vừa gộp từ main) và cod_test giảm 1 câu kiểm — đã ghi ở "Test bị xóa hoặc tắt" |
| Cổng của task, lần 2 (sau sửa reviewer vòng 1) | ci-task | `scripts/agent/ci-task.sh` | `== test-guard: 1 giây` (không báo); `ok … platform/queue 28.125s`, `ok … tests/e2e 43.421s`, `ok … tests/arch 23.311s`; `CI-TASK XANH (so với điểm tách nhánh 352eb56…)` |

## Test bị xóa hoặc tắt

- `internal/modules/cash/cod_test.go` `TestConsumerThroughRegistryAndWorker`: số câu kiểm giảm 39 → 38 (test-guard). Không
  bỏ kiểm nào: vòng `for range 2 { if _, err := w.RunOnce(…); err != nil { t.Fatal(err) } }` đổi sang
  `testkit.DrainDue(t, e.staff.Owner, w)`, hàm này vẫn làm test đỏ khi `RunOnce` lỗi (và khi quá 50 vòng). Tương tự
  `internal/httpapi/shipping_quotes_test.go` `TestShippingQuotesAPI` (vòng 5 lượt).

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `scripts/agent/test-guard.sh` chọn tệp task bằng `ls -t docs/tasks/task-*.md | head -1` (trừ khi có `TASK_FILE`): sau
  `git merge main`, tệp task của task khác vừa gộp có thể mới hơn → test-guard đòi ghi lý do vào tệp của task khác.
  Nên lấy task đang chạy từ trạng thái của `new-task.sh`. Không tự sửa (cấu hình quy trình chỉ chủ shop đổi).
- `tests/drills/drill_test.go:158-170` `drain` dừng ở lượt `n == 0`, không bù đồng hồ PostgreSQL lùi (cùng kiểu vòng
  drain Task 446 đã đổi). Nhãn build `drill`, chỉ chạy tay qua script diễn tập, không chạy trong `go test ./...`; để
  task sau (đổi sang `testkit.DrainDue`).

## Reviewer

- Vòng 1: CẦN SỬA — sót hai chỗ kiểu (3b): `queue/worker_test.go` `TestLostLeaseRollsBack` (RunOnce một lần trong
  goroutine, `<-started` không chốt chống treo) và `tests/e2e/workercrash_test.go` `TestWorkerCrashDoesNotSendTwice`;
  ghi chú: `tests/drills` drain (nhãn drill), dòng ci-task "xem dòng dưới" chưa có kết quả. Đã sửa cả hai (MakeDueSoon
  trước lượt; `waitClosed` thay `<-started`), ghi drills vào Việc phát hiện thêm, chạy lại ci-task.
- Vòng 2: ĐẠT — không có phát hiện phải sửa; hai chỗ sót đã sửa đúng; `MakeDueSoon` không làm sai ý các test kiểm lịch
  hẹn thật, `n == 0`, còn pending; rà (3a)(3b) không còn sót. Ghi chú không bắt buộc: thêm
  `webhooks/split_test.go:77` (vòng drain đã bù đồng hồ ở Task 446) vào danh sách "Giữ" — đã ghi ở đây.

Kết luận: ĐẠT

## Quyết định thay chủ shop

- Chỗ test kiểm **đúng một lượt** nhận việc: thêm `testkit.MakeDueSoon` trước lượt (giữ nguyên số lượt và các kiểm
  `n`, còn pending), không đổi sang `DrainDue` (sẽ chạy thêm lượt, đổi điều test kiểm). Chỗ vòng chỉ để "chạy hết"
  (cash, httpapi): `DrainDue`. Phương án khác: sửa `ClaimJobs` chịu lệch đồng hồ ở mã chạy — bỏ, vì Task 446 đã quyết
  không đổi hàng đợi; đây là lỗi của test. Dễ đảo ngược (chỉ tệp `_test.go`).
- `tests/drills` drain không sửa (nhãn build `drill`, ngoài `go test ./...`): ghi Việc phát hiện thêm.
