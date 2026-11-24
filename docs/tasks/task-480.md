# Task 480 — Đo và làm nhanh tests/arch

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

Đề bài (chủ shop 24/11, không có mục trong kế hoạch): gói `tests/arch` mất 112–135 s và chạy ở mọi ci-task (kèm
-race). Đo từng test, tìm gốc chậm, làm nhanh mà không bỏ hay nới luật nào; không đổi mã chạy CRM.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ sửa tệp `_test.go` của gói `tests/arch` (gói
  chỉ có test, không xuất ký hiệu): `scan_test.go` (`parseGoFiles` dùng bộ đệm cây đã phân tích; 9 chỗ gọi sẵn có trong 8
  tệp giữ nguyên, thêm 1 chỗ gọi ở lint_test.go), `customer_writer_test.go` (lọc rẻ trước regexp), `lint_test.go` (`TestNoErrorStringCompareInRealCode`
  dùng bộ đệm chung với bộ lọc mới `isProductGoFile` thay vòng duyệt riêng), `skipdirs_test.go` (`strayTree` dùng
  `writeTree` mới, bỏ đoạn ghi tệp lặp — jscpd), tệp mới `scancache_test.go` (test của bộ đệm, bộ lọc rẻ, `writeTree`);
  thêm `t.Parallel()` vào 23 test sẵn có (các test quét mã thật, lint, go list, 3 test có database riêng) trong
  boundaries, customer_writer, dynsql, errlog, errors_mapped, lint, money_fields, order_status_sql, probedeps, routes,
  sellerfilter, skipdirs, sqlcvet, sqlmode, sqlusage, testkit_deps, txusage, viewsdoc, webexempt. Không đổi mã chạy CRM.
- Gói phụ thuộc ngược (go list): không gói nào nhập `tests/arch`.
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: cả gói `go test -race ./tests/arch/` (đo trước/sau trong lượt test của máy, bản chép run-in-slot.py);
  các test ca mẫu (`*Sample`, `*Testdata*`, `TestLintCatchesPlantedMistakes`, `TestErrorStringCompareDetector`…) phải
  còn đỏ đúng mẫu — chúng nằm trong cả gói; test mới cho bộ đệm (lỗi phân tích chỉ báo khi tệp được chọn; kết quả trùng
  bản không đệm) và cho bộ lọc rẻ của regexp khách hàng. Đổi cổng kiểm của task (tệp ci-task.sh) nếu có chỉ là patch
  trong scratchpad cho điều phối viên.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` chỉ liệt kê mã
  `internal/**`, không có bộ quét AST. Trong `tests/arch` đã có `parseGoFiles`, `findSelectorCalls`, `skipDir`,
  `isTestdata` (scan_test.go).
- Dùng lại / viết mới vì: dùng lại `parseGoFiles` (giữ chữ ký, mọi chỗ gọi không đổi), chỉ thêm bộ đệm bên trong;
  `skipDir`, `isTestdata` giữ nguyên nên tập tệp quét không đổi. Không tạo gói dùng chung (bộ đệm là mã test của gói
  arch).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.10: không áp dụng: task chỉ sửa test kiến trúc, không chạm dữ liệu, module, HTTP, Valkey, mã chạy.
- 5.11: không thêm thư viện (chỉ `sync`, `bytes` của thư viện chuẩn).
- 5.12: giữ cách viết test của gói arch (mỗi luật một test mã thật + một test ca mẫu); biến cấp gói duy nhất có
  `//nolint:gochecknoglobals` kèm lý do, như tiền lệ `lockorder_frame_test.go`.
- 6.1 (đồng thời): có — test chạy song song (`t.Parallel`) dùng chung bộ đệm cây đã phân tích: bảng có `sync.Mutex`,
  mỗi cây dựng một lần bằng `sync.Once`; cây AST chỉ đọc sau khi dựng (ast.Inspect không sửa, `token.FileSet` an toàn
  đồng thời); mỗi lần gọi trả lát mới; không test nào gán vào nút AST (grep). Test dùng `t.Setenv` hay đo thời gian
  khóa golangci-lint (`TestLintNotBlockedByOtherRunners`, `TestLintWaitsForSameWorktreeRunner`) giữ tuần tự; Go chạy
  mọi test song song cấp cao nhất sau khi test tuần tự xong, nên `TMPDIR` giả không lọt sang chúng. Ba test có database
  (`TestSqlcVet`, `TestViewsDocMatchesDatabase`, `TestRoutesAllServed`) song song được: mỗi test tạo database riêng
  (`testkit.StartPostgres`/`OpenAppDB`), không đọc biến môi trường chung. Hai lint thật (`TestLintCleanOnRealCode`,
  `TestLintCatchesPlantedMistakes`) song song vẫn nối đuôi nhau qua khóa riêng của worktree (`--allow-serial-runners`,
  như trước). Gói vẫn chạy dưới -race nên tranh chấp của chính bộ đệm bị bắt.
- 6.2–6.13: không áp dụng: không có yêu cầu HTTP, khóa database, việc nền, gọi ngoài, tiền, dữ liệu cá nhân hay quyền.
- Không bỏ hay nới luật: tập tệp mỗi test quét và điều kiện báo giữ y hệt; lỗi phân tích một tệp vẫn làm test đỏ khi
  (và chỉ khi) tệp đó được chọn, như trước.

## Đo (24/11, trong lượt test của máy: bản chép run-in-slot.py ở scratchpad `task-480/slot.py`, nice +10, taskpolicy -b)

Lệnh: `go test -race -count=1 -json ./tests/arch/`, tổng hợp theo `Elapsed` của từng test cấp cao nhất.

Trước (HEAD af09ea5, cache golangci-lint ấm): gói 86,8 s, cả lệnh 94 s. Lần đầu worktree mới (cache lint nguội): gói
394,9 s, trong đó `TestLintCleanOnRealCode` 258 s (golangci-lint ./... nguội; chạy riêng: nguội 75 s, ấm 4,7 s).

| Test (trước, ấm) | Giây | Lệnh ngoài / việc nặng |
|---|---|---|
| TestCustomerWriter_OnlyCustomerModule | 13,3 | regexp `(?is)…` trên mọi tệp .go/.sql (pprof: 50% regexp, 40% chi phí -race) |
| TestSQLModeProductCode | 9,2 | phân tích AST ~1.280 tệp |
| TestLintCleanOnRealCode | 8,3 | golangci-lint run ./... |
| TestLintWaitsForSameWorktreeRunner | 7,7 | golangci-lint, cố ý chờ khóa 6 s |
| TestRawErrorLogProductCode | 6,0 | phân tích AST lại toàn repo |
| TestNoErrorStringCompareInRealCode | 5,6 | phân tích AST lại toàn repo (vòng duyệt riêng) |
| TestDynSQLProductCode | 5,4 | phân tích AST lại toàn repo |
| TestTxUsageProductCode | 4,8 | phân tích AST lại toàn repo |
| TestLintCatchesPlantedMistakes | 2,9 | golangci-lint trên testdata/lint |
| TestSqlcVet | 2,7 | sqlc vet ×2, database thử |
| TestTestkitNoModules | 2,4 | go list -json ./... |
| TestRoutesAllServed | 2,4 | httpapi.NewServer, database |
| TestPackagePatternSkipsStrayDirs | 2,3 | go list trên cây tạm |
| TestBoundariesRealCode | 2,1 | go list -e -json ./... |
| TestLintNotBlockedByOtherRunners | 2,0 | golangci-lint trên cây tạm |
| TestErrorsMappedRealCode | 1,6 | phân tích AST |
| 37 test còn lại | < 1,5 mỗi test | |

Gốc chậm: (1) mọi test chạy tuần tự; (2) 9 test tự phân tích lại cùng ~1.280 tệp của repo, dưới -race (go/parser bị
gắn mã kiểm tranh chấp, chậm vài lần); (3) regexp khách hàng chạy trên mọi byte dưới -race. golangci-lint chỉ chậm khi
cache nguội; trong ci-task `make lint` chạy trước nên cache đã ấm.

Sửa: bộ đệm cây đã phân tích dùng chung (mỗi gốc phân tích một lần), `t.Parallel()` cho 23 test độc lập, lọc rẻ
`bytes.Contains` trước regexp khách hàng. Không bỏ test, không đổi tập tệp quét hay điều kiện báo.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước/sau trên cùng máy | cả gói | `slot.py go test -race -count=1 -json ./tests/arch/` | trước: gói 86,8 s (cả lệnh 94 s); sau, 2 lần: gói 37,2 s / 32,8 s (cả lệnh 47,7 s / 42,2 s) |
| Trước/sau xen kẽ trong cùng một lượt (máy tải ~10–12 do phiên khác) | cả gói, bản HEAD (git archive) và bản sửa | `slot.py bash pair.sh` | lượt 1: trước 87,0 s → sau 28,6 s; lượt 2: trước 70,5 s → sau 59,9 s (lượt 2 máy bận gấp đôi: test tuần tự `TestLintWaitsForSameWorktreeRunner` 7,5 → 18 s) |
| Trong ci-task thật | bước "test -race … test kiến trúc" | `ci-task.sh` | `ok elimaz.com.vn/crm/tests/arch 24.273s`, bước 32 giây; trước đó ở ci-task 478/479/195: 96,5 / 112,5–123,9 / 135,2 s |
| Không -race (tham khảo cho patch) | cả gói | `slot.py go test -count=1 -json ./tests/arch/` | gói 25,5 s, cả lệnh 33,8 s |
| Bộ đệm: phân tích một lần, lọc theo keep của từng lần gọi, tệp hỏng chỉ lỗi khi được chọn | TestParseTreeCachedAndFiltered | `go test -race -count=1 -run '^(TestParseTreeCachedAndFiltered\|…)$' ./tests/arch/` | trước khi có mã: không biên dịch (`undefined: parseTree`); sau: PASS |
| Lọc rẻ không bỏ sót chỗ regexp khớp (chữ của mẫu lọc không có dạng gập ngoài ASCII) | TestCustomerWritePrefilterNeverMissesRegexp | như trên | trước: `undefined: customerPrefilter`; sau: PASS; `TestCustomerWriter_OnlyCustomerModule` 13,3 s → 1,46 s |
| Bộ lọc mới của so lỗi bằng chuỗi giữ phạm vi cũ (bỏ _test.go, testdata, spikes ở mọi tầng) | TestIsProductGoFile | `go test -race -count=1 -run '^(TestIsProductGoFile\|TestNoErrorStringCompareInRealCode\|TestErrorStringCompareDetector)$' ./tests/arch/` | ok |
| Ca âm có sẵn vẫn đỏ đúng mẫu | TestBoundariesTestdata, TestBoundariesTestdataM3, TestLintCatchesPlantedMistakes, TestErrorStringCompareDetector, TestSQLModeReportsSample, TestTxUsageReportsSample, TestDynSQLReportsSample, TestRawErrorLogReportsSample, TestSQLUsageReportsSample, TestSellerFilterReportsSample, TestErrorsMappedSample, TestMoneyFieldsSample, TestWebExemptSample, TestOrderStatusSQL_DetectsSample, TestTestkitModuleDeps_DetectsTransitive, TestSqlcVet (câu sai cột) | cả gói (các lần đo sau và ci-task) | PASS (mỗi test này đòi đúng các phát hiện trên testdata) |
| Mẫu sai cài tạm vào mã thật bị bắt qua đường mới (bộ đệm, lọc rẻ, bộ lọc mới) | TestCustomerWriter_OnlyCustomerModule, TestNoErrorStringCompareInRealCode | thêm tạm `internal/modules/order/zz480/zz.go` (`"UPDATE Customer.CUSTOMERS …"`, `strings.Contains(err.Error(), …)`), chạy 2 test, xóa tệp | `--- FAIL: TestCustomerWriter_OnlyCustomerModule … [internal/modules/order/zz480/zz.go]`, `--- FAIL: TestNoErrorStringCompareInRealCode … zz480/zz.go:9:35`; sau khi xóa: git status không còn tệp |
| Ba test có database chạy song song | TestSqlcVet, TestViewsDocMatchesDatabase, TestRoutesAllServed | `slot.py go test -race -count=1 -run '^(TestSqlcVet\|TestViewsDocMatchesDatabase\|TestRoutesAllServed)$' ./tests/arch/` | `ok … 4.055s` |
| Cổng kiểm của task | ci-task (sau gộp main 5a9ec78) | `scripts/agent/ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 5a9ec786…)`; lần trước đó đỏ ở lint do "parallel golangci-lint is running" (golangci-lint của phiên khác giữ khóa $TMPDIR chung), chạy lại xanh |

## Quyết định thay chủ shop

- **Giữ -race cho tests/arch trong mã; bỏ -race ở ci-task chỉ là patch tùy chọn, không tự áp.** Patch:
  `/private/tmp/claude-501/-Users-luongducquyen-Documents-Th-i-trang/3f611c4b-a555-41c3-82e1-b84aa5a61f94/scratchpad/task-480/ci-task-arch-norace.patch`
  (`git apply --check` qua): tests/arch chạy không -race trong ci-task, trừ khi chính tests/arch bị sửa. Lợi thêm chỉ
  ~8–12 s mỗi ci-task (25,5 s so với 33–37 s), trong khi test giờ chạy song song với bộ đệm chung nên -race có ích khi
  sửa tests/arch. Mã CRM mà gói gọi (httpapi.NewServer + ServeHTTP tuần tự qua httptest.Recorder, pgxpool) đã có -race
  ở gói riêng và toàn bộ -race vẫn chạy trên main. Phương án khác: áp patch (điều phối viên quyết); hoặc bỏ hẳn -race
  (không chọn: mất kiểm tranh chấp của bộ đệm khi chính tests/arch đổi).
- **Không bỏ hay hoãn test lint thật (`TestLintCleanOnRealCode`) dù trùng `make lint` của ci-task.** Đo: khi cache ấm nó
  chỉ 5–15 s và giờ chạy song song với các test quét; bỏ nó là nới luật khi chạy `go test ./...` ngoài ci-task.
- **`TestLintWaitsForSameWorktreeRunner` giữ tuần tự** (đo thời gian chờ khóa 6 s; chạy song song với lint thật thì
  thời gian của nó lẫn với lint khác).

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có mục trong kế hoạch; đề bài nằm trong lời giao của chủ shop 24/11)

## Việc phát hiện thêm

- `make lint` (bước lint của ci-task) đỏ "parallel golangci-lint is running" khi một golangci-lint của phiên khác giữ
  khóa `$TMPDIR/golangci-lint.lock` chung (gặp 1 lần ở task này, 12:43). Test arch đã tránh việc này bằng $TMPDIR riêng
  của worktree và `--allow-serial-runners`; Makefile `lint` thì chưa. Đề xuất cho điều phối viên: cùng cách cho
  `make lint` (không sửa ở task này: ngoài phạm vi, chạm Makefile dùng chung).

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên phần đã stage sau khi gộp main 5a9ec78). Không có phát hiện phải sửa.
Reviewer đã kiểm: bộ đệm cây giữ đúng tập tệp và cách báo lỗi phân tích của `parseGoFiles` cũ, an toàn song song (khóa
bảng, `sync.Once`, lát mới mỗi lần gọi, không test nào sửa cây AST, không dùng `.Obj`/`Scope`); `isProductGoFile` +
`skipDir` cho đúng phạm vi vòng duyệt cũ của `TestNoErrorStringCompareInRealCode`; `mayWriteCustomers` không bỏ sót
chỗ regexp `(?is)` khớp ("tomer.c" không có s/k, các chữ còn lại chỉ gập trong ASCII, kể cả UTF-8 hỏng); 23 test thêm
`t.Parallel()` không dùng `t.Setenv`/`os.Chdir`/biến bị ghi, ba test database dùng database riêng; tên test trong
bảng bằng chứng khớp mã thật; không test nào bị xóa, tắt hay nới. Ghi chú không bắt buộc: không có điểm cần sửa.
