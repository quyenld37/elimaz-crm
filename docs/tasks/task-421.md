# Task 421 — Test chập chờn còn lại (M1-x8)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 421, M1-x8). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Phía Go chỉ đổi tệp `_test.go` và `Makefile`, không
  dòng nào của mã chạy. Ký hiệu test mới (chỉ trong gói test của nó, grep không trùng): `realtime_test.statusSpy`,
  `realtime_test.hangGuard`, `(*rtEnv).spy`; `inventory_test.(*invEnv).fillLargeTenant`,
  `inventory_test.TestReconcile_LargeTenantTimeBudget` (nhãn `perf`), `reconcileBudget`, `(*invEnv).seedLarge`,
  `(*invEnv).reconcileWithin` (`reconcile` giữ chữ ký, gọi `reconcileWithin(batch, "30s")`; tên `seed` đã có trong
  `env_test.go` nên dùng `seedLarge`). Bỏ hàm test `drain` của
  `realtime_test` (chỉ `reconnect_test.go` dùng; `drain` của `queue_test` là hàm khác, không đổi).
- Gói phụ thuộc ngược (go list): không có (chỉ tệp test, gói `_test` không ai nhập).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: **một lỗi thật** tìm ra khi chạy lặp dưới tải: `console/src/screens/inventory/SupplierPicker.tsx`
  (`showAdd` thêm điều kiện `term === text.trim()`); nơi dùng ô chọn: `DocumentEdit.tsx` (phiếu nhập, Task M1-51c) —
  không đổi props, không đổi API. Test: `SupplierPicker.test.tsx` thêm ca "đang chờ gõ…"; `DocumentEdit.test.tsx` không
  đổi (ca thêm nhanh của nó đỏ cùng gốc). Đổi cấu hình test của console: `console/src/test-setup.ts` (hạn chờ
  `findBy*`/`waitFor` 30 s cho mọi test), `console/vite.config.ts` (`test.testTimeout` 60 s); test
  `src/shell/Banner.test.tsx`, `src/shell/apiHealth.test.ts`, `src/realtime/client.test.ts`; test mới
  `src/test-setup.test.ts`.
- Câu sqlc, bảng, pha migration: không có. Test queue tắt/bật trigger `jobs_notify` trong transaction dựng dữ liệu thử
  (database thử riêng; cùng transaction nên khi commit trigger vẫn bật).
- `Makefile`: đích `perf-test` thêm dòng chạy `TestReconcile_LargeTenantTimeBudget`.
- Test sẽ chạy: các gói đổi — `internal/imager`, `internal/platform/realtime`, `internal/httpapi`,
  `internal/modules/inventory` (vùng rủi ro cao → cả bộ), `internal/platform/queue` (vùng rủi ro cao → cả bộ;
  `claimplan_test.go`, `shutdown_test.go`),
  `internal/platform/files`; console `npm test`, `tsc`, `eslint`; chạy lặp ≥ 100 lần dưới tải mỗi test đã sửa; cổng
  `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit không có "chờ
  luồng SSE"); mẫu của Task 400 (test thời gian nhãn `perf`, `claimplan_perf_test.go`, đích `make perf-test`) và Task
  420 (chốt chống treo 30 s, chờ theo sự kiện); `heldStreams` sẵn có trong `client.test.ts` (fetch giả cho đồng hồ giả);
  `waitFor`/`findBy*` của Testing Library; `sync.OnceFunc`.
- Dùng lại / viết mới vì: test perf của inventory theo đúng khuôn Task 400; ba test realtime console dùng lại
  `heldStreams` + `vi.useFakeTimers` như test đầu của tệp. Viết mới `statusSpy` (bọc `realtime.Bus` trong env test —
  cách duy nhất biết luồng SSE đã hỏi `Status` mà không thêm móc vào mã chạy) và `fillLargeTenant` (tách phần dựng dữ
  liệu để test đúng/sai và test perf dùng chung). Hằng `hangGuard` của gói realtime: mỗi gói test tự khai (như
  `queue_test`), không có chỗ dùng chung giữa các gói test.

## Soát thiết kế

- 5.1–5.12: không áp dụng: chỉ đổi test, cấu hình test, một đích Makefile và một điều kiện hiển thị của ô chọn nhà
  cung cấp (console, không gọi API mới, không đổi đường ghi: vẫn `createSupplier` qua `useWriteAction` có khóa chống
  trùng); không dữ liệu, module, HTTP, thư viện mới.
- 6.1–6.13: không áp dụng cho mã chạy (không đổi dòng nào). Với chính test: không lấy giờ thật làm tiêu chí đúng/sai;
  chờ theo sự kiện (kênh, `statusSpy`, `waitFor` theo số lần hỏi, đồng hồ giả); giờ chỉ còn là chốt chống treo (30 s Go,
  30 s/60 s console). Khóa: `makeDue` dùng `FOR UPDATE SKIP LOCKED` (không chờ khóa dòng của worker); dựng dữ liệu thử
  queue không NOTIFY (không chờ khóa hàng đợi thông báo chung của cụm). Test vẫn tạo database riêng trên crm-shared.

## Gốc lỗi và cách sửa

| Test | Gốc | Sửa |
|---|---|---|
| `internal/imager/client_test.go` `TestClient_LimitsParallelAndRetriesBusy` (phần giữ chỗ) | ngủ 20 ms rồi coi yêu cầu đầu đã giữ chỗ; máy chủ giả trả sau 100 ms. Máy chậm: yêu cầu đầu chưa giữ chỗ, hoặc hẹn giờ 30 ms của yêu cầu sau tới muộn hơn lúc chỗ được nhả → không ra `ErrBusy` | máy chủ giả báo "đã nhận" qua kênh `held` và chỉ trả khi test đóng `release` (sau khi yêu cầu sau trả); thêm kiểm yêu cầu giữ chỗ không lỗi |
| `internal/platform/realtime/reconnect_test.go` `TestReconnectResyncsClients` | ngủ một nhịp tim + lô rồi bỏ khung trong 10 ms; ping ghi trước lúc rớt (luồng SSE đã đọc `Status` = true) có thể tới muộn hơn → "listening = false mà vẫn nhận :ping" | `statusSpy` bọc Bus: đếm số lần luồng SSE nhận true (= số ping đã/sắp ghi) và báo khi nó nhận false (mọi ping trước đã ghi xong, cùng goroutine); test đọc đủ đúng số ping đó rồi mới kiểm cửa sổ 4 nhịp không có khung nào. Hạn 10 s/1 s chờ resync và khung changed → chốt chống treo 30 s |
| `internal/httpapi/handlers_jobs_test.go` `TestRetryJobRetryLaterNotStored` | goroutine giữ khóa dòng tới khi test thả **hoặc hết 2 s đồng hồ thật**: dưới tải khóa nhả trước khi yêu cầu tới bước chờ khóa → 200 thay 503 | bỏ hẹn giờ, chỉ thả bằng kênh; `t.Cleanup` thả khóa (`sync.OnceFunc`) nếu test đỏ giữa chừng |
| `internal/modules/inventory/reconcile_test.go` `TestReconcile_LargeTenant` | ngưỡng 60 s trong test đúng/sai; thêm hai hạn ngầm tìm ra khi chạy lặp (×50 dưới tải: **47 đỏ**, `57014` ở câu dựng dữ liệu): câu chèn 50.000 dòng dữ liệu thử chạy dưới `statement_timeout` 30 s của pool, và Reconcile chạy dưới `SET LOCAL statement_timeout = '30s'` | test đúng/sai giữ OK + đếm 50.000, bỏ cả ba hạn thời gian: dữ liệu thử qua `seedLarge` (statement_timeout 10 phút, chốt chống treo), Reconcile qua `reconcileWithin(0, "10min")`; thêm `TestReconcile_LargeTenantTimeBudget` nhãn `perf` (`reconcile_perf_test.go`, dùng `reconcile` 30 s/câu + trần 60 s), chạy bằng `make perf-test`. Các test Reconcile khác (dữ liệu nhỏ) giữ `reconcile` 30 s |
| `internal/platform/queue/claimplan_test.go` `TestClaimLongQueueSameOrderKey` (Task 68, 73: `55P03` ở bước chèn) | dữ liệu thử đã là `INSERT … SELECT generate_series`; trigger `jobs_notify` (AFTER INSERT, mỗi dòng `pg_notify`) làm commit của mỗi câu chèn phải lấy khóa hàng đợi thông báo **chung của cả cụm PostgreSQL** (PostgreSQL giữ khóa này từ lúc chuẩn bị commit tới hết commit). Trên crm-shared nhiều phiên cùng commit NOTIFY (mọi lần ghi việc) → chờ quá `lock_timeout` 5 s của pool | dựng dữ liệu trong một transaction, `DISABLE TRIGGER jobs_notify` đầu và `ENABLE` cuối transaction: không NOTIFY nên không chờ khóa chung; trigger vẫn bật sau commit. Lượt chạy lặp đầu (×100, tải máy 40–70) đỏ 2 lần ở câu chèn 50.000 dòng `done`: lần này `57014` (quá `statement_timeout` 30 s của pool, máy quá tải) → transaction dựng dữ liệu đặt `SET LOCAL statement_timeout = '10min'` (chốt chống treo của dữ liệu thử; câu đo `EXPLAIN` vẫn chạy với hạn của pool) |
| `internal/platform/files` (Task 71: `TestFileProcess_HangDoesNotBlockWorker` "quá hạn khóa", `TestProcessPrivate_PropertyNeverPublic` rapid "can not reproduce") | **không tái hiện được** (xem Bằng chứng). Đọc mã: chỗ chờ khóa duy nhất của gói là `makeDue` (`UPDATE … WHERE status = 'pending'` bằng owner, lock_timeout 5 s) chạy lặp song song với worker thật trong `TestFileProcess_HangDoesNotBlockWorker`, phải chờ khóa dòng của câu nhận việc / ghi kết quả của worker | `makeDue` chỉ đổi dòng không bị khóa (`FOR UPDATE SKIP LOCKED`); nơi gọi lúc worker chạy song song gọi lặp mỗi 200 ms nên lần sau bắt được dòng đó; nơi gọi khác chạy `RunOnce` đồng bộ, không có khóa nào đang giữ. `PropertyNeverPublic`: mọi đường hết giờ của lệnh riêng tư là `not_sent` → sau 3 lần thành rejected `processing_failed`, vẫn qua; không tìm ra đường đỏ do tải, giữ nguyên |
| `internal/platform/queue/shutdown_test.go` `TestStopAfterLongHandlerReleases` (tìm ra ở lượt `ci-task.sh` đầu) | hạn ghi 50 ms đồng hồ thật (`SetRecordTimeout`) mà câu nhả việc phải chạy xong; dưới tải quá 50 ms → `ghi trạng thái việc: timeout`, việc kẹt `running` | hạn ghi 5 s (bằng mặc định), grace 6 s: handler vẫn chạy lâu hơn hạn ghi (điều test kiểm: hạn tính từ lúc handler trả); cùng cách `TestRecordAfterLongHandler` đã sửa ở Task 317 |
| console `src/shell/Banner.test.tsx` "/me tải xong, không có quyền màn nào → câu trống" (Task 412) | chờ cố định 50 ms đồng hồ thật rồi kiểm đồng bộ; vitest cả bộ dưới tải: /me chưa trả, chưa vẽ | `findByText`; ca "API không trả lời" chờ băng báo và hai đường đã gọi bằng `waitFor` |
| console `src/screens/inventory/SupplierPicker.test.tsx` 'thêm nhanh "Xưởng Hoa"…' và `DocumentEdit.test.tsx` "thêm nhanh nhà cung cấp trong ô chọn…" (tìm ra khi chạy cả bộ 10 lần dưới tải: 1 đỏ; chạy thư mục inventory 30 lần: 2 đỏ) | **lỗi thật của mã**: dòng "+ Thêm nhà cung cấp" tính trên kết quả của từ khóa cũ trong lúc chờ gõ 300 ms (`found.data` là kết quả thật của `q=""`, không phải placeholder), nên hiện ngay, mất khi bắt đầu hỏi từ khóa mới (placeholder), rồi hiện lại thành nút mới. Dưới tải test bấm trúng nút đã bị gỡ → không gửi gì (DOM lúc đỏ: nút thêm nhanh còn đó, giá trị "(trống)"). Người dùng thật cũng có thể bấm hụt, và trong lúc chờ gõ dòng này có thể hiện cho tên đã có (kết quả `q=""` chỉ là một phần danh sách) | `showAdd` đòi thêm `term === text.trim()` (đúng ý chú thích sẵn có "kết quả của lần gõ trước không đủ để biết"); test mới đồng hồ giả (chỉ `setTimeout`): ở 299 ms chưa có dòng thêm nhanh, hết chờ gõ thì có. Kiểm âm: bỏ điều kiện → đỏ |
| console: mọi `findBy*`/`waitFor` (67 + 67 + 61 chỗ ở 12 tệp) | hạn mặc định 1 s của Testing Library là tiêu chí ngầm | `configure({ asyncUtilTimeout: 30_000 })` trong `test-setup.ts` (chốt chống treo), `testTimeout` 60 s; test `test-setup.test.ts` giữ cấu hình |
| console `src/shell/apiHealth.test.ts` | ngủ 30–150 ms rồi kiểm; hạn một lần hỏi 60 ms nên dưới tải lần hỏi **đã trả 200** vẫn có thể bị tính là quá giờ | hạn một lần hỏi 10 phút (trừ ca kiểm chính việc quá giờ), chờ theo số lần hỏi/số lần báo bằng `waitFor` |
| console `src/realtime/client.test.ts` | "không trả header" (watchdog 150 ms, máy chủ thật: yêu cầu thứ hai chậm cũng bị watchdog cắt), "nhịp tim giữ kết nối" (ping 50 ms thật, watchdog 200 ms), "refetchEveryMs ≥ 2 lần trong 350 ms" (setInterval thật); `until` hạn 2 s / 1,5 s | ba ca chuyển sang đồng hồ giả + `heldStreams` (như ca đầu của tệp, Task 400); ca refetch kiểm chặt hơn: đúng 3 lần; `until` hạn mặc định 30 s (chốt chống treo) |

Giữ nguyên (không phải dạng đỏ oan): các ca console còn ngủ rồi kiểm "không có gì thêm" (`stop()` không hỏi thêm, 302/401/403
không yêu cầu thêm, `close()`, "không tải lại định kỳ khi chưa nối được", "502 hai lần rồi 200" sau khi đã connected,
Banner "đang tải"/"401"/"API trả lời") — cửa sổ âm: máy chậm chỉ làm test dễ qua hơn, không đỏ oan. Danh sách thấp của
Task 420 (vòng hỏi có hạn 5–10 s, ký URL hết hạn của SeaweedFS) không đổi.

## Quyết định thay chủ shop

- `files`: không tái hiện được lỗi Task 71 sau 4 lượt dưới tải (tổng ~60 lần chạy các test nghi). Chọn sửa chỗ chờ
  khóa duy nhất có thể (`makeDue` → `SKIP LOCKED`) thay vì để nguyên; dễ đảo ngược, không đổi tiêu chí. Phương án khác:
  để nguyên và chờ lần đỏ sau có log đầy đủ.
- Queue: tắt trigger `jobs_notify` trong transaction dựng dữ liệu (thay vì tăng `lock_timeout` riêng của test): bỏ hẳn
  việc chờ khóa chung, không chỉ kéo dài; dữ liệu thử không cần đánh thức worker. Gốc lỗi suy từ mã PostgreSQL và lỗi
  `55P03` đã ghi, không tái hiện được trên máy (cần nhiều phiên cùng commit NOTIFY).
- Console: đặt hạn chờ 30 s cho **mọi** `findBy*`/`waitFor` một chỗ (`test-setup.ts`) thay vì sửa từng chỗ trong 12
  tệp: cùng một gốc (hạn mặc định 1 s), một chỗ dễ đảo ngược.
- `SupplierPicker`: sửa mã màn hình (lỗi thật, brief cho phép "lỗi thật trong mã thì sửa mã có test"). Hành vi nhìn
  thấy chỉ khác ở chỗ dòng thêm nhanh không còn chớp hiện trong 300 ms chờ gõ; không có mockup mới vì không thêm trạng
  thái nào (mockup kho-phieu trạng thái B vẫn đúng). Phương án khác: giữ mã, sửa test chờ cho danh sách ổn định — bỏ
  qua vì người dùng thật cũng bấm hụt được.
- Ba ca của `client.test.ts` chuyển sang đồng hồ giả dù brief chỉ nêu `findBy*`/`waitFor`: cùng dạng "giờ thật làm
  tiêu chí", Task 400 đã làm như vậy với ca đầu của tệp.

## Bằng chứng

Tải: `loadloop.sh` của phiên (8 tiến trình `nice -n 10 perl -e 'alarm 600; 1 while 1'`, mỗi lượt ≤ 600 s, dừng bằng
đúng PID đã ghi); tải máy 33–72 trong lúc chạy (cả phiên khác). Một lần sơ suất lúc 06:12: lượt cũ còn sống khi bật lượt
mới, 16 tiến trình trong ~1,5 phút; đã dừng 8 PID của lượt cũ và sửa `load.sh` để không bật khi lượt trước còn sống.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| imager giữ chỗ ×100 dưới tải | `TestClient_LimitsParallelAndRetriesBusy` | `go test -race -count=100 -run '^TestClient_LimitsParallelAndRetriesBusy$' ./internal/imager/` | `ok … imager 136.541s` |
| realtime ×100 dưới tải | `TestReconnectResyncsClients` | `go test -race -count=100 -run '^TestReconnectResyncsClients$' ./internal/platform/realtime/` | `ok … realtime 829.708s` |
| realtime còn bắt lỗi (kiểm âm) | `TestReconnectResyncsClients` | tạm cho `sse.go` gửi ping bất kể `listening` (đã hoàn lại) | `reconnect_test.go:90: listening = false mà vẫn nhận ":ping"` FAIL |
| imager còn bắt lỗi (kiểm âm) | `TestClient_LimitsParallelAndRetriesBusy` | tạm bỏ chặn chỗ trống trong `client.go` (đã hoàn lại) | `client_test.go:120: cao nhất 2 việc cùng lúc (muốn 1)` FAIL |
| httpapi giữ khóa ×100 dưới tải | `TestRetryJobRetryLaterNotStored` | `go test -race -count=100 -run '^TestRetryJobRetryLaterNotStored$' ./internal/httpapi/` | `ok … httpapi 491.111s` |
| queue ×100 dưới tải, lượt 1 (chỉ tắt trigger) | `TestClaimLongQueueSameOrderKey` (4 ca) | `go test -race -count=100 -timeout 4h -run '^TestClaimLongQueueSameOrderKey$' ./internal/platform/queue/` | 98/100 qua, 2 đỏ `57014` ở câu chèn 50.000 dòng `done` → thêm `SET LOCAL statement_timeout` |
| queue ×100 dưới tải, bản cuối | `TestClaimLongQueueSameOrderKey` (4 ca) | như trên | `ok … queue 3843.789s` |
| inventory ×50 dưới tải, trước khi sửa hạn của dữ liệu thử | `TestReconcile_LargeTenant` | `go test -race -count=50 -run '^TestReconcile_LargeTenant$' ./internal/modules/inventory/` | 47/50 đỏ `57014` (`sửa thẳng bằng chủ bảng`) — tái hiện được |
| inventory ×100 dưới tải, bản cuối | `TestReconcile_LargeTenant` | như trên, hai lượt ×50 | `ok … inventory 2180.730s`; `ok … inventory 1213.072s` |
| test thời gian mới chạy được | `TestReconcile_LargeTenantTimeBudget` (nhãn `perf`) | `go test -race -tags perf -count=1 -run 'TestReconcile_LargeTenantTimeBudget' -v ./internal/modules/inventory/` | `50.000 số dư: 3.605040416s`, PASS |
| files: tái hiện lỗi Task 71 | cả gói; `TestProcessPrivate_PropertyNeverPublic`, `TestFileProcess_HangDoesNotBlockWorker`, `…LookerMissingSourceAndHang`, `…ManyImagesNoBusyRetry` | hai `go test -race -count=2 ./internal/platform/files/` song song; hai `-count=15` hai test nghi song song (hết 9 phút, không ca nào đỏ); `-count=6` bốn test song song với cả gói `files`, `imager`, `realtime` | không tái hiện: mọi lần `ok` (hai lần `-count=15` dừng vì hạn 9 phút của lệnh, không có `--- FAIL`) |
| files ×100 dưới tải, bản cuối | `TestFileProcess_HangDoesNotBlockWorker`, `TestFileProcess_RejectedNotImage` | `go test -race -count=100 -run '^(TestFileProcess_HangDoesNotBlockWorker\|TestFileProcess_RejectedNotImage)$' ./internal/platform/files/` | `ok … files 1181.515s` |
| console Banner, apiHealth, realtime client ×100 dưới tải | `src/shell/*`, `src/realtime/*`, `src/test-setup.test.ts` | `npx vitest run src/shell src/realtime src/test-setup.test.ts` lặp 100 lần | `console: 100 qua, 0 đỏ` |
| console cả bộ dưới tải, trước khi sửa SupplierPicker | cả bộ | `npx vitest run` lặp 10 lần | 9 qua, 1 đỏ: `SupplierPicker` 'thêm nhanh "Xưởng Hoa"' (30530 ms, giá trị "(trống)") |
| console thư mục inventory, trước khi sửa | `src/screens/inventory/*` | `npx vitest run src/screens/inventory` lặp 30 lần | 2 đỏ: `SupplierPicker.test.tsx:64` (nút thêm nhanh còn, giá trị "(trống)"), `DocumentEdit.test.tsx:76` (không có lần ghi `createSupplier`) |
| SupplierPicker: test mới đỏ trước, kiểm âm | "đang chờ gõ, danh sách còn của từ khóa cũ: chưa có dòng thêm nhanh" | `npx vitest run src/screens/inventory/SupplierPicker` trên mã cũ; lại sau khi tạm bỏ `term === text.trim()` | `SupplierPicker.test.tsx:92` FAIL (dòng thêm nhanh có ở 299 ms) cả hai lần |
| console thư mục inventory ×100 dưới tải, bản cuối | `src/screens/inventory/*` (106 test) | `npx vitest run src/screens/inventory` lặp 100 lần | `inventory console: đỏ 0/100`, `Tests 106 passed` |
| console cả bộ ×20 dưới tải, bản cuối | cả bộ (438 test) | `npx vitest run` lặp 20 lần | `cả bộ console: đỏ 0/20`, `Tests 438 passed (438)` |
| hạn chờ chung được đặt | `test-setup.test.ts` "findBy*/waitFor chờ tới chốt chống treo 30 s" | `npx vitest run` | qua (trong 438) |
| queue `TestStopAfterLongHandlerReleases` (tìm ra ở lượt `ci-task.sh` đầu: `ghi trạng thái việc: timeout`, việc kẹt `running`) | `TestStopAfterLongHandlerReleases` | `go test -race -count=100 -run '^TestStopAfterLongHandlerReleases$' ./internal/platform/queue/` (8 tiến trình tải) | `ok … queue 664.095s` (tải máy 25–33 lúc bắt đầu) |
| Cổng kiểm, lượt 1 (trước khi stage) | — | `ci-task.sh` | ĐỎ: gen-check (chưa stage), jscpd (ca "lỗi mạng"/"502" của `apiHealth.test.ts` trùng → gộp `it.each`), prettier `SupplierPicker.tsx` (đã `prettier --write`), `queue` › `TestStopAfterLongHandlerReleases` (đã sửa, xem trên), `testkit` › `TestCleanupStaleDropsOnlyIdleOldDatabases` (khóa advisory chung, hook Stop chạy testkit cùng lúc; xem Việc phát hiện thêm) |
| Cổng kiểm, lượt 2 (sau khi stage) | — | `ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 0a82e343…)`, 41 gói `ok` (gồm `testkit 3.784s`, `queue`, `inventory`, `files`, `realtime`, `imager`, `httpapi`, `tests/deploy`), console tsc/eslint/prettier/vitest xanh |
| kiểu, lint console | — | `npx tsc --noEmit`; `npx eslint .` | không lỗi |

## Test bị xóa hoặc tắt

Không test nào bị xóa hay tắt. Câu kiểm bị bỏ hoặc thay:

- `TestReconcile_LargeTenant`: bỏ `took > 60*time.Second` và hạn 30 s/câu của Reconcile → chuyển sang
  `TestReconcile_LargeTenantTimeBudget` (nhãn `perf`, cùng dữ liệu, cùng hai tiêu chí); test đúng/sai giữ OK + đếm đủ.
- `TestReconnectResyncsClients`: hạn 10 s chờ resync và 1 s chờ khung changed → chốt chống treo 30 s (chờ sự kiện).
- console `client.test.ts`: `until` hạn 2 s/1,5 s → 30 s; ca refetch "≥ 2" → "đúng 3" (chặt hơn).
- console `apiHealth.test.ts`: hạn một lần hỏi mặc định của hàm `watch` trong test 60 ms → 10 phút ở các ca không kiểm
  việc quá giờ (ca "không trả lời trong timeoutMs" giữ 40 ms); mọi câu kiểm giữ nguyên, chỉ đổi cách chờ.
- console `findBy*`/`waitFor`: hạn 1 s → 30 s (chốt chống treo); không câu kiểm nào đổi.
- `TestStopAfterLongHandlerReleases`: hạn ghi 50 ms → 5 s, grace 300 ms → 6 s; câu kiểm (`ErrStopGrace`, pending,
  attempts 0) giữ nguyên.
- Không phải câu kiểm nhưng ghi cho đủ: `TestRetryJobRetryLaterNotStored` bỏ hẹn giờ 2 s nhả khóa; `makeDue` của
  `files` bỏ qua dòng đang khóa; `fillLongQueue` của `queue` tắt trigger trong transaction dựng dữ liệu.

## Khác kế hoạch

- Kế hoạch nêu 7 nhóm test; làm thêm hai thứ tìm ra khi chạy lặp dưới tải: lỗi thật của `SupplierPicker` (mã màn hình,
  có test) và hai hạn `statement_timeout` ngầm của `TestReconcile_LargeTenant`, một của `TestClaimLongQueueSameOrderKey`.
- `files`: không tái hiện được lỗi Task 71 (xem Quyết định thay chủ shop).
- `queue` › `TestStopAfterLongHandlerReleases`: không có trong danh sách, đỏ ở lượt `ci-task.sh` đầu; cùng dạng nên sửa.

## Việc phát hiện thêm

- `internal/testkit/pgtemplate_test.go` › `TestCleanupStaleDropsOnlyIdleOldDatabases`: đỏ ở lượt `ci-task.sh` đầu và
  ở hook Stop chạy cùng lúc (`SELECT pg_advisory_lock(727391): timeout: context deadline exceeded`; lần kia `bỏ qua dọn
  …: timeout`). Test chờ (`pg_advisory_lock`, không thử) hai khóa advisory chung của cả cụm (`templateLockKey`,
  `cleanupLockKey`) mà mọi `testkit.StartPostgres` của mọi phiên trên crm-shared lấy khi dựng/dọn database mẫu; hạn
  ctx 1 phút. Khi nhiều phiên (ở đây cả hook Stop và ci-task của chính phiên này) cùng chạy testkit dưới tải, chờ quá 1
  phút → đỏ oan. Không sửa ở đây: task không chạm testkit, đổi testkit buộc chạy toàn bộ test; nên làm task riêng (chờ
  khóa với chốt chống treo dài hơn, hoặc khóa riêng theo tên database thử).

## Reviewer

Kết luận: ĐẠT (vòng 1).

- Phát hiện phải sửa: không có.
- Đã đối chiếu: mọi ký hiệu và tệp ở mục Ảnh hưởng khớp diff; `statusSpy` chỉ đếm lời gọi của luồng SSE (`waitListening`
  gọi thẳng hub), không còn tín hiệu `idle` cũ vì `newRTEnv` chờ `listening = true` trước khi mở luồng; `shutdown_test`
  vẫn phân biệt hạn ghi tính từ lúc bắt đầu (5 s < 6 s); `SKIP LOCKED` không làm sót việc; test perf giữ đủ ba tiêu chí
  cũ; không `t.Skip`/`.skip`/`.only`, không đụng mã chạy Go.
- Ghi chú không bắt buộc (không sửa): `<-held` trong test imager không có chốt chống treo (chỉ treo tới hạn `go test`,
  không qua sai); `testTimeout` 5 s → 60 s của vitest là chốt chống treo (đã nêu ở Ảnh hưởng, bảng Gốc lỗi); `makeDue`
  chỉ đổi thân hàm, chữ ký giữ nguyên.
