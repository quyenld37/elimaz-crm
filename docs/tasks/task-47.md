# Task 47 — Nhập Excel: tạo bản nhập, kiểm, xem trước

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 47, M1-47; phần áp dụng ở Task 323). Loại việc (quy trình mục 3): A. Vùng rủi ro cao: không (chỉ dùng `commands`, `queue`, `tenancy` qua API công khai, không sửa các gói đó).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào đã có. `importer.Refs` (11 chỗ, chỉ trong gói `importer` và test của nó) được **thêm** trường `ExistingProducts` (biến thể có sẵn theo mã mẫu) — chỗ dựng `Refs` cũ vẫn biên dịch. `importer.Validate` đổi hành vi: thêm lỗi `option_taken` (màu/size trùng biến thể có sẵn không có trong tệp) và đặt `ExistingProductID` theo mã mẫu. `catalog.ErrFileNotReady` (7 chỗ) chỉ mở rộng doc (tệp nhập không đúng mục đích hay chưa `uploaded`). Thêm mới: `catalog.Writer.CreateImport`, `catalog.GetImport`, `catalog.ImportTemplate`, `catalog.NewImportValidator`, `catalog.ImportValidateKind`, kiểu `Import`, `ImportSummary`, `ImportError`, hằng trạng thái; `files.ReadContent` (đọc nội dung tệp đã tải lên của một mục đích, có trần byte).
- Gói phụ thuộc ngược (go list): `catalog` ← `cmd/crm`, `internal/httpapi`, `internal/modules/inventory/invtest`, `internal/modules/order`; `files` ← `cmd/crm`, `internal/httpapi`, `catalog`, `files/filestest`, `testkit`; `importer` ← `catalog`.
- Operation OpenAPI bị chạm, oasdiff: thêm `POST /api/v1/catalog/imports` `createCatalogImport` (202), `GET /api/v1/catalog/imports/{id}` `getCatalogImport`, `GET /api/v1/catalog/imports/template` `getCatalogImportTemplate` (tệp `.xlsx`), cả ba `x-permission: catalog.import`; schema `CreateCatalogImportRequest`, `CatalogImport`, `CatalogImportSummary`, `CatalogImportError`. Chỉ thêm: oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: chưa có màn nhập (mốc sau); `console/src/api` sinh lại có thêm kiểu.
- Câu sqlc, bảng, pha migration: migration mới `0127_catalog_imports.sql` (pha mở rộng: một bảng, index, quyền, RLS). `queries/catalog/imports.sql` mới (`InsertImport`, `GetImport`, `GetImportForValidation`, `LockImport`, `FindAppliedImportBySHA`, `FinishValidation`, `FailValidation`, `ListVariantsBySKUs`, `ListVariantsByProductCodes`). `cmd/crm`: kho S3 của worker dựng không điều kiện (trước chỉ khi có `IMAGER_URL`; cấu hình S3 vốn đã bắt buộc). Model sqlc của mọi module sinh lại (thêm `CatalogImport`). Dùng lại `ListCategories`, `ListAttributes`, `ListAttributeValues`, `ListTags`, `GetFile` (qua `files`).
- Test sẽ chạy: `go test -race ./internal/modules/catalog/...` (gói đổi, gồm `importer`), `./internal/platform/files/...`, `./internal/platform/xlsx/...` (siết test theo ghi chú reviewer Task 38), `./internal/httpapi/...`, `./cmd/crm/...`, `./internal/modules/order/...`, `./internal/modules/inventory/...`; `tests/arch`; `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `xlsx` (`Read`, `Write`, `Limits`), `files` (`Status`, `FileStore.Open`, `MemStore`), `commands` (`CommandKind`, `Handler`, `Looker`, `queue.TerminalHook`), `queue.JobQueue.Enqueue` (DedupeKey), `httpapi.WriteTx`/`refusableWrite` + `catalogWrite`, `identity.RecordAudit` qua `Writer.audit`. Tìm `import`, `sha256`, `ReadAll` trong repo: không có hàm đọc nội dung tệp đã tải lên cho module (chỉ `Processor.hash` nội bộ).
- Dùng lại / viết mới vì: dùng lại `xlsx.Read` + `importer.Validate` + `importer.MergeCellErrors` + `importer.Template` (Task 38, không viết bộ đọc thứ hai); `ListCategories`/`ListAttributes`/`ListTags` để dựng `Refs`; đường ghi HTTP `catalogWrite`; khuôn lệnh của `files.Processor` (`Lookup`, `OnTerminal`). Viết mới: `files.ReadContent` (bucket, khóa của tệp là nội bộ của `files`, module không đọc bảng `platform.files`), bảng và lệnh `catalog.import.validate`.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: `catalog.imports` thuộc catalog (schema `catalog`), mã trong `internal/modules/catalog`.
- 5.2 Không ghi bảng module khác: chỉ ghi `catalog.imports`, `platform.jobs` qua `queue.Enqueue`, audit qua `identity.RecordAudit`; tệp chỉ đọc qua `files.Status`, `files.ReadContent`.
- 5.3 Đọc module khác: khóa ngoại `(tenant_id, file_id) → platform.files` là ngoại lệ ADR-27 mục 4 như `product_images`; SQL của catalog không đọc bảng kho tệp.
- 5.4 Nguyên tử: tạo dòng `validating` + lệnh + audit + khóa chống trùng trong một `WriteTx`.
- 5.5 Không nguyên tử đi sau commit: kiểm tệp là lệnh `catalog.import.validate` chạy ở worker.
- 5.6 Gọi ra ngoài: chỉ đọc kho tệp (không tác dụng phụ). Lệnh `commands` (in_doubt khi hết giữ chỗ), `AutoRetryNotSent = true`, `Lookup`: bản nhập đã rời `validating` → done, còn `validating` → not_sent (chạy lại an toàn: câu ghi kết quả có điều kiện `status = 'validating'`).
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant` (tenant của người gọi hay của lệnh); bảng mới có `tenant_id`, `UNIQUE (tenant_id, id)`, unique khác bắt đầu bằng `tenant_id`, khóa ngoại ghép, RLS.
- 5.8 Nghiệp vụ không biết HTTP: `imports.go` không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi `createCatalogImport` có `IdempotencyKey`, `x-permission: catalog.import`; hai đường đọc cũng có `x-permission`.
- 5.10 Valkey: không áp dụng (không dùng).
- 5.11 Thư viện mới: không (excelize đã có từ Task 38, chỉ qua `xlsx`).
- 5.12 Module mẫu: Writer + `version` + sentinel (Task 27), đường ghi Task 12, lệnh như `file.process`.

Mục 6:
- 6.1 Đồng thời: lệnh ghi kết quả khóa dòng `imports` (`FOR UPDATE`, nhóm 4 đầu tiên theo ADR-46) và chỉ ghi khi còn `validating`; hai lần chạy lệnh (lease hết, tra lại) không ghi đè nhau. Kiểm "đã áp" (`applied` cùng `file_sha256`) trong cùng transaction ghi. `version` tăng mỗi lần đổi (Task 323 dùng cho `apply`).
- 6.2 Gửi lại: `Idempotency-Key` + `WriteTx`; DedupeKey của lệnh = mã bản nhập (một lệnh mỗi bản nhập).
- 6.3 Thứ tự khóa: chỉ khóa một dòng `catalog.imports`; không khóa mẫu, biến thể (chỉ đọc).
- 6.4 Lỗi tạm DB: `WriteTx`/`RetryTx` chạy lại; lỗi đọc trong lệnh → not_sent, lịch thử lại.
- 6.5 Mức cách ly: READ COMMITTED; Refs đọc lúc kiểm có thể cũ khi áp — Task 323 kiểm lại `plan` với database lúc áp (đã có trong kế hoạch).
- 6.6 Việc nền lỗi: 8 lượt (CommandKind); tệp không đọc được (mất, không phải tệp nhập) → rejected → dead → `OnTerminal` đặt `failed`; tệp hỏng/không phải xlsx/quá lớn → `invalid` với lỗi cấp tệp (không thử lại vô ích).
- 6.7 Quá tải: không áp dụng (kho tệp nội bộ; lỗi kho → not_sent theo lịch).
- 6.8 Chết giữa chừng: in_doubt → `Lookup` quyết định; transaction tự lùi.
- 6.9 Treo: hạn một lần 30 s; đọc tối đa 5 MiB, 20.000 dòng (giới hạn `xlsx`); transaction chỉ đọc/ghi ngắn, đọc và kiểm ngoài transaction.
- 6.10 Tiền: giá `int64` đồng, đã kiểm ở `importer` (≤ 2^53−1).
- 6.11 Quy luật: không vùng rủi ro cao; quy luật "lỗi → không có plan", "duplicate → không có plan" có test cụ thể. Rapid cho áp dụng ở Task 323.
- 6.12 Dữ liệu cá nhân: tệp chỉ có dữ liệu sản phẩm; `plan`, `errors` không có dữ liệu cá nhân; câu lỗi không chép ô quá 50 ký tự; audit chỉ ghi mã bản nhập, mã tệp; `last_error` của lệnh chỉ là mã lý do.
- 6.13 Quyền: `catalog.import` (ScopeNone) ở `x-permission` cả ba operation; guard mặc định từ chối; test 403 khi chỉ có `catalog.manage`.

## Quyết định thay chủ shop

1. **`file_sha256` cho phép rỗng khi `validating`/`failed`** (CHECK: các trạng thái khác phải có, dài 32 byte). Vì: tệp nhập không phải ảnh nên `platform.files.content_sha256` rỗng, mã băm chỉ biết khi lệnh đọc tệp. Phương án khác: băm trong yêu cầu HTTP (đọc 5 MiB từ kho trên đường yêu cầu, trái "Execute tải tệp" của kế hoạch). Đảo ngược: migration sau `SET NOT NULL` khi đã có giá trị.
2. **Lệnh ghi kết quả trong chính `Execute` (transaction ngắn, có điều kiện `validating`) thay cho `OnDone`.** Vì: `OnDone` chỉ nhận kết quả qua `Outcome.Result`, mà Runner lưu nguyên `Result` vào `platform.jobs.result` — `plan` của tệp 20.000 dòng vài MB sẽ nằm hai lần, một lần trên bảng hàng đợi nóng. Bảo đảm tương đương: ghi có điều kiện nên chạy lại không đổi gì; `Lookup` trả done khi bản nhập đã rời `validating`. Phương án khác: `OnDone` như kế hoạch.
3. **Lỗi cấp tệp** (không phải xlsx, quá lớn, quá nhiều dòng, sai tiêu đề) → `invalid` với một lỗi `{row: 0, column: "", code: file_not_xlsx | file_too_large | too_many_rows | header_mismatch}`. Tệp mất trên kho, đã xóa, sai mục đích → `failed` (qua `rejected` → `OnTerminal`).
4. **Lưu tối đa 1.000 lỗi**, `summary.errors` là tổng số lỗi (giữ dòng `imports` nhỏ với tệp sai hàng loạt).
5. **Việc treo (2) của Task 38**: thêm `Refs.ExistingProducts` (mã mẫu → mã mẫu trong DB và các biến thể có sẵn); biến thể mới trùng màu+size với biến thể có sẵn không nằm trong tệp → lỗi dòng mới `option_taken` ở cột `Size*`; mẫu có sẵn (theo mã) có `ExistingProductID` dù mọi SKU đều mới (không thì áp sẽ gặp `ErrCodeTaken`).
6. **Thuộc tính trùng tên**: `Refs.Attributes` lấy thuộc tính đầu theo vị trí; danh mục chỉ lấy `active`.
8. **"Đã áp" theo người bán**: unique `imports_applied_sha_key` là `(tenant_id, seller_id, file_sha256) WHERE status = 'applied'` thay cho `(tenant_id, file_sha256)` của kế hoạch, và tìm trùng lọc theo người bán. Vì: luật (g) ADR-29 (test `CheckSchemaRules` đỏ với bản của kế hoạch) và bản nhập ghi vào danh mục của một người bán; cùng tệp áp cho người bán khác là danh mục khác. Phương án khác: miễn luật cho bảng này (sửa danh sách miễn của test kiến trúc, cần chủ shop).
7. **Tệp sai mục đích hay chưa `uploaded`** → dùng lại `ErrFileNotReady` (409 `file_not_ready`), không thêm mã lỗi.

## Bằng chứng

Môi trường: bộ chung crm-shared (`CRM_PG_PORT=5600`, `CRM_S3_PORT=20600`); test tự tạo database riêng; kho tệp của test catalog là `MemStore`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi viết mã: test đỏ | cả bộ dưới | `go test ./internal/modules/catalog/...`, `go test -run TestReadContent ./internal/platform/files/` | build FAIL: `undefined: catalog.NewImportValidator`, `undefined: files.ReadContent`, `unknown field ExistingProducts` |
| KH01 phần kiểm: `mau-200.xlsx` → `ready`, `summary {models 200, create 1000}`, `plan` đủ 200 mẫu (có phân loại), 0 mẫu trong DB, 1 audit `catalog.import.create`, lệnh `done` | `TestApply_200Models` | `go test -race -count=1 -run 'Import\|Apply_' ./internal/modules/catalog/` | `ok … catalog 14.848s` |
| `loi-tung-dong.xlsx` → `invalid`, đúng 7 lỗi đúng dòng/cột/mã (5, 9, 12, 20, 31, 44, 50), `summary.errors 7`, không plan, 0 mẫu | `TestImport_SevenRowErrors` | như trên | PASS |
| KH01 phần kiểm: đã có bản nhập `applied` cùng `file_sha256` → `duplicate`, `duplicateOf` = dòng đó, không plan | `TestApply_SameFileTwice` | như trên | PASS |
| Dữ liệu sẵn 200 mẫu, 1.000 biến thể, tệp sửa giá 3 biến thể → `summary {models 200, update 3, unchanged 997}`, mọi mẫu có `ExistingProductID` | `TestImport_ThreePriceChanges` | như trên | PASS |
| `getCatalogImportTemplate`/`ImportTemplate` → `.xlsx` đọc lại bằng `xlsx.Read` đủ cột của mẫu Task M1-38 và 2 cột thuộc tính, 0 dòng | `TestImportTemplate`, `TestCatalogAPI_Imports` | như trên; `go test -race -count=1 -run TestCatalogAPI_Imports ./internal/httpapi/` | PASS; `ok … httpapi 6.336s` |
| Việc treo (1): lỗi ô của `xlsx.Read` gộp với lỗi dòng (`MergeCellErrors`) → `invalid` `cell_too_long` ở dòng 8, cột `Mô tả` | `TestImport_CellErrorsMerged` | như trên | PASS |
| Việc treo (2): biến thể mới trùng màu+size với biến thể có sẵn không trong tệp → `option_taken`; có trong tệp và đổi size → nhường được; mẫu có sẵn theo mã có `ExistingProductID` | `TestValidate_OptionTakenByExistingVariant`, `TestValidate_ExistingProductByCode`, `TestImport_OptionTakenInDatabase` | `go test -race ./internal/modules/catalog/internal/importer/`; như trên | `ok … importer 2.002s`; PASS |
| Việc treo (3): ca "id của hai namespace quan hệ" có `rId2` thật trong quan hệ sổ; bỏ `relID` (thử tạm `len(ids) == 0`) → ca này đỏ (`ErrHeaderMismatch` thay cho `ErrNotXlsx`), trả lại → xanh | `TestReadStructureMismatch` | `go test -race -run TestReadStructureMismatch ./internal/platform/xlsx/` | thử tạm: `FAIL … id của hai namespace quan hệ`; sau khi trả lại: `ok … xlsx 1.591s` |
| Lỗi cấp tệp: HTML đổi đuôi → `invalid`, một lỗi `{row 0, file_not_xlsx}` | `TestImport_NotXlsx` | như trên | PASS |
| Tệp mất trên kho → lệnh `dead`, bản nhập `failed` (không kẹt `validating`) | `TestImport_SourceMissingFails` | như trên | PASS |
| Chạy lại lệnh sau khi xong không đổi bản nhập (version giữ nguyên); `Lookup` → `done` | `TestImport_RerunKeepsResult` | như trên | PASS |
| Tạo bản nhập: tệp chưa tải xong, mục đích ảnh → `ErrFileNotReady`; không có, tenant khác → `ErrNotFound`; 0 dòng | `TestCreateImport_Refused` | như trên | PASS |
| HTTP: 202 `validating`, gửi lại cùng khóa → cùng bản nhập, 1 lệnh; `getCatalogImport` `{errors:[], summary{…}}`; ảnh → 409 `file_not_ready`; tenant B → 404; bản nhập lạ → 404; chỉ `catalog.manage` → 403 cả 3 operation; mẫu có `Content-Type` xlsx, `Content-Disposition` | `TestCatalogAPI_Imports` | `go test -race -count=1 -run TestCatalogAPI_Imports ./internal/httpapi/` | `ok … httpapi 6.336s` |
| `files.ReadContent`: đúng nội dung; sai mục đích, chưa tải xong → `ErrNotReady`; đã xóa, tenant khác, không có → `ErrNotFound`; vượt trần → `ErrTooLarge`; mất trên kho → `ErrObjectNotFound` | `TestReadContent` | `go test -race -run TestReadContent ./internal/platform/files/` | `ok … files 2.191s` |
| Sau gộp main bde586e (luật người bán của Task 37): hai câu JOIN có điều kiện `seller_id` | `tests/arch` (sellerfilter) | `go test -count=1 -run Seller ./tests/arch/` | `ok … tests/arch 0.723s` |
| Gói đổi và gói phụ thuộc sau gộp main | — | `go test -race -count=1 ./internal/modules/catalog/... ./internal/platform/files/... ./internal/platform/xlsx/... ./cmd/crm/...` | `ok … catalog 31.775s`, `ok … importer`, `ok … files 51.855s`, `ok … xlsx 13.866s`, `ok … cmd/crm` |
| Toàn bộ cổng kiểm, sau gộp main bde586e, trên phần đã stage | — | `GOFLAGS=-p=4 make ci` | `exit 0`: 47 gói `ok`, 0 FAIL, golangci-lint `0 issues`, `No vulnerabilities found.`, jscpd `Found 0 clones.`, gitleaks `no leaks found` |
| Lần `make ci` trước đó đỏ: (a) `gen-check` vì mã sinh chưa stage; (b) `CheckSchemaRules`: unique `imports_applied_sha_key` thiếu `seller_id` (luật (g) ADR-29) → sửa theo Quyết định 8; (c) `TestDeepHealth` (gói không bị chạm) đỏ lúc máy tải nặng, chạy riêng `-count=3` qua | `TestCheckSchemaRules*`, `TestSchemaRulesStillClean`, `TestDeepHealth` | `go test -race -count=1 -run SchemaRules ./internal/platform/tenancy/ ./internal/modules/identity/`; `go test -race -count=3 -run TestDeepHealth ./internal/platform/observability/` | `ok … tenancy 7.110s`, `ok … identity 2.630s`, `ok … observability 5.815s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ghi kết quả kiểm trong `Execute` (transaction ngắn có điều kiện) thay cho `OnDone`; `file_sha256` được rỗng khi `validating`/`failed` (Quyết định 1, 2).
- Thêm `files.ReadContent` (kế hoạch chỉ nói "`Open`, 5 MiB": module không biết bucket, khóa của tệp).
- Thêm tệp `internal/modules/catalog/imports_validate.go` (lệnh kiểm) bên cạnh `imports.go`; test HTTP ở `internal/httpapi/handlers_catalog_imports_test.go`, handler ở `handlers_catalog_imports.go` thay cho sửa `handlers_catalog.go` (tệp đó đã dài).
- Mã lỗi thêm: `option_taken`, `file_not_xlsx`, `file_too_large`, `too_many_rows`, `header_mismatch` (lỗi trong `errors` của bản nhập, không phải mã HTTP); `summary.errors`.
- Test lỗi ô dùng ô `Mô tả` quá 2.000 ký tự (`cell_too_long`) thay cho `co-cong-thuc.xlsx` (tệp đó có tiêu đề khác mẫu nên chỉ ra `header_mismatch`); ô công thức đi cùng đường `MergeCellErrors`.

## Việc phát hiện thêm

- Cho Task 323: `plan` không biết mẫu có sẵn bị đổi tên, mô tả, trạng thái, phân loại (Refs không có các trường đó) nên `summary` chỉ đếm theo biến thể; bước áp cần so lại trường của mẫu (hoặc thêm vào Refs) để audit `{create, update, unchanged}` đúng.
- Mẫu đã xóa mềm (`deleted_at`) vẫn giữ mã (`products_code_key`): tệp có mã đó được `ready` như mẫu mới, lúc áp sẽ gặp `ErrCodeTaken`. Task 323 nên đổi thành lỗi dòng (hoặc kiểm ở đây khi có quyết định cho mẫu đã xóa).
- Tệp nhập nằm ở bucket `tmp` sau khi tải lên (kho tệp không chuyển tệp không phải ảnh sang `private`); nếu `tmp` có vòng đời xóa ngắn thì bản nhập để lâu trước khi áp vẫn ổn (plan đã ở database), chỉ việc kiểm lại tệp là không được.
- Câu kiểm `INSERT` audit của bản nhập chỉ có `fileId`, `status`; Task 323 thêm audit tổng lúc áp.
- (Reviewer) Tệp quá 5 MiB → bản nhập `invalid` ghi `file_sha256` là 32 byte 0 (chưa đọc được nội dung để băm). Không sai lúc này (tìm trùng chỉ xét `applied`), nhưng ai dùng `file_sha256` sau này cần biết; có thể đổi CHECK cho phép rỗng ở `invalid`.
- (Reviewer) `ListVariantsBySKUs` không lọc mẫu đã xóa, `ListVariantsByProductCodes` có lọc: SKU của mẫu đã xóa làm plan gắn `ExistingProductID` của mẫu đã xóa. Task 323 quyết cùng với mục mẫu đã xóa ở trên.
- (Reviewer) Hai biến thể có sẵn đổi màu/size cho nhau trong cùng tệp qua được bước kiểm; Task 323 phải đổi theo thứ tự hay dùng giá trị tạm để không vướng `variants_option_key`.
- (Reviewer) Chưa đo tệp 20.000 dòng trong hạn 30 s của lệnh kiểm; đo cùng phép đo thời gian áp của Task 323.
- (Reviewer) `CreateCatalogImport` có thể dùng `catalogWriteAsDefaultSeller` (có sau khi gộp Task 37) thay cho tự gọi `DefaultSeller`.

## Reviewer

Kết luận: ĐẠT (lần 1, agent `reviewer`, trên `git diff --cached` sau khi gộp main bde586e và `make ci` exit 0).

- Không có phát hiện phải sửa. Đã soát: chạy lại/in_doubt/lease hết của lệnh kiểm (khóa dòng + điều kiện `validating`), `Lookup`, `OnTerminal`; CHECK, RLS, quyền, unique theo người bán của migration 0127; `files.ReadContent` (đọc kho ngoài transaction, chặn trần hai lần); `option_taken`; handler mỏng, không dữ liệu cá nhân trong audit/log.
- Ghi chú không bắt buộc: sửa tên câu sqlc trong mục Ảnh hưởng (đã sửa); 5 ghi chú còn lại chép vào Việc phát hiện thêm (mã băm 0 của tệp quá lớn, mẫu đã xóa, hoán đổi màu/size, đo 20.000 dòng, `catalogWriteAsDefaultSeller`). Không sửa mã sau review.
