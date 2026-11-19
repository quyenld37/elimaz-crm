# Task 409 — Ảnh: lỗi tạm của imager không thành rejected vĩnh viễn; ghi trạng thái processing (M1-r4)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (task bù review độc lập M1, số toàn cục 409,
M1-r4). Phạm vi: C-02, C-07 của `docs/review-doc-lap-m1.md`, làm đúng mục "Quyết định cho Task 406, 409, 410, 411"
(19/11) cuối tệp đó. Loại việc (quy trình mục 3): A

## Cách làm (tóm tắt)

- **Phân loại lỗi** của lệnh `file.process` / `file.process_private`:
  - *Lỗi do chính tệp* (imager trả `not_image`, `type_not_allowed`, `too_large`, `too_many_pixels`, `decode_failed`;
    ảnh thô không còn `source_missing`; quá cỡ khi băm): `rejected` ngay như cũ.
  - *Lỗi hạ tầng tạm* (imager bận 503/chờ chỗ trống, không tới được, quá giờ, mã HTTP khác 200, lý do tạm `timeout`,
    `upload_failed`, `source_failed`, `internal`; kết quả imager lạ; lỗi kho, ký, database): `not_sent` **không tính
    lượt** (`commands.Outcome.KeepAttempt`), hẹn lại theo lùi dần theo tuổi (5 s → tối đa 1 giờ, ±20%), tới **trần thời
    gian** `ProcessorOptions.RetryMax` (biến `IMAGE_RETRY_MAX_HOURS`, mặc định 24 giờ, 1–168). Hết trần → tệp
    `rejected processing_failed`, lệnh `dead` → luật cảnh báo `dead:file.process` có sẵn kêu, đếm `image_rejected`.
- **Trần tính từ** lúc tệp vào `processing` (`updated_at` của dòng khi đang `processing`), không sớm hơn lúc ghi lệnh.
- **3 lượt** (`MaxAttempts = 3`) vẫn giữ cho phần còn tính lượt: lệnh in_doubt (worker chết giữa chừng) được tra lại và
  nhận lại.
- **`processing` được ghi thật** (C-07): đầu `Execute`, trong transaction đọc ngắn khóa dòng tệp, `uploaded → processing`
  và phát báo hiệu `files.file`. Tệp `lost` đang dựng lại giữ `lost`.
- **Chạy lại lệnh dead cứu được ảnh** (đuôi C-02): tệp `rejected` với lý do `processing_failed` (hết trần, hết lượt) khi
  lệnh được chạy lại → về `processing`, xử lý lại, trần tính lại từ đầu. Tệp bị từ chối vì nội dung vẫn `rejected`.
- **Hạn sống của lệnh** (`ExpiresAfter` 1 giờ) bỏ cho hai loại lệnh ảnh (trần do handler giữ, cấu hình được); lệnh
  đã xếp trước giữ `expires_at` cũ, hết hạn → `expired` → `OnTerminal` như trước.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep):
  - `commands.Outcome` thêm trường `KeepAttempt bool` (thêm, không phá): 42 chỗ dựng `commands.Outcome{` ngoài test
    không phải sửa (giá trị 0 = hành vi cũ). `Runner.notSent` đọc trường mới.
  - `files.ProcessKind()`, `files.ProcessPrivateKind()`: `ExpiresAfter` 1 giờ → 0; chỗ dùng: `upload.go` (ApplyUpload,
    RegisterPrivateImage), `verify.go` (enqueueRestore), `process.go` (Kind), test `process_test.go`,
    `process_private_test.go`.
  - `files.ProcessorOptions` thêm `RetryMax time.Duration`; hằng mới `files.DefaultRetryMax`. Chỗ dựng:
    `cmd/crm/main.go` (2 chỗ: verify không cần, process đặt `RetryMax`), `process_helpers_test.go`.
  - `config.Config` thêm `ImageRetryMax time.Duration` (vai trò worker, `IMAGE_RETRY_MAX_HOURS`).
- Gói phụ thuộc ngược (go list): `platform/files` ← `cmd/crm`, `internal/httpapi`, `modules/catalog`,
  `platform/files/filestest`, `internal/testkit`. `platform/commands` (vùng rủi ro cao) ← `cmd/crm`, `modules/catalog`,
  `modules/fulfillment`, `modules/inventory`, `platform/files`, `platform/observability/alertjobs`,
  `platform/selftest`. `platform/config` ← `cmd/crm` và các gói đọc cấu hình.
- Operation OpenAPI bị chạm, oasdiff: không (enum `processing` của trạng thái tệp đã có ở `openapi.yaml`).
- Chỗ dùng trên màn hình: không đổi mã; `useUpload.ts` đã coi mọi trạng thái khác `ready`/`rejected` là "đang xử lý".
- Câu sqlc, bảng, pha migration: không migration (CHECK của `platform.files.status` đã có `processing`; quyền UPDATE
  `status`, `reject_reason`, `updated_at`, `version` đã cấp cho `crm_app`). Câu mới `SetFileProcessing`
  (`queries/platform/files.sql`); câu đổi `RescheduleNotSent` (`queries/platform/commands.sql`, thêm tham số
  `keep_attempt`). Bảng `platform.files`, `platform.jobs`. `make gen` sinh lại `internal/platform/internal/store`.
- Test sẽ chạy: `go test -race ./internal/platform/commands/...` (cả bộ, vùng rủi ro cao), `./internal/platform/files/...`,
  `./internal/platform/config/...`, `./cmd/crm/...`, rồi `scripts/agent/ci-task.sh` (gói phụ thuộc ngược).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `queue.NextDelay` (lịch theo số lượt, ở đây lượt
  không tăng nên không dùng được làm lùi dần), `db.Backoff` (20 ms cho chạy lại transaction, sai thang), `jobs.sql`
  `ReleaseJob` (đã có mẫu `attempts = attempts - 1` "không tính lượt" khi Stop trước Execute — dùng cùng cách trong
  `RescheduleNotSent`), `Outcome.RetryAfter` (dùng để truyền thời gian lùi), luật cảnh báo `terminalRule` "dead" (dùng
  làm cảnh báo hết trần, không viết luật mới), `markRejected`/`reject` (dùng lại cho hết trần), `LockFile`.
- Dùng lại / viết mới vì: viết mới `retryDelay` (vài dòng, riêng của lệnh ảnh: lùi theo tuổi của việc xử lý vì lượt
  không tăng) trong `files`; viết mới `SetFileProcessing` (chưa có câu nào ghi `processing`). Không gói mới.

## Soát thiết kế

Mục 5:
- 5.1, 5.2 chủ dữ liệu: `platform.files` thuộc `platform/files`; `platform.jobs` thuộc `queue`/`commands`. Files không
  ghi `jobs`: việc trả lượt nằm trong `commands.Runner` (câu `RescheduleNotSent`), files chỉ báo `KeepAttempt`.
- 5.3: không đọc bảng module khác.
- 5.4, 5.5: ghi `processing` là một transaction ngắn riêng trước khi gọi imager (Execute ngoài transaction, Q22);
  kết quả vẫn ghi ở `OnDone`/`markRejected` như cũ.
- 5.6 gọi ra ngoài: vẫn là lệnh `commands`; `not_sent` chỉ trả khi chắc imager chưa nhận hay chạy lại an toàn
  (`AutoRetryNotSent`, đầu ra không ghi đè — đã có). `KeepAttempt` chỉ áp cho `not_sent`.
- 5.7: mọi câu chạm `platform.files` trong `tenancy.WithTenant` hoặc transaction tenant của Runner.
- 5.8: không nhập `net/http` mới. 5.9: không thêm đường ghi HTTP. 5.10: không Valkey. 5.11: không thư viện mới.
- 5.12: theo mẫu lệnh gọi ra ngoài (Task 12/42/43): handler trả Outcome, hook ghi trong transaction của Runner.

Mục 6:
- 6.1 đồng thời: `SetFileProcessing` chạy sau `LockFile` (FOR UPDATE) và có điều kiện trạng thái; `SetFileReady`,
  `SetFileRejected` đã nhận `processing`. Hai lệnh cùng tệp: dòng khóa tuần tự, lệnh sau thấy `processing`/`ready`.
  `RescheduleNotSent` vẫn có điều kiện `running AND locked_by` (mất giữ chỗ → 0 dòng).
- 6.2 gửi lại: dedupe lệnh như cũ; chạy lại Execute an toàn (biến thể không ghi đè, bản kê).
- 6.3 thứ tự khóa: load khóa một dòng `platform.files` rồi commit; không khóa bảng khác trong transaction đó.
  Runner: hook trước, `jobs` cuối (ADR-46) — không đổi.
- 6.4 lỗi tạm database: `tenancy.WithTenant` có RetryTx; lỗi còn lại → `not_sent` không tính lượt.
- 6.5 không áp dụng: không đổi mức cách ly.
- 6.6 việc nền lỗi: lỗi hạ tầng không tính lượt nhưng có trần thời gian (mặc định 24 giờ) rồi `dead` + cảnh báo; lỗi
  vĩnh viễn (nội dung) `dead` ngay. Lùi dần tới 1 giờ (thay cho lịch 5 s → 6 giờ theo lượt, vì lượt không tăng).
- 6.7 quá tải: `Retry-After` của imager là giá trị tối thiểu của thời gian chờ.
- 6.8 tiến trình chết: lease hết → in_doubt → tra lại (Lookup luôn not_sent) → về hàng chờ, có tính lượt (3).
  Tệp đang `processing` khi worker chết không kẹt: lệnh vẫn còn; hết 3 lượt → OnTerminal → `rejected processing_failed`.
- 6.9 treo: Execute 30 s như cũ; transaction ghi `processing` ngắn (chờ khóa 5 s theo cấu hình pool).
- 6.10 tiền: không áp dụng.
- 6.11 quy luật: "lỗi hạ tầng không bao giờ làm tệp `rejected` trước trần; lỗi nội dung luôn `rejected`" có test bảng
  theo từng loại lỗi (không phải vùng rủi ro cao của files; commands có test riêng cho `KeepAttempt`).
- 6.12 dữ liệu cá nhân: `last_error` chỉ mang lý do cố định; không thêm log.
- 6.13 quyền: không đổi (báo hiệu vẫn `Requires files.upload`).

## Bằng chứng

Lệnh chung: `go test -race -count=1 -timeout 20m ./internal/platform/files/... ./internal/platform/commands/...
./internal/platform/config/...` → `ok …/files 520.686s`, `ok …/commands 233.225s`, `ok …/config 3.115s`; rồi
`scripts/agent/ci-task.sh` (kết quả ở dòng cuối bảng).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Lỗi hạ tầng không tính lượt, tệp `processing`, quá 3 vòng vẫn chờ: `upload_failed`, `source_failed`, `internal`, `timeout` của imager, HTTP 500, bận 503, treo (quá giờ), imager tắt (đóng kết nối), và lệnh riêng tư | `TestFileProcess_InfraErrorsNotCounted` (9 ca con, mỗi ca 5 vòng: `pending`, `attempts = 0`, `processing`, `last_error` đúng lý do) | lệnh chung | ok |
| Lỗi do chính tệp `rejected` ngay, 1 lượt, imager 1 lần: `not_image`, `type_not_allowed`, `too_large`, `too_many_pixels`, `decode_failed`; lý do lạ → `processing_failed`; ảnh thô mất → `source_missing` | `TestFileProcess_FileErrorsRejectAtOnce` (7 ca con) | lệnh chung | ok |
| Imager ngừng lâu (6 vòng) rồi chạy lại → mọi ảnh `ready`, không ảnh nào `rejected` (C-02) | `TestFileProcess_ImagerDownThenUp` | lệnh chung | ok |
| Trần thời gian: tính từ lúc vào `processing` (lệnh cũ 48 giờ vẫn được thử); lùi 5 s ±20% ở lần đầu, ~tuổi khi 20 phút, lần cuối đúng lúc hết trần; hết trần → `rejected processing_failed`, lệnh `dead` (luật `dead:file.process` có sẵn cảnh báo), `image_rejected{processing_failed}` = 1 | `TestFileProcess_RetryCapRejects` | lệnh chung | ok |
| Hết trần khi lần chạy cuối đã dùng hết hạn Execute (imager treo) vẫn ghi được `rejected` (ctx riêng) | `TestFileProcess_HangDoesNotBlockWorker` (trần 3 s; worker vẫn xong 20 selftest + 5 ảnh) | lệnh chung | ok |
| Hết trần mà không ghi được `rejected` (database lỗi mãi): lần thử lại **tính lượt** (1, 2…) để 3 lượt chặn vòng lặp; database hồi lại → `rejected processing_failed` (phát hiện của reviewer lượt 1) | `TestFileProcess_RetryCapCountsWhenRejectFails` (báo hiệu giả trả lỗi làm transaction `markRejected` lùi lại) | `go test -race -count=1 -run 'TestFileProcess_RetryCap\|TestFileProcess_HangDoesNotBlockWorker\|TestFileProcess_RetryDeadRescues\|TestProcessPrivate_LookerMissingSourceAndHang\|TestRetryDelay' ./internal/platform/files/` | `ok … 13.250s` (trước khi sửa: FAIL `Attempts:0`) |
| Transaction ghi `processing` lùi lại không đặt lại mốc trần: lệnh đã quá trần, báo hiệu lỗi từ đầu → lượt 1, 2 có tính, tệp vẫn `uploaded` (phát hiện của reviewer lượt 2) | `TestFileProcess_RetryCapNotResetByRolledBackProcessing` | `go test -race -count=1 -timeout 20m ./internal/platform/files/...` (cả gói, sau sửa lượt 2) | `ok elimaz.com.vn/crm/internal/platform/files 132.281s`; golangci-lint gói files: 0 issues |
| Chạy lại lệnh dead do hết trần (`queue.RetryJob`) cứu được ảnh; ảnh hỏng nội dung chạy lại vẫn `rejected`, không gọi imager lần hai | `TestFileProcess_RetryDeadRescues` | lệnh chung | ok |
| `processing` được ghi thật trong lúc imager xử lý (C-07), có báo hiệu cho mỗi lần đổi trạng thái | `TestFileProcess_WritesProcessing`, `TestFileProcess_ReadyWithTwelveVariants` (2 báo hiệu), `TestFileProcess_RejectedNotImage` (2 báo hiệu) | lệnh chung | ok |
| Thời gian chờ: theo tuổi trong [5 s, 1 giờ] ±20%, không dưới gợi ý, không quá phần còn tới trần, ≥ 1 s | `TestRetryDelay` (7 ca) | lệnh chung | ok |
| Quy luật (rapid, 100 lần): ảnh riêng tư không ra kho công khai; lỗi hạ tầng không bao giờ `rejected`, lỗi nội dung không kẹt `processing` | `TestProcessPrivate_PropertyNeverPublic` | `go test -race -count=1 -run TestProcessPrivate_PropertyNeverPublic ./internal/platform/files/ -v` | `[rapid] OK, passed 100 tests`, PASS 90.48s |
| `KeepAttempt`: trả lượt (kể cả lần thứ MaxAttempts), hẹn theo `RetryAfter`; không tự gửi lại → dead; quá hạn sống → expired | `TestNotSentKeepAttempt` (4 ca con) | lệnh chung | ok |
| `IMAGE_RETRY_MAX_HOURS`: mặc định 24 giờ, 48 → 48 giờ, 0 / 169 / chữ → lỗi | `TestLoadWorkerImageRetryMax` | lệnh chung | ok |
| Cổng của task | — | `scripts/agent/ci-task.sh` (lần 2, sau khi sửa lint/jscpd của lần 1) | build, vet, lint, gen-check, sqlc-vet, catalog-check, jscpd, gitleaks, oasdiff, test-guard: xanh. Test: `ok` files 220.5s, commands 126.7s, config, cmd/crm, httpapi 466.4s, queue 394.8s, order 516.2s, fulfillment, customer, cash, tenancy, db, idempotency, webhooks, queries, tests/arch. Đỏ do database dùng chung `crm-shared` quá tải (nhiều phiên cùng chạy): identity, promotion (hết 10 phút), inventory `TestReconcile_LargeTenant` (statement timeout 57014), catalog `TestApply_*` (việc transactional quá hạn 30 s một lần), observability `TestDeepHealth`, e2e `TestPipelineUnderTwoSecondsOneTrace` (quá 2 s). Không gói nào trong số này dùng mã đã đổi trên đường bị lỗi; gói đỏ khác nhau giữa lần 1 và lần 2 (lần 1 queue, order đỏ; lần 2 xanh) |
| Cổng của task, lần 3 (sau sửa của reviewer lượt 1) | — | `scripts/agent/ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh 212aa08…)`, exit 0: mọi cổng tĩnh và mọi gói test xanh (files 141.7s, commands 42.5s, queue 121.1s, httpapi 132.9s, config, cmd/crm…) |
| Chạy riêng các test đỏ của lần 2 (không phải hết giờ) | `TestDeepHealth`, `TestPipelineUnderTwoSecondsOneTrace`, `TestApply_200Models`, `TestApply_SameFileTwiceEndToEnd`, `TestApply_ThreePriceChanges`, `TestReconcile_LargeTenant` | `go test -race -count=1 -run … ./internal/platform/observability/ ./tests/e2e/ ./internal/modules/catalog/ ./internal/modules/inventory/` | `ok` observability 2.463s, e2e 7.415s, catalog 103.713s, inventory 32.071s |

## Test bị xóa hoặc tắt

Không test nào bị xóa hay tắt. Kỳ vọng đổi theo chính sách mới (quyết định 19/11), không nới:
- `TestFileProcess_TransientReasonsRetry`: lỗi tạm sau 1 vòng `attempts` 1 → 0, tệp `uploaded` → `processing`.
- `TestFileProcess_HangDoesNotBlockWorker`: treo trước đây `dead` sau 3 lượt; nay không tính lượt nên `dead` khi hết
  trần (đặt `RetryMax` 3 s), `attempts` 3 → 1; tệp vẫn `rejected processing_failed`, worker vẫn làm xong việc khác.
- `TestProcessPrivate_LookerMissingSourceAndHang`: treo sau 4 vòng giờ `pending`/0 lượt/`processing` (kiểm thêm), rồi
  lùi mốc quá trần → `dead`, `attempts` 1 (trước là 3), tệp `rejected processing_failed`.
- `TestProcessPrivate_PropertyNeverPublic`: thêm điều kiện (lỗi hạ tầng → `processing`, không `rejected`); hạn một lần
  chạy 200 ms → 500 ms (load nay có thêm một transaction ghi `processing`, ảnh tốt dưới `-race` có lúc quá 200 ms);
  cuối mỗi vòng đặt lệnh còn chờ thành `expired` (lệnh lỗi hạ tầng giờ chờ tới 24 giờ, không dọn thì mọi vòng sau
  chạy lại chúng, thời gian tăng theo bình phương).
- `TestPublicVariants`: hạn một lần chạy 200 ms → 500 ms (cùng lý do; lần chạy đầu lỗi chập chờn ở 200 ms).
- `TestFileProcess_ReadyWithTwelveVariants`, `TestFileProcess_RejectedNotImage`: 1 báo hiệu → 2 (thêm `processing`).

## Khác kế hoạch

- Đổi cấu trúc (tách `Execute` thành `execute`/`process`, `load` thành `load`/`check`) và đổi hành vi nằm chung một
  commit: phần tách chỉ để chèn `retryLater` và ghi `processing` trong cùng transaction khóa dòng, không có ý nghĩa
  riêng; hook chặn commit tạm chưa review nên không tách rẻ được.
- `docs/threat-model-m1.md` dòng D cập nhật chính sách (3 lần → trần thời gian).

## Quyết định thay chủ shop

1. **Cơ chế "không tính lượt"** đặt ở `commands` (trường `Outcome.KeepAttempt`, câu `RescheduleNotSent` trả lại lượt
   như `ReleaseJob` đã làm) thay vì nâng `MaxAttempts` của lệnh ảnh lên rất lớn. Vì: đúng chữ quyết định ("không tính
   vào 3 lượt"), giữ 3 lượt cho lần chạy in_doubt (worker chết giữa chừng); giá trị 0 = hành vi cũ nên không lệnh nào
   khác đổi. Khác: `MaxAttempts` lớn + trần thời gian, không chạm `commands` (vùng rủi ro cao) nhưng mất giới hạn 3 cho
   vòng in_doubt. Đảo ngược: bỏ `KeepAttempt` ở `retryLater`.
2. **Trần 24 giờ do handler giữ**, cấu hình bằng `IMAGE_RETRY_MAX_HOURS` (1–168, mặc định 24), không dùng
   `ExpiresAfter` của loại (giá trị đó cố định lúc xếp lệnh ở nhiều chỗ, không đọc được cấu hình). `ExpiresAfter` 1 giờ
   bỏ cho hai loại lệnh ảnh. Không đổi `docker/` (mặc định đủ; Task 410 sửa compose).
3. **Mốc tính trần** là lúc tệp vào `processing` (không sớm hơn lúc ghi lệnh): worker ngừng 2 ngày không làm ảnh bị từ
   chối ngay ở lần thử đầu; chạy lại lệnh dead được cửa sổ mới. Khác: tính từ `created_at` của lệnh.
4. **Lùi dần theo tuổi** (5 s → tối đa 1 giờ, ±20%; không dưới `Retry-After`/30 s khi quá giờ; lần cuối đúng lúc hết
   trần): ~35 lần gọi trong 24 giờ khi imager tắt hẳn. Khác: lịch 5 s → 6 giờ theo lượt (không dùng được vì lượt không
   tăng).
5. **Phân loại**: mọi `not_sent` của lệnh ảnh là lỗi hạ tầng (kể cả `timeout` do imager báo — quyết định liệt kê "hết
   giờ" là lỗi tạm; ảnh độc gây treo bị chặn bởi trần và lùi dần). Ảnh làm tiến trình con chết đã được imager báo
   `decode_failed` (nội dung) nên vẫn `rejected` ngay.
6. **Chạy lại lệnh dead cứu được ảnh** (đuôi của C-02, review đề xuất): tệp `rejected processing_failed` quay về
   `processing` khi lệnh được chạy lại; lý do nội dung thì không. Khác: không cứu, nhân viên tải lại ảnh. Đảo ngược: bỏ
   nhánh `retryableReject`.
7. **Cảnh báo khi hết trần** dùng luật `dead:<kind>` có sẵn (lệnh `dead` sau khi ghi `rejected`) cộng chỉ số
   `image_rejected{processing_failed}`; không thêm luật mới.

## Việc phát hiện thêm

- Ảnh riêng tư: ảnh thô ở `crm-tmp` tự xóa sau 24 giờ (R10). Nếu đặt `IMAGE_RETRY_MAX_HOURS` > 24, lần thử sau 24 giờ
  ra `rejected source_missing` thay cho `processing_failed` (vẫn không cứu được, chỉ khác lý do). Ghi để Task 410 (xác
  minh xóa crm-tmp sau 24 giờ) biết; không đổi trong task này.
- `file.regenerate` (`regenerate.go`) vẫn trả `not_sent` có tính lượt khi lỗi kho/database; ngoài phạm vi C-02.

## Reviewer

Lượt 1: **CẦN SỬA** — `retryLater`: quá trần mà không ghi được `rejected` (chưa nạp được tệp, database lỗi) vẫn bật
`KeepAttempt` nên lệnh chờ mãi, không bao giờ `dead`/cảnh báo (hạn sống đã bỏ). Đã sửa: quá trần thì không bật
`KeepAttempt` (lượt được tính, 3 lượt chặn), test `TestFileProcess_RetryCapCountsWhenRejectFails`. Ghi chú không bắt
buộc (giữ nguyên, chấp nhận): lý do lạ của imager được ghi `processing_failed` nên chạy lại lệnh sẽ gọi imager thêm một
lần; `elapsed` so giờ máy ứng dụng với `now()` của database (lệch vài giây).

Lượt 2: **CẦN SỬA** — `load`: khi transaction ghi `processing` lùi lại (báo hiệu `pg_notify` hay commit lỗi), `jb`
vẫn mang `Status = processing` và `updated_at` chưa commit, nên `retryLater` coi như vừa vào processing, trần bị đặt lại
mỗi lần, lệnh chờ mãi. Đã sửa: chỉ gán trạng thái và mốc `processing` vào `jb` sau khi transaction commit; lỗi thì giữ
dòng đã đọc. Test `TestFileProcess_RetryCapNotResetByRolledBackProcessing` (đỏ trước khi sửa: `Attempts:0`). Ghi chú
không bắt buộc, giữ nguyên: `markRejected` đếm `image_rejected` trước commit (mã có từ trước Task 409); test lượt 1
chưa đi tới lượt 3 → `dead` (khi báo hiệu vẫn lỗi thì `OnTerminal` cũng lỗi, lệnh thành in_doubt theo luật của Runner).

Lượt 3: **ĐẠT** — không phát hiện phải sửa. Reviewer soát riêng mọi đường lỗi database của `load`/`retryLater`
(LockFile lỗi, SetFileProcessing/publish/commit lỗi, commit mơ hồ, RetryTx chạy lại, tệp đã processing, chạy lại lệnh
dead): không đường nào làm lệnh chờ mãi; `TestFileProcess_RetryCapNotResetByRolledBackProcessing` bắt đúng lỗi lượt 2.
Ghi chú không bắt buộc (chấp nhận, giữ nguyên): lệch đồng hồ ứng dụng/database vài giây; `image_rejected` đếm trước
commit (có từ trước); lý do lạ của imager → `processing_failed` nên chạy lại gọi imager thêm một lần.

Kết luận: ĐẠT
