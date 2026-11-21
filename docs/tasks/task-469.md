# Task 469 — Test gói order chạy song song, đo trước/sau

Kế hoạch: task chung của điều phối viên (21/11, chủ shop OK), số x-par1 trong `../../docs/superpowers/plans/danh-so-task-toan-cuc.md`. Loại việc (quy trình mục 3): A (chỉ mã test và tài liệu, không đổi mã chạy)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm `t.Parallel()` vào hàm Test trong các tệp
  `internal/modules/order/*_test.go` (gói `order_test` và `order`), và giới hạn `-parallel` trong `TestMain`
  (`env_test.go`). Không đổi tệp không phải `_test.go`, không đổi `internal/testkit`, không chạm `internal/httpapi`
  (Task 189 làm song song).
- Gói phụ thuộc ngược (go list): tệp `_test.go` không được gói nào nhập; không gói nào bị ảnh hưởng. Gói con
  `order/internal/{state,pricing}` không đổi.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: cả gói `./internal/modules/order/...` (đo trước 3 lần, đo sau 3 lần cùng cách đo, 5 lần liền `-race`
  đều xanh, 1 lần `-race -count=3` cho các test mới Parallel); `scripts/agent/ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit: `OpenStaff`
  mỗi test một database `crm_test_<ngẫu nhiên>` nhân bản từ mẫu, pool MaxConns 4; `SetRapidChecks` trong TestMain;
  `ForEachSeller` cố ý chạy lần lượt). Mẫu Parallel của gói `cash` (37/45 test gọi `t.Parallel()` ở dòng đầu).
- Dùng lại / viết mới vì: dùng lại `testkit` như cũ, theo đúng mẫu `cash`. Giới hạn `-parallel` viết trong `TestMain`
  của gói order (vài dòng, chỉ gói này cần vì 214 test cùng mở database); chưa đưa vào testkit vì chưa có gói thứ hai
  cần (đổi testkit kéo theo chạy toàn bộ test ở ci-task).
- jscpd (lần ci-task đầu): dòng `t.Parallel()` thêm vào làm 5 đoạn dựng dữ liệu đầu test vượt ngưỡng 50 token. Trả
  lời: gộp, vì cùng mục đích và cùng lý do thay đổi (cùng một bộ dữ liệu mẫu): `newDupPair` (duplicates_test.go, dùng ở
  5 test có đúng ba dòng dupEnv + v1 + cặp đơn), `(*orderEnv).discountOrder` (edit_test.go, 2 test ngưỡng duyệt),
  `TestCarrierReturn_TD02Receive` dùng lại `carrierCase` sẵn có (returns_qty_test.go: đúng các bước pickedUpDH01 →
  số dư → openCarrierReturn → mã dòng). Không đổi mốc nền jscpd.
- funlen: `TestTimeline_DH06` vượt 81 > 80 dòng vì thêm `t.Parallel()`; bỏ một dòng trống, không đổi lệnh nào.

## Soát thiết kế

Task chỉ đổi cách chạy test, không đổi mã chạy.

- 5.1–5.12: không áp dụng: không thêm dữ liệu, bảng, đường ghi, thư viện; test vẫn theo mẫu module (PostgreSQL thật qua testkit).
- 6.1 (hai việc cùng lúc): áp cho chính các test: mỗi test chạy song song phải có database riêng (OpenStaff/CreateDatabase)
  và không dùng trạng thái chung của tiến trình. Đã rà: không có biến cấp gói đổi được trong test (chỉ `regexp`),
  không `os.Setenv`/`t.Setenv`, không cổng cố định, không `testkit.CaptureSpans` (TracerProvider toàn cục), không advisory
  lock, không đổi role/cấu hình máy chủ; đồng hồ giả là biến cục bộ của từng test. `pg_stat_database.deadlocks` lọc theo
  `current_database()` (database riêng). `testkit.MigrateBefore` dùng goose Provider riêng, tắt registry toàn cục.
  Test con dùng chung một env (`eachSeller`, `eachM2Seller`, bảng của `TestSettings…`, `TestConfirm…KeepsHold`) giữ
  chạy lần lượt.
- 6.2–6.8, 6.10–6.13: không áp dụng: không đổi đường nghiệp vụ, tiền, quyền, việc nền, dữ liệu cá nhân.
- 6.9 (treo lâu): test chạy song song chậm hơn từng cái; giới hạn thời gian câu lệnh 30 s, chờ khóa 5 s của pool giữ
  nguyên; chạy 5 lần liền để bắt test chập chờn do tải.
- Số kết nối: mỗi test mở tối đa 8 kết nối (2 pool × 4) + 1 kết nối quản trị lúc tạo database; `crm-shared`
  max_connections 300, nhiều phiên cùng chạy → giới hạn số test song song của gói (xem Quyết định thay chủ shop).

## Đo trước/sau

Cách đo (giống nhau cho mọi lần): script trong scratchpad của phiên chạy
`go test -race -count=1 ./internal/modules/order/...` (thêm cờ ghi ở cột "Cờ"), mỗi giây lấy mẫu số kết nối TCP tới
5600 của tiến trình `order.test` (`lsof -c order.test`, lấy lớn nhất) và tổng kết nối client của máy chủ
(`pg_stat_activity`, lớn nhất; mẫu thưa nên thấp hơn cột trước), load 1 phút lúc đầu/cuối. Máy 10 nhân, crm-shared
(PostgreSQL 5600, max_connections 300) dùng chung với các phiên khác. "Bận" = load ≥ 7.

| Lần | Cờ | Thời gian | Kết nối order.test lớn nhất | Kết nối máy chủ lớn nhất | Load đầu → cuối | Kết quả |
|---|---|---|---|---|---|---|
| trước 1 | (mặc định, không Parallel) | 172 s | 16 | 9 | 8.52 → 5.48 (bận) | ok |
| trước 2 | (như trên) | 159 s | 16 | 9 | 5.04 → 3.56 (vừa) | ok |
| trước 3 | (như trên) | 158 s | 16 | 9 | 3.56 → 4.38 (vừa) | ok |
| sau, thử | `-parallel 10` | 49 s | 68 | 36 | 6.92 → 7.25 | ok |
| sau, thử | `-parallel 4` | 72 s | 28 | 15 | 7.25 → 9.93 | ok |
| sau, thử | mặc định mới (6) | 62 s | 48 | 25 | 4.01 → 7.37 | ok |
| sau 1 | mặc định mới (6) | 63 s | 48 | 25 | 8.56 → 9.48 (bận) | ok |
| sau 2 | (như trên) | 55 s | 40 | 20 | 9.48 → 8.50 (bận) | ok |
| sau 3 | (như trên) | 59 s | 42 | 22 | 8.50 → 8.30 (bận) | ok |
| sau 4 | (như trên) | 60 s | 48 | 25 | 8.30 → 8.41 (bận) | ok |
| sau 5 | (như trên) | 63 s | 42 | 22 | 8.41 → 9.75 (bận) | ok |
| sau, lặp 3 | `-count=3 ./internal/modules/order/` | 215 s (3 lượt) | 50 | 26 | 8.96 → 10.29 (bận) | ok |

Kết quả: cả gói từ ~158–172 s xuống ~55–63 s (nhanh ~2,7 lần) dù máy bận hơn lúc đo sau; kết nối lớn nhất từ 16 lên
48 (trần lý thuyết 6 × 9 = 54), dưới 1/5 max_connections 300.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (2) mọi test của gói có database riêng chạy song song | 213 hàm Test của `internal/modules/order` (mọi hàm trừ TestMain) có `t.Parallel()` ở dòng đầu; test con `TestReceivePartialReturn_DamagedMissing/*` (mỗi ca `newM2Env` riêng) cũng Parallel | `grep -c "t.Parallel()" internal/modules/order/*_test.go` | tổng 214 (213 hàm + 1 test con) |
| (2) test con dùng chung env giữ lần lượt | `eachSeller`/`testkit.ForEachSeller`, `eachM2Seller`, `TestSettings…` (kiểm sau vòng lặp), `TestConfirm…KeepsHold` | đọc mã, không đổi | không thêm Parallel |
| (3) giới hạn kết nối | `TestMain` (`env_test.go`): `-parallel` = min(6, GOMAXPROCS) khi người chạy không đặt; `-parallel 10`/`4` ghi đè được | measure với `-parallel 10`, `-parallel 4` | 68 và 28 kết nối (khác mặc định 48): cờ ghi đè có hiệu lực |
| (4) cả gói 5 lần liền -race đều xanh | cả gói | `go test -race -count=1 ./internal/modules/order/...` ×5 | sau 1–5: `ok elimaz.com.vn/crm/internal/modules/order` mỗi lần (55–63 s) |
| (4) 1 lần -race -count=3 cho test mới Parallel | cả gói order (mọi test đều mới Parallel) | `go test -race -count=3 ./internal/modules/order/` | `ok` (215 s) |
| (5) không nới tiêu chí, không bỏ test, không đổi mã chạy | diff chỉ thêm dòng `t.Parallel()` và TestMain | `git diff --cached --stat` | chỉ tệp `_test.go`, `CLAUDE.md`, tệp task |
| test sửa để qua jscpd/funlen vẫn xanh | `TestDuplicates_{CancelShipped,CancelVersion,CancelledConsumerParallel,NotDuplicate,Cancel}`, `TestEdit_DiscountApproval{,AmountUp}`, `TestCarrierReturn_TD02{Receive,Requested}`, `TestTimeline_DH06` | `go test -race -count=1 -run '^(…)$' -v ./internal/modules/order/` | 10 × `--- PASS`, `ok … order 3.222s` |
| cổng tĩnh + cả gói sau khi gộp main và sửa | cả gói order (Parallel, mặc định 6), tests/arch (lint thật) | `scripts/agent/ci-task.sh` (lần 2; lần 1 ĐỎ ở funlen, jscpd như mục Tái dùng) | `ok …/order 71.882s`, `ok …/tests/arch`, `CI-TASK XANH` |
| (6) quy ước trong CLAUDE.md | mục Lệnh, dòng "Test mới của gói lớn … `t.Parallel()`" | đọc | có |

## Quyết định thay chủ shop

- Số test song song mặc định của gói order là 6 (đặt trong `TestMain`, không đổi testkit). Vì sao: 6 cho 55–63 s với
  tối đa 48 kết nối; 10 chỉ nhanh thêm ~13 s nhưng tới 68 kết nối, trong khi nhiều phiên cùng chạy (và go test chạy
  nhiều gói cùng lúc) trên máy chủ 300 kết nối; 4 chậm hơn (72 s). Phương án khác: để mặc định GOMAXPROCS (10) như
  gói cash; hoặc đặt giới hạn chung trong testkit cho mọi gói (để task sau khi gói thứ hai cần). Dễ đảo ngược: đổi một
  hằng `orderParallel`, hoặc gọi `-parallel N`.
- Một commit cho cả việc thêm `t.Parallel()` và ba chỗ gom dữ liệu dựng test (bước 3 CLAUDE.md muốn tách cấu trúc
  và hành vi): cả hai chỉ ở tệp `_test.go`, phần gom chỉ để qua jscpd do dòng Parallel gây ra, và không đổi điều test
  kiểm. Phương án khác: hai commit (gom trước, Parallel sau), mỗi commit một lượt reviewer.
- Không thêm "test thất bại trước": task chỉ đổi cách chạy test có sẵn, không có hành vi mới để kiểm; bằng chứng là
  các lần chạy lặp và bảng đo.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lượt 1, trên bản đã gộp main 565db01).

- Phát hiện phải sửa: không có.
- Ảnh hưởng: 47 tệp chỉ thêm `t.Parallel()`; `TestMain` tôn trọng `-parallel` của người chạy (`go test -parallel N`
  thành `-test.parallel`, `flag.Visit` thấy); `newDupPair`, `discountOrder`, `TD02Receive` dùng `carrierCase` cho kết
  quả tương đương phần dựng cũ; không đổi tệp không phải test, testkit, httpapi.
- Bằng chứng: chỉ `TestMain` không có `t.Parallel()` (213 hàm + 1 test con); không có Setenv, CaptureSpans, advisory
  lock, cổng cố định; `pg_stat_database` lọc `current_database()`; test con dùng chung env vẫn lần lượt.
- Ghi chú không bắt buộc: mục Ảnh hưởng chưa nhắc hai hàm dựng dữ liệu mới (mục Tái dùng đã nêu); các gói lớn khác
  khi thêm Parallel cần tự cân số kết nối (chưa có giới hạn `-parallel` như order).
