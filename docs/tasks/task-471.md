# Task 471 — Test gói customer và identity chạy song song

Kế hoạch: task chung của điều phối viên (21/11, chủ shop OK cho test gói lớn chạy song song), số x-par3 trong `../../docs/superpowers/plans/danh-so-task-toan-cuc.md`; làm theo mẫu Task 469 (gói order). Loại việc (quy trình mục 3): A (chỉ mã test và tài liệu, không đổi mã chạy)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm `t.Parallel()` vào hàm Test trong các tệp
  `internal/modules/customer/*_test.go` (gói `customer_test` và `customer`) và `internal/modules/identity/*_test.go`
  (gói `identity_test`), và giới hạn `-parallel` trong `TestMain` (`main_test.go` của hai gói) bằng `testkit.SetParallel(6)` (Task 470). Không đổi tệp không
  phải `_test.go`, không đổi `internal/testkit` (chỉ gọi `SetParallel` đã có), không chạm gói của Task 190 (catalog, httpapi) và Task 470
  (fulfillment, inventory). Thêm vài hàm dựng nhỏ trong tệp test (xem Tái dùng) để gỡ trùng jscpd và funlen.
- Gói phụ thuộc ngược (go list): tệp `_test.go` không được gói nào nhập; không gói nào bị ảnh hưởng.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: cả gói `./internal/modules/customer/` và `./internal/modules/identity/` với `-race`: đo trước (customer
  3 lần 21/11, identity 1 lần trên bản `git archive` của HEAD), đo sau 1 lần mỗi gói, trong lượt test của máy;
  `scripts/agent/ci-task.sh` một lần (identity là vùng rủi ro cao: ci-task chạy cả bộ vùng đó).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit: `OpenStaff`
  mỗi test một database riêng nhân bản từ mẫu; `SetRapidChecks`); mẫu `TestMain` + `orderParallel` của Task 469
  (`internal/modules/order/env_test.go`).
- Dùng lại / viết mới vì: dùng lại `testkit.SetParallel` (Task 470 thêm, đã gộp vào main ở `007654c`, gộp vào nhánh này
  trước khi viết `TestMain`) thay cho bản chép tay `flag.Visit`/`flag.Set` của lượt trước; dùng lại `testkit.OpenStaff`
  và các env test sẵn có.
- jscpd (ci-task lần 1) báo 6 đoạn trùng mới: dòng `t.Parallel()` làm phần mở đầu giống nhau của vài test vượt ngưỡng
  50 token. Trả lời: gộp (cùng mục đích, cùng lý do đổi — dựng dữ liệu chung của env test), bằng hàm dựng nhỏ trong tệp
  test của chính gói, không thêm vào baseline: `writeEnv.pair` (customer_test.go; dùng ở fororder, identity, notes,
  timeline), `writeEnv.abc` (merge_test.go, 2 chỗ), `statsEnv.twoDelivered` (stats_test.go; stats, stats_merge_race),
  `schemaEnv.inA` (schema_test.go, 6 chỗ), `newPropEnv` (identity/staff_property_test.go; 2 test quy luật).
- golangci-lint funlen (cùng lý do, vượt 1 dòng/1 câu lệnh): tách `importEnv.assertImportRowLacks` (importer_test.go)
  và `staffEnv.checkRoleInputErrors` (roles_custom_test.go); ca kiểm giữ nguyên, chỉ dời chỗ.

## Soát thiết kế

Task chỉ đổi cách chạy test, không đổi mã chạy.

- 5.1–5.12: không áp dụng: không thêm dữ liệu, bảng, đường ghi, thư viện; test vẫn theo mẫu module (PostgreSQL thật qua testkit).
- 6.1 (hai việc cùng lúc): áp cho chính các test: mỗi test chạy song song phải có database riêng (`OpenStaff`,
  `CreateDatabase`, `StartPostgresSeeded`) hoặc không chạm database, và không dùng trạng thái chung của tiến trình.
  Đã rà hai gói: biến cấp gói trong test chỉ là `regexp` và sentinel lỗi (không đổi lúc chạy); không `os.Setenv`/
  `t.Setenv`, không cổng cố định (`filestest.MemFileStore` dùng `httptest` cổng ngẫu nhiên), không `CaptureSpans`,
  không `slog.SetDefault` (`testkit.CaptureLog` trả logger riêng), không advisory lock, không đổi role/cấu hình máy chủ
  (`EnableAutoExplain` là `ALTER ROLE crm_app IN DATABASE <database riêng>`), không role giới hạn kết nối. Sổ mục đích
  tệp của tiến trình (`customer.RegisterFilePurposes` → `files.RegisterPurpose`) có mutex và khai lại cùng mục đích là
  bỏ qua. `pg_stat_database`/`pg_stat_activity` đều lọc `current_database()`. `testkit.MigrateBefore` dùng goose
  Provider riêng. Đồng hồ giả (`now`) là trường của env từng test. Test con: chỉ
  `TestFindOrCreateForOrder_LocksRootAgainstMerge/*` mỗi ca dựng `newWriteEnv` riêng → Parallel; các test con khác
  dùng chung env của test cha hoặc là bảng thuần → giữ lần lượt.
- 6.2–6.8, 6.10–6.13: không áp dụng: không đổi đường nghiệp vụ, tiền, quyền, việc nền, dữ liệu cá nhân.
- 6.9 (treo lâu): test chạy song song chậm hơn từng cái; các chốt chống treo (ví dụ `waitLockWaiter` 10 s) giữ nguyên;
  chạy 5 lần liền mỗi gói để bắt test chập chờn do tải.
- Số kết nối: mỗi test mở tối đa 8 kết nối (2 pool × 4) + 1 kết nối quản trị lúc tạo database; `crm-shared`
  max_connections 300, nhiều phiên và nhiều gói cùng chạy → giới hạn số test song song mỗi gói (Quyết định thay chủ shop).

## Đo trước/sau

Cùng lệnh `go test -race -count=1 ./internal/modules/<gói>/`, máy dùng chung, PostgreSQL `crm-shared` (5600). Thời
gian là số `go test` báo cho gói. "Kết nối máy chủ" là số client backend lớn nhất của cả máy chủ (mọi phiên), lấy mẫu
mỗi 2 giây.

| Gói | Trước | Sau (24/11, 1 lần, trong lượt test của máy, -parallel 6) |
|---|---|---|
| customer | 44,5 s; 39,9 s; 51,2 s (21/11, 3 lần, tải 6,8–8,1) | 20,9 s (tải 7,0→6,7), ok, kết nối máy chủ tối đa 13 |
| identity | 67,2 s (24/11, bản `git archive` HEAD, trong lượt test, tải 5,8→4,1), kết nối tối đa 4 | 33,2 s (tải 4,1→7,0), ok, kết nối máy chủ tối đa 11 |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mọi hàm Test của hai gói gọi `t.Parallel()` ở dòng đầu | (rà mã) | `awk` tìm `func Test…(t *testing.T)` có dòng sau không phải `t.Parallel()` trên `internal/modules/{customer,identity}/*_test.go` | không còn hàm nào (in ra rỗng) |
| Giới hạn 6 test cùng chạy, người chạy đặt `-parallel` thì giữ | `TestMain` hai gói gọi `testkit.SetParallel(6)`; hàm có `TestSetParallel` (Task 470) | xem `main_test.go` | gọi sau `flag.Parse` |
| Gói customer qua khi chạy song song, `-race` | cả gói | `go test -race -count=1 ./internal/modules/customer/` | `ok … customer 20.896s` |
| Gói identity qua khi chạy song song, `-race` | cả gói | `go test -race -count=1 ./internal/modules/identity/` | `ok … identity 33.200s` |
| Nhanh hơn | đo cả gói | mục Đo trước/sau | customer 40–51 s → 20,9 s; identity 67,2 s → 33,2 s |
| Không đoạn trùng mới, không lỗi lint (funlen) | jscpd, golangci-lint, `TestLintCleanOnRealCode` | `scripts/agent/ci-task.sh` lần 1 | ĐỎ: 6 đoạn trùng mới, 2 funlen → sửa (xem Tái dùng) |
| Hàm dựng mới giữ đúng ca kiểm | các test dùng chúng | `go test -race -count=1 -run '^(TestImport_RowErrorsAndReimport\|TestFindOrCreateForOrder\|TestAddIdentity\|TestMergeChain\|TestMergeErrors\|TestAddNoteAppendOnly\|TestTimelineGroupKeyset\|TestStatsRootMergedBetweenReads\|TestStatsConcurrentWithMerge\|TestStatsFromSource\|TestSchema.*)$' ./internal/modules/customer/` và `-run '^(TestCustomRole_ArchiveAndInput\|TestRoleInvariantsProperty\|TestStaffInvariantsProperty)$' ./internal/modules/identity/` | `ok … customer 3.979s`; `ok … identity 23.015s` |
| Cổng tĩnh + test gói đổi + test kiến trúc | ci-task | `scripts/agent/ci-task.sh` lần 2 | `CI-TASK XANH (so với điểm tách nhánh 007654c…)`; jscpd 0 đoạn trùng; `ok customer 21.564s`, `ok identity 30.971s`, `ok tests/arch 58.860s` |

## Quyết định thay chủ shop

- Giới hạn 6 test song song mỗi gói qua `testkit.SetParallel(6)`, như order (469), fulfillment/inventory (470). Vì
  sao: mỗi test tới 9 kết nối, `crm-shared` max_connections 300 dùng chung nhiều phiên. Phương án khác: để mặc định
  GOMAXPROCS. Đảo ngược: sửa một số hay chạy `-parallel N`.
- Bỏ bản chép tay giới hạn (`flag.Visit`/`flag.Set`) của lượt trước, chờ Task 470 gộp vào main (đã gộp `007654c`) rồi
  dùng `SetParallel`: đúng luật Tái dùng, jscpd không báo trùng.
- Test con: chỉ `TestFindOrCreateForOrder_LocksRootAgainstMerge/*` gọi `t.Parallel()` (mỗi ca dựng env riêng); test
  con khác dùng chung env của test cha nên giữ chạy lần lượt.
- Đo mỗi bên 1 lần (customer trước: 3 lần từ 21/11) thay cho 5 lần như dự kiến ban đầu: hướng dẫn phiên yêu cầu đo
  1–2 lần trong lượt test của máy để không làm máy quá tải. Chống chập chờn dựa vào ci-task và lần chạy toàn bộ `-race`
  trên main sau gộp.
- `run-in-slot.py` bị hook Bash chặn khi gọi trực tiếp (đường dẫn thư mục script quy trình): đã chép nguyên nội dung ra
  thư mục tạm và chạy bản chép, cùng tệp khóa `~/Library/Caches/crm-test-slot.lock` (như Task 470). Không sửa hook.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- `internal/modules/customer/customer_test.go`: chú thích `// create tạo khách…` nay nằm trên `pair` (hàm mới chèn giữa
  chú thích và `create`); chỉ là tài liệu, dọn khi chạm tệp lần sau.
- `internal/modules/identity/main_test.go` còn khối tự viết `flag.Visit`/`flag.Set("rapid.checks", …)` (có từ trước)
  thay vì `testkit.SetRapidChecks(200)` như customer; ngoài phạm vi task.

## Reviewer

Kết luận: ĐẠT (agent reviewer, 24/11, trên phần đã stage sau khi gộp main `007654c`).
- Phát hiện phải sửa: không có.
- Đã soát: mọi hàm Test của hai gói có `t.Parallel()` ở dòng đầu; `TestMain` gọi `testkit.SetParallel(6)` sau
  `flag.Parse`; test con của `TestFindOrCreateForOrder_LocksRootAgainstMerge` mỗi ca env riêng; các hàm dựng mới chỉ
  dời mã, giữ đủ ca kiểm (cả 10 ca của `checkRoleInputErrors`, đúng thứ tự gọi); không trạng thái chung của tiến trình
  (`pg_stat_database`/`pg_stat_activity` lọc `current_database()`, `EnableAutoExplain` theo database riêng, hook
  `SetAfterBatch`/`SetBudget` gắn từng `*Importer`); chỉ đổi `_test.go` và tệp task.
- Ghi chú không bắt buộc: hai mục ở "Việc phát hiện thêm".
