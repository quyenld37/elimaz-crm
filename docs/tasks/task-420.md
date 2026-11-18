# Task 420 — Test chờ sẵn sàng bằng giờ ngủ (M1-x7)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 420, M1-x7). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có trong mã chạy. Chỉ đổi tệp `_test.go`; thêm
  `db.Sleep` trong `internal/platform/db/export_test.go` (chỉ biên dịch khi test gói `db`, bọc hàm `sleep` sẵn có).
  Hàm/hằng test mới trong gói `queue_test`: `waitFirstRound`, `hangGuard` (grep: chưa có tên trùng trong gói).
- Gói phụ thuộc ngược (go list): không có (chỉ tệp test; `queue_test`, `commands_test`, `db_test`, `webhooks_test`
  không ai nhập).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: vùng rủi ro cao bị chạm nên chạy cả bộ của `internal/platform/{queue,commands,db,webhooks}`
  (`go test -race`); chạy lặp dưới tải: `TestNotifyWakesWorker` ×200, các test đã sửa ×50–200; cổng `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit không có hàm
  "chờ worker sẵn sàng"); `metrics.Registry.Drain` (đã dùng ở `TestOldestPendingGauge`); `LockoutOptions.Now` (đồng hồ
  tiêm sẵn, đã dùng ở `TestLockoutCountsFailuresInWindow`); `queue.SetRecordTimeout` trong export_test làm mẫu cho
  `db.Sleep`.
- Dùng lại / viết mới vì: chờ lượt đầu dùng gauge `jobs_oldest_pending_age_s` sẵn có (dispatch đặt nó ngay sau câu nhận
  việc) thay vì thêm móc vào mã chạy của worker; đồng hồ lockout dùng `Now` sẵn có. Viết mới `waitFirstRound` (một chỗ
  dùng, trong gói test queue) và `db.Sleep` (cửa test cho hàm chưa xuất).

## Soát thiết kế

- 5.1–5.12: không áp dụng: chỉ đổi test, không dữ liệu, module, HTTP, thư viện mới.
- 6.1–6.13: không áp dụng cho mã chạy (không đổi dòng nào của mã chạy). Với chính test: không lấy ngưỡng giờ thật làm
  tiêu chí đúng/sai; chờ theo sự kiện (gauge, kênh, `ctx.Deadline`, đồng hồ tiêm); giờ chỉ còn là chốt chống treo
  (`hangGuard` 30 s, rộng hơn mọi độ trễ do tải, ngắn hơn hẳn đường sai 1 giờ). Test vẫn tạo database riêng trên
  crm-shared qua testkit như cũ.

## Gốc lỗi

1. **Worker có tín hiệu "đã LISTEN" không:** có, ngầm: `Worker.Start` gọi `listenConn` (Acquire + `LISTEN jobs`)
   **đồng bộ** trước khi trả và trước khi chạy goroutine của vòng nhận việc. Vậy sau `Start` mọi NOTIFY đều tới kết nối
   LISTEN; không có cửa sổ "NOTIFY tới trước LISTEN" trong mã thật. NOTIFY tới lúc vòng đang `dispatch` cũng không mất:
   `signal()` bỏ tín hiệu chỉ khi kênh `wake` (sức chứa 1) đã có một tín hiệu chờ, và tín hiệu đó làm vòng chạy thêm
   một lượt sau khi commit của việc mới đã thấy được (NOTIFY chỉ gửi lúc commit). Kết luận: mã đúng thiết kế, không phải
   lỗi. Việc ghi trước Start do lượt đầu của vòng nhận; việc ghi lúc mất kết nối LISTEN do `signal()` sau khi nối lại;
   còn lại là Poll.
2. **Test sai ở đâu:** (a) giờ ngủ 200 ms không bảo đảm gì: máy chậm thì lượt đầu chưa nhận việc xong, việc mới bị chính
   lượt đầu nhận (test qua mà không kiểm NOTIFY); (b) tiêu chí là giờ thật: "nhận trong 1 s" và "hết 3 s thì đỏ". Dưới
   tải ~20 với `-race`, quét + nhận + chạy handler có thể quá 3 s → đỏ "NOTIFY không đánh thức worker" dù NOTIFY đã tới.
   Phiên trước chạy bản cũ 200 lần dưới tải (8 tiến trình `perl` nice +10, tải ~30) không tái hiện được lần đỏ nào; gốc
   xác định bằng đọc mã như trên.
3. **Sửa:** Poll 1 giờ (chỉ NOTIFY đánh thức được), chờ gauge `jobs_oldest_pending_age_s` của lượt đầu (đặt ngay sau câu
   nhận việc) rồi mới ghi việc, chờ handler bằng kênh với chốt chống treo 30 s. Kiểm âm: bỏ `w.signal()` sau
   `WaitForNotification` trong `worker.go` (không commit) → test đỏ "NOTIFY không đánh thức worker".

## Quét `time.Sleep` và ngưỡng giờ thật trong test Go

Lệnh: `grep -rn 'time\.Sleep' --include='*_test.go' internal cmd spikes`; trên vùng rủi ro cao thêm
`grep -rn 'time.Since'` và `grep -rn 'time\.After('` (hạn chờ dẫn tới `t.Fatal`). "Chờ theo vòng hỏi" = vòng lặp hỏi điều kiện tới hạn chót (đúng dạng, không đổi).

Đã sửa (vùng rủi ro cao; dòng theo bản cũ):

| Tệp:dòng | Dạng | Sửa |
|---|---|---|
| `internal/platform/queue/worker_test.go:397,402` `TestNotifyWakesWorker` | ngủ 200 ms rồi coi là sẵn sàng; tiêu chí 1 s / 3 s | chờ gauge lượt đầu, Poll 1 giờ, chốt chống treo 30 s |
| `internal/platform/queue/worker_test.go:316` `TestHandlerTimeout` | tiêu chí "< 2 s" | bỏ; `sawErr` đã phân biệt (không hủy thì handler chờ hết 5 s và ctx còn sống) |
| `internal/platform/queue/shutdown_test.go:34` `TestStopReturnsUnfinishedJob` | tiêu chí "Stop < 1 s" | thay bằng `ErrStopGrace` + việc về pending (không hủy thì việc còn running hoặc done) |
| `internal/platform/commands/runner_test.go:201` `TestExecuteTimeoutInDoubt` | tiêu chí "< 3 s" | bỏ; không có Timeout thì Execute treo, `deadline` trong last_error đã phân biệt |
| `internal/platform/commands/runner_test.go:365` `TestCommandTimeoutBelowLease` | tiêu chí "< 2 s" là thứ duy nhất phân biệt hạn 100 ms với 5 s | đo `ctx.Deadline()` còn lại lúc Execute bắt đầu ≤ lease/2 |
| `internal/platform/db/retry_test.go:384` `TestRetryTxStopsWhenContextCanceledDuringWait` | hẹn hủy 5 ms bằng đồng hồ thật, tiêu chí "trả trong 50 ms"; dưới tải hẹn giờ có thể tới sau Backoff 10–30 ms → hàm chạy lần 2 → đỏ | hủy đồng bộ trong `OnRetry`, kiểm `context.Canceled` và hàm không chạy thêm; thêm `TestSleepStopsOnCancel` (chờ 1 giờ, hủy, phải trả) |
| `internal/platform/webhooks/lockout_test.go:100` `TestLockoutOverHTTP` | chặn 200 ms đồng hồ thật: tải cao có thể hết chặn trước lần thứ 4 (ra 200 thay 429); ngủ 250 ms để hết chặn | đồng hồ tiêm `Now`, tua 250 ms |
| `internal/platform/queue/worker_test.go:443` `TestPollPicksDueJob`; `queue/shutdown_test.go:31` `TestStopReturnsUnfinishedJob` (phát hiện của reviewer) | hạn 3 s chờ handler chạy làm tiêu chí — đúng chuỗi quét + nhận + chạy có thể quá 3 s dưới tải | `hangGuard` 30 s (chỉ chống treo) |
| `internal/platform/queue/besteffort_test.go:221,232` (`waitClosed`, `waitErr`); `internal/platform/webhooks/ingest_test.go:240` | hạn 10 s / 5 s chờ sự kiện | đổi sang chốt chống treo 30 s cho đồng nhất (reviewer: chưa phân loại) |

Giữ nguyên trong vùng rủi ro cao (không phải dạng lỗi):

- `queue/worker_test.go:453`, `commands/runner_test.go:337`: handler ngủ lâu hơn hạn ghi — cận dưới; máy chậm chỉ làm
  ngủ lâu hơn, tiêu chí không đổi.
- `commands/runner_test.go:215,236`: ngủ 150 ms cho lệnh hạn 100 ms hết hạn — cận dưới.
- `queue/shutdown_test.go:52`: việc của handler. `queue/shutdown_test.go:65`: cửa sổ âm sau `Stop` (Stop đồng bộ; máy
  chậm chỉ làm test dễ qua hơn, không đỏ oan) — rủi ro bỏ sót lỗi, không chập chờn.
- `queue/besteffort_test.go:247`, `commands/runner_test.go:261`, `webhooks/spool_test.go:211`: chờ theo vòng hỏi.
  `besteffort_test.go:267`, `commands/property_test.go:87`: việc của handler / điểm dừng ngẫu nhiên.
- `inventory/deadlock_test.go:79`: cửa sổ âm 2 s đọc bộ đếm deadlock — không đỏ oan.
- `inventory/reconcile_test.go:222` "50.000 số dư < 60 s": ngưỡng hiệu năng trong test đúng/sai (rộng, chưa từng đỏ) —
  để sau (Việc phát hiện thêm).
- `queue/worker_test.go:71` và các ca kiểm `run_at` trong khoảng quanh mốc đo trước/sau lúc ghi — cận tính từ mốc đo,
  không phụ thuộc tải.

Ngoài vùng rủi ro cao (để làm sau, chưa sửa):

| Tệp:dòng | Dạng | Rủi ro |
|---|---|---|
| `internal/imager/client_test.go:121` | ngủ 20 ms rồi coi yêu cầu đầu đã giữ chỗ; yêu cầu sau hạn 30 ms | **cao**: máy chậm thì yêu cầu đầu chưa giữ chỗ → không ra `ErrBusy` → đỏ oan |
| `internal/platform/realtime/reconnect_test.go:75` | ngủ một nhịp tim + lô để bỏ khung cũ, sau đó coi "không có gì" là đúng | trung bình: khung cũ tới muộn hơn cửa sổ → đỏ oan |
| `internal/platform/files/process_private_test.go:107`, `original_url_test.go:79`, `s3store_test.go:149` | ngủ 2–3 s cho đường dẫn ký hết hạn (đồng hồ của SeaweedFS) | thấp: cận dưới; mỗi ca chậm 2–3 s |
| `internal/imager/client_test.go:93` | máy chủ giả ngủ 100 ms | thấp: việc của máy chủ giả |
| `internal/platform/schedules/schedules_test.go:212`, `observability/pgwatch_test.go:55`, `realtime/env_test.go:91`, `realtime/reconnect_test.go:167`, `realtime/silent_test.go:106`, `files/process_test.go:220,273`, `imager/pipeline_test.go:599`, `imager/server_test.go:147` | chờ theo vòng hỏi có hạn chót 5–10 s | thấp |
| `internal/httpapi/handlers_jobs_test.go:296` | goroutine giữ khóa dòng tới khi test thả hoặc hết 2 s đồng hồ thật | trung bình: dưới tải, khóa có thể nhả trước khi yêu cầu đang kiểm tới bước chờ khóa |
| `internal/platform/files/process_helpers_test.go:138` | độ trễ giả của bộ nối | thấp |
| `spikes/flashsale/buy_test.go:176,252,286,450` | module đo riêng | ngoài phạm vi |

## Quyết định thay chủ shop

- Chờ lượt đầu của worker bằng gauge `jobs_oldest_pending_age_s` có sẵn thay vì thêm móc (hook) vào mã chạy của
  `queue.Worker`. Vì sao: không đổi mã chạy ở vùng rủi ro cao, dễ đảo ngược. Phương án khác: trường `onIdle` không xuất,
  gọi sau mỗi `dispatch`, đặt qua export_test — rõ ý hơn nhưng thêm mã chạy chỉ để test.
- Sửa thêm 6 test cùng dạng trong vùng rủi ro cao ngoài `TestNotifyWakesWorker` (kế hoạch: "sửa các chỗ trong vùng rủi
  ro cao"); chốt chống treo 30 s là giờ thật duy nhất còn lại, không phải tiêu chí đúng/sai.
- Ngưỡng 60 s của `inventory/reconcile_test.go` giữ lại, ghi việc sau (cần tách test nhãn `perf` như Task 400).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TestNotifyWakesWorker ≥ 200 lần dưới tải | `TestNotifyWakesWorker` | 8 tiến trình `nice -n 10 perl -e 'alarm 600; 1 while 1'` (dừng bằng PID) + `go test -race -count=200 -run '^TestNotifyWakesWorker$' ./internal/platform/queue/`, chạy song song với 4 lệnh dưới | `ok … queue 263.694s`; tải máy 56 lúc xong |
| Test Stop/Timeout của queue dưới tải | `TestStopReturnsUnfinishedJob`, `TestHandlerTimeout` | `go test -race -count=50 -run '^(TestStopReturnsUnfinishedJob\|TestHandlerTimeout)$' ./internal/platform/queue/` | `ok … queue 192.241s` |
| Test Timeout của commands dưới tải | `TestExecuteTimeoutInDoubt`, `TestCommandTimeoutBelowLease` | `go test -race -count=50 -run '^(TestExecuteTimeoutInDoubt\|TestCommandTimeoutBelowLease)$' ./internal/platform/commands/` | `ok … commands 192.313s` |
| RetryTx dừng khi hủy, dưới tải | `TestRetryTxStopsWhenContextCanceledDuringWait`, `TestSleepStopsOnCancel` | `go test -race -count=200 -run '^(TestRetryTxStopsWhenContextCanceledDuringWait\|TestSleepStopsOnCancel)$' ./internal/platform/db/` | `ok … db 244.769s` |
| Lockout qua HTTP dưới tải | `TestLockoutOverHTTP` | `go test -race -count=200 -run '^TestLockoutOverHTTP$' ./internal/platform/webhooks/` | `ok … webhooks 438.075s` |
| Test mới còn bắt lỗi (kiểm âm) | `TestNotifyWakesWorker` | tạm bỏ `w.signal()` sau `WaitForNotification` trong `worker.go`; `go test -count=1 -run TestNotifyWakesWorker ./internal/platform/queue/` | `worker_test.go:407` `NOTIFY không đánh thức worker` FAIL (đã hoàn lại tệp) |
| Test mới còn bắt lỗi (kiểm âm) | `TestSleepStopsOnCancel` | tạm bỏ nhánh `ctx.Done()` trong `sleep` của `tx.go` | `retry_test.go: chờ không dừng khi ctx hủy` FAIL (đã hoàn lại tệp). (Lần kiểm âm này chạy với bản cũ của `TestRetryTx…DuringWait` còn so lời bọc; bản cuối không kiểm điều đó nữa.) |
| Cổng kiểm | — | `ci-task.sh` (sau khi gộp main 811a025) | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard xanh; test: 38 gói `ok` (queue 81 s, commands, db, webhooks…), một đỏ `tests/arch` › `TestPackagePatternSkipsStrayDirs`: "parallel golangci-lint is running" (khóa của golangci-lint do phiên khác giữ, không do task) |
| Chạy lại gói đỏ vì môi trường | `tests/arch` | `go test -race -count=1 ./tests/arch/` | `ok … tests/arch 17.060s` |
| Cổng kiểm sau khi sửa phát hiện của reviewer | — | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 811a0251…)`, 39 gói `ok` |
| Sau sửa vòng 2 (chỉ `retry_test.go`) | cả gói `db` (vùng rủi ro cao); hai test đã sửa ×50 | `go test -race -count=1 ./internal/platform/db/...`; `go test -race -count=50 -run` hai test trên `./internal/platform/db/`; golangci-lint gói db | `ok … db 18.525s`; `ok … db 10.385s`; `0 issues` |

## Test bị xóa hoặc tắt

Không test nào bị xóa hay tắt. Các câu kiểm giờ thật bị bỏ hoặc thay (lý do: lấy giờ thật làm tiêu chí đúng/sai, đỏ oan
khi máy tải cao; mỗi câu được thay bằng câu kiểm theo sự kiện phân biệt được đường đúng và đường sai):

- `TestNotifyWakesWorker`: bỏ "nhận trong 1 s" và hạn 3 s → Poll 1 giờ + chờ lượt đầu theo gauge + chốt chống treo 30 s.
- `TestHandlerTimeout`: bỏ "< 2 s" → giữ `sawErr` (đã có) và last_error `deadline`.
- `TestStopReturnsUnfinishedJob`: bỏ "Stop < 1 s" → thêm kiểm `ErrStopGrace`, giữ kiểm pending/attempts 0.
- `TestExecuteTimeoutInDoubt`: bỏ "< 3 s" → giữ in_doubt + `deadline` + một lần gọi.
- `TestCommandTimeoutBelowLease`: bỏ "< 2 s" → hạn của ctx Execute còn ≤ lease/2 lúc bắt đầu (chặt hơn: đường sai còn ~5 s).
- `TestRetryTxStopsWhenContextCanceledDuringWait`: bỏ "trả trong 50 ms sau khi hủy" → giữ `context.Canceled` + hàm
  chỉ chạy 1 lần; việc "lúc chờ phản ứng với ctx" chuyển sang test mới `TestSleepStopsOnCancel`. Không kiểm lời bọc
  "chờ chạy lại transaction" (phát hiện của reviewer vòng 2: dưới tải timer Backoff có thể chín cùng lúc ctx hủy,
  `select` chọn ngẫu nhiên, vòng dừng ở lần kiểm ctx đầu vòng — vẫn đúng, nhưng sẽ đỏ oan; cũng tránh so lỗi bằng chuỗi).
- `TestLockoutOverHTTP`: ngủ 250 ms → tua đồng hồ tiêm 250 ms; tiêu chí giữ nguyên.

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `internal/imager/client_test.go:121`: ngủ 20 ms rồi coi yêu cầu đầu đã giữ chỗ — rủi ro đỏ oan cao dưới tải; nên chờ
  theo sự kiện (máy chủ giả báo đã nhận yêu cầu).
- `internal/httpapi/handlers_jobs_test.go:296`: giữ khóa tối đa 2 s đồng hồ thật — rủi ro trung bình.
- `internal/platform/realtime/reconnect_test.go:75`: cửa sổ ngủ để bỏ khung cũ — rủi ro trung bình.
- `internal/modules/inventory/reconcile_test.go:222`: ngưỡng 60 s trong test đúng/sai — tách sang test nhãn `perf`.

## Reviewer

Kết luận: ĐẠT (vòng 3).

- Vòng 1 — CẦN SỬA: quét bỏ sót dạng `time.After(N)` → `t.Fatal`; `TestPollPicksDueJob` và `TestStopReturnsUnfinishedJob`
  còn hạn 3 s làm tiêu chí; `besteffort_test.go:221,232`, `ingest_test.go:240` chưa phân loại. Đã sửa: đổi sang chốt
  chống treo 30 s, thêm `grep time.After(` vào lệnh quét, thêm `handlers_jobs_test.go:296` vào danh sách để sau.
- Vòng 2 — CẦN SỬA: `TestRetryTxStopsWhenContextCanceledDuringWait` đòi lời bọc "chờ chạy lại transaction" có thể đỏ
  oan dưới tải (timer Backoff chín cùng lúc ctx hủy, `select` chọn ngẫu nhiên). Đã sửa: bỏ câu so chuỗi, giữ
  `context.Canceled` + một lần gọi; nhánh `ctx.Done()` do `TestSleepStopsOnCancel` kiểm.
- Vòng 3 — ĐẠT: không còn phát hiện. Ghi chú không bắt buộc: `waitFirstRound` dựa vào gauge của lượt đầu; nếu câu đo
  lỗi tạm thời thì lượt sau đặt gauge, rủi ro thấp.
