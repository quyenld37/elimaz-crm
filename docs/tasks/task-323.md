# Task 323 — Nhập Excel: áp dụng (M1-47b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 323, M1-47b; phần kiểm, xem trước ở Task 47). Loại việc (quy trình mục 3): A. Kế hoạch ghi "vùng rủi ro cao (đường áp dụng)": không sửa gói nào trong danh sách vùng cao của CLAUDE.md (`tenancy`, `queue`, `commands`, `idempotency`, `identity`, `guard.go`), chỉ dùng API công khai; vẫn có test rapid cho quy luật tất cả hoặc không.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào đã có. Thêm mới: `catalog.Writer.ApplyImport`, `catalog.ImportApplyKind`, `catalog.NewImportApplier` (kiểu `ImportApplier`: `queue.Handler` + `queue.TerminalHook`), sentinel `catalog.ErrImportNotReady`, mã lỗi dòng lúc áp (`code_taken`, `barcode_taken`, `changed_since_validation`); trong `importer` (internal): `Recheck` (dùng lại kiểu `Refs` cho dữ liệu lúc áp); bí danh `catalog.ImportPlannedProduct`. Thêm `db.Savepoint` (`internal/platform/db/savepoint.go`, chỉ thêm; gói `db` không thuộc danh sách vùng cao) — `db` có nhiều gói phụ thuộc ngược nhưng không ký hiệu cũ nào đổi. Dùng lại không đổi: `CreateProduct`, `UpdateProduct`, `AddVariant`, `UpdateVariant`, `SetClassification` (cùng gói).
- Gói phụ thuộc ngược (go list): `catalog` ← `cmd/crm`, `internal/httpapi`, `catalog/catalogtest`, `inventory/invtest`, `order`, `testkit`; `importer` ← `catalog`.
- Operation OpenAPI bị chạm, oasdiff: thêm `POST /api/v1/catalog/imports/{id}/apply` `applyCatalogImport` (`x-permission: catalog.import`, `IdempotencyKey`, thân `VersionRequest`, 202 `CatalogImport`); sửa mô tả `createCatalogImport`, `CatalogImport.status`. Chỉ thêm: oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: chưa có màn nhập (Task 52); `console/src/api` sinh lại có thêm hàm.
- Câu sqlc, bảng, pha migration: không migration (kế hoạch). `queries/catalog/imports.sql` thêm `LockImportForApply`, `StartApply`, `LockImportApplySeller` (khóa tư vấn), `GetImportPlan`, `FinishApply`, `FailApply`, `MarkApplyDuplicate`, `LockProductsByIDs` (khóa nhiều mẫu theo id tăng dần); dùng lại `GetImportForValidation`, `FindAppliedImportBySHA`, `ListVariantsBySKUs`, `ListVariantsByProductCodes`, `BarcodeExists`, `GetProductCategory`. Bảng chạm: `catalog.imports` (UPDATE cột đã cấp quyền), `catalog.products`, `variants`, `product_attribute_values`, `product_tags`, `barcode_counters` qua hàm công khai; `platform.jobs` qua `queue.Enqueue`; `identity.audit_logs`, `platform.domain_events` qua Writer.
- Test sẽ chạy: `go test -race ./internal/modules/catalog/...` (gồm `importer`), `./internal/httpapi/ -run CatalogAPI_Import`, `./cmd/crm/...`, `tests/arch`; `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `queue` (`Handler`, `TerminalHook`, `DefaultPolicy`, `JobQueue.Enqueue` DedupeKey), `httpapi` (`catalogWriteAsDefaultSeller`, `refusableWrite`), `identity.RecordAudit` (qua `Writer.audit`); tìm `advisory`, `xact_lock`: chỉ có `identity` `LockRoleNames` (mẫu khóa tư vấn trong transaction, ADR-46 mục "Khóa tư vấn"); tìm `Savepoint`/`tx.Begin` lồng: chưa có chỗ nào dùng.
- Dùng lại / viết mới vì: ghi mẫu, biến thể, phân loại bằng chính `CreateProduct`, `UpdateProduct`, `AddVariant`, `UpdateVariant`, `SetClassification` (kế hoạch: không viết SQL ghi thứ hai) nên sự kiện `product.updated`/`price.changed`, báo hiệu, audit từng mẫu đi theo đường có sẵn; `existingRefs` của Task 47 để đọc lại SKU, mã mẫu lúc áp; `importer.RowError` cho lỗi lúc áp; `catalogWriteAsDefaultSeller` cho handler. Viết mới: `importer.Recheck` (hàm thuần so plan với dữ liệu hiện tại, test không cần database), việc transactional `catalog.import.apply`, câu khóa tư vấn theo người bán (cùng mẫu với `LockRoleNames`), câu khóa nhiều mẫu theo id tăng dần (ADR-46 luật "sửa nhiều dòng").

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: mọi bảng ghi thuộc catalog; mã trong `internal/modules/catalog`.
- 5.2 Không ghi bảng module khác: chỉ `catalog.*`; `platform.jobs` qua `queue.Enqueue`, sự kiện qua `events.Emit`, audit qua `identity.RecordAudit`.
- 5.3 Đọc module khác: không đọc.
- 5.4 Nguyên tử: cả lần áp (mọi mẫu, biến thể, phân loại, audit tổng, trạng thái `applied`) trong **một** transaction của việc transactional; lỗi nghiệp vụ giữa chừng lùi về savepoint đầu (không còn gì của lần áp) rồi ghi `failed` + lỗi dòng trong cùng transaction; lỗi khác lùi cả transaction, việc chạy lại.
- 5.5 Không nguyên tử đi sau commit: `product.updated`, `price.changed` là sự kiện ghi cùng transaction, phát sau commit (đường có sẵn của Writer).
- 5.6 Gọi ra ngoài: không (plan đã nằm trong database) → việc transactional, không phải lệnh.
- 5.7 Ngữ cảnh shop: worker chạy việc trong `tenancy.WithTenant` của tenant của việc; mọi câu mới lọc `seller_id` (ADR-29); không bảng mới.
- 5.8 Nghiệp vụ không biết HTTP: `imports_apply.go`, `importer/apply.go` không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi: `applyCatalogImport` có `IdempotencyKey`, `x-permission: catalog.import`; DedupeKey việc = mã bản nhập.
- 5.10 Valkey: không áp dụng (không dùng).
- 5.11 Thư viện mới: không.
- 5.12 Module mẫu: Writer + `version` (khóa dòng rồi so) + sentinel (Task 27); đường ghi Task 12; việc transactional có `TerminalHook` như `file.process`.

Mục 6:
- 6.1 Đồng thời: `ApplyImport` khóa dòng bản nhập (`FOR UPDATE`), so trạng thái `ready` và `version`, đổi sang `applying` có điều kiện → hai yêu cầu cùng version: đúng một qua, người sau `version_conflict`/`import_not_ready`. Handler khóa lại dòng bản nhập, chỉ chạy khi còn `applying`; hai bản nhập của cùng người bán chạy lần lượt nhờ khóa tư vấn theo (tenant, người bán) lấy trước mọi khóa dòng, nên lần sau thấy đúng dữ liệu lần trước vừa ghi (không trùng SKU, không deadlock giữa hai lần áp). Kiểm lại plan với database **sau khi** khóa mẫu có sẵn (mã mẫu có, SKU có, version biến thể khớp).
- 6.2 Gửi lại: `Idempotency-Key` + `WriteTx`; DedupeKey = mã bản nhập (một việc mỗi bản nhập); handler chạy lại thấy không còn `applying` thì xong, không ghi gì.
- 6.3 Thứ tự khóa (ADR-46 nhóm 4): khóa tư vấn → `catalog.imports` → danh mục (khóa chia sẻ trong `SetClassification`) → `catalog.products` (khóa trước mọi mẫu có sẵn của plan theo id tăng dần) → bảng nối → `variants` → `barcode_counters` → `platform.jobs` cuối. Thêm dòng khóa tư vấn vào ADR-46. Để không khóa danh mục sau mẫu, `lockPlan` khóa chia sẻ mọi danh mục của plan (id tăng dần) **trước** khi khóa mẫu; `SetClassification` khóa lại danh mục đã giữ. Biến thể được khóa theo thứ tự dòng của tệp (không theo id): an toàn vì mọi đường sửa biến thể khóa mẫu trước (ADR-46 nhóm 4), mà mọi mẫu có sẵn của plan đã bị khóa từ đầu.
- 6.4 Lỗi tạm DB: việc nền có lịch thử lại (8 lượt); `WriteTx`/`RetryTx` ở yêu cầu HTTP.
- 6.5 Mức cách ly: READ COMMITTED; kiểm lại plan sau khi giữ khóa tư vấn và khóa mẫu; SKU mới do người khác chèn cùng lúc → unique `variants_sku_key` báo lỗi → lùi savepoint → `failed` với lỗi dòng.
- 6.6 Việc nền lỗi: lỗi nghiệp vụ không thử lại (ghi `failed` + lỗi dòng, việc `done`); lỗi khác thử lại theo lịch, hết lượt → `dead` → `OnTerminal` đặt bản nhập `failed` (không kẹt `applying`).
- 6.7 Quá tải: không áp dụng (không gọi ra ngoài).
- 6.8 Chết giữa chừng: transaction tự lùi; việc về hàng chờ khi hết giữ chỗ (`LeaseRetry`), chạy lại từ đầu.
- 6.9 Treo: hạn việc 30 s; khóa tư vấn chịu `lock_timeout` 5 s (lần áp kia chạy lâu → lỗi tạm → thử lại theo lịch). Đo thời gian áp 1.000 biến thể (mục tiêu dưới 20 s) và tệp 20.000 dòng (việc treo của Task 47), ghi vào Bằng chứng.
- 6.10 Tiền: giá `int64` đồng, đã kiểm ở `importer`; ghi qua `UpdateVariant`/`CreateProduct` (kiểm lại `validMoney`).
- 6.11 Quy luật: rapid 100 lượt "số mẫu, biến thể hoặc bằng trước, hoặc bằng trước + plan" với lỗi cài ở dòng ngẫu nhiên.
- 6.12 Dữ liệu cá nhân: plan, lỗi, audit chỉ có dữ liệu sản phẩm và mã; payload việc chỉ có mã bản nhập, mã nhân viên.
- 6.13 Quyền: `catalog.import` ở `x-permission`; test 403 khi chỉ có `catalog.manage`.

## Quyết định thay chủ shop

1. **Khóa tư vấn theo (tenant, người bán) cho việc áp** (`LockImportApplySeller`, ghi vào ADR-46 mục "Khóa tư vấn"). Vì: hai bản nhập cùng tạo `AO01` chèn dòng mới (không có dòng để khóa); thứ tự chèn khác nhau trên hai unique (`products_code_key`, `variants_sku_key`) có thể deadlock. Chạy lần lượt thì lần sau kiểm lại trên dữ liệu lần trước vừa ghi → `failed` với lỗi dòng. Phương án khác: chỉ khóa dòng và để database báo deadlock rồi thử lại. Đảo ngược: bỏ một câu.
2. **Lỗi do dữ liệu đổi từ lúc kiểm không làm việc thử lại**: ghi trong một savepoint; lỗi nghiệp vụ (`ErrSKUTaken`, `ErrBarcodeTaken`, `ErrDuplicateOption`, `ErrCodeTaken`, `ErrNotFound`, `ErrVersionConflict`) → lùi savepoint (không còn gì của lần áp), bản nhập `failed` + một lỗi dòng, việc `done`. Lỗi khác (hết hạn 30 s, lỗi database, dữ liệu không hợp lệ dù đã kiểm) → lùi cả transaction, thử lại theo lịch, hết lượt → `dead` → `OnTerminal` đặt `failed`; chỉ payload hay plan hỏng → `ErrPermanent` → `dead` ngay. Phương án khác: trả lỗi để cả transaction lùi (kế hoạch "lỗi bất kỳ → cả transaction lùi; OnTerminal → failed") — nhưng như vậy nhân viên chờ 8 lượt (tới hàng giờ) và không có lỗi dòng nào; kết quả dữ liệu vẫn là "không ghi gì".
3. **Mẫu có sẵn: ô trống giữ nguyên** (mô tả, trạng thái, danh mục, thuộc tính, nhãn); ô có giá trị thì thay. Tên mẫu bắt buộc nên luôn theo tệp. Vì: an toàn (không xóa phân loại vì sót ô), giống luật cột Trạng thái của Task 38. Phương án khác: tệp là nguồn đầy đủ, ô trống là xóa.
4. **Đổi màu/size cho nhau trong cùng tệp** (việc reviewer Task 47 nêu): mọi biến thể có sẵn đổi màu+size được dời tạm sang size `~nhập dòng N` trước, rồi mới sửa, tạo theo thứ tự dòng (đổi chéo, dây chuyền, tạo vào chỗ vừa trống) (thêm một lần `UpdateVariant`, một sự kiện `product.updated`, một audit). Vì: unique `variants_option_key` không DEFERRABLE và task này không có migration. Phương án khác: migration đổi ràng buộc sang DEFERRABLE.
5. **Biến thể "giữ nguyên" mà version đã đổi từ lúc kiểm cũng làm lần áp `failed`** (`changed_since_validation`): tệp được kiểm trên dữ liệu cũ, áp sẽ âm thầm bỏ qua sửa của nhân viên kia. Phương án khác: bỏ qua biến thể giữ nguyên.
6. **Tệp cùng nội dung vừa được áp bởi bản nhập khác** (hai bản cùng tệp đều ready) → bản sau thành `duplicate` lúc áp (bỏ plan), không ghi gì.
7. **Người áp nằm trong payload việc** (`actorId`, chỉ là mã), `applied_by` ghi khi `applied`. Audit: `catalog.import.apply` (yêu cầu), `catalog.import.applied` (tổng `{create, update, unchanged, productsCreated, productsUpdated}`) hoặc `catalog.import.apply_failed` (`{errors}`), cộng audit từng mẫu/biến thể do các hàm công khai tự ghi.
9. **Savepoint qua hàm chung `db.Savepoint`** (gói `internal/platform/db`, có test `TestSavepoint`, dòng "khi nào không dùng" trong danh mục dùng chung). Vì: test kiến trúc `TestTxUsageProductCode` (ADR-46) cấm gọi `Begin` ngoài gói `db`; savepoint là bước bên trong transaction của worker, không mở transaction mới. Phương án khác: gõ `SAVEPOINT`/`ROLLBACK TO SAVEPOINT` bằng SQL trong catalog (lách test kiến trúc), hoặc bỏ savepoint (Quyết định 2 thành "chờ hết lượt").
8. Mã lỗi dòng mới lúc áp: `code_taken`, `barcode_taken`, `changed_since_validation` (cùng `sku_belongs_to_other_product`, `option_taken` đã có). Mã HTTP mới `import_not_ready` 409 (kế hoạch).

## Bằng chứng

Môi trường: bộ chung crm-shared (`CRM_TEST_DATABASE_URL` cổng 5600); test tự tạo database riêng; kho tệp test là `MemStore`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Trước khi viết mã: test đỏ | cả bộ dưới | `go vet ./internal/modules/catalog/... ./internal/httpapi/` | build FAIL: `undefined: importer.CodeCodeTaken`, `undefined: catalog.ImportPlannedProduct` (và `ApplyImport`, `ErrImportNotReady`, `NewImportApplier`, `Recheck`) |
| Recheck: không đổi → 0 lỗi; SKU mới bị mẫu khác lấy, mã mẫu mới vừa tạo, mẫu có sẵn bị xóa, version biến thể sửa/giữ nguyên đổi, SKU chuyển mẫu, biến thể bị xóa, SKU mới vừa tạo ở chính mẫu → đúng 1 lỗi đúng dòng, cột, mã; nhiều lỗi xếp theo dòng | `TestRecheck`, `TestRecheck_SortedByRow` | `go test -race -count=1 ./internal/modules/catalog/internal/importer/` | `ok … importer 2.744s` |
| KH01: `mau-200.xlsx` → ready {200, create 1000} → apply → applied, 200 mẫu, 1.000 biến thể, danh mục/thuộc tính/nhãn đúng plan từng mẫu, 1 audit tổng `{create 1000, update 0, unchanged 0}` | `TestApply_200Models` | `go test -race -count=1 -v -run 'TestApply_200Models\|TestApply_AllOrNothingProperty' ./internal/modules/catalog/` | PASS; **thời gian áp 4,46 s** (gồm xếp việc + worker, có `-race`, máy tải 13,8 trên 10 nhân) — dưới mục tiêu 20 s |
| Quy luật tất cả hoặc không, rapid 100 lượt (1–30 mẫu, lỗi cài ở dòng ngẫu nhiên lúc áp) | `TestApply_AllOrNothingProperty` | như trên | `[rapid] OK, passed 100 tests (19.05s)` |
| invalid → `ErrImportNotReady`, không xếp việc, 0 mẫu; version cũ → `ErrVersionConflict`; bản nhập lạ → `ErrNotFound`; áp lần hai khi applying → `ErrImportNotReady` | `TestApply_Refused` | `RAPID_CHECKS=5 go test -race -count=1 -run 'Apply\|Import' ./internal/modules/catalog/` | `ok … catalog 49.409s` |
| KH01: áp, tải lại đúng tệp → duplicate, `duplicateOf` = lần một; số dòng, tổng version, tổng giá không đổi; áp bản duplicate → `ErrImportNotReady` | `TestApply_SameFileTwiceEndToEnd` (+ `TestApply_SameFileTwice` phần kiểm của Task 47) | như trên | PASS |
| Sửa giá 3 biến thể trên 200 mẫu sẵn → áp → đúng 3 `price.changed`, 3 giá mới, 997 biến thể giữ version 1, audit tổng `{update 3, unchanged 997}` | `TestApply_ThreePriceChanges` | như trên | PASS |
| SKU của plan bị tạo tay cho mẫu khác giữa kiểm và áp → failed, 1 lỗi dòng 3 `SKU*` `sku_belongs_to_other_product`, dữ liệu không đổi, việc done | `TestApply_SKUTakenMeanwhile` | như trên | PASS |
| Lỗi ở giữa lúc ghi (barcode dòng 7 vừa bị lấy, 2 mẫu đã ghi trước đó) → lùi hết, failed, lỗi dòng 7 `barcode_taken` | `TestApply_WriteErrorRollsBackEverything` | như trên | PASS |
| Hai bản nhập cùng chạm `AO01-S`, hai worker chạy song song → một applied, một failed; 1 biến thể `AO01-S`, 1 mẫu; mọi việc done (không deadlock) | `TestApply_TwoImportsSameSKUConcurrently` | như trên | PASS |
| Mẫu có sẵn: đổi tên, thêm danh mục + nhãn, đổi size S↔M cho nhau, thêm biến thể → applied, đúng dữ liệu | `TestApply_ExistingProductSwapAndClassify` | như trên | PASS |
| Việc dead → `OnTerminal` → bản nhập failed | `TestApply_DeadJobFailsImport` | như trên | PASS |
| HTTP: chỉ `catalog.manage` → 403; 50 yêu cầu song song cùng `Idempotency-Key` → chỉ 202 (applying, cùng bản nhập) hoặc 409 `request_in_progress`, đúng **1 việc**; version cũ → 409 `version_conflict`; invalid, đang applying → 409 `import_not_ready`; không có → 404 | `TestCatalogAPI_ImportApply`, `TestCatalogAPI_Imports` | `go test -race -count=1 -run 'TestCatalogAPI_Import' ./internal/httpapi/` | `ok … httpapi 4.464s` |
| `db.Savepoint`: lỗi trong savepoint (kể cả câu SQL trùng khóa làm transaction hỏng) chỉ lùi phần trong savepoint, transaction dùng tiếp, phần trước/sau còn sau commit | `TestSavepoint` | `go test -race -count=1 -run 'TestSavepoint' ./internal/platform/db/` | `ok … db 3.316s` |
| Lần `make ci` đầu đỏ: (a) nolintlint thừa ở test rapid → bỏ; (b) `gen-check` vì mã sinh chưa stage → stage; (c) `TestTxUsageProductCode` cấm `tx.Begin` trong catalog → chuyển sang `db.Savepoint` (Quyết định 9), chạy lại test catalog | `TestTxUsageProductCode`, bộ catalog | `go test -count=1 -run TxUsage ./tests/arch/`; `RAPID_CHECKS=20 go test -race -count=1 -run 'Apply\|Import\|Recheck' ./internal/modules/catalog/ ./internal/modules/catalog/internal/importer/` | `ok … tests/arch 0.578s`; `ok … catalog 70.911s`, `ok … importer 1.252s` |
| Lần `make ci` thứ hai đỏ: `TestApply_ThreePriceChanges` thành `failed` không lỗi dòng khi máy quá tải — việc hết hạn giữa bước ghi, lỗi hết hạn bị gói thành `ErrPermanent` (dead ngay). Sửa: lỗi không phải nghiệp vụ trả lên để chạy lại. Test mới hết hạn 300 ms giữa lúc ghi → Handle trả lỗi không `ErrPermanent`, bản nhập vẫn applying, 0 mẫu; thử tạm mã cũ → đỏ, trả lại → xanh. (Cùng lần đó `identity`, `inventory` — không bị chạm — hết 600 s / `statement timeout` vì máy tải nặng; xem dòng `make ci` cuối) | `TestApply_TimeoutMidWriteRetries` | `go test -race -count=1 -run TestApply_TimeoutMidWriteRetries ./internal/modules/catalog/` | mã cũ: `FAIL … permanent, mong lỗi chạy lại được`; mã mới: `ok … catalog 6.519s` |
| Sau review lần 1 (CẦN SỬA): đổi dây chuyền A S→M, B M→L, tạo C vào S trước dòng A → applied đúng size; với `moveAside` cũ → `failed` `option_taken` dòng 3. Quyết định 6: hai bản cùng tệp đều ready → bản sau duplicate, dữ liệu không đổi | `TestApply_ChainedOptionMoves`, `TestApply_SameFileBothReady` | `go test -race -count=1 -run 'TestApply_ChainedOptionMoves\|TestApply_SameFileBothReady\|TestApply_ExistingProductSwap' ./internal/modules/catalog/`; thử với bản stage cũ của `imports_apply_write.go` | `ok … catalog 30.358s`; bản cũ: `FAIL … failed [{Row:3 Column:Size* Code:option_taken …}]` |
| Lần `make ci` thứ ba (sau review lần 2): mọi bước tới `govulncheck` qua (47 gói `ok`, `0 issues`, `No vulnerabilities found`); `oasdiff` đỏ `setProductImageColor … api-path-removed` vì main đã có thêm operation của task khác **sau** lần gộp main abd3410 (lệnh giao: chỉ gộp main một lần) → so với đúng bản đã gộp: exit 0. `jscpd` đỏ: 3 đoạn trùng trong `imports_apply_test.go`, 1 giữa `decodeApplyPayload`/`decodeImportPayload` → gộp bằng hàm phụ test (`mustReady`, `applyExpect`, `createOther`, `applyJob`) và `decodePayload[T]`; `jscpd` `Found 0 clones` | `TestApply_*`, `TestImport_*` | `go run ./tools/apicheck -base-ref=abd3410 -rev openapi/openapi.yaml -approved openapi/breaking-approved.yaml`; `jscpd … --fail-on-new-clones=0 .`; `RAPID_CHECKS=20 go test -race -count=1 -run 'Apply\|Import' ./internal/modules/catalog/` | `exit 0`; `Found 0 clones.`; `ok … catalog 69.446s` |
| **`make ci` cuối** (sau mọi sửa, trên phần đã stage, `OASDIFF_BASE=-base-ref=abd3410`) | toàn bộ | `GOFLAGS=-p=2 make ci OASDIFF_BASE=-base-ref=abd3410`; chạy riêng lại 2 gói đỏ vì môi trường; các bước sau `test`: `make vuln oasdiff catalog-check jscpd gitleaks console-check`; `test-guard` cố định so với `main` (đã có test của task khác gộp sau abd3410) → kiểm tương đương bằng `git diff --cached --diff-filter=D abd3410` | build, vet, `0 issues`, gen-check, sqlc-vet qua; `test`: 45 gói `ok`, chỉ `tests/arch` `TestLint*` đỏ vì "parallel golangci-lint is running" (phiên khác) và `tests/e2e` `TestBootEachRoleThenGracefulStop/ingest` quá 10 s lúc load 18 → chạy riêng: `ok … tests/arch 5.775s`, `ok … tests/e2e 9.205s`; `No vulnerabilities found.`; oasdiff exit 0; `Found 0 clones.`; 0 tệp test bị xóa, 0 `t.Skip`/`.only` thêm; `no leaks found`; console `Tests 190 passed (190)` |
| Worker nối việc áp | build + test gói | `go test -race -count=1 ./cmd/crm/` | `ok … cmd/crm 1.575s` |
| **Việc treo Task 47: đo tệp 20.000 dòng** (4.000 mẫu × 5 biến thể, 1,06 MB, có danh mục, nhãn, giá nhập; test tạm không commit, không `-race`, gọi thẳng `Execute`/`Handle`) | đo tạm | `go test -count=1 -v -run TestZZMeasure20000 ./internal/modules/catalog/` | kiểm (lệnh validate): **0,82 s** (giới hạn 30 s: đạt). Áp một transaction: **2 phút 7 s** → **vượt** giới hạn 30 s của việc (≈ 6,3 ms/biến thể). Tải máy lúc đo: load average 12,7 → 17,0 trên 10 nhân (nhiều phiên khác chạy cùng). Xem Việc phát hiện thêm. |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Mã ghi ở `internal/modules/catalog/imports_apply.go` (yêu cầu áp, việc, khóa, kiểm lại) và `imports_apply_write.go` (các bước ghi) thay cho sửa `imports.go`; `importer/apply.go` chỉ có `Recheck` (hàm thuần): gói `importer` không gọi được Writer của `catalog` (vòng nhập).
- Handler ở `handlers_catalog_imports.go` (chỗ của các handler bản nhập từ Task 47) thay cho `handlers_catalog.go`.
- Lỗi do dữ liệu đổi từ lúc kiểm: lùi về savepoint và ghi `failed` + lỗi dòng ngay (việc `done`), không đợi hết lượt rồi `OnTerminal` (Quyết định 2). `OnTerminal` vẫn đặt `failed` cho lỗi hạ tầng hết lượt.
- Thêm `TestApply_SameFileTwiceEndToEnd` (đường thật) bên cạnh `TestApply_SameFileTwice` của Task 47 (chèn thẳng dòng applied) thay cho sửa ca cũ.
- Test thêm ngoài kế hoạch: lỗi giữa lúc ghi lùi hết, đổi size cho nhau, việc dead.
- ADR-46 thêm dòng khóa tư vấn của việc áp.

## Việc phát hiện thêm

- **Tệp lớn vượt hạn 30 s của việc áp** (số đo ở Bằng chứng: 20.000 biến thể 2 phút 7 s; 1.000 biến thể 4,5 s có `-race`). Với giới hạn đọc hiện tại (20.000 dòng) tệp trên khoảng 3.000–4.000 biến thể (tùy tải máy) sẽ hết hạn mỗi lượt → 8 lượt (giãn tới hàng giờ) → `dead` → `failed`, không ghi gì (an toàn nhưng nhân viên chờ lâu và không biết lý do). Cần chủ shop chọn: (a) hạ trần số dòng của tệp nhập sản phẩm (ví dụ 3.000 dòng, báo `too_many_rows` ngay lúc kiểm) — đơn giản, giữ "tất cả hoặc không"; (b) ghi theo lô nhiều transaction — mất "tất cả hoặc không" của Q10; (c) tăng tốc đường ghi (chèn hàng loạt) — trái luật kế hoạch "dùng chính các hàm công khai, không viết SQL ghi thứ hai". Đề xuất (a). KH01 (~200 mẫu) không bị ảnh hưởng.
- (Reviewer) Mẫu có sẵn để trống ô Danh mục nhưng đổi nhãn/thuộc tính: `SetClassification` khóa chia sẻ danh mục hiện tại của mẫu sau khi `lockPlan` đã khóa mẫu (lệch thứ tự ADR-46 nhóm 4, không gây deadlock hiện nay). Có thể khóa thêm danh mục hiện tại của các mẫu có sẵn trong `lockPlan`.
- (Reviewer) Rapid chỉ cài lỗi bị `Recheck` chặn; có thể cài thêm lỗi barcode để quy luật đi qua đường lùi savepoint giữa lúc ghi.
- Main sau abd3410 mở rộng vùng rủi ro cao thêm `internal/platform/db` (commit a4492e7, gộp sau lần gộp main của task này): `db.Savepoint` là phần thêm vào gói đó; đã chạy cả bộ test `db` trong `make ci`. Điều phối viên khi gộp nên chạy lại `oasdiff` so với main mới (sẽ qua sau khi gộp, vì task này chỉ thêm operation).
- `summary` của bản nhập vẫn chỉ đếm biến thể; đổi tên, mô tả, phân loại của mẫu có sẵn chỉ thấy ở audit tổng (`productsUpdated`), chưa có ở màn xem trước (Task 52 có thể cần).
- Mẫu đã xóa mềm vẫn giữ mã: tệp dùng mã đó được `ready`, lúc áp thành `failed` với lỗi dòng `code_taken` (câu báo có nhắc "hoặc thuộc mẫu đã xóa"). Nếu muốn báo ngay lúc kiểm, sửa `ListVariantsByProductCodes`/Validate ở task sau.

## Reviewer

Lần 1 (agent `reviewer`, trên `git diff --cached` sau khi gộp main abd3410): **CẦN SỬA**.
- Phải sửa: `moveAside` chỉ dời biến thể muốn lấy chỗ đang có chủ → đổi dây chuyền (A S→M trước B M→L) hay tạo mới vào chỗ vừa trống vẫn `failed` `option_taken` dù bước kiểm cho qua. Đã sửa: dời tạm mọi biến thể đổi màu+size; thêm `TestApply_ChainedOptionMoves` (đỏ với mã cũ) và `TestApply_SameFileBothReady` (Quyết định 6 chưa có test).
- Ghi chú không bắt buộc (không sửa, chép vào Việc phát hiện thêm): khóa danh mục *cũ* của mẫu có sẵn sau khi khóa mẫu (không deadlock: đường sửa/lưu trữ danh mục không khóa mẫu, các lần áp đã tuần tự); rapid chưa đi qua đường lùi savepoint giữa lúc ghi (có `TestApply_WriteErrorRollsBackEverything`); payload hỏng làm bản nhập kẹt applying (không xảy ra qua đường thật). Ghi chú "unique tính trên cả tenant" không đúng: `imports_applied_sha_key` là `(tenant_id, seller_id, file_sha256)` (Quyết định 8 của Task 47).

Lần 2 (agent `reviewer`): **ĐẠT**. Bản sửa `moveAside` đúng và đủ (chỗ tạm khác nhau theo dòng, ≤ 50 ký tự; biến thể đứng yên giữ chỗ bị `checkExistingOptions`/`duplicate_option_in_file` chặn ở bước kiểm); soát lại toàn diff: savepoint, thứ tự khóa, chống trùng, OnTerminal, quyền, dữ liệu cá nhân đều đạt. Không phát hiện phải sửa.

Sau lần 2, `make ci` đỏ ở jscpd → gộp đoạn trùng (hàm phụ test, `decodePayload[T]`), không đổi hành vi.

Lần 3 (agent `reviewer`, trên phần đã stage sau khi gộp đoạn trùng): **ĐẠT**. `decodePayload[T]` giữ nguyên điều kiện cũ (tenant, JSON, mã khác rỗng; bên áp thêm `ActorID`); các hàm phụ test không bỏ hay nới câu kiểm nào. Không phát hiện phải sửa.

Kết luận: ĐẠT
