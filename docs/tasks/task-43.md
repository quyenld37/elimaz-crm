# Task 43 — Lệnh file.process ở worker — gọi imager, ghi biến thể, chống trùng

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 43, M1-43; gồm R10 `file.process_private`).
Loại việc (quy trình mục 3): A. Vùng rủi ro cao (`internal/platform/files`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `files.FileStore` thêm phương thức `PresignPutAnySize` (ký PUT không ký `content-length`, việc (3) của Task 42).
    Hiện thực: `S3Store`, `MemStore` (2 chỗ, cùng gói). Chỗ dùng interface ngoài gói: `httpapi.Deps.Files`
    (`server.go:31`), `handlers_files.go` (gọi `CreateUpload`, `InspectUpload`), `cmd/crm/main.go` (nối `S3Store`),
    `testkit.MemFileStore`/`S3FileStore` (trả kiểu cụ thể). Không ai khác hiện thực interface → không sửa thêm.
  - `files.VariantURL` thêm trường `Preset int` (thêm trường, không đổi trường cũ); chỗ dùng: `files.Describe`,
    `handlers_files.go:92,119` qua `Info.Variants` (handler chép `Width/Format/URL`, không đổi).
  - Mới: `files.NewProcessHandler`, `files.NewProcessPrivateHandler`, `files.ProcessorOptions`, `files.Imager`,
    `files.Publisher`, `files.PublicVariants`, `files.RegisterPrivateImage`, `files.NewPrivateImage`, `files.MaxPixels`;
    `imager.Client`, `imager.NewClient`, `imager.ErrBusy`. `files.ProcessKind()`, `ProcessPrivateKind()` giữ nguyên (đã có
    từ Task 41, dạng hàm thay cho `var` của kế hoạch — giữ như Task 41).
  - `files.DownloadURL`: nhánh mục đích `private` (R10) — trước trả `ErrNotReady`, nay ký biến thể JPEG lớn nhất trên
    `BucketPrivate`. Chỗ dùng: chưa có ngoài test (mốc sau gọi).
  - `config.Config` thêm `S3ImagerEndpoint` (biến `S3_IMAGER_ENDPOINT`, worker, tùy chọn, mặc định = `S3_ENDPOINT`;
    việc (2) của Task 42).
- Gói phụ thuộc ngược (`go list`): `internal/platform/files` ← `cmd/crm`, `internal/httpapi`, `internal/testkit`.
  `internal/imager` ← `cmd/imager`, (mới) `internal/platform/files`. `internal/platform/config` ← mọi vai trò.
- Operation OpenAPI bị chạm, oasdiff: không có (không đổi `openapi.yaml`; `getFile` đã trả biến thể qua `Describe`).
- Chỗ dùng trên màn hình: không có (màn ảnh là Task 50).
- Câu sqlc, bảng, pha migration: không có migration (bảng `files`, `file_variants`, quyền đã có từ 0124). Câu mới trong
  `queries/platform/files.sql`: `FindReadyDuplicate`, `ListFileVariantsFull`, `SetFileReady`, `SetFileRejected`,
  `InsertFileVariant`, `InsertPrivateImageFile`, `ListPublicVariants`. Chỉ `crm_app` đã có quyền (SELECT/INSERT
  `file_variants`, UPDATE các cột trạng thái của `files`).
- Test sẽ chạy: `./internal/platform/files/...` (cả bộ, vùng cao; MemStore và S3 thật trên SeaweedFS 8443),
  `./internal/imager/...`, `./internal/platform/config/...`, `./internal/platform/commands/...` (vùng cao, dùng qua
  Runner), `./internal/httpapi/...`, `./cmd/crm/...`, `./tests/arch/...`, `./tests/e2e/...`; `tests/e2e`
  tag `imager` với imager thật (compose profile `imager`).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands, queue,
  realtime, metrics, files, tenancy); tìm `Publisher` (có `selftest.Publisher` cùng hình dạng), `imager` client (chưa
  có), `PresignPut`, `ListFileVariants`, `Preset`, `PrivateVariantKey`, `TmpKey`, `ServerKey`, `enqueueProcess`.
- Dùng lại / viết mới vì:
  - Dùng lại: `commands.CommandKind`/`Runner`/`Looker`/`DoneHook`, `queue.TerminalHook`, `queue.JobQueue.Enqueue`
    (`DedupeKey`), `tenancy.WithTenant`, `realtime.Signal` qua `Publish`, `metrics.Registry`, `files.Preset`, các hàm khóa
    trong `keys.go`, `checkLimits`, `enqueueProcess` (RegisterPrivateImage gọi lại đúng hàm xếp lệnh của CompleteUpload),
    kiểu `imager.Request`/`Result` (một hợp đồng duy nhất cho server và client).
  - Viết mới `imager.Client` đặt cạnh `imager.Server` trong `internal/imager` (không ở `internal/adapters`): imager là
    dịch vụ của chính CRM, kiểu dây đã nằm ở gói này; test client chạy trên chính `Server.Handler()`.
  - `files.Publisher` (một phương thức `Publish`) thay vì nhập `selftest.Publisher`: gói nền tảng không nhập gói thử;
    `realtime.PGHub` hiện thực cả hai.

## Soát thiết kế

- 5.1: kho tệp, biến thể là của `platform` (Q38: M1 là chủ); không bảng mới.
- 5.2: chỉ ghi `platform.files`, `platform.file_variants`; `platform.jobs` chỉ qua `Enqueue` và `commands.Runner`.
- 5.3: không đọc bảng module khác. `PublicVariants` là public API cho catalog (Task 44) thay cho SQL lên `platform`.
- 5.4: `RegisterPrivateImage` chèn dòng và xếp lệnh trong transaction của bên gọi.
- 5.5–5.6: gọi `imager` và kho (Open, Head, Copy, Put) **ngoài** transaction trong `Execute`; ghi kết quả trong
  `OnDone`/`OnTerminal` (transaction của Runner). Lệnh `commands` có `in_doubt`. ADR-46 điểm 3: không gọi mạng trong
  transaction — ký đường dẫn là tính tại chỗ.
- 5.7: mọi câu trong `tenancy.WithTenant` của tenant của lệnh; tìm trùng thêm `tenant_id =` (RLS vẫn chặn).
- 5.8: `files` không nhập `net/http` cho imager (client ở `internal/imager`); module nghiệp vụ không bị chạm.
- 5.9: không áp dụng: không đường ghi HTTP mới.
- 5.10: không áp dụng: không Valkey.
- 5.11: không thêm thư viện.
- 5.12: theo mẫu lệnh ra ngoài (Task 12/13: `commands`, `DoneHook`, `TerminalHook`, `Looker`).
- 6.1: hai lệnh cùng tệp: `DedupeKey = fileId`; `OnDone` khóa dòng `files` (`FOR UPDATE`) và chỉ đổi khi còn
  `uploaded`/`processing`/`lost`; biến thể `ON CONFLICT DO NOTHING`. Hai tệp cùng ảnh cùng lúc: khóa đầu ra cố định,
  ghi `If-None-Match: *`; lần thua nhận 412 → `upload_failed` (lỗi tạm) → chạy lại thấy đối tượng đã có (Head) → bỏ qua.
- 6.2: chạy lại an toàn: khóa đầu ra theo mã băm (công khai) hay theo mã tệp (riêng tư), không ghi đè; Execute bỏ qua
  biến thể đã có trên kho, đọc bản kê (`manifest.json`) nếu đã có thì không gọi imager.
- 6.3: thứ tự khóa ADR-46 nhóm 12b: `files` → `file_variants`, rồi `jobs` (Runner ghi cuối). Một dòng `files` mỗi lần.
- 6.4: transaction của Runner tự chạy lại lỗi tạm (`RetryTx`).
- 6.5: READ COMMITTED + `FOR UPDATE`.
- 6.6: hết 3 lượt → `dead` → `OnTerminal` đặt `rejected processing_failed`; `expired` (quá 1 giờ) cũng vậy.
- 6.7: imager trả 503 `Retry-After` (bận) → `not_sent` với `RetryAfter`; lý do tạm `timeout` → `not_sent` 30 s.
- 6.8: worker chết giữa chừng → `in_doubt` → `Looker` trả `not_sent` (chạy lại an toàn, xem Quyết định Q3).
- 6.9: hạn một lần chạy 30 s (ADR-45, D3 của Task 24); đường dẫn ký: nguồn 5 phút, PUT 15 phút.
- 6.10: không áp dụng: không tiền.
- 6.11: rapid: lệnh riêng tư không bao giờ ký/ghi lên `BucketPublic`/`BucketOriginals` hay sinh khóa `orig/`, `img/`
  với mọi chuỗi kết quả của imager; lý do từ chối của tệp luôn thuộc tập đã khai.
- 6.12: không log đường dẫn ký sẵn, nội dung ảnh; `last_error` chỉ mang mã lý do. Ảnh riêng tư (R10) chỉ ở `crm-tmp`
  (thô) và `crm-private` (đã bỏ siêu dữ liệu); không có đường dẫn công khai.
- 6.13: `DownloadURL` nhánh private chỉ trả đường dẫn ký có hạn tới biến thể; tenant khác → `ErrNotFound`. Signal
  realtime `Requires: files.upload`.

## Quyết định thay chủ shop

- **Q1 — Lỗi tạm của imager** (việc (1) Task 42): `timeout`, `upload_failed`, `source_failed`, `internal`, HTTP lỗi,
  503 bận, quá giờ → `not_sent` (chạy lại, tối đa 3 lần); chỉ `not_image`, `type_not_allowed`, `too_large`,
  `too_many_pixels`, `decode_failed` là từ chối hẳn. Phương án khác: theo nguyên văn kế hoạch ("rejected khác →
  rejected") — sẽ từ chối vĩnh viễn ảnh tốt khi kho chập chờn.
- **Q2 — Địa chỉ ký cho imager** (việc (2) Task 42): biến `S3_IMAGER_ENDPOINT` (worker, tùy chọn, mặc định bằng
  `S3_ENDPOINT`); worker dựng `S3Store` thứ hai chỉ để ký đường dẫn đưa imager. Dev trên máy: `http://seaweedfs:8333`.
  Phương án khác: chạy worker trong compose (đổi nhiều hơn, không đảo ngược dễ).
- **Q3 — Looker luôn trả `not_sent`** thay cho "đủ đầu ra → done": `commands` gọi `OnDone` với `result = nil` khi done
  do tra lại, mà `OnDone` cần kích thước, loại thật, cỡ từng biến thể (không được gọi mạng trong transaction). Execute
  chạy lại là an toàn và rẻ: đọc bản kê `manifest.json` (ghi sau khi imager trả ok) thì xong ngay, không gọi imager.
  Phương án khác: mở rộng `commands` cho Looker trả kết quả (sửa vùng cao cho một chỗ dùng).
- **Q4 — Bản kê `manifest.json`** cạnh biến thể (`img/<sha>/v<preset>/manifest.json` trên public;
  `priv/<tenant>/<file>/v<preset>/manifest.json` trên private): loại thật, kích thước, cỡ từng biến thể (không có dữ liệu
  cá nhân). Nhờ vậy tenant khác cùng ảnh, hay lần chạy lại sau khi mất câu trả lời, không gọi lại imager. Bản kê ghi
  `If-None-Match`; công khai đọc được qua `FILES_PUBLIC_BASE_URL` (chỉ số, không có gì riêng tư). Biến thể đủ mà chưa có
  bản kê (chết giữa hai bước): gọi imager với một đầu ra thử ghi vào `crm-tmp` (tự xóa 24 giờ) để lấy kết quả kiểm.
- **Q5 — Từ chối vì nội dung:** Execute ghi `rejected <lý do>` lên dòng tệp trong một transaction ngắn rồi trả
  `rejected` (lệnh `dead` như spec 04 mục 5.3); `OnTerminal` chỉ đặt `processing_failed` khi tệp chưa có kết quả. Lý do:
  `OnTerminal` không nhận lý do. Phương án khác: lệnh `done` kèm `{"rejected":…}` (không tăng `jobs_dead`) — trái nghĩa
  `rejected` của spec.
- **Q6 — Dòng `files` của ảnh công khai giữ `bucket`, `key` cũ** (`tmp/…`); ảnh gốc ở `OriginalKey(content_sha256)`.
  Lý do: `UNIQUE (tenant_id, bucket, key)` sẽ va khi hai tệp cùng tenant cùng ảnh. Ảnh riêng tư đổi sang
  `private`, `ServerKey` (mỗi tệp một khóa). Task 44 lấy ảnh gốc theo mã băm.
- **Q7 — Tìm trùng theo cùng mục đích** cho cả công khai: mục đích khác có `AllowedTypes` khác, dùng lại kết quả kiểm
  của mục đích khác có thể lọt loại không cho phép. Bản kê dùng chung giữa mục đích: kiểm lại `detectedType` thuộc
  `AllowedTypes`.
- **Q8 — `MaxPixels` = 40 triệu** (Q12 của kế hoạch), hằng `files.MaxPixels`.
- **Q9 — `expired`** (lệnh còn chờ quá 1 giờ) xử lý như `dead`: tệp `rejected processing_failed` (kế hoạch chỉ nói
  `dead`; để `uploaded` mãi thì màn hình quay mãi).
- **Q11 — Thiếu `IMAGER_URL` ở worker:** không đăng ký hai lệnh xử lý ảnh, log cảnh báo, lệnh chờ trong hàng đợi
  (không mất). Phương án khác: bắt buộc biến (vỡ cấu hình dev, test khởi động hiện có).
- **Q12 — Imager bận (phát hiện của reviewer lần 1):** `imager.Client` giữ tối đa `IMAGER_MAX_PARALLEL` việc gửi
  cùng lúc (biến mới của worker, mặc định 2, đặt bằng biến cùng tên của imager), việc thừa chờ chỗ trống; gặp 503 thì
  chờ `Retry-After` rồi gửi lại trong cùng lần chạy. Chỉ khi hết hạn 30 s mà vẫn chờ mới thành `not_sent busy` (tốn
  một lượt). Phương án khác: thêm kết quả "hoãn, không tính lượt" vào `commands` (sửa vùng cao, đổi hợp đồng dùng chung)
  — ghi ở Việc phát hiện thêm nếu có nhiều worker dùng chung một imager.
- **Q10 — `PresignPutAnySize`** là phương thức mới của `FileStore` (không ký kích thước, vẫn ký loại và
  `If-None-Match: *`), thay cho cho phép `size = 0` ở `PresignPut` (dễ ký nhầm đường tải lên của trình duyệt).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| TP01: ok → ready, 12 dòng `file_variants` public, `getFile` (`Describe`) trả 12 URL `FILES_PUBLIC_BASE_URL/img/<sha>/v1/`; ảnh gốc ở `orig/<sha>`; nguồn imager là `orig`; `MaxPixels` 40 MP; 1 báo hiệu `files.file`; chỉ số `image_process_ms{ok}` | `TestFileProcess_ReadyWithTwelveVariants` | `go test -race -count=1 ./internal/platform/files/` (PostgreSQL 5543, SeaweedFS 8443) | `ok … 39.858s` |
| Cùng ảnh lần hai cùng tenant → imager không gọi lại, kết quả `{"reuse": <id>}`, 12 khóa giống hệt; tenant B cùng ảnh → không gọi imager (bản kê) | `TestFileProcess_DuplicateReusesVariants` | như trên | PASS |
| `not_image` → tệp `rejected not_image`, lệnh `dead` sau 1 lần, imager 1 lần, 0 biến thể, `image_rejected{not_image}` | `TestFileProcess_RejectedNotImage` | như trên | PASS |
| Lỗi tạm (`upload_failed`, `source_failed`, `internal`, `timeout`) → `pending`, tệp vẫn `uploaded`; 503 bận → hẹn theo `Retry-After` (7 s) | `TestFileProcess_TransientReasonsRetry` | như trên | PASS |
| 8 ảnh cùng lúc, worker 8 việc, imager giả chỉ nhận 2 (quá thì 503): không lần 503 nào, cao nhất 2 việc cùng lúc, mọi lệnh done ở lượt 1 | `TestFileProcess_ManyImagesNoBusyRetry` | `go test -race -count=1 ./internal/platform/files/` | PASS |
| Client: tối đa 1 việc cùng lúc với 3 lời gọi; 503 → chờ 1 s gửi lại (4 lần gọi); chờ chỗ quá hạn → `ErrBusy` | `TestClient_LimitsParallelAndRetriesBusy` | `go test -race -count=1 ./internal/imager/` | PASS |
| Treo: hạn 1 s → `not_sent`, chạy lại; 3 lần → `dead`, tệp `rejected processing_failed`; cùng lúc 5 ảnh khác ready, 20 lệnh selftest done, một worker | `TestFileProcess_HangDoesNotBlockWorker` | như trên | PASS |
| Looker: ghi xong mà mất câu trả lời → `in_doubt` → `Lookup` `not_sent` → `ResolveInDoubt` đưa về hàng chờ → done, imager vẫn 1 lần | `TestFileProcess_LookerAfterLostAnswer` | như trên | PASS |
| Chạy lại sau khi ghi một phần: chỉ xin 6 biến thể thiếu; đủ 12 mà thiếu bản kê → 1 đầu ra thử vào `crm-tmp` | `TestFileProcess_SkipsExistingVariants` | như trên | PASS |
| Tenant isolation: lệnh của A trỏ tệp của B → `dead not_found`, tệp của B không đổi | `TestFileProcess_TenantIsolation` | như trên | PASS |
| `PublicVariants`: một câu cho nhiều tệp; tệp chưa ready, private, tenant khác, không có → không trả; danh sách rỗng → rỗng | `TestPublicVariants` | như trên | PASS |
| R10: 1 lệnh `file.process_private`, 0 `file.process`; ready, 12 biến thể `private` dưới `priv/<tenant>/<file>/v1/`; `crm-public`, `crm-originals` rỗng, không khóa `orig/`, `img/`; mọi `PutURL` ở private; `Describe`, `PublicVariants` không trả | `TestProcessPrivate_NeverInPublicBucket` | như trên | PASS |
| R10: `DownloadURL` ký `priv/…/v1/1600.jpeg` trên private (không phải `TmpKey`, không có dạng public); hạn 2 s, đọc sau 3 s → 403; trước khi xong `ErrNotReady`; tenant B `ErrNotFound` | `TestProcessPrivate_SignedExpiringLink` | như trên | PASS |
| R10: `not_image` → rejected, 0 biến thể, `ErrRejected`; `RegisterPrivateImage` svg → `ErrTypeNotAllowed`, quá cỡ → `ErrTooLarge`, `product_image` → `ErrPurposeMode`, chưa đăng ký → `ErrUnknownPurpose`; không dòng, không lệnh | `TestProcessPrivate_RejectsNonImage` | như trên | PASS |
| `file.process` xếp nhầm cho mục đích private → `rejected purpose_private`, không gọi imager, `crm-public` rỗng, tệp không đổi; tải lên nhân viên mục đích private → chỉ `file.process_private` → ready | `TestFileProcess_RefusesPrivatePurpose` | như trên | PASS |
| Chống trùng riêng tư: cùng tenant 1 lần imager; tenant B gọi lại, khóa dưới `priv/<B>/`; cùng ID hai lần → 1 dòng, 1 lệnh | `TestProcessPrivate_Dedupe` | như trên | PASS |
| Looker private `not_sent`; ảnh thô bị xóa → `rejected source_missing`; treo → 3 lần `dead`, `processing_failed` | `TestProcessPrivate_LookerMissingSourceAndHang` | như trên | PASS |
| Quy luật (rapid): mọi kết quả imager → không đối tượng công khai, lý do thuộc tập đã khai, không kẹt trạng thái chờ | `TestProcessPrivate_PropertyNeverPublic` | `go test -count=1 -run TestProcessPrivate_PropertyNeverPublic -v ./internal/platform/files/` | `[rapid] OK, passed 100 tests` |
| Ký PUT không cỡ (MemStore và S3 thật): cỡ bất kỳ 200; ghi đè 412; khác loại 403 | `TestFileStore_PresignPutAnySize` | như dòng 1 | PASS cả `mem`, `s3` |
| Client imager: 200 → Result; 503 mãi tới hết hạn ctx → `BusyError` 3 s; 400 → lỗi; treo → `DeadlineExceeded` | `TestClient_Process` | `go test -race -count=1 ./internal/imager/` | `ok … 8.687s` |
| `S3_IMAGER_ENDPOINT` mặc định `S3_ENDPOINT`, khai được, sai dạng → lỗi nêu tên; `IMAGER_MAX_PARALLEL` mặc định 2, 0 → lỗi | `TestLoadWorkerImagerEndpoint` | `go test -race -count=1 ./internal/platform/config/` | `ok` |
| TP01 với imager thật: `photo-gps.jpg` (có EXIF) → ready ~1–2,7 s, 12 biến thể đọc được trên `crm-public`, không còn `Exif`/`GPS` | `TestFilesPipeline_PublicPhotoGPS` (tag `imager`) | `CRM_IMAGER_PORT=4643 go test -tags imager -count=1 -run TestFilesPipeline ./tests/e2e/` (compose `crm-t43`, profile imager) | `ok … 3.220s`; log `ready sau 1.647s` |
| R10 với imager thật: ảnh GPS → ready, 12 biến thể chỉ ở `crm-private`, không còn `Exif`/`GPS`, không có `orig/<sha>` hay `img/<sha>/…`; `DownloadURL` trỏ `crm-private/priv/…/1600.jpeg`; `html-as.jpg` → `not_image`, `pixel-bomb.png` → `too_many_pixels`, 0 biến thể | `TestFilesPipeline_PrivateImages` (tag `imager`) | như trên | PASS |
| Sau sửa phát hiện reviewer lần 1 (client giới hạn song song, kiểm preset lúc khởi động, OnTerminal bỏ tệp không phải ảnh) | cả các gói dưới | `go test -race -count=1 ./internal/platform/files/ ./internal/imager/ ./internal/platform/config/ ./internal/httpapi/ ./tests/arch/... ./tests/e2e/`; `CRM_IMAGER_PORT=4643 go test -tags imager …` | tất cả `ok` (files 211 s, e2e 216 s; tag imager `ok 18.9s`) |
| Không vỡ chỗ dùng (sau khi gộp main) | gói phụ thuộc ngược và vùng cao | `go test -race -count=1 ./internal/platform/files/ ./internal/imager/ ./internal/platform/config/ ./internal/platform/commands/ ./internal/httpapi/ ./tests/arch/... ./tests/e2e/` | tất cả `ok` |
| Lint, trùng mã, danh mục | golangci-lint (cả tag `imager`), jscpd, `make catalog` | `golangci-lint run ./...`; `make jscpd` | `0 issues`; jscpd: 2 bản trùng mới đều trong mã của task khác (xem Việc phát hiện thêm), 0 của task này |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `Looker` luôn `not_sent` (Q3) thay cho "đủ đầu ra → done"; test Looker kiểm kết quả cuối (done, không gọi imager lần
  hai) qua đường `in_doubt → pending → done`.
- Lỗi tạm của imager → `not_sent` (Q1), không phải "rejected khác → rejected".
- Từ chối vì nội dung ghi `reject_reason` ngay trong Execute (Q5); `OnTerminal` chỉ ghi `processing_failed`.
- Thêm bản kê `manifest.json` và đầu ra thử vào `crm-tmp` (Q4) — không có trong kế hoạch.
- `ProcessKind`, `ProcessPrivateKind` giữ dạng hàm (đã có từ Task 41), không đổi sang `var`.
- `platform.file_variants_v` thuộc Task 44 (chưa có): test R10 kiểm `PublicVariants` và `Describe`, chưa kiểm view.
- TP01 "tải qua API": test đầu cuối gọi đúng các hàm mà handler `createUpload`/`completeUpload` gọi
  (`CreateUpload`, PUT ký sẵn, `CompleteUpload`), không dựng tiến trình api (cần Cloudflare Access giả). "Ảnh công khai
  đọc được qua `FILES_PUBLIC_BASE_URL`": dev không có đọc ẩn danh `crm-public`, test đọc bằng danh tính crm-app.
- Thêm `S3_IMAGER_ENDPOINT` (Q2) và `PresignPutAnySize` (Q10) — ba việc Task 42 để lại.
- `MemStore`, `S3Store`: tách hàm `presignPut` dùng chung cho `PresignPut` và `PresignPutAnySize` trong cùng commit
  hành vi (hàm cũ giữ nguyên hành vi, test hợp đồng cũ vẫn qua).
- Bỏ câu sqlc `ListFileVariants` (thay bằng `ListFileVariantsFull`, có cả bộ kích thước và cỡ; `Describe` dùng câu mới).

## Việc phát hiện thêm

- Host của đường dẫn ký cho **trình duyệt** (Task 41 ghi) vẫn là `S3_ENDPOINT`: cần biến riêng trước production (giống
  `S3_IMAGER_ENDPOINT`), chưa làm ở task này.
- Dev không có đọc ẩn danh `crm-public` qua `FILES_PUBLIC_BASE_URL`; cần khi làm màn ảnh (Task 50) hoặc staging.
- Dòng `files` của ảnh công khai giữ `bucket/key = tmp/…` sau khi ready (Q6): `DownloadURL` cho `product_image` sẽ ký
  tới `crm-tmp` (hết hạn sau 24 giờ). Task 44 lấy ảnh gốc theo `OriginalKey(content_sha256)`; cân nhắc cột riêng hoặc
  `DownloadURL` theo mã băm cho mục đích ảnh standard.
- `TestImager_HangKilledWithinDeadline` (Task 42) đỏ một lần khi máy đang chạy nhiều test song song (script giả chưa
  kịp ghi pid trong 1 s); chạy riêng 3 lần đều qua. Có thể nới thời điểm chờ pid.
- `make jscpd` báo 2 bản trùng mới không thuộc task này, đến từ main: `internal/platform/db/migrate_test.go` (79–84 /
  239–244, Task 318) và `internal/platform/queue/errors_test.go` 117–122 / `worker_test.go` 104–109.
- `imager.Client` không đặt `http.Client.Timeout` (hạn theo ctx 30 s của lệnh).
- Nhiều worker dùng chung một imager (khi mở rộng): giới hạn song song ở mỗi worker không đủ, 503 kéo dài vẫn tốn lượt
  thử (`MaxAttempts = 3`). Khi đó nên thêm kết quả "hoãn, không tính lượt" vào `commands` (Q12).
- Biến thể riêng tư dùng chung khóa giữa hai tệp trùng nội dung cùng tenant (tệp B trỏ `priv/<t>/<A>/…`): việc xóa ảnh
  riêng tư ở mốc sau (khách yêu cầu xóa dữ liệu) phải xét ràng buộc này.

## Reviewer

Lần 1: CẦN SỬA — một phát hiện về tính đúng: imager bận (503) vẫn tốn lượt thử, tải nhiều ảnh cùng lúc có thể làm
ảnh tốt thành `rejected processing_failed` vĩnh viễn (worker 8 việc, imager 2, `Retry-After` 1 s, `MaxAttempts` 3).
Đã sửa theo Q12, thêm `TestFileProcess_ManyImagesNoBusyRetry`, `TestClient_LimitsParallelAndRetriesBusy`. Ghi chú không
bắt buộc đã làm: kiểm `IMAGE_PRESET_VERSION` có bộ kích thước lúc khởi động worker; `OnTerminal` bỏ qua tệp không phải
ảnh. Ghi chú còn lại đã chép vào Việc phát hiện thêm.

Lần 2: ĐẠT — bản sửa 503 đúng gốc (client chia chung một giới hạn cho hai lệnh, 503 gửi lại trong cùng lần chạy, chờ
quá hạn → `BusyError` = chắc chắn chưa gửi); test mới kiểm đúng tình huống; test 503 cũ không bị nới. Ghi chú không bắt
buộc: chờ quá 30 s (ảnh treo giữ hết chỗ, nhiều worker) vẫn tốn một lượt — đã ở Việc phát hiện thêm; việc chờ chỗ
chiếm chỗ của worker vài giây, không kẹt.

Kết luận: ĐẠT
