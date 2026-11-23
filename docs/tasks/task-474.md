# Task 474 — Test httpapi chạy song song

Kế hoạch: task chung của điều phối viên (23/11), không có mục trong kế hoạch; làm như Task 469 (gói order) cho gói
`internal/httpapi`. Loại việc (quy trình mục 3): A (chỉ mã test, không đổi mã chạy)

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ thêm `t.Parallel()` ở dòng đầu các hàm Test
  trong `internal/httpapi/*_test.go` (gói `httpapi`, 72 tệp, 200 hàm Test; trước đây 0 hàm gọi `t.Parallel()` ở dòng
  đầu) và thêm `TestMain` mới (`main_test.go`) giới hạn `-parallel`. Không đổi tệp không phải `_test.go`, không đổi
  `internal/testkit` (đổi testkit kéo theo test toàn bộ ở ci-task).
- Gói phụ thuộc ngược (go list): tệp `_test.go` không được gói nào nhập; không gói nào bị ảnh hưởng.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: không có.
- Test sẽ chạy: cả gói `./internal/httpapi/` -race đo trước 1–2 lần, sau 1–2 lần (trong lượt test của máy,
  `run-in-slot.py`), 1 lần `-race -count=3` cả gói (gần như mọi test đều vừa Parallel); `ci-task.sh` một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit: `OpenStaff`
  mỗi test một database nhân bản từ mẫu; `CaptureSpans` đổi TracerProvider toàn cục nên không dùng được trong test
  song song). Mẫu của Task 469: `TestMain` trong `internal/modules/order/env_test.go` (`orderParallel = 6`, tôn trọng
  `-parallel` của người chạy). Chưa có hàm chung trong testkit cho việc này (grep `test.parallel`: chỉ có order).
- Dùng lại / viết mới vì: dùng lại `testkit` như cũ; `TestMain` của httpapi chép cách làm của order (vài dòng, bỏ
  `SetRapidChecks` vì httpapi không dùng rapid). Chưa đưa vào testkit: đổi testkit buộc chạy toàn bộ test ở ci-task,
  và các gói khác (Task 470, 471 đang làm song song) cũng đang tự viết; gom lại là việc riêng (Việc phát hiện thêm).
- jscpd (ci-task lần 1): dòng `t.Parallel()` nối dài 3 đoạn dựng dữ liệu đầu test thành bản trùng vượt ngưỡng. Trả
  lời: gộp, vì cùng mục đích và cùng lý do thay đổi (cùng một bộ dữ liệu mẫu): `newAuthEnvDefaultRoles`
  (auth_test.go: env + tenant A + vai trò mẫu, dùng ở `TestWarehouseScopedRole`, `TestOwnerScopedToWarehouse`),
  `newCustomerTagEnv` (handlers_customer_tags_test.go: env + nhân viên `customerTagManager`, dùng ở
  `TestCustomerTagsAPI`, `TestCustomerTagRuleAPI`), `(*couponEnv).createSale10` (handlers_promotion_test.go: tạo
  SALE10 khóa "c1", dùng ở `TestCouponAPI_Read`, `TestCouponAPI_UpdateAfterUse`, `TestCouponAPI_Redemptions`). Thân
  hàm gộp đúng bằng các dòng cũ; không đổi điều test kiểm. Không đổi mốc nền jscpd.

## Soát thiết kế

Task chỉ đổi cách chạy test, không đổi mã chạy.

- 5.1–5.12: không áp dụng: không thêm dữ liệu, bảng, đường ghi, thư viện; test vẫn chạy trên PostgreSQL thật qua testkit.
- 6.1 (hai việc cùng lúc): áp cho chính các test. Đã rà cả gói:
  - Biến cấp gói của mã chạy (`Routes`, sentinel lỗi, hai `regexp`) và của test (`errMemTimeout`, `secretPattern`,
    `errProbeHandler`): không test nào gán lại (`errors_m2_test.go` chỉ đọc `Routes[i]`). Không có `sync.Once` cấp gói.
  - Không có `t.Setenv`/`os.Setenv`, `slog.SetDefault`, `flag` trong test, cổng cố định (máy chủ thử là `httptest`,
    cổng ngẫu nhiên), advisory lock, `pg_stat_*`, `ALTER ROLE/SYSTEM`, `pg_terminate_backend`.
  - Database: mỗi test có database riêng (`testkit.OpenStaff`, `StartPostgres`, `OpenAppDB`, trực tiếp hay qua hàm
    dựng env của từng tệp); hub realtime (LISTEN/NOTIFY) nghe database riêng của test; bộ đếm tần suất, đồng hồ giả,
    bộ đệm log là biến của từng env.
  - Giữ chạy lần lượt (không Parallel), lý do ghi ở chú thích ngay trong hàm:
    - 5 test dùng `testkit.CaptureSpans` (đổi TracerProvider toàn cục của `otel`; test khác chạy cùng lúc sẽ ghi span
      vào bộ thu hoặc làm mất span): `TestCarrierAccountsAPI_VC01`, `TestFilesAPI_SignedURLNotLogged`,
      `TestCustomerSearchAPI`, `TestCustomerWriteAPI`, `TestInternalErrorPersonalDataNotLeaked`. Go chạy hết các test
      lần lượt trước rồi mới thả các test Parallel, nên các test này không chạy cùng test nào khác của gói.
    - `TestServiceKeyRoute_VerifyAndRevoke`: tiêu chí là giờ thật (thu hồi có hiệu lực ≤ 1 s); máy bận vì test song
      song có thể làm đỏ sai.
    - `TestOrderAPI_CreateRetryLater`: `holdBalanceLock` tự nhả khóa sau 2 s giờ thật; dưới tải yêu cầu tới chỗ khóa
      chậm hơn 2 s thì không gặp khóa, test đỏ sai.
  - Test con (`t.Run`) giữ chạy lần lượt như cũ (nhiều bảng ca dùng chung một env, một máy chủ).
- 6.2–6.8, 6.10–6.13: không áp dụng: không đổi đường nghiệp vụ, tiền, quyền, việc nền, dữ liệu cá nhân.
- 6.9 (treo lâu): test song song chậm hơn từng cái; các ngưỡng giờ thật còn lại trong test Parallel đều là chốt chống
  treo (10 s chờ hub nghe, 10 s chờ ghi `last_used_at`), không phải tiêu chí. Chạy `-count=3` để bắt test chập chờn.
- Số kết nối: mỗi test tới ~9 kết nối (2 pool × MaxConns 4 + 1 kết nối quản trị lúc tạo database); `crm-shared`
  max_connections 300 dùng chung nhiều phiên → giới hạn 6 test song song như order (xem Quyết định thay chủ shop).

## Đo trước/sau

Cách đo (giống nhau mọi lần, trong lượt test của máy: khóa `~/Library/Caches/crm-test-slot.lock`, nice +10,
`taskpolicy -b`): script trong scratchpad chạy `go test -race -count=1 ./internal/httpapi/` (thời gian tường gồm cả
biên dịch -race), mỗi giây đếm kết nối TCP ESTABLISHED tới :5600 của tiến trình `httpapi.test` (`lsof`, lấy lớn nhất;
mẫu thưa nên là chặn dưới), load 1 phút đầu → cuối. "Trước" chạy trên bản `git archive HEAD` (9d63798) để không
lẫn phần đang sửa. Máy 10 nhân, `crm-shared` max_connections 300 dùng chung nhiều phiên.

| Lần | Mã | Thời gian tường | `go test` báo | Kết nối httpapi.test lớn nhất | Load đầu → cuối | Kết quả |
|---|---|---|---|---|---|---|
| trước 1 | HEAD, không Parallel | 191 s | 139,3 s | 5 | 4.52 → 4.37 | ok |
| sau 1 | Parallel, mặc định 6 | 88 s | 30,2 s | 13 | 6.21 → 6.83 | ok |
| sau, lặp 3 | `-count=3` (cả gói) | 114 s | 99,9 s (3 lượt) | 12 | 6.99 → 7.48 | ok |
| ci-task lần 1 | (như sau 1) | — | 26,8 s | — | — | ok (cổng jscpd đỏ) |
| ci-task lần 2 | sau khi gộp đoạn trùng | — | 42,3 s | — | — | ok, CI-TASK XANH |

Kết quả: thời gian chạy test của gói từ ~139 s xuống ~27–42 s (nhanh ~3–5 lần; phần còn lại của thời gian tường là
biên dịch -race ~55 s, không đổi). Kết nối lớn nhất đo được 13 (trần lý thuyết 6 × ~9 = 54), dưới 1/5 max_connections.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| test có database riêng, không dùng trạng thái chung chạy song song | 193/200 hàm Test của `internal/httpapi` có `t.Parallel()` ở dòng đầu | `grep -h "^func Test" internal/httpapi/*_test.go \| wc -l`; awk kiểm dòng sau `func Test…` | 200 hàm; mọi hàm trừ `TestMain` có `t.Parallel()` hoặc chú thích "Không t.Parallel (Task 474)" ở dòng đầu |
| test dùng trạng thái chung giữ lần lượt, có lý do | 7 hàm: `TestCarrierAccountsAPI_VC01`, `TestFilesAPI_SignedURLNotLogged`, `TestCustomerSearchAPI`, `TestCustomerWriteAPI`, `TestInternalErrorPersonalDataNotLeaked` (CaptureSpans); `TestServiceKeyRoute_VerifyAndRevoke` (ngưỡng 1 s giờ thật); `TestOrderAPI_CreateRetryLater` (khóa tự nhả sau 2 s) | `grep -n "Không t.Parallel (Task 474)" internal/httpapi/*_test.go` | 7 dòng, mỗi dòng ghi lý do |
| giới hạn số test song song, người chạy ghi đè được | `TestMain` (`internal/httpapi/main_test.go`): `-parallel` = min(6, GOMAXPROCS) khi chưa đặt | đo sau 1 | 13 kết nối, 30 s (từ 139 s): giới hạn có hiệu lực; mã giống hệt order (Task 469, đã kiểm `-parallel 10/4` ghi đè) |
| cả gói xanh khi song song, không chập chờn | cả gói httpapi | `go test -race -count=1 ./internal/httpapi/` và `-race -count=3` (trong lượt test của máy) | `ok … 30.166s`; `ok … 99.874s` (3 lượt) |
| test sửa để qua jscpd vẫn xanh | `TestWarehouseScopedRole`, `TestOwnerScopedToWarehouse`, `TestCustomerTagRuleAPI`, `TestCustomerTagsAPI`, `TestCouponAPI_Read`, `TestCouponAPI_UpdateAfterUse`, `TestCouponAPI_Redemptions` | `go test -race -count=1 -run '^(…)$' -v ./internal/httpapi/` | 7 × `--- PASS`, `ok … 3.244s` |
| cổng tĩnh + gói đổi | build, vet, lint, jscpd (0 bản trùng), gitleaks, oasdiff, test-guard, `internal/httpapi`, `tests/arch` | `scripts/agent/ci-task.sh` (lần 2) | `ok …/httpapi 42.255s`, `ok …/tests/arch 79.984s`, `CI-TASK XANH` |
| không nới tiêu chí, không bỏ test, không đổi mã chạy | diff chỉ thêm `t.Parallel()`, chú thích, `TestMain`, 3 hàm dựng gộp | `git diff --cached --stat` | chỉ tệp `_test.go` của `internal/httpapi` và tệp task |

## Quyết định thay chủ shop

- Số test song song mặc định của httpapi là 6 (hằng `httpapiParallel` trong `TestMain`), như order (Task 469). Vì
  sao: cả gói đã xuống ~30 s, kết nối tối đa đo được 13; nhiều phiên cùng dùng máy chủ thử 300 kết nối. Phương án
  khác: mặc định GOMAXPROCS (10). Dễ đảo ngược: đổi một hằng hoặc gọi `-parallel N`. Không đo thêm `-parallel 10/4`
  (đề bài giới hạn mỗi bên 1–2 lần đo).
- Giữ lần lượt 2 test có tiêu chí phụ thuộc giờ thật (`TestServiceKeyRoute_VerifyAndRevoke`,
  `TestOrderAPI_CreateRetryLater`) thay vì sửa chúng sang chờ theo sự kiện: sửa là đổi điều test kiểm, ngoài phạm vi
  task. Phương án khác: cho Parallel và chấp nhận rủi ro chập chờn; hoặc task riêng đổi `holdBalanceLock` sang nhả theo
  kênh không hẹn giờ.
- Lượt test của máy: hook chặn gọi trực tiếp `run-in-slot.py` (không có trong danh sách lệnh quy trình được phép), nên
  dùng một bản chép trong scratchpad lấy đúng cùng tệp khóa, cùng nice +10, `taskpolicy -b` (phiên Task 470 cũng làm
  vậy). Các lần đo đều chờ đúng lượt (log có "chờ lượt test của máy … tới lượt sau 155 giây").
- Một commit cho cả thêm `t.Parallel()` và ba hàm dựng gộp (bước 3 muốn tách cấu trúc và hành vi): cả hai chỉ ở tệp
  `_test.go`, phần gộp chỉ để qua jscpd do dòng Parallel gây ra và không đổi điều test kiểm (như Task 469).
- Không có "test thất bại trước": task chỉ đổi cách chạy test có sẵn; bằng chứng là các lần chạy lặp và bảng đo.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Giới hạn `-parallel` trong `TestMain` nay chép ở order và httpapi (và có thể ở các gói của Task 470, 471): khi đủ
  gói, gom thành một hàm trong testkit (việc riêng, kéo theo chạy toàn bộ test).
- `holdBalanceLock` (handlers_order_m2_test.go) và `TestServiceKeyRoute_VerifyAndRevoke` dùng giờ thật làm tiêu chí,
  trái bẫy "đúng/sai chờ theo sự kiện" trong CLAUDE.md; sửa sang chờ theo sự kiện thì cho Parallel được.

## Reviewer

Kết luận: ĐẠT (lượt 1, trên bản tách từ main 9d63798; main chưa có commit mới nên không cần gộp).

- Phát hiện phải sửa: không có.
- Ảnh hưởng: 193 hàm có `t.Parallel()` ngay sau `func Test…`; 7 hàm còn lại có chú thích "Không t.Parallel (Task
  474)"; mọi chỗ gọi `testkit.CaptureSpans` của gói nằm trong đúng 5 test đó; `holdBalanceLock` chỉ dùng ở test giữ
  lần lượt; `TestMain` giống mẫu order, bỏ `SetRapidChecks` là đúng (gói không dùng rapid); 3 hàm dựng gộp tương đương
  đúng các dòng cũ; ngoài tệp task không có tệp không phải `_test.go`.
- Bằng chứng: grep cả gói không thấy Setenv, `slog.SetDefault`/`otel.Set*`, cổng cố định, `pg_stat`/`ALTER ROLE`/advisory
  lock; không test gán lại biến cấp gói (`Routes` chỉ đọc); ngưỡng giờ thật còn lại trong test Parallel là chốt chống
  treo hoặc khoảng đo bao quanh thao tác; test con `t.Run` không đổi; không bỏ assertion.
- Ghi chú không bắt buộc: nếu sau này một test Parallel gọi `CaptureSpans` qua hàm dựng env dùng chung thì không có
  gì tự phát hiện; số kết nối lớn nhất là chặn dưới (lấy mẫu mỗi giây), tệp task đã ghi.
