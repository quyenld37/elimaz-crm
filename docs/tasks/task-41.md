# Task 41 — FileStore, bảng tệp, mục đích tệp, API tải lên bằng đường dẫn ký sẵn, ghi tệp từ máy chủ

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 41, M1-41). Loại việc (quy trình mục 3): A.
Vùng rủi ro cao mới của M1: `internal/platform/files` (ký đường dẫn, kiểm quyền).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Gói mới `internal/platform/files` (chưa ai nhập): `Bucket` (`BucketTmp|Public|Originals|Private`), `FileStore`,
    `PresignedPut`, `ObjectInfo`, `S3Config`, `NewS3Store`/`S3Store`, `MemStore`/`NewMemStore`, khóa (`TmpKey`,
    `OriginalKey`, `VariantKey`, `ServerKey`, `PrivateVariantKey`), mục đích (`Purpose`, `Registry`, `RegisterPurpose`,
    `PurposeOf`, `CheckPurposes`), `CreateUpload`, `CompleteUpload` (+ hai bước `InspectUpload`, `ApplyUpload` cho
    `httpapi.WriteTx`), `Register`/`NewFile`, `MarkDeleted`, `Status`/`FileStatus`, `DownloadURL`, `Describe`, `Preset`,
    `ProcessKind`, `ProcessPrivateKind`, các sentinel lỗi.
  - `testkit.MemFileStore(t)`, `testkit.S3FileStore(t)` mới.
  - `config.Config`: không đổi trường. `config.Load` đổi hành vi: nhóm `S3_*` + `FILES_PUBLIC_BASE_URL` bắt buộc cho
    `api`, `worker` (việc treo 1 của Task 25); `CRM_ENV` chỉ nhận `dev`, `test`, `staging`, `production` (việc treo 2).
    Người gọi `config.Load`: `cmd/crm/main.go` (1 chỗ) và test của `config`; e2e (`tests/e2e/boot_test.go` `roleEnv`)
    dựng env cho api/worker → thêm biến S3.
  - `httpapi.Deps`: thêm trường `Files files.FileStore`; người dựng `Deps`: `cmd/crm/main.go`, `serve_test.go`
    `testServer` (2 chỗ).
- Gói phụ thuộc ngược (go list): `internal/platform/config` ← `cmd/crm`, `internal/httpapi`, `internal/platform/logging`,
  `metrics`, `telemetry`. `internal/httpapi` ← `cmd/crm`. Gói `files` mới ← `httpapi`, `testkit`, `cmd/crm`.
- Operation OpenAPI bị chạm, oasdiff: thêm 3 operation mới (không phá vỡ): `createUpload` `POST /api/v1/files/uploads`,
  `completeUpload` `POST /api/v1/files/{id}/complete`, `getFile` `GET /api/v1/files/{id}`; quyền `files.upload` (đã có
  trong `identity.Permissions` và `docs/quyen.md` từ Task 26, không thêm dòng quyền). Không đổi operation cũ.
- Chỗ dùng trên màn hình: chưa có (màn tải ảnh ở M1-50). Client sinh `console/src/api` chưa có (Task M0-17).
- Câu sqlc, bảng, pha migration: migration mới `0124_files.sql`, pha **mở rộng** (3 bảng mới, quyền, index; không sửa
  bảng cũ): `platform.file_purposes` (không RLS, `crm_app` chỉ `SELECT`), `platform.files`, `platform.file_variants`
  (có RLS). Câu mới trong `queries/platform/files.sql` → `internal/platform/internal/store` (khối sqlc platform có
  sẵn). Thứ tự khóa: nhóm 12b `platform.files` → `platform.file_variants` đã có trong `docs/adr/0046-thu-tu-khoa.md`
  (Task 25), task này chỉ khóa `files`.
- Cấu hình: `cmd/crm` dựng `S3Store` cho api (không gọi mạng lúc khởi động) và kiểm mục đích Go khớp bảng lúc khởi động
  api, worker.
- Test sẽ chạy: `go test -race ./internal/platform/files/... ./internal/httpapi/... ./internal/platform/config/...
  ./internal/testkit/... ./tests/arch/... ./tests/e2e/... ./cmd/...`, cộng cả bộ vùng rủi ro cao `internal/platform/files`.
  S3 thật: SeaweedFS compose dev `crm-t41` (`CRM_S3_PORT=8441`).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có gì về kho tệp,
  ký đường dẫn, mục đích tệp (`internal/platform/files` là của task này). Có và dùng lại: `tenancy.WithTenant` (mọi
  truy vấn `files`), `db.RetryTx` gián tiếp, `queue.JobQueue.Enqueue` + `DedupeKey` (một lệnh `file.process` mỗi tệp,
  chống trùng tầng database `UNIQUE (tenant_id, kind, dedupe_key)`), `commands.CommandKind` (chính sách lệnh
  `file.process`, `file.process_private`), `httpapi.WriteTx` + middleware chống trùng (đường ghi mẫu Task 12),
  `testkit.OpenStaff`, `testkit.AccessKeyServer`, `testServer`/`serveAs` của `httpapi`. Cách dựng client S3 giống
  `tests/probe` (Task 40) nhưng viết lại trong gói `files` vì `tests/probe` là gói test có tag riêng, không nhập được.
- Dùng lại / viết mới vì: `FileStore` viết mới theo Interfaces của kế hoạch (hai bản hiện thực thật S3/Mem và bản giả
  cho test). Sổ mục đích viết mới: không có sổ đăng ký tương tự trong `internal/platform` (`schedules.Registry`,
  `events.Registry` là sổ của đối tượng khác, giữ lý do thay đổi khác). Ký HMAC của `MemStore` dùng `crypto/hmac`
  chuẩn.

## Soát thiết kế

- 5.1: kho tệp là của `platform` (04 mục 3; kế hoạch Q-X2, Q38: M1 là chủ); bảng trong schema `platform`.
- 5.2: không ghi bảng module khác. `platform.jobs` chỉ ghi qua `queue.Enqueue`.
- 5.3: không đọc bảng module khác (không khóa ngoại tới `identity.users` cho `created_by`, chỉ cột uuid).
- 5.4: `CreateUpload`, `Register`, `MarkDeleted`, `ApplyUpload` chạy trong transaction của bên gọi, cùng lệnh xếp hàng.
- 5.5: xử lý ảnh đi sau commit bằng lệnh `file.process` (Task 43 viết handler); task này chỉ xếp lệnh.
- 5.6: gọi kho tệp (mạng) luôn **ngoài** transaction: `Head` trước `WithTenant` (ADR-46), `Put` trước `Register`. Ký
  đường dẫn là tính tại chỗ (không mạng) nên được làm trong transaction. Không có lệnh ra ngoài nào ở task này.
- 5.7: `files`, `file_variants` có RLS bằng `platform.enable_tenant_rls`, `UNIQUE (tenant_id, id)`, khóa ngoại ghép
  `(tenant_id, file_id)`; `file_purposes` không có `tenant_id` (danh mục tĩnh), `crm_app` chỉ `SELECT`.
- 5.8: handler mỏng ở `internal/httpapi/handlers_files.go`; lỗi đổi sang HTTP ở `errors.go`.
- 5.9: `createUpload`, `completeUpload` có `IdempotencyKey`, `x-permission: files.upload`; `getFile` `files.upload`.
- 5.10: không áp dụng: không Valkey.
- 5.11: aws-sdk-go-v2 (ADR-44) đã có trong `go.mod` từ Task 40; không thêm thư viện.
- 5.12: theo mẫu identity (gói công khai + store sinh), đường ghi Task 12 (`WriteTx`).
- 6.1: hai `complete` cùng lúc: `SELECT … FOR UPDATE` dòng `files` trong transaction, chỉ chuyển từ `awaiting_upload`;
  lệnh có `DedupeKey = fileId`. `Register` cùng khóa: `INSERT … ON CONFLICT (tenant_id, bucket, key) DO NOTHING` rồi
  đọc lại id cũ. Kho tệp: `Put` có `If-None-Match: *`, đường dẫn ký sẵn PUT cũng ký `If-None-Match: *`; `CopyIfAbsent`
  kiểm `Head` đích trước rồi `CopyObject` có `If-None-Match: *`.
- 6.2: `Idempotency-Key` + `WriteTx` cho hai đường ghi; `complete` còn tự đúng khi gửi lại (trạng thái đã đổi thì trả
  trạng thái hiện tại, không xếp thêm lệnh).
- 6.3: chỉ khóa một dòng `platform.files` (nhóm 12b), không khóa bảng khác trong cùng transaction ngoài
  `platform.jobs` (ghi mới qua Enqueue) và dòng khóa chống trùng (nhóm 13, cuối).
- 6.4: lỗi tạm → `db.ErrRetryLater` → 503 `retry_later` (sẵn có ở `WriteTx`).
- 6.5: READ COMMITTED + `FOR UPDATE`.
- 6.6–6.8: lệnh `file.process` dùng `commands.CommandKind` (3 lần, `in_doubt` khi hết giữ chỗ); handler ở Task 43.
- 6.9: đường dẫn PUT hạn 15 phút; transaction api theo `db.TxAPI`.
- 6.10: không áp dụng: không tiền.
- 6.11: quy luật bằng rapid: khóa sinh ra không bao giờ chứa tên tệp người dùng, luôn đúng dạng; lệnh không xếp hai lần.
- 6.12: tên tệp gốc là dữ liệu người dùng, chỉ lưu cột `original_name`, không bao giờ vào khóa kho, log, lỗi. Khóa S3
  không vào log (`S3Config` có `LogValue` che bí mật). Đường dẫn ký sẵn không log.
- 6.13: mặc định từ chối: ba route cần `files.upload`; tenant khác → 404 (RLS + `ErrNotFound`); chỉ mục đích có
  `Upload` mới tải lên được; `DownloadURL` đòi `ready`, chưa xóa, chưa hết hạn; tệp mục đích `private` không bao giờ
  được ký đường dẫn tới bản thô.

## Quyết định thay chủ shop

- **Q1 — Việc treo 1, 2 của Task 25.** Nhóm `S3_ENDPOINT`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`,
  `FILES_PUBLIC_BASE_URL` bắt buộc với `api` và `worker` (hai vai trò dùng kho tệp); `ingest`, `migrate` không đọc.
  `CRM_ENV` chỉ nhận `dev`, `test`, `staging`, `production`; giá trị khác (ví dụ `prod`) là lỗi khởi động nêu tên biến.
  Phương án khác: chỉ api bắt buộc (worker chưa dùng tới Task 43) — không chọn vì việc treo ghi rõ "vai trò cần kho
  tệp" và worker là bên xử lý ảnh. Đảo: một hàm trong `config.go`.
- **Q2 — Sổ mục đích là biến cấp gói.** Interfaces khai `RegisterPurpose`, `PurposeOf` là hàm gói và `CreateUpload`,
  `Register` không nhận sổ, nên sổ của tiến trình là `defaultRegistry` (có khóa, chỉ thêm, M1 có sẵn
  `product_image`, `catalog_import`; `//nolint:gochecknoglobals` kèm lý do như `queue.DefaultPolicy`). Thêm
  `NewRegistry`/`Registry.Check` để test so một tập khác với bảng mà không đụng sổ chung. `api`, `worker` gọi
  `files.CheckPurposes` lúc khởi động: lệch → thoát lỗi nêu khóa (e2e `TestBootPurposeMismatchFails`). Phương án khác:
  truyền sổ vào từng hàm (trái chữ ký kế hoạch); chỉ kiểm trong test (lệch ở production không bị chặn).
- **Q3 — `CompleteUpload` có thêm hai bước `InspectUpload` (đọc + `Head` ngoài transaction) và `ApplyUpload(tx)`.**
  Đường HTTP phải ghi kết quả cùng khóa chống trùng trong `httpapi.WriteTx` (mẫu đường ghi Task 12), mà
  `CompleteUpload(ctx, d, …)` theo kế hoạch tự mở transaction. `CompleteUpload` giữ đúng chữ ký, là hai bước ghép lại.
- **Q4 — Đường dẫn PUT ký thêm `If-None-Match: *`.** Ngoài `content-length`, `content-type`, chữ ký gồm
  `If-None-Match: *` (trả trong `headers`), nên PUT lần hai lên cùng khóa bị kho từ chối (đo trên SeaweedFS 4.48 và
  `MemStore`): đúng luật "không ghi đè khóa đã có". Trình duyệt chỉ cần gửi đúng `headers` (CORS `crm-tmp` cho mọi
  header). Đảo: bỏ một trường.
- **Q5 — `createUpload` phát lại được (không `no-replay`).** Thân lưu trong khóa chống trùng gồm `putUrl` (hạn 15
  phút, chỉ PUT đúng một khóa `tmp/…`, đúng kích thước, không ghi đè): không phải bí mật trả một lần như khóa dịch vụ;
  phát lại giúp trình duyệt gửi lại nhận cùng `fileId`. Phương án khác: `no-replay` (gửi lại cùng khóa nhận 409).
- **Q6 — Mã lỗi.** Mục đích lạ hoặc không nhận tải lên → 422 `file_purpose_not_allowed` (kế hoạch nói 422, không đặt
  tên mã); tên quá 255 ký tự → 400 `invalid_input` (mã có sẵn).
- **Q7 — Tenant của tệp.** `UploadInput` có thêm `TenantID` (khóa `TmpKey` cần tenant). `Register` (chữ ký kế hoạch
  không có tenant) đọc tenant của ngữ cảnh transaction và đòi `Key` bắt đầu bằng `priv/<tenant đó>/`, không có `..`,
  `//` → ngược lại `ErrInvalidFile`: chặn mã của một tenant đăng ký đối tượng của tenant khác. `Register` cũng kiểm
  `AllowedTypes`, `MaxBytes` của mục đích như tải lên.
- **Q8 — Cột `deleted_by`.** `MarkDeleted` nhận `actor` theo kế hoạch nhưng bảng không có chỗ ghi; thêm cột
  `deleted_by uuid NULL` (pha mở rộng). Phương án khác: ghi audit (platform phải gọi identity) hoặc bỏ qua `actor`.
- **Q9 — Loại lệnh.** `files.ProcessKind()`, `files.ProcessPrivateKind()` là hàm (không phải `var` như kế hoạch Task 43
  ghi) đặt ở `upload.go` vì task này đã xếp lệnh; chính sách đúng kế hoạch Task 43 (30 s, 3 lần, `CommandKind`). Task
  43 dùng lại, không khai lần hai.
- **Q10 — `complete` kiểm cả loại.** Ngoài `size_mismatch`, đối tượng có `Content-Type` khác loại đã khai (chỉ có
  được bằng đường khác đường ký sẵn) → `rejected` `type_mismatch`.
- **Q11 — Tệp mục đích `private` qua `DownloadURL` → `ErrNotReady`** tới khi Task 43 thêm nhánh ký biến thể JPEG
  (không bao giờ ký tới bản thô, R10). `Describe` (getFile) chỉ trả biến thể trên `BucketPublic`.
- **Q12 — Ràng buộc thêm trong bảng.** `status = 'rejected'` ⇔ có `reject_reason`; `content_sha256` 32 byte;
  `file_variants.bucket` chỉ `public`/`private`; `crm_app` chỉ `UPDATE` các cột trạng thái (không đổi tenant, mục đích,
  tên gốc, loại khai, kích thước khai), không `DELETE`.
- **Q13 — testkit tìm kho dev.** `testkit.S3Config`: `CRM_TEST_S3_URL` → `CRM_S3_PORT` → hỏi docker cổng của
  `seaweedfs` thuộc `COMPOSE_PROJECT_NAME` → 8333; rồi đòi header `Server: SeaweedFS` (cổng 8333 của máy này là dự án
  khác). Lý do: phiên này không có `CRM_S3_PORT` trong môi trường của hook Stop. Không bỏ qua test khi thiếu kho.
- **Q14 — `TestHealthcheckCommand` chạy api trên database đã migrate** (trước dùng database `crm` chưa migrate): api
  giờ kiểm `platform.file_purposes` lúc khởi động, đúng ý "không phục vụ trên schema lệch".

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test đỏ trước khi có mã | cả bộ mới | `go test ./internal/platform/files/ ./internal/httpapi/`; `go test ./internal/platform/config/` | FAIL: "no non-test Go files in …/files", httpapi build failed; config: "api không khai kho tệp phải lỗi", "ingest CRM_ENV=\"prod\" phải lỗi" |
| TP03: 25 MiB → 422 `file_too_large`, svg → `file_type_not_allowed`, mục đích lạ/không Upload → 422 | `TestCreateUpload_TooLarge`, `TestCreateUpload_PurposeWithoutUpload`, `TestFilesAPI_CreateUploadLimits` | `go test -race -count=1 ./internal/platform/files/ ./internal/httpapi/` (CRM_PG_PORT=5541, SeaweedFS crm-t41 8441) | PASS; không có dòng nào sau khi bị từ chối |
| Ghi từ máy chủ: Put 10 KB → Register ready; lần hai cùng khóa cùng id; Put trùng → ErrObjectExists nội dung giữ; DownloadURL đúng byte; hết hạn → ErrExpired; MarkDeleted → ErrNotFound; tenant B → ErrNotFound | `TestRegister_ServerWrittenFile/{mem,s3}` | như trên | PASS trên MemStore và SeaweedFS |
| Register từ chối mục đích lạ, private (ErrPurposeMode), loại, dung lượng, khóa tenant khác | `TestRegister_Refusals`, `TestPurposes_PrivateMode` | như trên | PASS |
| Go khớp bảng: thiếu dòng/thừa dòng/khác mode/khác bucket → báo lệch nêu khóa | `TestPurposes_GoMatchesTable` | như trên | PASS |
| CHECK mode private + bucket public → 23514; khóa sai dạng; crm_app không chèn 42501; purpose lạ → 23503 | `TestPurposes_TableConstraints` | như trên | PASS |
| `RegisterPurpose{Private, BucketPublic}` → panic (và 6 ca sai khác) | `TestRegisterPurpose_PanicsOnInvalid` | như trên | PASS |
| Tải bằng putUrl rồi complete → uploaded, 1 lệnh file.process; gửi lại không thêm; PUT lần hai không ghi đè | `TestUpload_PutThenComplete/{mem,s3}`, `TestFilesAPI_UploadFlow` | như trên | PASS |
| 50 complete cùng lúc → 1 lệnh | `TestUpload_ConcurrentComplete` | như trên | PASS |
| complete trước khi tải → 409 upload_missing; 900 byte vào chữ ký 1.000 → kho từ chối; khác kích thước qua đường khác → rejected size_mismatch | `TestUpload_MissingAndSizeMismatch/{mem,s3}`, `TestFilesAPI_UploadFlow` | như trên | PASS |
| PQ08: tenant B GET/complete tệp của A → 404 | `TestUpload_OtherTenantNotFound`, `TestFilesAPI_UploadFlow`, `TestFilesAPI_PermissionsAndUnknown` | như trên | PASS; không quyền → 403 |
| Khóa không chứa tên gốc (`../../etc/passwd.jpg`, `<script>.png`, rapid) | `TestUpload_KeyHasNoUserFileName`, `TestKeys_Property`, `TestKeys_Formats` | như trên | PASS (rapid 100 ca mỗi test) |
| CopyIfAbsent lên đích đã có → không đổi | `TestFileStore_Contract/{mem,s3}` | như trên | PASS |
| Delete bằng crm-app → crm-admin đọc bản cũ theo phiên bản | `TestS3Store_DeleteKeepsVersion` (3 bucket), `TestMemStore_DeleteKeepsVersion` | như trên | PASS: 1 bản + 1 dấu xóa, đọc lại "con-day" |
| GET ký sẵn hết hạn → bị từ chối | `TestFileStore_PresignGetExpires/{mem,s3}` | như trên | PASS |
| Tạo upload phát lại cùng khóa: cùng fileId, putUrl | `TestFilesAPI_CreateUploadReplay` | như trên | PASS, `Idempotent-Replayed: true`, thêm đúng 1 tệp |
| Việc treo: S3 bắt buộc api/worker; CRM_ENV lạ → lỗi | `TestLoadS3RequiredForFileRoles`, `TestLoadEnvMustBeKnown` | `go test -race ./internal/platform/config/` | PASS; `prod` + hãng giả → lỗi, không trả Config |
| Khởi động lệch mục đích → api, worker thoát lỗi nêu khóa | `TestBootPurposeMismatchFails` | `go test -race -count=1 ./tests/e2e/` | PASS |
| Không vỡ cổng kiểm | — | `make ci CRM_PG_PORT=5541 CRM_S3_PORT=8441` (sau khi stage) | build, vet, lint "0 issues", gen-check, sqlc-vet, toàn bộ `go test -race ./...` ok, oasdiff, catalog-check; jscpd lần đầu báo 1 đoạn trùng (`randomBytes` giống `tests/probe`) → rút gọn, "Found 0 clones"; gitleaks "no leaks found"; govulncheck "No vulnerabilities found"; `test-guard main` báo tệp realtime của Task 16 (có trên `main` mới, chưa gộp vào nhánh này — không phải test bị xóa ở đây) |

## Test bị xóa hoặc tắt

Không xóa hay tắt test nào. Đổi vì hành vi đổi theo việc treo của Task 25:
- `internal/platform/config/m1_test.go`: `TestLoadM1WithoutNewVariablesStillValid` (api/worker không khai S3 vẫn hợp
  lệ) đổi thành `TestLoadM1OptionalVariablesHaveDefaults` (khai S3, kiểm mặc định các biến tùy chọn);
  `TestLoadS3GroupAllOrNothing` (khai một biến thì phải đủ nhóm) thay bằng `TestLoadS3RequiredForFileRoles` (luật mới
  chặt hơn: thiếu cả nhóm cũng lỗi, nêu đủ bốn tên). Dữ liệu thử của worker/api (`base()` → `workerEnv()`, `apiEnv()`)
  thêm nhóm S3.
- `tests/e2e/boot_test.go`: `roleEnv` thêm biến S3 cho api, worker; `TestHealthcheckCommand` dùng database đã migrate (Q14).

## Khác kế hoạch

- Thêm `InspectUpload`, `ApplyUpload`, `UploadCheck` (Q3); `Describe`/`Info`/`VariantURL` cho `getFile` và kết quả
  `completeUpload`; `Registry`, `NewRegistry`, `M1Purposes`, `CheckPurposes` (Q2); `ProcessKind()`,
  `ProcessPrivateKind()` (Q9); `MemStore.Versions`, `Keys`, `SetBaseURL`, `NewMemStore`; `testkit.S3FileStore`,
  `testkit.S3Config`. Lỗi thêm: `ErrUploadNotAllowed`, `ErrInvalidFile`, `ErrPurposeMismatch`.
- Cột `deleted_by` (Q8); ràng buộc thêm (Q12). `UploadInput.TenantID` (Q7).
- `Preset(version)` là hàm trả `[]VariantSpec` (kế hoạch Task 43 ghi `files.Presets[…]`); không có biến toàn cục.
- Thêm `tests/e2e` `TestBootPurposeMismatchFails`; `cmd/crm` kiểm mục đích lúc khởi động.
- Tệp test thêm ngoài danh sách: `helpers_test.go` (gói files).

## Việc phát hiện thêm

- Đường dẫn ký sẵn dùng `S3_ENDPOINT` làm host (chữ ký SigV4 gồm host). Production: trình duyệt cần host công khai của
  kho (khác địa chỉ nội bộ api dùng), imager cần host trong mạng `imaging` (Task 40 đã ghi). Cần một biến/endpoint ký
  riêng khi làm compose production (Task 21) hoặc Task 43; chưa thêm biến vì kế hoạch chỉ có `S3_ENDPOINT`.
- Test của `files` cần SeaweedFS dev đang chạy (`make db-up` đã dựng). CI (Task M0-23) phải có dịch vụ SeaweedFS
  và bootstrap bucket, không thì các ca `s3` đỏ.
- Môi trường của phiên này (và hook Stop) không có `CRM_S3_PORT`; testkit tìm cổng qua docker (Q13). Điều phối viên
  nên đặt `CRM_S3_PORT` cùng các biến cổng khác cho phiên sau.
- `main` đã có thêm Task 16, 314, 315 sau điểm tách của nhánh này: lúc gộp cần xem `cmd/crm/main.go`,
  `internal/httpapi/server.go` (Deps), `openapi/openapi.yaml` (luật `x-authenticated` của Task 315), `errors.go`,
  `docs/danh-muc-*`, mã sinh (chạy lại `make gen`, `make catalog`).
- Hạn 1 ngày của `crm-tmp` vẫn chưa xác minh xóa thật (ghi chú của Task 40); đối tượng mồ côi khi `Put` xong mà
  `Register` lỗi chờ việc dọn Q16.

## Reviewer

Kết luận: ĐẠT (vòng 1).

- Không có phát hiện phải sửa. Reviewer đã kiểm: RLS và `Register` chặn khóa của tenant khác; `If-None-Match: *` ở
  `Put`, PUT ký sẵn, `CopyObject`; `Head` ngoài transaction (ADR-46); khóa dòng + `DedupeKey` cho complete; R10 không
  ký tới bản thô; migration chỉ thêm, quyền `crm_app` theo cột; map lỗi chỉ ở `errors.go`; mọi dòng bằng chứng.
- Ghi chú không bắt buộc (để lại, không ảnh hưởng tính đúng lúc này): `Register` trả id cũ khi khóa đã có dòng mà
  không so mục đích hay `deleted_at` (khóa `ServerKey` xác định duy nhất ID); `Register` chỉ kiểm tiền tố `priv/`,
  chưa kiểm bucket của mục đích (chưa có mục đích standard nào ngoài `private` đi đường này); `InspectUpload` mở
  `WithTenant` không kèm `db.TxAPI` (dùng giới hạn mặc định của pool); `putUrl` lưu trong khóa chống trùng (Q5); host ký
  sẵn là `S3_ENDPOINT` (đã ở "Việc phát hiện thêm").
