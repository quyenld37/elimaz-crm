# Task 317 — Test kiến trúc bỏ node_modules; test nhạy thời gian chờ có điều kiện (M0-18b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi tệp `_test.go` (`tests/arch`,
  `internal/platform/db`, `internal/platform/queue`, `internal/platform/commands`, `internal/imager`) và `go.mod`
  (thêm chỉ thị `ignore node_modules`, bỏ thư mục tên đó ở mọi tầng, Go ≥ 1.25; module đang ở go 1.27); xóa
  `console/go.mod` (Quyết định 1).
- Gói phụ thuộc ngược (go list): không gói nào nhập gói test. Chỉ thị `ignore` đổi tập gói của mọi mẫu `./...`
  (`go build/vet/test`, `golangci-lint`, `govulncheck`, `go list` trong `tests/arch`, hook Stop): bỏ gói Go lạc trong
  `console/node_modules` (ví dụ `flatted/golang/pkg/flatted`); tập gói thật không đổi (32 gói trước và sau trên 6c4cced; 33 sau khi gộp `main` 75f8e58, không gói nào trong
  `node_modules` hay `console`).
- Operation OpenAPI bị chạm, oasdiff: không.
- Chỗ dùng trên màn hình: không (console chỉ bị bỏ khỏi phần quét Go; `npm` không đọc `go.mod`).
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: `go test -race ./tests/arch/`, `./internal/platform/db/`, `./internal/platform/queue/`,
  `./internal/platform/commands/` (vùng rủi ro cao: cả bộ), `./internal/imager/`; lặp `-count=20` bốn test nhạy
  thời gian dưới tải (4 gói chạy song song cùng lúc); `make ci`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có hàm chờ có
  điều kiện cho test (mã chung chỉ là mã sản phẩm); `tests/arch/scan_test.go` đã có `skipDir` (bỏ thư mục ẩn, gồm
  `.worktrees`, và `node_modules`) dùng chung cho `parseGoFiles`; `lint_test.go` có vòng quét riêng cũng bỏ
  `node_modules`. Lỗi chỉ nằm ở các lệnh chạy mẫu `./...` (golangci-lint, `go list`), không ở vòng quét.
- Dùng lại / viết mới vì: dùng lại `skipDir`, `parseGoFiles`, `golangciLint`, `goListPackages`, `holdRow` (tách
  phần nhả khóa thành hàm trả về để test nhả theo điều kiện). Không viết gói chờ chung: mỗi test chỉ cần một vòng
  poll vài dòng, và mã chung phải nằm trong `internal/platform/<việc>` cho mã sản phẩm, không cho test.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.10, 5.12: không áp dụng: task chỉ đổi test và phạm vi quét của công cụ Go, không thêm dữ liệu, đường ghi,
  module hay lời gọi ra ngoài.
- 5.11: không thêm thư viện; chỉ thị `ignore` của `go.mod` là tính năng của bộ công cụ Go đang dùng (1.27).
- 6.1–6.13: không áp dụng cho mã sản phẩm (không đổi). Riêng phần test: không nới ngưỡng nghiệp vụ nào
  (`RetryTx` vẫn tối đa 4 lần, `IMAGER_STEP_TIMEOUT=1s` → trả lời ≤ 2 s, hạn ghi kết quả tính từ lúc handler xong);
  chỉ thay chờ theo đồng hồ bằng chờ theo sự kiện (khóa nhả khi `RetryTx` báo lần thử lại đầu; đo 2 s từ lúc lệnh treo
  thật sự chạy) và nới hạn chờ dọn dẹp của test (poll tới khi đạt).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước: module tạm dùng `go.mod`, `.golangci.yml` thật, tệp Go sai lint ở `console/node_modules/x`, `console/node_modules/x/y`, `internal/foo/node_modules/z`, `.worktrees/…` | `TestPackagePatternSkipsStrayDirs` | `go test -count=1 -run StrayDirs ./tests/arch/` (trước khi sửa `go.mod`) | FAIL: `go list ./... = [elimaz.com.vn/crm/clean …/console/node_modules/x …/console/node_modules/x/y …/internal/foo/node_modules/z]`, golangci-lint báo 6 lỗi trong `node_modules` |
| Sau khi thêm `ignore node_modules` vào `go.mod`: chỉ còn gói sạch, 0 lỗi lint; vòng quét `parseGoFiles` chỉ thấy `clean/clean.go` | `TestPackagePatternSkipsStrayDirs`, `TestParseGoFilesSkipsStrayDirs` | như trên | `ok … tests/arch 1.592s` |
| Cây thật có `console/node_modules/x/bad.go` (tạo tạm, xóa sau): `go list`, `go build`, `go vet`, cả bộ test kiến trúc | `TestLintCleanOnRealCode` và cả gói | `go list ./... \| grep -c node_modules; go build ./... && go vet ./... && go test -race -count=1 ./tests/arch/` | `0`; `ok … tests/arch 21.592s` |
| Tập gói thật không đổi | — | `go list ./... \| wc -l` | 32 trước và sau |
| Bốn test nhạy thời gian, bản cũ (HEAD 6c4cced, `git archive`), 4 gói song song + 10 tiến trình `yes`, load 37 | 4 test | `go test -race -count=20 -run '^<Test>$' <gói>` ×4 song song | ĐỎ: db 5/20 ("hàm được gọi 1 lần, muốn 2 hoặc 3"), queue 6/20 ("commit transaction: timeout: context deadline exceeded"), commands 9/20 (ghi in_doubt hết hạn); imager 20/20 qua lần này |
| Bản mới, cùng cách chạy, 20 tiến trình `yes`, load 43 | `TestRetryTxLockTimeoutRecoversWhenLockReleased`, `TestRecordAfterLongHandler`, `TestRecordAfterLongExecute`, `TestImager_HangKilledWithinDeadline` | như trên | `ok … db 69.3s`, `ok … queue 81.0s`, `ok … commands 80.9s`, `ok … imager 22.9s` (80/80) |
| Toàn bộ cổng sau khi gộp `main` 75f8e58, có `console/node_modules/flatted/golang/pkg` thật (npm install của `console-check`), không còn `console/go.mod` | `make ci` | `make ci` | exit 0: 27 gói `ok`, "No vulnerabilities found.", "Found 0 clones.", "no leaks found" |
| Cả bộ gói đổi (sau khi gộp `main` 75f8e58) | cả gói | `go test -race -count=1 ./internal/platform/db/... ./internal/platform/queue/... ./internal/platform/commands/... ./internal/imager/... ./tests/arch/...`; `golangci-lint run ./...` | 5 gói `ok`; `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Bước 1 không cần sửa vòng quét của test kiến trúc: `skipDir` (`scan_test.go`) và vòng quét của
  `TestNoErrorStringCompareInRealCode` đã bỏ `node_modules` và thư mục ẩn (`.worktrees`). Lỗi nằm ở mẫu `./...`
  (golangci-lint, `go list` của test ranh giới, và cả `go build/vet/test` của `make ci`, hook Stop); sửa bằng chỉ thị
  `ignore node_modules` trong `go.mod`. Mã sinh: golangci-lint đã bỏ qua (`exclusions.generated: strict`); vòng quét
  `parseGoFiles` **cố ý** vẫn đọc mã sinh (luật cấm simple protocol áp cho mọi nơi; `findDynSQL` đã tự bỏ
  `internal/store`), nên không thêm luật bỏ mã sinh ở đó.
- Test thứ tư là `commands.TestRecordAfterLongExecute` (ghi ở `task-16.md`, "Việc phát hiện thêm"), cùng kiểu với
  `queue.TestRecordAfterLongHandler`.
- Hai test hạn ghi: thứ đỏ là hạn ghi 50 ms của chính test (`SetRecordTimeout`), không phải ngưỡng nghiệp vụ (5 s).
  Đổi thành 1 s, handler/Execute chạy quá hạn 200 ms (hạn tính từ lúc nhận việc thì ctx ghi chắc chắn đã hết, máy nhanh
  hay chậm như nhau). Giấc ngủ còn lại là kịch bản "handler chạy lâu", không phải chờ kết quả. `TestRecordAfterLongExecute`
  đặt hạn lệnh 5 s (hạn 1 s của `newCmd` sẽ cắt Execute 1,2 s).

## Quyết định thay chủ shop

1. **Một cách duy nhất để Go bỏ `node_modules`: `ignore node_modules` trong `go.mod` gốc; xóa `console/go.mod`**
   (bản vá nhanh `aa981b3` trên `main`, module giả `console-ignore`). Vì sao: CLAUDE.md "chuyển sang cách mới thì xóa
   cách cũ"; chỉ thị `ignore` (Go ≥ 1.25) bỏ `node_modules` ở mọi tầng, không cần module giả, và test
   `TestPackagePatternSkipsStrayDirs` khóa nó. Phương án khác: giữ cả hai (thừa); chỉ giữ `console/go.mod` (không phủ
   `node_modules` ở chỗ khác, không có test). Đảo lại dễ: khôi phục tệp từ `aa981b3`.
2. **Imager: đo 2 s từ giờ sửa của pidfile** (lúc lệnh treo thật sự khởi động) thay vì từ đầu `Process`. Ngưỡng
   "≤ 2 s" của kế hoạch M1-42 giữ nguyên; chỉ bỏ phần tải nguồn trước đó khỏi phép đo. Hạn chờ dọn nhóm tiến trình
   (`assertGroupGone`) 1 s → 10 s, poll 20 ms, thoát ngay khi nhóm mất.
3. **db: nhả khóa trong `OnRetry` lần 1** (đồng bộ) thay vì hẹn giờ 100 ms; test chặt hơn: đúng 2 lần gọi, một lần thử
   lại 55P03. `TestRetryTxLockTimeoutRetriesThenGivesUp` giữ khóa tới hết test thay vì 2 s. `holdRow` → `lockRow`.

## Việc phát hiện thêm

(không có)

## Reviewer

Lượt 1: **ĐẠT**, không phát hiện về tính đúng. Đã kiểm: bốn test sửa vẫn bắt lỗi gốc (hạn ghi tính từ lúc nhận việc →
ctx hết 0,2 s trước lúc ghi; RetryTx không thử lại 55P03 → lỗi; lệnh treo không bị giết → chờ `sleep 60`; sót tiến trình
con → nhóm sống ~60 s > hạn 10 s); không race (`OnRetry` gọi đồng bộ trong vòng lặp `RetryTx`, trước backoff); `ignore`
không làm mất gói thật; ngưỡng nghiệp vụ (`recordTimeout` 5 s, 4 lần chạy, ≤ 2 s) giữ nguyên. Ghi chú đã làm: xóa comment
sót của `holdRow`; mục Ảnh hưởng ghi đúng `ignore node_modules` (mọi tầng) và 33 gói sau khi gộp. Ghi chú không làm:
`TestPackagePatternSkipsStrayDirs` không có ca đối chứng lint trên gói thường (bằng chứng đỏ trước khi sửa thay cho nó;
phần `go list` vẫn khóa chỉ thị dù luật lint đổi).

Lượt 2 (sau khi xóa comment, cập nhật tệp task): **ĐẠT**, không phát hiện mới; xác nhận không còn chỗ dùng `holdRow`,
`console-ignore`, `console/go.mod`; hạn được nới chỉ là hạn của test (`SetRecordTimeout`, `assertGroupGone`).

Kết luận: ĐẠT
