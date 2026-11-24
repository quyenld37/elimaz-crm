# Task 484 — Test e2e chạy song song

Kế hoạch: task chung của điều phối viên (24/11), không có mục trong kế hoạch; làm như Task 469, 470, 474, 475 cho gói
`tests/e2e` (đường găng của bước test thường ở ci-task: Task 198 đo 401 s trên 573 s). Loại việc (quy trình mục 3): A
(chỉ mã test, không đổi mã chạy)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có (gói `e2e` chỉ có tệp `_test.go`, không xuất gì).
  Trong gói: thêm `TestMain` (tệp mới `tests/e2e/main_test.go`) gọi `testkit.SetParallel(6)`; `buildPackage` (2 chỗ
  gọi trực tiếp + `buildBinary` 14 chỗ gọi, 15 lần dựng mỗi lượt) đổi thân: dựng mỗi gói main một lần cho cả tiến trình test (thư mục chung
  tạo trong `TestMain`), chữ ký giữ nguyên; `bootAndStop` cho vai trò ingest thư mục tệp tạm riêng (`t.TempDir()`)
  thay `os.TempDir()` dùng chung; thêm `t.Parallel()` dòng đầu cho các test chạy tệp chạy thật và
  `TestWorkerCrashDoesNotSendTwice`; thêm hàm `runForStderr` (gom 3 test chạy tệp chạy lấy stderr, jscpd). Không đổi `internal/testkit`, không đổi tệp không phải `_test.go`.
- Gói phụ thuộc ngược (go list): `elimaz.com.vn/crm/tests/e2e` chỉ có tệp test, không gói nào nhập.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: đo cả gói `go test -race -count=1 -v ./tests/e2e/` trước (HEAD 83849c3, chưa sửa) và sau, trong lượt
  test của máy (bản chép `run-in-slot.py`); `-count=3` cả gói (gần như mọi test vừa song song) để bắt chập chờn; đếm
  kết nối PostgreSQL lớn nhất lúc chạy; `ci-task.sh` một lần.
- Tệp có nhãn build (`files_pipeline_test.go` nhãn `imager`, `filesync_test.go` nhãn `filesync`) không chạy trong
  ci-task (cần dịch vụ compose riêng): không đổi, xem Soát thiết kế.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `testkit.SetParallel` (Task 470) đang dùng ở order,
  fulfillment, inventory, httpapi, catalog, cash, identity; `testkit.StartPostgres`, `OpenStaff` đã cho mỗi test một
  database riêng. Không có hàm dùng chung nào dựng tệp chạy một lần (grep `go build`, `exec.Command.*"build"` trong
  `internal/testkit`: không có; chỉ `tests/e2e/boot_test.go` dựng tệp chạy).
- Dùng lại / viết mới vì: dùng lại `testkit.SetParallel`; bộ nhớ đệm tệp chạy viết trong `boot_test.go` (thay thân
  `buildPackage`), không đưa vào `testkit` vì chỉ gói e2e dựng tệp chạy (một chỗ dùng).

## Soát thiết kế

Task chỉ đổi cách chạy test, không đổi mã chạy.

- 5.1–5.12: không áp dụng: không thêm dữ liệu, bảng, đường ghi, thư viện; test vẫn chạy tệp chạy thật trên PostgreSQL
  thật.
- 6.1 (hai việc cùng lúc) áp cho chính các test. Đã rà từng tệp:
  - Đo trước: 235 s, trong đó mỗi test chạy tệp chạy thật tốn 10–29 s cho `go build` (liên kết lại `cmd/crm` mỗi lần
    gọi `buildBinary`, 15 lần + 3 lần `tools/fakecarrier`); phần chạy thật 0,3–3 s. Dựng một lần cho cả tiến trình
    (`sync.Once` theo gói, lỗi dựng trả cho mọi test cần nó) là phần lợi lớn nhất; tệp chạy chỉ đọc sau khi dựng.
  - Test chạy tệp chạy thật (boot, ingest, worker, migrate, seed, `TestBinaryWorkerHandlesSelftest`): mỗi test database
    riêng (`StartPostgres`/`CreateDatabase`), cổng lấy từ cổng 0 (`freePort`), env truyền cho tiến trình con
    (`cmd.Env`, không `t.Setenv`), tiến trình con riêng, nhịp sống worker đọc trong database của chính nó. Chỗ dùng
    chung duy nhất: ingest của `bootAndStop` lấy `SPOOL_DIR=os.TempDir()` (mọi ingest chạy cùng lúc — kể cả phiên
    khác — chung thư mục tệp tạm, trái "một tiến trình ingest trên một thư mục" của `webhooks.Spool`) → đổi sang
    `t.TempDir()` như `TestIngest*` đã làm. → `t.Parallel()`.
  - `TestWorkerCrashDoesNotSendTwice`: `OpenStaff`, `FakeExternal` riêng, worker trong tiến trình không đặt gì toàn
    cục. → `t.Parallel()`.
  - Giữ lần lượt (chú thích "Không t.Parallel (Task 484)"): 6 test dựng `newStack` (`TestDatabaseOutageKeepsWebhooks`,
    `TestRetryFiftyParallelOneResult`, `TestPipelineUnderTwoSecondsOneTrace`, ba `TestMotDonDiTron*`) — `newStack` gọi
    `telemetry.Setup`, đặt `otel.SetTracerProvider` của cả tiến trình (otelpgx, otelhttp, queue lấy tracer lúc dựng);
    hai stack cùng lúc thì span có thể vào exporter của stack kia, test đếm span theo dấu vết đỏ sai. Hai test còn lấy
    giờ thật làm tiêu chí (đường ống < 2 s, mỗi bước < 5 s). Go chạy hết test lần lượt trước khi thả test Parallel,
    nên 6 test này vẫn chạy một mình như trước (~26 s); không đổi `telemetry` (mã chạy).
  - `TestFileSync` (nhãn `filesync`): đối chiếu đếm cả bucket ở kho dùng chung, test ảnh ghi vào cùng bucket → không
    thêm Parallel (vốn lần lượt). `TestFilesPipeline_*` (nhãn `imager`): không đổi — cần profile imager của compose,
    không chạy được trên `crm-shared` để kiểm; vốn lần lượt nên vẫn đúng.
  - `freePort` (đóng cổng rồi tiến trình con mới mở lại): khe hở có sẵn từ trước, cổng tạm của macOS cấp ngẫu nhiên nên
    hai test song song gần như không trùng; giữ nguyên.
  - Test con (`t.Run` của `TestBootEachRoleThenGracefulStop`) giữ lần lượt (chung một database).
- 6.2–6.8, 6.10–6.13: không áp dụng: không đổi đường nghiệp vụ, tiền, quyền, việc nền, dữ liệu cá nhân.
- 6.9 (treo lâu): hạn chờ sẵn sàng 10 s/15 s/20 s của test Parallel là chốt chống treo; tải song song làm chậm khởi
  động tiến trình con — đo bằng `-count=3`.
- Số kết nối: tiến trình con mở pool riêng; giới hạn 6 test song song như các gói lớn (`crm-shared` 300 kết nối dùng
  chung); đo số lớn nhất.

## Đo trước/sau

Mọi lần đo trong lượt test của máy (bản chép `run-in-slot.py` trong scratchpad, cùng khóa, nice +10, `taskpolicy -b`);
kết nối: mỗi giây đếm kết nối TCP ESTABLISHED tới :5600 của `e2e.test`, `crm`, `fakecarrier` (`lsof`; mẫu thưa nên là
chặn dưới). Máy 10 nhân; load 1 phút lúc đo `-count=3`: 5–6.

| Lần | Lệnh | Thời gian `go test` báo | Kết nối lớn nhất |
|---|---|---|---|
| Trước (HEAD 83849c3) | `go test -race -count=1 -v ./tests/e2e/` | 235,4 s (mỗi test có `go build` 10–29 s) | (không đo) |
| Sau | như trên | 38,3 s (thật 50,2 s gồm biên dịch) | 12 |
| Sau, chống chập chờn | `go test -race -count=3 -v ./tests/e2e/` | 76,4 s (3 lượt), 75 `--- PASS`, 0 FAIL | 12 |
| ci-task | `scripts/agent/ci-task.sh` | `ok …/tests/e2e 42.200s` | — |

Phần còn lại của thời gian: 6 test stack lần lượt (~22 s) và các test Parallel chờ tiến trình con (~13 s mỗi cái, chạy
cùng lúc).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Gói e2e nhanh hơn mà mọi test vẫn qua | cả gói `tests/e2e` (22 hàm Test, 3 test con) | `go test -race -count=1 -v ./tests/e2e/` trước/sau | 235,4 s → 38,3 s; mọi test `--- PASS` |
| Không chập chờn khi chạy song song | cả gói, 3 lượt | `go test -race -count=3 -v ./tests/e2e/` | `ok … 76.366s`, 75 PASS, 0 FAIL |
| Số kết nối trong giới hạn | cả gói | `measure.sh` (lsof mỗi giây) | lớn nhất 12 (trần `SetParallel(6)`) |
| 16 hàm Test có `t.Parallel()` dòng đầu; 6 test stack có chú thích "Không t.Parallel (Task 484)" | (dò tệp) | `grep -c "t.Parallel()" tests/e2e/*_test.go` | boot 6, ingest 2, worker 2, migrate 3, seed 1, pipeline 1, workercrash 1 |
| Gom `runForStderr` (jscpd) không đổi điều test kiểm | `TestBootMissingVariableFails`, `TestBootUnknownRoleFails`, `TestMigrateRoleHasNoHealthcheck`, `TestBootEachRoleThenGracefulStop` | `go test -race -count=1 -run '^(…)$' ./tests/e2e/` | `ok … 20.081s` |
| Cổng tĩnh + gói đổi (bản đã gộp main 4128a2a) | build, vet, lint, jscpd, gitleaks, oasdiff, test-guard, `tests/arch`, `tests/e2e` | `scripts/agent/ci-task.sh` | `Found 0 clones.`, `ok …/tests/arch 36.918s`, `ok …/tests/e2e 42.200s`, `CI-TASK XANH` |

## Quyết định thay chủ shop

- Dựng tệp chạy một lần cho cả tiến trình test (biến cấp gói `builds` có khóa, `sync.Once` theo gói) thay vì mỗi test
  một lần: phần lợi lớn nhất (≈ 190 s). Tệp chạy giống hệt nhau (cùng mã nguồn, cùng cờ). Phương án khác: dựng sẵn
  trong `TestMain` (dựng cả khi `-run` chỉ chọn test không cần).
- Giữ lần lượt 6 test stack thay vì khóa riêng cho `telemetry.Setup`: chúng có tiêu chí giờ thật (< 2 s, < 5 s), chạy
  một mình như trước; thêm song song chỉ lợi ~10 s. Phương án khác: mutex quanh stack để chồng lên test tệp chạy.
- Giới hạn 6 test song song như các gói lớn. Dễ đảo ngược: hằng `e2eParallel` hoặc `-parallel N`.
- Không đổi tệp có nhãn `imager`, `filesync` (không chạy được để kiểm trên `crm-shared`; vốn lần lượt).
- Gom phần chạy tệp chạy lấy stderr của 3 test vào `runForStderr` để qua jscpd (dòng `t.Parallel()` nối dài đoạn trùng).
- Lượt test của máy: hook chặn lệnh nhắc `scripts/agent/` nên đọc `run-in-slot.py` rồi ghi bản chép vào scratchpad.
- Không có "test thất bại trước": task chỉ đổi cách chạy test có sẵn; bằng chứng là đo và chạy lặp.
- Gộp main trước review: tua thẳng 83849c3 → 4128a2a (Task 483, không chung tệp).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 2, agent reviewer trên `git diff --cached` 12 tệp, bản đã gộp main 4128a2a)

- Lượt 1: CẦN SỬA — chỉ tệp task: số chỗ gọi `buildBinary` ghi 16, thật là 14 (15 lần dựng mỗi lượt vì
  `migrateRunner` dùng ở 2 test). Đã sửa dòng Ảnh hưởng và Soát thiết kế. Mã test được xác nhận đúng: test Parallel
  không dùng `telemetry.Setup`, `t.Setenv`, biến cấp gói (ngoài `builds` có khóa); `builds` đọc/ghi map dưới khóa,
  `sync.Once` mỗi gói, lỗi dựng giữ lại cho mọi test; `runForStderr` giữ nguyên hạn, env, tham số, điều kiện.
- Lượt 2: ĐẠT, mã không đổi so với lượt 1.
- Ghi chú không bắt buộc (không làm): `roleEnv` vẫn đặt `SPOOL_DIR=os.TempDir()` cho ingest; mọi chỗ khởi ingest hiện
  đều đè bằng `t.TempDir()`; test ingest mới quên đè sẽ dùng chung thư mục khi chạy song song.
