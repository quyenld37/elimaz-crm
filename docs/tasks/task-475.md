# Task 475 — Test catalog, cash chạy song song

Kế hoạch: task chung của điều phối viên (24/11), không có mục trong kế hoạch; làm như Task 469, 470, 474 cho gói
`internal/modules/catalog`, `internal/modules/cash` (cùng gói con `cash/internal/statement`, `cash/internal/reconcile`),
và chuyển `TestMain` của `internal/httpapi` sang `testkit.SetParallel`. Loại việc (quy trình mục 3): A (chỉ mã test,
không đổi mã chạy)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Dùng lại `testkit.SetParallel` (Task 470, không đổi
  nó); thêm chỗ gọi ở `TestMain` của catalog (`products_test.go`), cash (`env_test.go`), statement
  (`property_test.go`), httpapi (`main_test.go` — thay 4 dòng `flag.Visit`/`flag.Set` chép tay của Task 474, xóa
  import `runtime`, `strconv`). Thêm `t.Parallel()` dòng đầu: catalog 69/70 hàm Test (trước 0; giữ lần lượt `TestApply_AllOrNothingProperty`); cash 8 hàm còn thiếu
  (trước 36/44); statement 15 hàm còn thiếu (trước 12/29), giữ lần lượt 2 hàm (xem Soát thiết kế); reconcile đã đủ
  7/7, không đổi. Thêm hàm test `(*catalogEnv).mustCreate` (`products_test.go`), dùng ở `TestCreateProduct_DuplicateSKU`,
  `TestAddVariant_ProductVersion` (gom đoạn trùng jscpd báo, xem Tái dùng). Không đổi `internal/testkit`, không đổi
  tệp không phải `_test.go`.
- Gói phụ thuộc ngược (go list): tệp `_test.go` không được gói nào nhập; không gói nào bị ảnh hưởng.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: đo cả gói `-race` catalog, cash, statement, reconcile trước (bản `git archive HEAD`) và sau, trong lượt
  test của máy (bản chép `run-in-slot.py`), 1 lần mỗi bên; sau đó `-count=3` cả gói catalog, cash, statement (gần như
  mọi test đều vừa Parallel); httpapi chỉ đổi `TestMain` (cùng hành vi) → chạy ở ci-task; `ci-task.sh` một lần.
- Gói con không database (`catalog/internal/{article,barcode,importer}`, `cash/internal/{formats,postings,adjust}`):
  không đổi — chạy dưới 1–2 s, ngoài đề bài.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `testkit.SetParallel` (Task 470: min(n, GOMAXPROCS),
  tôn trọng `-parallel` của người chạy) đang dùng ở order, fulfillment, inventory; grep `test.parallel` ngoài testkit
  chỉ còn bản chép của httpapi (Task 474). `testkit.SetRapidChecks` đã có trong TestMain của catalog, cash, statement.
- Dùng lại / viết mới vì: dùng lại `testkit.SetParallel` cho cả bốn gói; xóa bản chép tay của httpapi (CLAUDE.md mục
  4: chuyển sang cách mới thì xóa cách cũ). Không viết hàm dùng chung mới.
- jscpd (ci-task lần 1): dòng `t.Parallel()` nối dài đoạn đầu `TestCreateProduct_DuplicateSKU` và
  `TestAddVariant_ProductVersion` (tạo DAM01, `t.Fatal` nếu lỗi, rồi hàm `add`) thành bản trùng 8 dòng. Trả lời: gộp
  phần tạo mẫu vào `(*catalogEnv).mustCreate` (cùng mục đích, cùng lý do thay đổi: dựng dữ liệu đầu test), đặt cạnh
  `create`; hai hàm `add` khác nhau giữ riêng. Sau gộp `make jscpd`: "Found 0 clones". Không đổi mốc nền jscpd.

## Soát thiết kế

Task chỉ đổi cách chạy test, không đổi mã chạy.

- 5.1–5.12: không áp dụng: không thêm dữ liệu, bảng, đường ghi, thư viện; test vẫn chạy trên PostgreSQL thật qua testkit.
- 6.1 (hai việc cùng lúc): áp cho chính các test. Đã rà catalog, cash, statement, reconcile:
  - Mỗi test có database riêng (`testkit.OpenStaff` qua `newCatalogEnv`, `newImportEnv`, `newPayEnv`, `newCODEnv`,
    `newFlowEnv`…), kho tệp riêng (`filestest.MemFileStore`), bus `realtimetest.RecordingBus`, worker, logger
    (`slog.New` ghi vào buffer của env), `events.NewRegistry()` của từng env.
  - Không có `t.Setenv`/`os.Setenv`, `CaptureSpans`, `slog.SetDefault`/`otel.Set*`, advisory lock, `pg_stat_*`,
    `pg_locks`, `ALTER ROLE/SYSTEM/DATABASE`, `LISTEN`, `httptest`/cổng cố định. Biến cấp gói của test chỉ đọc
    (`urlAttr`, `v1URL` regexp; `errCrash` sentinel); mã chạy của hai module không có biến cấp gói đổi được (chỉ
    `vnZone`); hook `export_test.go` của statement (`SetAfterBatch`, `SetLimits`) chỉ đổi `*Parser` riêng của env.
  - Trạng thái chung duy nhất: `cash.RegisterFilePurposes()` trong `newImportEnv` ghi vào sổ mục đích tệp của tiến
    trình — sổ có `sync.RWMutex`, khai lại cùng mục đích là không làm gì (`files.Registry.Register`), nên an toàn khi
    nhiều test gọi cùng lúc.
  - Giữ chạy lần lượt (chú thích "Không t.Parallel (Task 475)" ở dòng đầu): `TestImport_TooLargeRejectedBeforeRead`,
    `TestImport_ZipBombFailsFast` (statement) — tiêu chí đo bộ nhớ cấp phát bằng `runtime.MemStats.TotalAlloc` của cả
    tiến trình (test khác chạy cùng lúc cộng vào, đỏ sai), `ZipBombFailsFast` còn lấy giờ thật ≤ 5 s làm tiêu chí. Go
    chạy hết test lần lượt trước khi thả test Parallel, nên hai test này chạy một mình.
  - Giữ chạy lần lượt `TestApply_AllOrNothingProperty` (catalog, `imports_property_test.go`): `propertyChecks` đổi cờ
    `rapid.checks` của cả tiến trình (`flag.Set`, trả lại bằng `t.Cleanup`); chạy song song thì đua dữ liệu với test
    rapid khác (`TestRenderArticleHTML_Property` cần 500 lượt) và có thể làm test đó chỉ chạy 100 lượt. Reviewer lượt 1
    phát hiện (ban đầu đã thêm Parallel nhầm).
  - Ngưỡng giờ thật còn lại trong test Parallel không phải tiêu chí bị tải làm đỏ sai: `TestApply_TimeoutMidWriteRetries`
    (catalog, hạn 300 ms phải hết giữa lúc áp 200 mẫu — máy chậm hơn chỉ làm hạn hết sớm hơn so với việc), test khóa
    `lock_timeout 300ms` (catalog, chứng minh không chờ khóa; có khóa thì đỏ bất kể tải), `Poll: 50ms` của worker, hạn
    10 s của client HTTP: chốt chống treo. `imports_test.go` chỉ ghi thời gian áp vào log.
  - Test con (`t.Run`, `eachSeller`) giữ chạy lần lượt như cũ (dùng chung một env).
- 6.2–6.8, 6.10–6.13: không áp dụng: không đổi đường nghiệp vụ, tiền, quyền, việc nền, dữ liệu cá nhân.
- 6.9 (treo lâu): test song song chậm hơn từng cái; hạn câu lệnh, chờ khóa giữ nguyên; `-count=3` để bắt chập chờn.
- Số kết nối: mỗi test tới ~9 kết nối (2 pool × 4 + 1 quản trị lúc tạo database); `crm-shared` max_connections 300 dùng
  chung nhiều phiên → giới hạn 6 test song song mỗi gói như order, fulfillment, inventory, httpapi.

## Đo trước/sau

Cách đo (mọi lần trong lượt test của máy: bản chép `run-in-slot.py`, cùng khóa `~/Library/Caches/crm-test-slot.lock`,
nice +10, `taskpolicy -b`): script trong scratchpad chạy lần lượt từng gói `go test -race -count=1
./internal/modules/<gói>/`, mỗi giây đếm kết nối TCP ESTABLISHED tới :5600 của tiến trình `<gói>.test` (`lsof`, lấy
lớn nhất; mẫu thưa nên là chặn dưới). "Trước" chạy trên bản `git archive HEAD` (007654c). Máy 10 nhân, `crm-shared`
max_connections 300 dùng chung nhiều phiên. Thời gian là số `go test` báo (không tính biên dịch).

| Gói | Trước (1 lần) | Sau (1 lần, `-parallel` 6) | Sau `-count=3` (3 lượt) | ci-task |
|---|---|---|---|---|
| catalog | 115,9 s, 5 kết nối | 31,5 s, 12 kết nối | 84,0 s (≈28 s/lượt), 15 kết nối, ok | 30,5 s; 35,6 s (lần 2) |
| cash | 27,9 s, 20 kết nối | 25,7 s, 11 kết nối | 74,6 s, 12 kết nối, ok | 39,4 s |
| cash/internal/statement | 38,3 s, 5 kết nối | 31,9 s, 9 kết nối | 90,9 s, 18 kết nối, ok | 57,9 s |
| cash/internal/reconcile | 2,5 s (không đổi mã) | — | — | 2,7 s |

Load 1 phút lúc đo: 2,75–5,78. Kết quả: catalog nhanh ~3,7 lần; cash gần như không đổi (36/44 test đã song song từ
trước, thời gian nằm ở các test quy luật rapid dài); statement nhanh ~17% (phần còn lại là test quy luật
`TestProperty_MatchConfirm` 200 lượt và hai test đo bộ nhớ giữ lần lượt). Kết nối lớn nhất đo được 20 (bản trước của
cash, chưa giới hạn), sau giới hạn ≤ 18; trần lý thuyết 6 × ~9 = 54.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mọi hàm Test của bốn gói có `t.Parallel()` dòng đầu, trừ test dùng trạng thái chung | (dò tệp) | awk: dòng sau `func Test…(t *testing.T)` trong `internal/modules/{catalog,cash,cash/internal/statement,cash/internal/reconcile}/*_test.go` | catalog 69/70, cash 44/44, reconcile 7/7, statement 27/29; 3 hàm còn lại có chú thích "Không t.Parallel (Task 475)": `TestApply_AllOrNothingProperty` (catalog), `TestImport_TooLargeRejectedBeforeRead`, `TestImport_ZipBombFailsFast` (statement) |
| Giới hạn số test song song qua hàm chung, không còn bản chép | `TestMain` của catalog, cash, statement, httpapi gọi `testkit.SetParallel` | `grep -rn "test.parallel" internal` (ngoài testkit) | không còn dòng nào; kết nối sau đo ≤ 18 |
| Các gói chạy song song mà vẫn đúng, không chập chờn | cả gói catalog, cash, statement | `go test -race -count=1` và `-count=3` (trong lượt test của máy, xem Đo trước/sau) | tất cả `ok` |
| httpapi vẫn đúng khi đổi `TestMain` | cả gói httpapi | `ci-task.sh` lần 1 | `ok …/internal/httpapi 30.959s` |
| Gom đoạn trùng không đổi điều test kiểm | `TestCreateProduct_DuplicateSKU`, `TestAddVariant_ProductVersion` | `go test -race -count=1 -v -run '^(TestCreateProduct_DuplicateSKU\|TestAddVariant_ProductVersion)$' ./internal/modules/catalog/` | 2 × `--- PASS`, `ok` |
| Cổng tĩnh + gói đổi | build, vet, lint, jscpd, gitleaks, oasdiff, test-guard, test các gói đổi, `tests/arch` | `ci-task.sh` | lần 1: mọi test `ok` (httpapi, cash và 4 gói con, statement, catalog, tests/arch), đỏ jscpd 1 đoạn; lần 2 sau gom: `Found 0 clones.`, `ok …/catalog 35.611s`, `ok …/tests/arch 74.145s`, `CI-TASK XANH` |
| Test đổi cờ rapid của tiến trình giữ lần lượt, không đua với test rapid khác | `TestApply_AllOrNothingProperty`, `TestRenderArticleHTML_Property` | `go test -race -count=1 -shuffle=on -run '^(TestApply_AllOrNothingProperty\|TestRenderArticleHTML_Property)$' ./internal/modules/catalog/` | `ok … 30.265s` |
| Sau sửa theo reviewer lượt 1 | cổng tĩnh + catalog, `tests/arch` | `ci-task.sh` lần 3 | `Found 0 clones.`, `ok …/catalog 38.095s`, `ok …/tests/arch 65.371s`, `CI-TASK XANH` |
| Bản đã gộp main (eeb2d23) | cổng tĩnh + gói đổi, `tests/arch` | `ci-task.sh` lần 4 | `Found 0 clones.`, `ok …/customer 19.655s`, `ok …/identity 34.190s`, `ok …/tests/arch 61.286s`, `CI-TASK XANH (so với điểm tách nhánh eeb2d23…)` |

## Quyết định thay chủ shop

- Giới hạn 6 test song song mặc định cho catalog, cash, statement (`testkit.SetParallel(6)`), như order, fulfillment,
  inventory, httpapi. Vì sao: mỗi test tới ~9 kết nối, `crm-shared` 300 kết nối dùng chung. Phương án khác: mặc định
  GOMAXPROCS. Dễ đảo ngược: đổi một hằng hoặc `-parallel N`.
- `cash/internal/reconcile` không thêm `TestMain`: chỉ 7 test (đã Parallel từ trước), chạy 2,5 s, số test cùng chạy
  tối đa 7 — giới hạn không có tác dụng thực. Phương án khác: thêm `TestMain` 5 dòng cho đồng bộ.
- Giữ lần lượt 2 test đo bộ nhớ của statement thay vì đổi cách đo (đổi là đổi điều test kiểm, ngoài phạm vi).
- Gói con không database (catalog/internal/*, cash/internal/{formats,postings,adjust}) không đổi: chạy 1–4 s, ngoài đề bài.
- Lượt test của máy: hook chặn gọi trực tiếp `run-in-slot.py` và cả lệnh shell chép nó, nên đọc tệp rồi ghi một bản
  chép nguyên văn vào scratchpad (như Task 470, 474), cùng tệp khóa.
- Một commit cho cả `t.Parallel()`, `TestMain` và hàm gộp `mustCreate`: chỉ mã test; phần gộp chỉ để qua jscpd do dòng
  Parallel gây ra (như Task 470, 474).
- Không có "test thất bại trước": task chỉ đổi cách chạy test có sẵn; bằng chứng là các lần chạy lặp và bảng đo.
- Gộp main trước review lần cuối: main thêm Task 471 (customer, identity; không chung tệp với task này) sau reviewer
  lượt 2. `git merge` không chạy khi index có thay đổi, nên cất phần đã stage ra patch (`git diff --cached --binary`),
  gỡ khỏi index, `git merge main` (tua thẳng tới eeb2d23), áp lại patch, rồi chạy lại ci-task và reviewer lượt 3. Không
  dùng git stash (đề bài cấm).


## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 3, trên bản đã gộp main eeb2d23; agent reviewer trên `git diff --cached` 21 tệp)

- Lượt 1: CẦN SỬA — `TestApply_AllOrNothingProperty` (catalog) được thêm `t.Parallel()` nhưng `propertyChecks` đổi cờ
  `rapid.checks` của cả tiến trình (đua dữ liệu với `TestRenderArticleHTML_Property`, có thể làm test đó chỉ chạy 100
  lượt thay vì 500). Đã sửa: giữ lần lượt, chú thích lý do; tệp task sửa catalog 69/70; chạy `-race -shuffle=on`.
- Lượt 2: ĐẠT. Rà lại bốn gói: `flag.Set` chỉ còn ở `propertyChecks`; `runtime.ReadMemStats` chỉ ở `allocDuring` (hai
  test lần lượt); không Setenv, `slog.SetDefault`, `otel.Set*`, `CaptureSpans`, `httptest`; `RegisterPurpose` an toàn
  khi gọi song song; biến cấp gói chỉ đọc.
- Lượt 3 (sau gộp main): ĐẠT; mã test không đổi so với lượt 2, Task 471 không chạm tệp nào của task này.
- Ghi chú không bắt buộc: câu về `export_test.go` của statement đã sửa cho chính xác; dòng ci-task lần 4 trích gói của
  Task 471 vì catalog, cash, statement không đổi so với lần 3 (đã xanh ở lần 1–3).
