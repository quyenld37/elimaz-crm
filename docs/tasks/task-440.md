# Task 440 — Test chập chờn đợt 21/11 (chỉ sửa test, không nới tiêu chí)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task của điều phối viên 21/11, không có
trong kế hoạch; số toàn cục 440, dòng `| chung (21/11) | x-flaky | 440 | Test chập chờn đợt 21/11 |` đã thêm vào
`../../docs/superpowers/plans/danh-so-task-toan-cuc.md`). Loại việc (quy trình mục 3): A. Làm như Task 421: tái hiện,
tìm gốc, sửa test để kiểm đúng điều cần kiểm, không nới tiêu chí, không retry, không skip.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi tệp `_test.go` (gói `_test`, không ai
  nhập), một đích Makefile và một dòng `docs/ca-chay-tay.md`; không dòng mã chạy nào (`git diff main --stat -- ':!*_test.go'`
  chỉ ra `Makefile`, `docs/`). Ký hiệu test mới, grep không trùng trong gói: `imager_test.startGate`,
  `gateOnPidfile`, `gatedRunner`, `startGateGuard`, `hangLimits`, `TestStartGate`, `TestImager_HangKilledTimeBudget`,
  `TestExecRunner_TimeoutTimeBudget` (nhãn `perf`, `kill_perf_test.go`); `realtime_test.frameGate`, `newFrameGate`,
  `(*statusSpy).hold`, `(*statusSpy).Subscribe`.
- Gói phụ thuộc ngược (go list): không có (chỉ tệp test).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- `Makefile`: đích `perf-test` thêm một dòng chạy hai test perf của imager.
- Tệp đổi: `internal/modules/cash/postings_test.go`, `internal/httpapi/handlers_customer_write_test.go`,
  `internal/httpapi/handlers_health_test.go`, `internal/imager/{pipeline_test.go,runner_test.go,startgate_test.go,kill_perf_test.go}`,
  `internal/platform/realtime/{env_test.go,sse_test.go}`, `Makefile`, `docs/ca-chay-tay.md`.
- Test sẽ chạy: gói `internal/modules/cash` (vùng rủi ro cao → cả bộ), `internal/imager` (vùng cao → cả bộ, kèm
  `-tags perf`), `internal/httpapi`, `internal/platform/realtime` (cả bộ vì `env_test.go` dùng chung); chạy lặp ×100 dưới
  tải mỗi test đã sửa; cổng `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit không có cổng giữ
  khung SSE hay ctx chờ tiến trình con khởi động); mẫu Task 421 (`statusSpy` bọc Bus, `hangGuard` chốt chống treo 30 s,
  test thời gian nhãn `perf` + đích `make perf-test` theo Task 400); câu kiểm SĐT đầy đủ ba dạng của
  `customer/write_test.go` (Task 421).
- Dùng lại / viết mới vì: dùng lại `statusSpy` (thêm `Subscribe`/`hold` vào cùng kiểu bọc Bus, không tạo lớp bọc thứ
  hai), `hangGuard` của gói realtime, `assertGroupGone`, `fakeVips`, `limits` của imager; câu kiểm SĐT chép đúng ba dạng
  của `customer/write_test.go`. Viết mới `startGate`/`gatedRunner` (cách duy nhất biết lệnh con đã thật sự khởi động mà
  không thêm móc vào mã chạy: `Runner` là interface sẵn có ở phía dùng) và `frameGate` (biết hub đã gửi đủ khung).

## Soát thiết kế

- 5.1–5.12: không áp dụng: chỉ đổi test, một đích Makefile, một dòng tài liệu; không dữ liệu, module, HTTP, quyền, tiền,
  thư viện mới.
- 6.1–6.13: không áp dụng cho mã chạy (không đổi dòng nào). Với chính test: không lấy giờ thật làm tiêu chí đúng/sai
  (trần thời gian của imager chuyển sang test nhãn `perf` như Task 421); chờ theo sự kiện (pidfile đã ghi xong, khung
  resync đầu của hub, cổng đã giữ đủ khung), giờ chỉ còn là chốt chống treo 30 s; một đồng hồ cho mỗi phép so (mốc nhịp
  sống, lần chép tệp ghi bằng đồng hồ Go mà `DeepHealth` so). Goroutine mới (`gateOnPidfile`) có đường dừng: thoát khi
  ctx cha hết + pidfile có, hoặc sau chốt 30 s; ctx cha luôn có hạn (hạn bước, hạn của test). Test vẫn tạo database riêng
  trên crm-shared.

## Gốc lỗi và cách sửa

| Test | Gốc | Sửa |
|---|---|---|
| `internal/modules/cash/postings_test.go` `TestPostings_ReversalAfterRemit` (đỏ khi gộp 109: `\d{9,}` khớp mã vận đơn `E261121602JMK`) | văn bản cảnh báo được phép (và phải) chứa mã vận đơn, mã vận đơn là chuỗi ngẫu nhiên có thể chứa ≥ 9 chữ số liền | bỏ đúng mã vận đơn khỏi văn bản rồi mới tìm `\d{9,}` trong phần còn lại; vẫn kiểm văn bản có mã vận đơn, không mã đơn, không SĐT thật của đơn (vòng `leak` giữ nguyên) |
| `internal/httpapi/handlers_customer_write_test.go` `TestCustomerWriteAPI` (thân 409 cấm `"912"`) | thân lỗi có `ownerId`, `request_id` là UUID ngẫu nhiên; chữ số hex chứa `"912"` (~0,7%) | cấm SĐT đầy đủ ở ba dạng (`912345678`, `912 345 678`, đảo ngược `87654321948`) như `customer/write_test.go` (Task 421) |
| `customer/write_test.go` `TestCreateCustomerPhoneTaken` | **đã sửa ở Task 421** (lượt 2) trên main: câu kiểm đã là ba dạng SĐT đầy đủ | không đổi |
| `observability/rules_test.go:71`, `:323`, `identity/audit_test.go:87` (mẩu 4 số `"0911"`, `"0922"`) | **đã sửa ở Task 428** trên main (`77fb7a3`: kiểm vắng SĐT bằng số đủ 10 chữ số) | không đổi |
| `internal/imager/pipeline_test.go` `TestImager_HangKilledWithinDeadline` và cùng gốc `runner_test.go` `TestExecRunner_TimeoutKillsProcessGroup` (log điều phối viên: 25 lần và 2 lần "script giả chưa chạy: … pid: no such file") | hạn bước 1 s (500 ms) tính từ lúc gọi lệnh con; máy tải cao làm `sh` khởi động chậm hơn hạn → bị giết trước khi script ghi pidfile, test không còn nhóm tiến trình để kiểm. Thêm một tiêu chí giờ thật ngầm: "≤ 2 s kể từ lúc lệnh treo chạy", "≤ 1,5 s" | ctx của lệnh treo bọc bằng `gateOnPidfile`: chỉ hết khi hạn cha đã hết **và** script đã ghi xong pidfile (dòng có `\n`), lỗi vẫn là `DeadlineExceeded` của ctx cha; với Process, `gatedRunner` bọc `ExecRunner` thật, chỉ gác lệnh hang, và ghi hạn còn lại của ctx bước (kiểm Process có đặt hạn bước ≤ `StepTimeout`). Vẫn kiểm: rejected `timeout`, lỗi bọc `DeadlineExceeded`, cả nhóm tiến trình (sh + sleep con) không còn. Hai trần thời gian giữ nguyên số, chuyển sang `kill_perf_test.go` (nhãn `perf`, `make perf-test`) |
| `internal/httpapi/handlers_health_test.go` `TestHealthDeep` (`workerHeartbeatAgeS = 929` thay 200) và `TestHealthDeepFileSyncAge` (42 phút thay 30), cùng một lần gộp | test ghi mốc bằng `now()` của PostgreSQL, `DeepHealth` so với `time.Now()` của Go; mã chạy ghi cả hai mốc bằng đồng hồ của máy (`metrics.flusher`: `UpsertHeartbeat(At: now.UTC())`; `scripts/filesync/lib.sh`: `to_timestamp($(date +%s))`). Trên máy dev đồng hồ VM Docker lệch đồng hồ máy: cả hai số lệch cùng ~12 phút (729 s, 12 phút) | `beat` và dòng `file_sync_runs` ghi mốc bằng `time.Now()` của Go (đúng đồng hồ của bên ghi thật và bên đọc); ngưỡng giữ nguyên (199–230 s, 30–31 phút) |
| `internal/httpapi/handlers_order_read_test.go` `TestOrderRead_EditCancelTimeline` | **gốc ở mã chạy** (xem Dừng để hỏi): `order.Timeline` trộn mốc của hai đồng hồ — lịch sử trạng thái `occurred_at` do handler đưa `time.Now()` của Go, mục sửa lấy `identity.audit_logs.created_at` mặc định `now()` của PostgreSQL. Lúc lệch ~12 phút (cùng lần gộp với hai test trên), mục sửa có mốc sớm hơn mục tạo → thứ tự sai đúng như log (`mục 1 summary[action] = <nil>`, mục 1 là `to: new`) | không sửa (không sửa mã chạy; sửa test là che lỗi) |
| `internal/platform/realtime/sse_test.go` `TestStreamBatchDedupe` ("LISTEN rớt khi máy bận", Task 97, 320) | **tái hiện được**: hub `listen` gọi `setListening(true)` **rồi** `broadcast(resync)`. `newRTEnv` chỉ chờ `listening`, nên khi máy bận client của test đăng ký vào giữa hai bước và nhận thêm một `resync` (trông như LISTEN rớt rồi nối lại). Chèn tạm `time.Sleep(300ms)` giữa hai dòng (không commit): đỏ `sse_test.go:160: mong một changed: "resync" {}`. Gốc thứ hai đọc mã: ba NOTIFY của ba transaction phải tới trong cùng cửa sổ lô 100 ms đồng hồ thật, máy bận thì sang lô sau → hai khung `changed` | `newRTEnv` đăng ký một client dò **trước** khi hub chạy, chờ khung resync đầu của hub (sự kiện) rồi hủy dò — `broadcast` giữ khóa đọc nên client đăng ký sau không nhận khung đó (cùng ý cho mọi test của gói). `TestStreamBatchDedupe` mở luồng qua `frameGate`: giữ khung hub gửi tới khi đủ 3 (kiểm cả 3 là `changed` giống hệt nhau), thả cùng lúc vào hàng chờ của luồng → lô ở luồng SSE vẫn phải gộp thành một khung; `expectNothing` 3 lô giữ nguyên. Hạn 1 s / 2 s chờ khung → chốt chống treo `hangGuard` 30 s. Với độ trễ tạm vẫn còn: cả gói qua ×3 |

## Quyết định thay chủ shop

- **Trần thời gian của test lệnh treo imager chuyển sang test nhãn `perf`** (không bỏ): chọn vì luật CLAUDE.md (Task 400:
  test đúng/sai không lấy ngưỡng giờ thật) và tiền lệ Task 421 (`TestReconcile_LargeTenantTimeBudget`). Số giữ nguyên
  (≤ 2 s sau khi lệnh treo chạy với hạn bước 1 s; `ExecRunner` ≤ 1,5 s với hạn 500 ms); chạy bằng `make perf-test`, đã
  thêm vào `docs/ca-chay-tay.md`. Phương án khác: giữ trần trong test đúng/sai (vẫn đỏ khi máy bận); tăng hạn bước
  (nới tiêu chí). Dễ đảo: đưa lại hai câu kiểm thời gian vào test thường.
- **Không sửa `TestOrderRead_EditCancelTimeline`**: gốc ở mã chạy (trộn đồng hồ), brief nói khi đó dừng hỏi. Phương án
  khác: ép thứ tự trong test (che lỗi thật).
- **Sửa cả `TestExecRunner_TimeoutKillsProcessGroup`** (không có trong danh sách): cùng gốc, cùng cách sửa với
  `TestImager_HangKilledWithinDeadline`, đã đỏ 2 lần trong log ("script giả chưa chạy").
- Tên task trong `new-task.sh` viết "21-11" (dấu `/` làm `sed` của script lỗi: `bad flag in substitute command`, tệp
  rỗng đã xóa), rồi sửa tiêu đề tệp task về "21/11".

## Dừng để hỏi

- **`order.Timeline` trộn hai đồng hồ (mã chạy, `internal/modules/order/timeline.go`)**: mục trạng thái dùng
  `ord.order_status_history.occurred_at` (handler đưa `time.Now().UTC()` của Go), mục sửa dùng
  `identity.audit_logs.created_at` (`DEFAULT now()` của PostgreSQL). Trên production (một máy Linux, container chung đồng
  hồ nhân) gần như không lệch; trên máy dev (VM Docker) lệch phút → thứ tự lịch sử sai và `TestOrderRead_EditCancelTimeline`
  đỏ. Đề xuất task riêng (vùng order, rủi ro cao): ghi mốc của audit bằng đồng hồ của bên gọi (truyền `at` vào
  `identity.RecordAudit`) hoặc đổi `occurred_at` sang `now()` của transaction — chọn một đồng hồ cho cả timeline. Cần
  OK vì đổi chữ ký `identity.RecordAudit` (nhiều chỗ gọi) hoặc đổi đường ghi lịch sử trạng thái.

## Bằng chứng

Tải: `scratchpad/load.sh 8 <lệnh>` — 8 vòng bận CPU `renice +10`, dừng đúng các PID đã ghi khi lệnh xong; mỗi lượt ≤ 10
phút (`-test.timeout 590s`); tải máy 20–23 (cả phiên khác). Binary dựng bằng `go test -race -c`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tái hiện realtime (trước khi sửa, độ trễ tạm 300 ms giữa `setListening` và `broadcast`) | `TestStreamBatchDedupe` | `go test -race -count=1 -run '^TestStreamBatchDedupe$' ./internal/platform/realtime/` | `sse_test.go:160: mong một changed: "resync" {} <nil>` FAIL |
| Sau sửa, vẫn giữ độ trễ tạm: cả gói qua | cả gói realtime | `go test -race -count=3 ./internal/platform/realtime/` | `ok … realtime 42.890s`; đã hoàn `pghub.go` (`git status` không có tệp mã chạy) |
| Tái hiện imager (giả máy bận: tạm `sleep 1.5` trước khi `hang.sh` ghi pidfile, không commit) | `TestImager_HangKilledTimeBudget`, `TestExecRunner_TimeoutTimeBudget` (= cách kiểm cũ) | `go test -tags perf -count=1 -run TimeBudget ./internal/imager/` | FAIL `script giả chưa chạy … pid: no such file or directory` (đúng lỗi của log) |
| Cùng giả máy bận: test đúng/sai mới qua | `TestImager_HangKilledWithinDeadline`, `TestExecRunner_TimeoutKillsProcessGroup`, `TestStartGate` | `go test -race -count=1 -run '…' -v ./internal/imager/` | PASS (1.85s), PASS (1.53s), PASS; đã hoàn `hang.sh` |
| Trần thời gian imager trên máy (không giả chậm) | hai test perf | `go test -tags perf -count=1 -run TimeBudget -v ./internal/imager/` | `TestImager_HangKilledTimeBudget (1.01s)`, `TestExecRunner_TimeoutTimeBudget (0.50s)` PASS |
| realtime ×100 dưới tải | `TestStreamBatchDedupe` | `load.sh 8 rt.test -test.run '^TestStreamBatchDedupe$' -test.count 100` | rc=0, 0 FAIL, `PASS` (1:15) |
| imager ×100 dưới tải | `TestImager_HangKilledWithinDeadline`, `TestExecRunner_TimeoutKillsProcessGroup`, `TestStartGate` | `load.sh 8 im.test -test.run '^(…)$' -test.count 100` | rc=0, 0 FAIL, `PASS` (2:39); sau khi tự soát gắn goroutine của `gateOnPidfile` vào `t.Cleanup` (chủ chờ nó dừng): chạy lại ×100 dưới tải rc=0, 0 FAIL, `PASS` (2:38); `golangci-lint run ./internal/imager/...` (cả `--build-tags perf`) `0 issues`, `go test -race ./internal/imager/` ok |
| cash ×100 dưới tải | `TestPostings_ReversalAfterRemit` | `load.sh 8 cash.test -test.run '^TestPostings_ReversalAfterRemit$' -test.count 100` | rc=0, 0 FAIL, `PASS` (28 s) |
| httpapi ×100 dưới tải | `TestCustomerWriteAPI`, `TestHealthDeep`, `TestHealthDeepFileSyncAge` | `load.sh 8 http.test -test.run '^(…)$' -test.count 100` | rc=0, 0 FAIL, `PASS` (1:37) |
| Cổng kiểm (lượt 1 đỏ: 2 chỉ thị `nolint` thừa ở `startgate_test.go`, test-guard đòi ghi câu kiểm thời gian rời `runner_test.go` → sửa, ghi mục Test bị xóa) | build, vet, lint, test-guard, test gói đổi + vùng cao, arch, deploy | `scripts/agent/ci-task.sh` (lượt 2) | `CI-TASK XANH (so với điểm tách nhánh 4d32b49…)`; `ok … httpapi 91.773s`, `ok … imager 12.790s`, `ok … cash 76.236s`, `ok … realtime 14.269s`, `ok … tests/arch 27.344s` |

Ghi chú: lệch đồng hồ của `TestHealthDeep*` không tái hiện được lúc làm (đồng hồ VM Docker và máy khớp:
`docker exec … date -u` = `date -u`); gốc suy từ log (hai test cùng lần gộp lệch cùng ~12 phút) và đọc mã bên ghi thật.

## Test bị xóa hoặc tắt

- `internal/imager/runner_test.go` `TestExecRunner_TimeoutKillsProcessGroup`: bỏ câu kiểm `time.Since(start) > 1500ms`
  (số câu kiểm 13 → 12, test-guard báo). Lý do: ngưỡng giờ thật trong test đúng/sai (Task 400). Câu kiểm không mất:
  chuyển nguyên số sang `TestExecRunner_TimeoutTimeBudget` (`kill_perf_test.go`, nhãn `perf`, `make perf-test`).
- `internal/imager/pipeline_test.go` `TestImager_HangKilledWithinDeadline`: câu kiểm "trả lời ≤ 2 s kể từ lúc lệnh treo
  chạy" chuyển nguyên số sang `TestImager_HangKilledTimeBudget` (cùng tệp perf); test đúng/sai thêm câu kiểm Process đặt
  hạn bước cho lệnh con (≤ `StepTimeout`). Xem Quyết định thay chủ shop.

## Khác kế hoạch

- Task không có trong kế hoạch (điều phối viên giao 21/11). Mục (2) `TestCreateCustomerPhoneTaken` và mục (3) đã được
  Task 421, 428 sửa trên main: không đổi. Mục (5) `TestOrderRead_EditCancelTimeline`: gốc ở mã chạy, không sửa (Dừng để
  hỏi).

## Việc phát hiện thêm

- `observability/rules_test.go` `TestRuleDeadNoPayload:77` đỏ một lần 20/11 (`key "dead:msg.send" có = true, muốn
  false`): việc dead có `finished_at` theo `time.Now()` của Go, lần báo `alert_events.sent_at` theo `now()` của
  PostgreSQL (`helpers_test.go` `alertSent`) — cùng dạng trộn đồng hồ; cả tệp `rules_test.go` trộn hai đồng hồ ở nhiều
  chỗ. Cần xem luật trong `rules.go` so mốc theo đồng hồ nào rồi sửa test theo đúng đồng hồ đó (task chập chờn kế).
- `observability.DeepHealth` so `LastBackupAt`, `walArchivedAge` với `time.Now()` của Go trong khi mốc do PostgreSQL /
  script ghi: đúng trên production một máy, lệch trên máy dev; không có test đỏ, ghi lại.
- `scripts/agent/new-task.sh`: tên task có `/` làm `sed` lỗi và để lại tệp task rỗng (cấu hình quy trình, chỉ chủ shop đổi).

## Reviewer

Kết luận: ĐẠT (lượt 1, trên bản đã gộp main `4d32b49`).

- Phát hiện phải sửa: không có. Ảnh hưởng khớp từng tệp; không tệp mã chạy nào trong phần stage.
- Đã kiểm: câu kiểm cash chỉ bỏ đúng mã vận đơn (chặt hơn, không lỏng); ba dạng SĐT như `customer/write_test.go:82`;
  bên ghi thật của nhịp sống và lần chép tệp dùng đồng hồ máy (`flusher.go:178`, `scripts/filesync/lib.sh:101`);
  `gateOnPidfile` không mở trước hạn cha, `Err()` giữ `DeadlineExceeded`, goroutine có chủ (`t.Cleanup`); trần thời
  gian chép nguyên số sang test `perf`, có tên, lý do; `newRTEnv` dò resync đúng với `pghub.go:176-177` (RLock/Lock);
  `frameGate` vẫn để `pending.add` thật gộp khung; khẳng định Task 421, 428 đã sửa mục (2), (3) đúng với mã.
- Ghi chú không bắt buộc (không sửa): goroutine chờ `parent.Done()` không có chốt riêng (nếu Process bỏ hạn bước thì
  test đỏ do hết giờ `go test`, vẫn đỏ); `frameGate.add` gọi `send` khi giữ khóa — đúng vì `pending.add` không chặn.
