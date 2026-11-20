# Task 429 — Test nhập Excel sản phẩm chờ cả lượt chạy lại hẹn giờ (M1-x12)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 429, M1-x12). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): **thêm** `(*testkit.Staff).DrainJobs` (`internal/testkit/queue.go`,
  mới, 0 chỗ dùng trước đó; 2 chỗ dùng sau task) cùng hàm riêng `makeJobsDue`, hằng `drainRounds`. Đổi hàm test không xuất `(*importEnv).drain`
  của gói `catalog_test` (`imports_test.go`; 5 chỗ gọi có sẵn: `validate`, `applyNow`, `TestApply_TwoImportsSameSKUConcurrently`
  ở `imports_apply_test.go`, `TestApply_SameFileTwiceEndToEnd`, `TestImport_SourceMissingFails`) và của gói
  `customer_test` (`importer_test.go`; 1 chỗ gọi có sẵn: `run`); hai hàm này chỉ còn gọi `e.staff.DrainJobs`; thêm hai test hồi quy `TestApply_DrainRunsScheduledRetry`, `TestImport_DrainRunsScheduledRetry`.
- Gói phụ thuộc ngược (go list): `internal/testkit` được nhiều gói test nhập → `ci-task.sh` chạy toàn bộ test (luật:
  đổi testkit). `testkit` nhập thêm `internal/platform/queue`: đã kiểm không vòng nhập (không test nội bộ `package queue`
  hay của các gói queue phụ thuộc — db, tenancy, metrics, logging, observability — nhập testkit).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không. Test dùng câu `UPDATE platform.jobs SET run_at = now()` bằng pool chủ bảng
  (`platform.jobs` không có RLS — migration 0007 chỉ GRANT, không POLICY; giống `makeDue` của `internal/platform/files`). Câu SQL thô nằm trong `internal/testkit` — chỗ arch test `findDynSQL`
  miễn (hạ tầng test).
- Test sẽ chạy: `go test -race ./internal/modules/catalog/... ./internal/modules/customer/...`; ba test của task lặp
  ≥ 50 lần dưới tải (6 tiến trình nice +10, dừng bằng PID); `ci-task.sh` của quy trình.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có hàm testkit
  nào chạy worker tới hết việc; grep `run_at = now()` trong test: `files.(*procEnv).makeDue`, helper `ownerExec` của
  `queue`/`commands`, `events/emit_test.go` — đều là hàm test riêng gói, không xuất được sang gói khác.
- Dùng lại / viết mới vì: lúc đầu viết hai hàm riêng gói; jscpd báo 2 đoạn trùng mới giữa catalog và customer (cổng
  `--fail-on-new-clones=0`). Hai đoạn cùng mục đích **và** cùng lý do thay đổi (ngữ nghĩa hẹn chạy lại của queue) →
  gộp thành `(*testkit.Staff).DrainJobs`. Không làm gói `queue/queuetest` kiểu `files/filestest`: arch test cấm
  `Exec` ngoài `*_dynsql.go` trừ `_test.go` và `internal/testkit`; không thêm câu sqlc chỉ dùng cho test vào store chạy
  thật. `files.(*procEnv).makeDue` giữ nguyên (cần `SKIP LOCKED`, gọi tường minh giữa các bước của test).

## Soát thiết kế

- 5.1–5.12: không áp dụng: không đổi mã chạy thật, dữ liệu, module, đường ghi, quyền hay thư viện; chỉ sửa hàm test.
- 6.x (đồng thời, hàng đợi): đây chính là nội dung task. Gốc lỗi: `drain()` dừng khi `RunOnce` trả 0 (không còn việc
  **đến hạn**), nhưng việc áp/lệnh bị dời (lỗi tạm, hết ngân sách thời gian) được queue hẹn `run_at = now() + chờ`
  nên bản nhập còn `applying`/`validating`. Sửa: hết việc đến hạn thì đưa `run_at` của việc `pending` hẹn sau về
  `now()` và chạy tiếp; dừng khi không còn việc hẹn; trần 50 vòng rồi `Fatal`. Không nới tiêu chí: việc vẫn đi đúng số
  lượt và đúng chính sách (lượt 3 hết ngân sách → failed `apply_too_long` vẫn làm test đỏ; hết 8 lượt → dead → failed),
  không thử lại cả test. Worker chạy đồng bộ trong test nên không có dòng bị khóa khi `UPDATE` (khác `files.makeDue`
  cần `SKIP LOCKED` vì worker chạy song song).
- Quét `drain()` cùng kiểu: `internal/platform/files` đã có `makeDue` gọi tường minh và nhiều test của nó cố ý kiểm
  việc còn `pending` sau lỗi → không đổi. `customer` import: cùng lỗi tiềm ẩn (lệnh lỗi tạm bị hẹn lại) → sửa cùng cách.

## Quyết định thay chủ shop

- Chọn "đẩy `run_at` của việc hẹn về hiện tại, có trần vòng" thay vì đồng hồ giả: queue tính `run_at` bằng `now()` của
  PostgreSQL, không có chỗ cắm đồng hồ trong worker; thêm chỗ cắm là đổi mã chạy thật (task chỉ cho sửa test). Đảo
  ngược: bỏ nhánh `&& s.makeJobsDue(t) == 0` trong `DrainJobs`.
- Gộp vào `internal/testkit` (vùng rủi ro cao) thay vì nới mốc nền `jscpd-baseline.json`: mốc nền chưa từng đổi từ
  Task đầu; gộp đúng luật "cùng mục đích, cùng lý do thay đổi". Giá: `ci-task.sh` chạy toàn bộ test.
- Không đổi `drain()` của `order/duplicates_test.go`, `fulfillment/env_test.go`: kế hoạch chỉ nêu customer import và
  files; fulfillment có test cố ý kiểm việc hẹn chạy lại. Ghi ở "Việc phát hiện thêm".

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `drain()` catalog chạy cả việc áp bị hẹn chạy lại (gốc lỗi, tái hiện xác định không cần tải) | `TestApply_DrainRunsScheduledRetry` | `go test -race -count=1 -run TestApply_DrainRunsScheduledRetry ./internal/modules/catalog/` với `drain()` cũ, rồi mới | cũ: `FAIL … imports_apply_test.go:568: applying [], mong applied`; mới: `ok … 2.665s` |
| `drain()` customer chạy cả lệnh nhập bị hẹn chạy lại | `TestImport_DrainRunsScheduledRetry` | như trên với `./internal/modules/customer/` | cũ: `FAIL … Status:pending … mong done 1 dòng`; mới: `ok … 3.906s` |
| Ba test chạy lặp ≥ 50 lần khi có tải đều qua (đo trên bản hai hàm riêng gói trước khi gộp vào testkit; logic giống hệt; bản gộp được kiểm lại bằng các test hồi quy và toàn bộ test trong ci-task) | `TestApply_ThreePriceChanges`, `TestApply_200Models`, `TestApply_SameFileTwiceEndToEnd` | binary `go test -c -race`, `-test.count=13 -test.parallel 6` × 4 lượt + `-test.count=3` × 1 lượt; mỗi lượt kèm 6 tiến trình bận CPU `nice -n 10`, dừng bằng PID (trap), lượt ≤ 10 phút | `ThreePriceChanges PASS=53 FAIL=0`, `200Models PASS=53 FAIL=0`, `SameFileTwiceEndToEnd PASS=52 FAIL=0` (lượt 1, 2 chạm trần `-test.timeout 570s` khi còn vài lần đang chạy — các lần dở không tính) |
| Gói đổi không vỡ test khác (đưa về hạn không làm vòng lặp với việc khác; số đo trên bản hai hàm riêng gói, logic giống hệt bản gộp) | cả gói | `go test -race -count=1 ./internal/modules/catalog/... ./internal/modules/customer/...` | `ok …/catalog 152.081s`, `ok …/customer 149.839s` |
| Sau khi gộp: hồi quy và test chính vẫn qua | `TestApply_DrainRunsScheduledRetry`, `TestImport_DrainRunsScheduledRetry`, `TestImport_RowErrorsAndReimport`, `TestApply_ThreePriceChanges` | `go test -race -count=1 -run 'DrainRunsScheduledRetry\|TestImport_RowErrorsAndReimport\|TestApply_ThreePriceChanges' ./internal/modules/catalog/ ./internal/modules/customer/` | `ok …/catalog 11.800s`, `ok …/customer 2.420s` |
| Cổng kiểm của task | — | `ci-task.sh` lần 1 (bản hai hàm riêng) | ĐỎ chỉ ở jscpd (2 clone mới); mọi test khác `ok` |
| Cổng kiểm của task | — | `ci-task.sh` lần 2 và 3 (bản gộp testkit) | cổng tĩnh xanh hết: build, vet, lint, catalog-check, **jscpd** (hết 2 clone), gitleaks, oasdiff, test-guard. Phần "test toàn bộ" (do đổi testkit) bị công cụ dừng ở trần 1 giờ rồi 2 giờ (máy bị nhiều phiên dùng chung, lint một lần mất 920 s); lần 3 đã chạy 24 gói, gồm `ok …/modules/catalog 111.897s`, `ok …/modules/customer 105.126s`; 1 đỏ chập chờn `httpapi` (xem Việc phát hiện thêm) |
| Phần còn lại của test toàn bộ | 49 gói chưa chạy tới ở lần 3 | `go test -race -count=1 <49 gói>` (danh sách = `go list ./...` trừ các gói lần 3 đã báo) | 41 `ok` (gồm `testkit`, `platform/queue`, `platform/files`); 1 đỏ `observability` › `TestRuleDeadNoPayload` → chạy riêng `-count=10` trên nhánh này: 0 FAIL, trên `main` cùng commit: 0 FAIL (chập chờn, không liên quan) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Dưới tải của máy này (6 tiến trình nice +10 trên 10 lõi), mã cũ **không** đỏ trong 38 lần chạy (9,5 phút) — tải
  nice +10 không đủ đẩy lần áp quá ngân sách 25 s. Vì vậy gốc lỗi được chứng minh bằng hai test hồi quy xác định (việc
  bị hẹn chạy lại 1 giờ → `drain()` cũ dừng, bản nhập kẹt `applying`/`pending`), còn phép lặp 50 lần dưới tải chứng
  minh bản mới không đỏ, chưa chứng minh được bản cũ đỏ trên máy này.
- Thêm hai test hồi quy (kế hoạch không ghi) để giữ hành vi của `drain()`.
- "ci-task.sh xanh" chưa có trong **một** lần chạy liền mạch: cổng tĩnh xanh; phần test toàn bộ chạy thành hai đợt
  (lần 3 bị dừng ở trần 2 giờ + đợt chạy tiếp 49 gói còn lại), tổng cộng đủ mọi gói, chỉ có 2 test chập chờn không
  liên quan (chạy riêng đều qua).

## Việc phát hiện thêm

- `internal/httpapi` › `TestOrderRead_EditCancelTimeline` đỏ một lần trong lúc chạy toàn bộ test dưới tải (mục
  `status` đứng trước mục `edit` trên dòng thời gian, `at` 01:54:09 so với 01:39:11); chạy riêng `-count=3` qua cả 3.
  Không liên quan task (`httpapi` không dùng `DrainJobs` hay hàm test đã đổi). Gợi ý: một task riêng soát thứ tự
  sắp xếp/mốc giờ của timeline đơn.
- `internal/platform/observability` › `TestRuleDeadNoPayload` đỏ một lần trong lần chạy gói (`key "dead:msg.send" có =
  true, muốn false` ở bước "đã báo rồi thì không báo lại"); chạy riêng 10 lần trên nhánh và 10 lần trên `main` đều qua.
  Có vẻ so mốc `alertSent(…, 0)` với `finished_at` sát nhau theo giây. Gợi ý task riêng.
- Test toàn bộ không xong trong 2 giờ trên máy dùng chung: điều phối viên chạy lại trên `main` sau khi gộp.

- `order/duplicates_test.go` `(*dupEnv).drain` và `fulfillment/env_test.go` `(*shipEnv).drain` cùng dạng "dừng khi
  hết việc đến hạn"; chưa thấy đỏ. Nếu đỏ khi máy bận thì sửa cùng cách, nhưng giữ các test cố ý kiểm việc hẹn lại.

## Reviewer

Kết luận: ĐẠT (vòng 1, trên phần đã stage sau khi gộp main 8bdebd2).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp hai chiều: `DrainJobs` mới có 2 chỗ dùng; drain catalog đúng 5 chỗ gọi cũ, customer 1 chỗ; không vòng
  nhập; SQL thô trong testkit được arch test miễn; `platform.jobs` không RLS.
- Bằng chứng: hai test hồi quy kiểm đúng gốc lỗi; không test nào cố ý đòi việc còn `pending` sau drain (các test kiểm
  việc hẹn lại gọi `Handle`/`Execute` trực tiếp); không test bị xóa, tắt hay nới — trần vòng còn chặt hơn (trước dừng
  im lặng sau 20 vòng, nay `Fatal` sau 50).
- Ghi chú không bắt buộc: "ci-task xanh" chưa có trong một lần chạy liền mạch (đã nêu ở "Khác kế hoạch"), điều phối
  viên nên chạy lại toàn bộ test trên `main` sau khi gộp; `makeJobsDue` đổi mọi việc pending nhưng mỗi việc chỉ đếm
  một lần nên không lặp mãi.
