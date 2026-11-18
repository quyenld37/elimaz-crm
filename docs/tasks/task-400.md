# Task 400 — Sửa gốc các test chập chờn khi máy tải cao (M1-x5)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 400, M1-x5). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi mã test của gói `queue_test` (gói ngoài):
  `TestClaimLongQueueSameOrderKey` bỏ câu kiểm thời gian; vòng bốn ca (newEnv → fillLongQueue → explainAnalyze) tách ra
  hàm test `eachLongQueueCase` để test mới dùng chung (jscpd báo trùng 7 dòng khi chép); hằng `claimBudget` dời sang tệp
  test mới `internal/platform/queue/claimplan_perf_test.go` (nhãn build `perf`, test `TestClaimLongQueueTimeBudget`).
  Hằng `claimBudget` trước chỉ dùng trong `claimplan_test.go` (grep: 2 chỗ, cùng tệp). Không đổi mã chạy của `queue`.
- Gói phụ thuộc ngược (go list): không có (chỉ tệp `_test.go`, gói `queue_test` không ai nhập).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: chỉ `console/src/realtime/client.test.ts` (test "200 rồi im lặng…"); `client.ts` không đổi.
- Câu sqlc, bảng, pha migration: không có.
- Makefile: thêm đích `perf-test` chạy `go test -tags perf -count=1 -run '^TestClaimLongQueueTimeBudget$' -v
  ./internal/platform/queue/` (theo mẫu `probe-test`, `test-imager-compose` dùng nhãn build); CLAUDE.md thêm một dòng ở
  mục Lệnh.
- Test sẽ chạy: `go test -race ./internal/platform/queue/...` (vùng rủi ro cao: cả bộ của gói), `go vet -tags perf` và
  `make perf-test`; `cd console && npx vitest run src/realtime/client.test.ts`, `npx tsc --noEmit`, `npx eslint
  src/realtime`; chạy lặp ≥ 200 lần dưới tải cho từng test; cổng kiểm `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit: `ExplainAsApp`,
  `RequireNoSeqScan`); console: các test đã dùng `vi.useFakeTimers` + `vi.advanceTimersByTimeAsync`
  (`ErrorsPage.test.tsx`, `ProductList.test.tsx`); test "opaqueredirect" cùng tệp đã dùng `fetchImpl` giả.
- Dùng lại / viết mới vì: test hiệu năng dùng lại `eachLongQueueCase` (tách từ test cũ: `explainAnalyze`,
  `fillLongQueue`, `claimSQL`, `newEnv` có sẵn trong gói) (cùng gói `queue_test`, nhãn `perf` biên dịch cùng các tệp này). `testkit.ExplainAsApp` không đo BUFFERS hay thời
  gian nên không thay được `explainAnalyze`. Phía console: dùng `fetchImpl` (chỗ cắm sẵn của `createRealtimeClient`)
  với `ReadableStream` do test giữ và đồng hồ giả của vitest; viết mới hàm nhỏ `heldStreams()` trong tệp test vì chưa có
  bản giả luồng SSE nào.

## Soát thiết kế

- 5.1–5.12: không áp dụng: chỉ đổi test và một đích Makefile; không dữ liệu, module, HTTP, thư viện mới.
- 6.1–6.13: không áp dụng cho mã chạy (không đổi). Về chính test: test realtime mới không dùng đồng hồ thật, không mở
  cổng mạng; test queue vẫn tạo database riêng qua testkit trên crm-shared như cũ.

## Gốc lỗi (tái hiện)

1. `client.test.ts` › "200 rồi im lặng…": `client.ts` canh im lặng **cả lúc chờ header** (đúng spec 04 mục 6: cổng vào
   treo yêu cầu cũng phải nối lại) với watchdogMs 200 ms đồng hồ thật. Đây là test đầu tiên của tệp gọi `fetch` thật của
   Node tới máy chủ thật: lần `fetch` đầu của một worker vitest phải nạp undici và mở kết nối, dưới tải mất hơn 200 ms →
   watchdog hủy yêu cầu đầu → thứ tự `reconnecting, connected, reconnecting, connected`. Mã đúng; test sai vì lấy ngưỡng
   giờ thật làm tiêu chí. Tái hiện: 60 lần khởi động vitest nguội (4 lượt song song, kèm 30 tiến trình `yes` của phiên):
   **2/60 đỏ**, đúng thông điệp `expected [ 'reconnecting', 'connected', …(2) ] to deeply equal [ 'connected',
   'reconnecting', …(1) ]`; chạy nóng `repeats: 200` trong một worker kèm 16 tiến trình `yes` thì không đỏ (chỉ lần
   `fetch` nguội mới chậm).
2. `TestClaimLongQueueSameOrderKey`: câu kiểm `execMs > 50 ms` của EXPLAIN ANALYZE trên PostgreSQL dùng chung. Máy rảnh
   16–18 ms; kèm 30 tiến trình ăn CPU 17–32 ms; khi nhiều phiên cùng chạy test database (tải 25–35) vượt 50 ms. Số trang
   (`buffers` ~41.000, trần 60.000) và "không Seq Scan trên jobs" không phụ thuộc tải.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tái hiện lỗi realtime trước khi sửa | `client.test.ts` › "200 rồi im lặng…" (bản cũ) | 15 lượt × 4 `npx vitest run src/realtime/client.test.ts -t "200 rồi im lặng"` song song, kèm 30 tiến trình `yes` của phiên (dừng bằng PID) | 2/60 đỏ: `expected [ 'reconnecting', 'connected', …(2) ] to deeply equal [ 'connected', 'reconnecting', …(1) ]` |
| Realtime: chạy lặp ≥ 200 lần dưới tải, khởi động nguội | `client.test.ts` › "200 rồi im lặng…" (bản mới) | 50 lượt × 4 vitest nguội song song, kèm 30 tiến trình `yes` | `cold: pass=200 fail=0`, tải máy 79,9 |
| Realtime: chạy lặp ≥ 200 lần dưới tải, cùng worker | như trên | thêm tạm `{ repeats: 200 }` (không commit), `npx vitest run src/realtime/client.test.ts` kèm 30 tiến trình `yes` | `Tests 16 passed (16)`, tải 81 |
| Realtime: test kiểm đúng mốc watchdog, không qua suông | như trên | sửa tạm `client.ts` watchdog thành `watchdogMs + 5` (đã hoàn lại bằng `git checkout`) | đỏ: `expected [ 'connected' ] to deeply equal [ 'connected', 'reconnecting' ]` |
| Realtime: cả tệp dưới tải | 16 ca của `client.test.ts` | 10 lượt × 4 vitest nguội song song, 30 tiến trình `yes` | 40/40 lượt `16 passed` |
| Queue: chạy lặp ≥ 200 lần dưới tải (bản cuối) | `TestClaimLongQueueSameOrderKey` (4 ca mỗi lần) | `go test -race -count=200 -timeout 3h -run '^TestClaimLongQueueSameOrderKey$' -v ./internal/platform/queue/` kèm 20 tiến trình `yes` của phiên (tải máy 35–83) | 200/200 `--- PASS`, `ok … 2673.089s`; thời gian 15,26–42,74 ms (trần cũ 50 ms bị đẩy sát, đúng gốc lỗi), số trang 40.936–41.269 (trần 60.000), không Seq Scan |
| Queue: trần thời gian 50 ms vẫn còn, chạy riêng | `TestClaimLongQueueTimeBudget` | `make perf-test` | `--- PASS` 4 ca: 15,6–24,8 ms |
| Queue: cả bộ gói (vùng rủi ro cao) | mọi test `internal/platform/queue` | `go test -race -count=1 ./internal/platform/queue/...` | `ok … 228.181s` |
| Cổng kiểm của task | — | `ci-task.sh` (sau khi stage) | `CI-TASK XANH (so với điểm tách nhánh 97f26b0…)` |
| Lint nhãn perf | `claimplan_perf_test.go` | `go vet -tags perf ./internal/platform/queue/`, `golangci-lint run --build-tags perf ./internal/platform/queue/...` | sạch, `0 issues.` |

## Test bị xóa hoặc tắt

- `internal/platform/queue/claimplan_test.go` › `TestClaimLongQueueSameOrderKey`: bỏ một câu kiểm (`t.Errorf` khi
  `execMs > 50 ms`). Không nới, không xóa yêu cầu: câu kiểm dời nguyên ngưỡng 50 ms sang
  `TestClaimLongQueueTimeBudget` (`claimplan_perf_test.go`, nhãn build `perf`, `make perf-test`), vì thời gian EXPLAIN
  ANALYZE trên PostgreSQL dùng chung phụ thuộc tải máy (gốc lỗi mục 2) nên không dùng làm tiêu chí đúng/sai. Hai tiêu chí
  không phụ thuộc tải (không Seq Scan trên jobs, ≤ 60.000 trang) giữ nguyên ở test cũ.
- `console/src/realtime/client.test.ts` › "200 rồi im lặng…": viết lại bằng đồng hồ giả và `fetchImpl` giả; không bớt
  câu kiểm (thêm: chưa nối lại ở 199 ms, onResync một lần, chưa mở yêu cầu thứ hai trước khi hết backoff 20 ms).

## Khác kế hoạch

(không có)

## Quyết định thay chủ shop

- Test thời gian đặt sau nhãn build `perf` + đích `make perf-test` (theo mẫu `probe`, `imagercompose` đã có), không phải
  benchmark: giữ nguyên dạng "đỏ khi > 50 ms" của Task 394, chạy được bằng một lệnh. Phương án khác: `Benchmark…` (không
  tự đỏ khi vượt trần), hay biến môi trường + `t.Skip` (bị cấm: không skip). CI không chạy nhãn `perf` (máy CI dùng chung
  cũng bận); điều phối viên chạy `make perf-test` trên máy rảnh khi cần. Dễ đảo ngược: xóa dòng `//go:build perf`.
- Test realtime dùng `fetchImpl` giả thay máy chủ HTTP thật cho riêng ca này: đồng hồ giả của vitest không dùng chung
  được với `fetch` thật của Node (undici tự dùng bộ hẹn giờ). Các ca khác của tệp vẫn chạy trên máy chủ thật.

## Việc phát hiện thêm

- Các ca khác của `client.test.ts` vẫn dùng đồng hồ thật với biên rộng (nhịp tim 50 ms so với watchdog 200 ms; `close()`
  với watchdog 100 ms). Chạy cả tệp 40 lần khởi động nguội dưới tải 80 đều qua; chưa đổi (ngoài phạm vi hai test được
  giao). Nếu sau này đỏ thì chuyển sang `heldStreams()` + đồng hồ giả như ca "200 rồi im lặng".

## Reviewer

Kết luận: ĐẠT (lượt 1), không phát hiện phải sửa. Ảnh hưởng khớp từng dòng (`claimBudget` chỉ còn ở
`claimplan_perf_test.go`, ngưỡng 50 ms không nới); test realtime đi qua `fetchImpl` thật của `createRealtimeClient`, kiểm
đúng mốc 199/200 ms và backoff 20 ms. Ghi chú không bắt buộc: (1) CI không chạy nhãn `perf`, trần 50 ms chỉ được giữ khi
có người chạy `make perf-test` — báo chủ shop/điều phối viên (đã nêu ở Quyết định thay chủ shop); (2) ca realtime mới
gửi `RESYNC` ở mốc 0 nên không kiểm "mỗi khung đặt lại watchdog" — đã có ca "nhịp tim giữ kết nối" kiểm riêng, không
sửa.
