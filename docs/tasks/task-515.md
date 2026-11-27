# Task 515 — Kho tệp xuất crm-exports (bucket riêng, tự xóa) cho M6

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (G5; khối "Chốt thay chủ shop 27/11/2026 ~11:20",
dòng G5 kho tệp xuất). Loại việc (quy trình mục 3): A (hạ tầng + gói `files`, vùng rủi ro cao).

Điều kiện "M1–M5 đóng" của M6-00 được miễn: chủ shop 27/11 sáng duyệt M6 và cho làm luôn dù M5 chưa đóng mốc (221 còn
chờ staging). Task này phải xong trước Task 237 (M6-14).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** trong `internal/platform/files`:
  `BucketExports` (hằng `Bucket`), `ExportReportsPrefix`, `ExportCustomersPrefix` (hằng chuỗi), `ExportKey(prefix,
  tenantID, name)`, trường mới `Purpose.KeyPrefix`. Không đổi chữ ký nào có sẵn. Hành vi đổi: `Bucket.valid()` nhận
  thêm `exports` (dùng bởi `Purpose.validate`, `MemStore`, `S3Store.bucket`); `Purpose.validate` thêm luật cho bucket
  exports; `samePurpose` so thêm `KeyPrefix`; `Register` (ghi tệp máy chủ) với mục đích ở bucket exports đòi khóa
  `<KeyPrefix><tenantId>/…` thay cho `priv/<tenantId>/…`. Các `files.Purpose{…}` có sẵn (cash, content, customer,
  order, fulfillment, conversation ×2) không đặt `KeyPrefix` và không ở bucket exports nên không đổi gì.
- Gói phụ thuộc ngược (go list): mọi gói nhập `internal/platform/files`: `cmd/crm`, `internal/httpapi`,
  `internal/modules/{cash,catalog,content,conversation,customer,fulfillment,order}` (+ gói con),
  `internal/queries/webcatalog`, `internal/platform/files/filestest`, `tests/drills`, `tests/e2e`. Chỉ biên dịch lại;
  không gói nào dùng bucket mới.
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: migration mới `0606_files_exports_bucket.sql`, pha mở rộng: nới CHECK `bucket` của
  `platform.file_purposes` và `platform.files` thêm giá trị `exports` (giữ bốn giá trị cũ); `UPDATE
  platform.file_purposes SET bucket = 'exports' WHERE key IN ('report_export','customer_export')` (0 dòng khi Task 226
  chưa gộp; trỏ hai mục đích M6 sang bucket mới khi đã gộp). Không câu sqlc mới. CHECK `platform.file_variants.bucket`
  (`public`, `private`) không đổi: tệp xuất không có biến thể ảnh.
- Cấu hình: `internal/platform/config` thêm hằng `ReportExportObjectTTL`, `CustomerExportObjectTTL`; trần của
  `REPORT_FILE_TTL` hạ từ 720h xuống 168h, `CUSTOMER_EXPORT_TTL` từ 168h xuống 24h (QĐ-515-7). `Config.Report` không
  đổi kiểu; chỉ `cmd/crm` đọc (chưa dùng hai trường này).
- Hạ tầng: `docker/seaweedfs/bootstrap.sh` (bucket `<tiền tố>exports`, không giữ phiên bản, chính sách từ chối việc
  nới cấu hình và mọi việc xóa với crm-app, hai luật hết hạn theo tiền tố, bật TTL fast path), `docker/seaweedfs/s3.json`
  (crm-app Read/Write/List `crm-exports`; crm-filesync không có quyền gì), `docker/compose.prod.yml`
  (chỉ chú thích tên bucket), `docs/RUNBOOK.md` (bước máy thử/thật cho Task 244), `docs/do-luong/seaweedfs-m1.md` (số đo).
  Filesync (`scripts/filesync/lib.sh`) không đổi: đã chỉ chép ba bucket `public originals private`; test khóa điều đó.
  `docker/compose.dev.yml` không đổi: dịch vụ `s3-bootstrap` đã gắn `bootstrap.sh`, `s3.json` của repo. Tài liệu M6:
  `docs/threat-model-m6.md` (hai dòng nhóm I), `docs/m6/doi-chieu-gia-dinh.md` (G5 đã chốt); `docs/danh-muc-dung-chung.md`
  sinh lại (`make catalog`).
- Test sẽ chạy: `go test -race -run` các test mới/sửa của `internal/platform/files` (keys, purpose, exports_bucket: MemStore và S3Store
  thật); `go test ./tests/deploy/ -run` (bootstrap, s3.json, filesync bỏ qua); `go test -tags probe ./tests/probe/
  -run 'TestExports'` trên SeaweedFS thật của bộ Docker crm-t515; test migration (tests/arch, gói files). Cả gói
  `files` (vùng rủi ro cao) chạy ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `files`
  (`ServerKey`, `TmpKey`, `serverPrefix`, `Register`, `RegisterPurpose`); `bootstrap.sh` (`must`, `policy`, mẫu
  fast path của crm-tmp); `tests/probe/tmp_expiry_test.go` (`weedShell`, `metaOf`, `volumeTTLs`, `requireChunksIn`,
  `appClient`); `tests/probe/seaweedfs_test.go` (`requireDenied`, kiểm việc nới cấu hình); `tests/deploy`
  (`sourceLib`, `config`).
- Dùng lại / viết mới vì: dùng lại toàn bộ hàm của `bootstrap.sh` và test probe; `ExportKey` viết mới cạnh
  `ServerKey` (cùng tệp `keys.go`) vì luật hết hạn S3 chỉ khớp tiền tố cố định ở đầu khóa, nên khóa tệp xuất phải bắt
  đầu bằng `reports/`/`customers/`, không bằng `priv/<tenant>/` (lý do đo ở mục Quyết định).

## Soát thiết kế

- 5.1 Chủ dữ liệu: bucket và cột `bucket` thuộc `platform/files` (M1); mục đích `report_export`, `customer_export` do
  M6 (reporting) đăng ký, migration của task chỉ đổi bucket của hai dòng đó trong bảng danh mục của `files`.
- 5.2, 5.3: không ghi bảng module khác; không đọc view.
- 5.4–5.6: không áp dụng: không có transaction nghiệp vụ, không gọi ra ngoài mới (kho tệp vẫn qua `FileStore`).
- 5.7 Ngữ cảnh shop: `Register` vẫn đọc tenant từ ngữ cảnh transaction và đòi khóa chứa đúng tenant đó
  (`<KeyPrefix><tenantId>/`), giữ luật cách ly tenant của khóa riêng tư.
- 5.8, 5.9: không áp dụng (không HTTP, không đường ghi API).
- 5.10: không áp dụng.
- 5.11: không thêm thư viện; luật vòng đời là tính năng của SeaweedFS 4.48 đã ghim.
- 5.12: theo mẫu crm-tmp (bucket không phiên bản + fast path) của Task 410.
- 6.1, 6.2: `Register` lặp lại cùng khóa vẫn trả id cũ (không đổi); kho không ghi đè (`Put` → `ErrObjectExists`).
- 6.3–6.11: không áp dụng (không khóa dòng, không việc nền, không tiền).
- 6.12 Dữ liệu cá nhân: danh sách khách có SĐT → bucket không giữ phiên bản, không chép kho ngoài, tự xóa sau 1 ngày
  (luật `customers/`), crm-app không xóa vĩnh viễn được, crm-filesync không đọc được bucket; tên khóa không chứa dữ
  liệu cá nhân (tenant, mã lần chạy, mã băm).
- 6.13 Quyền: crm-app chỉ Read/Write/List bucket này, bị chính sách bucket từ chối DeleteObject, DeleteObjectVersion,
  PutBucketVersioning, PutLifecycleConfiguration, PutBucketPolicy, DeleteBucketPolicy, PutBucketCors, DeleteBucketCors,
  DeleteBucket (đặt lại luật hết hạn hay bật phiên bản là việc của crm-admin).

## Quyết định thay chủ shop

| Mã | Câu hỏi | Chọn | Vì sao | Phương án khác |
|---|---|---|---|---|
| QĐ-515-1 | Luật tự xóa theo tiền tố có chạy thật trên `weed server` không | Hai luật S3 `Expiration` lọc theo tiền tố (`reports/` 7 ngày, `customers/` 1 ngày) + bật `s3.bucket.lifecycle.fastpath` cho cả bucket | Đo 27/11 trên SeaweedFS 4.48 (bộ crm-t515): với fast path, PutObject gắn `ttlSec` theo luật khớp tiền tố của khóa: `reports/…` 604800, `customers/…` 86400, khóa ngoài hai tiền tố 0. Filer, volume tự xóa như crm-tmp (Task 410), không cần lifecycle worker | Chạy `weed worker` + `weed admin` (thêm tiến trình, tài nguyên); TTL theo thư mục filer (`fs.configure`), cấu hình ngoài S3, khó kiểm lại |
| QĐ-515-2 | Khóa tệp xuất | `reports/<tenantId>/…`, `customers/<tenantId>/…` (`files.ExportKey`) thay cho `priv/<tenantId>/reports/…` của QĐ-223-2 | Luật S3 chỉ khớp tiền tố ở đầu khóa, không có ký tự đại diện cho tenant. Vẫn giữ tenant ngay sau tiền tố nên `Register` kiểm được khóa thuộc tenant ngữ cảnh | Một luật cho mỗi tenant (phải sửa luật khi thêm tenant); fast path 7 ngày cả bucket (danh sách khách nằm 7 ngày, trái 24 giờ của Q12) |
| QĐ-515-3 | Mục đích nào được dùng tiền tố nào | Trường mới `Purpose.KeyPrefix`, bắt buộc với bucket exports và chỉ nhận một trong hai tiền tố; `Register` đòi khóa bắt đầu bằng `KeyPrefix + tenant + "/"` | Hạn xóa đi theo tiền tố; khóa của danh sách khách lỡ đặt `reports/` sẽ nằm 7 ngày. Ràng ở mục đích nên lỗi lập trình bị chặn lúc ghi, không cần M1 biết tên mục đích M6 | Chỉ kiểm khóa có một trong hai tiền tố (không chặn được nhầm hạn) |
| QĐ-515-4 | Biến môi trường mới cho tên bucket | Không thêm: tên thật = `S3_BUCKET_PREFIX` + `exports`, như bốn bucket cũ | Một nguồn tên, `S3Store` đã dựng tên như vậy; thêm biến là thêm chỗ lệch | `S3_EXPORTS_BUCKET` riêng |
| QĐ-515-5 | Trỏ hai mục đích M6 sang bucket mới khi Task 226 đang làm song song (0604 ghi `private`) | Migration `0606` nới CHECK và `UPDATE … SET bucket = 'exports'` hai dòng (0 dòng nếu 226 chưa gộp). Phía Go (`reporting/purposes.go`: `Bucket: files.BucketExports`, `KeyPrefix`) đổi trong task này nếu 226 đã gộp vào main lúc gộp main; chưa thì ghi ở "Việc phát hiện thêm" cho điều phối viên | Không sửa việc của phiên khác; migration đúng ở cả hai thứ tự gộp | Đợi 226 gộp rồi mới làm (chặn Task 237) |
| QĐ-515-7 | `REPORT_FILE_TTL` (Task 225: tới 720h), `CUSTOMER_EXPORT_TTL` (tới 168h) dài hơn hạn kho tự xóa | Hạ trần: `REPORT_FILE_TTL` ≤ `config.ReportExportObjectTTL` (168h), `CUSTOMER_EXPORT_TTL` ≤ `config.CustomerExportObjectTTL` (24h); mặc định giữ nguyên; test khớp bootstrap như `TmpObjectTTL` | Ứng dụng không được hứa tải tệp mà kho đã xóa (404 thay cho 410). Chưa máy nào đặt hai biến (Task 225 mới gộp, M6 chưa chạy) nên hạ trần không phá gì | Kho tự xóa theo biến (đọc biến lúc bootstrap: thêm phụ thuộc giữa ứng dụng và hạ tầng) |
| QĐ-515-6 | crm-app có được xóa thường (DeleteObject) trên crm-exports | Không: chính sách từ chối cả DeleteObject | Bucket không giữ phiên bản, xóa thường là xóa vĩnh viễn; ADR-36 không cho ứng dụng xóa vĩnh viễn. Dọn là việc của luật hết hạn; ứng dụng chặn tải sau hạn bằng `files.MarkDeleted`/`ExpiresAt` | Cho xóa để dọn sớm (trái ADR-36) |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Bucket crm-exports được tạo, không giữ phiên bản | `TestExports_NotVersioned` | `go test -tags probe -count=1 -v -run 'TestExports_…' ./tests/probe/` (SeaweedFS 4.48 của bộ crm-t515, dựng lại từ volume rỗng) | `KẾT QUẢ: crm-exports không giữ phiên bản` `--- PASS`; trước khi sửa: `NoSuchBucket` |
| Luật hết hạn đúng: reports/ 7 ngày, customers/ 1 ngày, fast path bật | `TestExports_LifecycleRules` | như trên | `KẾT QUẢ: reports/ 7 ngày, customers/ 1 ngày, fast path bật` `--- PASS` |
| Tệp ghi thật mang hạn theo tiền tố | `TestExports_UploadStampedByPrefix` | như trên | `reports/ → ttlSec 604800, volume 6 Ttl 1w`; `customers/ → ttlSec 86400, volume 8 Ttl 1d` `--- PASS` |
| crm-app không xóa, không nới cấu hình crm-exports | `TestExports_AppCannotDelete`, `TestAppCannotLoosenBuckets` (thêm `crm-exports`) | như trên | `crm-app DeleteObject trên crm-exports → AccessDenied`; `--- PASS: TestAppCannotLoosenBuckets` |
| Filesync bỏ qua crm-exports | `TestExports_NotSyncedOffsite` (probe); `TestFilesyncLib_SkipsExports`, `TestSeaweedIdentities_ExportsBucket` (deploy) | probe như trên; `go test -count=1 -run 'Exports\|SkipsExports' ./tests/deploy/` | `crm-filesync không đọc crm-exports; kho ngoài không có crm-exports`; `ok …/tests/deploy 0.575s`; trước khi sửa s3.json: `crm-app thiếu Read:crm-exports` |
| bootstrap.sh đúng và chạy lại không lỗi | `TestSeaweedBootstrap_ExportsBucket`; `docker compose -p crm-t515 … restart s3-bootstrap` | như trên | PASS; log lần hai: `tạo crm-exports: 409` … `hạn TTL fast path crm-exports: bật`, `main xong` |
| Ứng dụng ghi/đọc/ký tải được ở bucket exports (MemStore + SeaweedFS thật); migration 0606 nhận `exports` | `TestRegister_ExportsBucket/{mem,s3}`, `TestPurposes_TableConstraints` | `go test -race -count=1 -run '^(TestRegister_ExportsBucket\|TestRegister_ExportsKeyRefusals\|TestPurposes_TableConstraints\|TestRegisterPurpose_PanicsOnInvalid\|TestRegisterPurpose_PanicsOnEachChangedField\|TestKeys_Formats)$' ./internal/platform/files/` | `ok …/internal/platform/files 4.868s` |
| Register ép đúng tiền tố của mục đích và tenant (8 ca) | `TestRegister_ExportsKeyRefusals` | như trên | PASS (8 ca con) |
| Luật mục đích exports (5 ca) và samePurpose so `KeyPrefix` | `TestRegisterPurpose_PanicsOnInvalid`, `TestRegisterPurpose_PanicsOnEachChangedField/KeyPrefix` | như trên | PASS |
| Định dạng khóa | `TestKeys_Formats` (2 ca mới) | như trên | PASS |
| Hạn ứng dụng không vượt hạn kho, khớp bootstrap | `TestLoad_ReportBadValues` (3 ca mới), `TestExportObjectTTLMatchesBootstrap` | `go test -count=1 -run '^(TestExportObjectTTLMatchesBootstrap\|TestTmpObjectTTLMatchesBootstrap\|TestLoad_Report.*)$' ./internal/platform/config/` | `ok …/internal/platform/config 0.384s` |
| Test kiến trúc | `./tests/arch/` | `go test -count=1 -skip TestLint ./tests/arch/`; `go test -count=1 -run TestLint ./tests/arch/` | `ok 11.423s`; `ok 23.257s` |
| Cổng tĩnh + toàn bộ test (migration có DROP CONSTRAINT → ALL) trên bản đã gộp main `6848b446` | cổng tĩnh, `go test` ALL | ci-task của quy trình (bước 5) | `static: success`, `test (1): success`, `test (2): success` … `CI-TASK XANH (từ xa trên blacksmith, so với điểm tách nhánh 6848b446…)` (run 45944079877) |

## E2E viết sẵn, chưa chạy

(không có)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Khóa tệp xuất đổi từ `priv/<tenantId>/reports|customers/…` (QĐ-223-2) sang `reports|customers/<tenantId>/…`
  (QĐ-515-2); bucket của hai mục đích M6 từ `private` sang `exports` (G5 chốt (a)).

## Việc phát hiện thêm

- **Task 226 (M6-03) chưa gộp lúc gộp main của task này** (main `6848b446`): khi 226 gộp, `internal/modules/reporting/purposes.go`
  phải đổi `Bucket: files.BucketPrivate` → `files.BucketExports` và thêm `KeyPrefix: files.ExportReportsPrefix`
  (`report_export`), `files.ExportCustomersPrefix` (`customer_export`); không thì `files.CheckPurposes` lệch với bảng
  (migration `0606` đã đổi hai dòng sang `exports`) và `TestPurposes_GoMatchesTable` đỏ. Không phải sửa `0604`.
- **Task 237 (M6-14), 238 (M6-15):** khóa tệp là `files.ExportKey(p.KeyPrefix, tenant, runID + "/" + sha256hex + ".xlsx")`
  (thay `priv/<tenant>/reports/…` của QĐ-223-2); `ExpiresAt` tính từ một mốc lấy **trước** `Put` (hạn kho tính từ lúc
  ghi đối tượng, nên `expires_at` không được muộn hơn hạn kho vài giây).
- Công cụ (không sửa ở task này): `go test ./tests/arch/` quá hạn 10 phút trên máy tải cao để lại `golangci-lint` mồ côi
  (cha là PID 1) giữ khóa lint; lần chạy sau của `TestLint*` treo theo. Đã dừng đúng hai PID của phiên này; chạy lại
  `TestLint*` riêng xanh trong 23 s.

## Reviewer

Kết luận: ĐẠT (lượt 1, 27/11/2026, trên phần đã stage sau khi gộp main `6848b446`).

- Phát hiện phải sửa: không có. Ảnh hưởng khớp thực tế; không chữ ký cũ nào bị đổi; không test nào bị xóa, tắt, nới.
- Ghi chú không bắt buộc: (1) `0606` dựng lại CHECK trên `platform.files` giữ khóa ACCESS EXCLUSIVE lúc quét bảng, như
  tiền lệ `0300`; bảng nhỏ, giữ nguyên. (2) Bước máy thử/thật trong RUNBOOK chưa chạy thật: việc của Task 244, khi đó
  trích kết quả. (3) Task 237: `expires_at` lấy mốc trước `Put` (kế hoạch dòng 742 đang tính sau `Put`), đã ghi ở
  "Việc phát hiện thêm". (4) Khi gộp Task 226, `TestPurposes_GoMatchesTable` đỏ tới khi `reporting/purposes.go` đổi
  sang `BucketExports` + `KeyPrefix`, đã ghi ở "Việc phát hiện thêm". `verify.go` (`mainTargets`) bỏ qua tệp có
  `ExpiresAt`: Task 237 đặt `ExpiresAt` như kế hoạch.

## Gộp vào main

Phiên gộp tuần tự, 27/11/2026. Main trước khi gộp: `68a0f7a4` (đã có Task 225, 226, 200, 513).

- **Xung đột git:** không có (`git merge --no-ff --no-commit task-515` tự gộp được, kể cả
  `internal/platform/files/purpose_test.go`). Số migration không trùng (0601, 0603, 0604 của 226; 0606 của 515).
- **Xung đột ngữ nghĩa (đã báo trước ở "Việc phát hiện thêm" của task này):** Task 226 đã gộp vào main với
  `internal/modules/reporting/purposes.go` đăng ký `report_export`, `customer_export` ở `files.BucketPrivate`, còn
  migration `0606` của task này đổi hai dòng danh mục sang `exports`; để nguyên thì `files.CheckPurposes` lệch bảng lúc
  khởi động và `TestPurposes_GoMatchesTable`, `TestFilePurposes_ExportRows` đỏ. Giải: `reporting/purposes.go` đổi sang
  `files.BucketExports` + `KeyPrefix` (`files.ExportReportsPrefix` cho `report_export`, `files.ExportCustomersPrefix`
  cho `customer_export`); `reporting/purposes_test.go` kiểm dòng `bucket = 'exports'`, `KeyPrefix` đúng từng mục đích,
  khóa tệp dựng bằng `files.ExportKey(prefix, tenant, …)` (khóa `priv/…` cũ bị `files.Register` từ chối ở bucket
  exports). Không sửa `0604` (migration đã commit). Không thêm tính năng.
- **Kiểm trên máy:** `go build ./...`, `go vet` các gói `reporting`, `platform/files`, `platform/config`, `cmd/crm`: qua.
- **Test từ xa (bể ubicloud; blacksmith hết phút tháng):**
  - Lượt 1, tree `15919fe3`, gói `internal/platform/files`, `internal/platform/config`, `internal/modules/reporting`,
    `cmd/crm`, `tests/deploy`, `tests/arch` (-race: files, config, reporting), run 45946050879: `MERGE-TEST ĐỎ (ubicloud)`,
    chỉ `TestLintCleanOnRealCode` (gofumpt ở `reporting/purposes.go:21`, `purposes_test.go:36`, do phần sửa gộp);
    mọi test khác xanh. Sửa: chạy `gofumpt -w` hai tệp (chỉ xuống dòng literal).
  - Lượt 2, tree `c4ab0385`, gói `tests/arch`, `internal/modules/reporting` (-race: reporting), run 45947659860:
    static success, test (1), (2) success → `REMOTE-TEST XANH`, `MERGE-TEST XANH (ubicloud)`.
  - Không có test chập chờn.
- **Reviewer (bản gộp):** ĐẠT lượt 1 (phần sửa ngữ nghĩa: sổ Go khớp bảng sau 0604 + 0606, `files.Register` nhận khóa
  `ExportKey`, không còn chỗ nào của 226 giả định bucket private); ĐẠT lại sau gofumpt (chỉ đổi định dạng). Phát hiện
  phải sửa: không có. Ghi chú không bắt buộc: chú thích `cmd/crm/main.go:647`, `cmd/crm/purposes_test.go:11` chỉ nhắc
  0604 (không sửa, không ảnh hưởng tính đúng).
