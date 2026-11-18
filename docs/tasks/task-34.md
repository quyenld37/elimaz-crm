# Task 34 — Danh mục, thuộc tính, nhãn

Tiêu đề trong kế hoạch: Task 34 (M1-34). Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm** vào gói `catalog` (không sửa ký hiệu đã có): phương thức `Writer.CreateCategory`, `UpdateCategory`, `ArchiveCategory`, `CreateAttribute`, `AddAttributeValue`, `CreateTag`, `SetClassification`; hàm đọc `ListCategories`, `ListAttributes`, `ListTags`, `GetClassification`; kiểu `Category`, `CategoryInput`, `CategoryPatch`, `Attribute`, `AttributeValue`, `Tag`, `Classification`; sentinel `ErrCategoryCycle`, `ErrCategoryTooDeep`, `ErrCategoryInUse`, `ErrAlreadyExists`. `mapWriteError` (không xuất) thêm tên ràng buộc mới; người gọi hiện có (products.go, variants.go) không đổi hành vi. `httpapi.catalogError` thêm dòng; 0 chỗ dùng ngoài `errors.go`, `handlers_catalog.go`.
- Bổ sung sau review: kiểu `AttributeInput`, `AttributeValueInput`, `TagInput`, `ProductClassification`, hằng `MaxCategoryDepth`, `CategoryActive`, `CategoryArchived`; hai câu mới `SetProductCategory`, `GetProductCategory` trong `queries/catalog/products.sql` (không sửa câu cũ). Tất cả chỉ thêm, không có chỗ gọi cũ.
- Gói phụ thuộc ngược (go list): `catalog` được nhập bởi `internal/httpapi`, `cmd/crm`. Sẽ chạy test hai gói này và `tests/arch`.
- Operation OpenAPI bị chạm, oasdiff: thêm 10 operation (`listCategories`, `createCategory`, `updateCategory`, `archiveCategory`, `listAttributes`, `createAttribute`, `addAttributeValue`, `listTags`, `createTag`, `setProductClassification`), thêm schema mới, thêm mã lỗi `already_exists` vào `x-error-codes`. Không sửa operation cũ → oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: chưa có màn dùng (Task M1-50 dùng). `console/src/api/**` sinh lại; `console/src/app/errors.ts` thêm câu cho `already_exists` (test `errors.test.ts` đòi đủ câu).
- Câu sqlc, bảng, pha migration: migration mới `0120_catalog_classification.sql` (pha mở rộng: chỉ CREATE, thêm khóa ngoại `products(tenant_id, category_id) → categories` trên cột đã có, mọi dòng hiện có là NULL; thêm quyền UPDATE `category_id`). Sáu bảng mới, sáu view. Câu sqlc mới ở `queries/catalog/{categories,attributes,tags}.sql`; sinh lại `internal/modules/catalog/internal/store`.
- Test sẽ chạy: `go test -race ./internal/modules/catalog/... ./internal/httpapi/... ./tests/arch/... ./cmd/crm/...`; `cd console && npm test`, `npx tsc --noEmit`; hook Stop (build, vet, lint).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `textnorm` (Fold), `tenancy`, `events`, `db`; tìm "slug", "tree", "recursive", "depth" trong `internal/` → chưa có. Mẫu ghi của Task 27 (`catalog.Writer`, `lockProduct`, `mapWriteError`, `productChanged`, `audit`), lớp HTTP `catalogWrite`, `WriteTx`.
- Dùng lại / viết mới vì: dùng lại `Writer` (sự kiện, audit cùng tx), `lockProduct` (khóa dòng mẫu + so version), `productChanged` (phát `product.updated` + báo hiệu), `mapWriteError` (mở rộng tên ràng buộc), `catalogWrite` ở httpapi, `textnorm.Fold` để sinh slug từ tên (Fold chỉ cho cột tìm kiếm, nhưng ở đây chỉ dùng kết quả làm gợi ý slug ASCII rồi kiểm mẫu `^[a-z0-9-]{1,80}$`; slug là định danh URL, không so khớp chữ gốc). Viết mới phần cây danh mục (đệ quy, độ sâu) vì chưa có ở đâu; là logic riêng của catalog nên ở lại module.

## Soát thiết kế

- 5.1 Chủ dữ liệu: danh mục, thuộc tính, nhãn thuộc `catalog` (05 mục 2; dòng 717: không có `seller_id`, dùng chung cả cửa hàng). Bảng trong schema `catalog`.
- 5.2 Không ghi bảng module khác: chỉ ghi `catalog.*`, `identity.audit_logs` qua `identity.RecordAudit`, sự kiện qua `events`.
- 5.3 Đọc module khác qua view: không đọc module khác. Công bố sáu view mới cho M5, M7 (`docs/views.md` đã có dòng).
- 5.4 Nguyên tử: `SetClassification` đổi `category_id`, thay hai bảng nối, tăng version, phát sự kiện, audit trong một tx.
- 5.5 Việc sau commit: `product.updated` qua `events` (ghi trong tx, giao sau commit).
- 5.6 Gọi ra ngoài: không áp dụng.
- 5.7 Ngữ cảnh shop: mọi bảng có `tenant_id`, `UNIQUE (tenant_id, id)`, unique khác bắt đầu bằng `tenant_id`, khóa ngoại ghép, `platform.enable_tenant_rls`; mọi truy vấn trong `WriteTx`/`tenancy.WithTenant`. Gán giá trị/nhãn của tenant khác: RLS ẩn + khóa ngoại ghép `(tenant_id, …)` → `ErrNotFound`.
- 5.8 Không biết HTTP: module chỉ trả sentinel; đổi mã ở `errors.go`.
- 5.9 Đường ghi: 7 operation ghi có `IdempotencyKey` và `x-permission: catalog.manage`; đọc `catalog.read`.
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: theo `catalog` Task 27 (Writer, version, view `_v`).
- 6.1 Đồng thời: mọi sửa có `version` (khóa dòng rồi so). Đổi cha danh mục: khóa **mọi** dòng danh mục của shop theo `id` tăng dần rồi mới kiểm vòng và độ sâu bằng truy vấn đệ quy (câu mới sau khi có khóa nên thấy dữ liệu đã commit); tạo danh mục con khóa dòng cha `FOR SHARE` → không thể đổi cha và thêm con cùng lúc mà vượt 3 cấp hay tạo vòng. Lưu trữ khóa dòng danh mục `FOR UPDATE` rồi đếm con và mẫu active; `SetClassification` khóa danh mục được gán `FOR SHARE` → không gán được vào danh mục đang/đã lưu trữ.
- 6.2 Gửi lại: `Idempotency-Key`; unique tự nhiên (slug, tên cùng cha, mã thuộc tính, giá trị, mã nhãn).
- 6.3 Thứ tự khóa: ADR-46 nhóm 4: `categories → attributes → attribute_values → tags → products → product_attribute_values → product_tags`. `SetClassification`: danh mục (share) → mẫu (update) → xóa/ghi bảng nối. Đổi cha: mọi danh mục theo id tăng dần.
- 6.4 Lỗi tạm: `WriteTx` (`RetryTx`) chạy lại; hàm thuần trên tx, không giữ trạng thái giữa lần chạy.
- 6.5 Cách ly: READ COMMITTED + khóa dòng như trên.
- 6.6–6.8: không áp dụng: không có việc nền, gọi ngoài.
- 6.9 Treo lâu: dùng `db.TxAPI` mặc định; cây ≤ 3 cấp, vài chục dòng.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật luôn đúng: catalog không thuộc vùng rủi ro cao; quy luật cây (không vòng, ≤ 3 cấp) có test song song đổi cha chéo nhau.
- 6.12 Dữ liệu cá nhân: không có; audit chỉ mã, tên danh mục, số lượng.
- 6.13 Quyền: mặc định từ chối qua `x-permission`; test người chỉ có `catalog.read` gọi ghi → 403.

## Quyết định thay chủ shop

- **Q1 — Mã lỗi cho trùng tên, trùng mã.** Kế hoạch chỉ ghi "hai danh mục cùng tên cùng cha → 409", "giá trị trùng → 409", không nêu mã. Chọn: một mã mới `already_exists` (409, câu "Đã có mục cùng tên hoặc cùng mã") cho trùng slug, trùng tên cùng cha, trùng mã thuộc tính, trùng giá trị trong một thuộc tính, trùng mã nhãn; khai ở `x-error-codes` và `console/src/app/errors.ts`. Vì: `code_taken` hiện mang câu "Mã mẫu đã có" (sai nghĩa cho danh mục); một mã chung đủ cho màn hình (ô đang nhập đã cho biết là gì). Phương án khác: năm mã riêng (`category_name_taken`, `slug_taken`…): nhiều mã, nhiều câu hơn mà màn hình chưa cần; đổi được về sau bằng cách thêm mã.
- **Q2 — `SetClassification` là phương thức của `Writer`, nhận thêm `actor`, trả `ProductClassification`.** Kế hoạch ghi `SetClassification(ctx, tx, productID, version, c) error`. Chọn: `(w Writer) SetClassification(ctx, tx, productID, version, c, actor) (ProductClassification, error)` cùng mẫu với `UpdateProduct` của Task 27: cần `Writer` để phát `product.updated` và audit, cần `actor` cho audit, trả version mới để màn hình sửa tiếp không phải đọc lại. Các hàm tạo, sửa khác (`CreateCategory`…) cũng là phương thức của `Writer`. Tên giữ đúng kế hoạch.
- **Q3 — Slug.** Kế hoạch có cột `slug` nhưng không nói ai đặt. Chọn: trường `slug` không bắt buộc; để trống thì sinh từ tên (`textnorm.Fold`, đoạn không phải chữ/số thành `-`); có gửi thì chuẩn hóa chữ thường và kiểm `^[a-z0-9-]{1,80}$`. Trùng slug trong shop → `already_exists`, nhân viên đặt slug khác.
- **Q4 — Danh mục đã lưu trữ.** Không gán cho mẫu, không thêm con, không làm cha mới được (trả `not_found`, như danh mục không có); vẫn đổi tên được. Lưu trữ lần hai trả nguyên. Chưa có thao tác bỏ lưu trữ (kế hoạch không có). Lưu trữ chỉ chặn bởi mẫu `active` chưa xóa và danh mục con `active` (theo kế hoạch "còn mẫu active"; câu của màn hình đã ghi "đang có sản phẩm hoặc danh mục con").
- **Q5 — Đọc lại phân loại.** Kế hoạch không có operation đọc phân loại của một mẫu. Chọn: hàm Go `GetClassification` (dùng ở test, Task M1-50 sẽ cần) và `PUT …/classification` trả phân loại sau khi thay; không thêm trường vào `Product` (đổi câu trả lời của 3 operation cũ, ngoài phạm vi). Ghi ở "Việc phát hiện thêm".
- **Q6 — Khóa khi đổi cha.** Kế hoạch ghi "truy vấn đệ quy dưới khóa dòng". Chọn: khóa mọi dòng danh mục của shop theo id tăng dần (cây vài chục dòng) rồi mới chạy truy vấn đệ quy; thêm con khóa chia sẻ dòng cha. Vì khóa từng dòng trên đường đi không chặn được hai người đổi cha chéo nhau (A dưới B, B dưới A) cùng lúc. Có test song song 10 lần.
- **Q7 — Mã thuộc tính, mã nhãn** chuẩn hóa chữ thường, mẫu `^[a-z0-9][a-z0-9_-]{0,39}$`; tên và giá trị 1–100 ký tự; một mẫu tối đa 100 giá trị thuộc tính và 100 nhãn (Nhanh có 8 thuộc tính, 30 giá trị).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, đỏ trước khi có mã | `classification_test.go`, `TestCatalogAPI_Classification` | `go vet ./internal/modules/catalog/` (trước khi có `classification.go`) | `undefined: catalog.CategoryInput` (không biên dịch được) |
| Váy > Váy dài > Váy maxi được; cấp 4 → too_deep; Váy làm con Váy maxi → cycle; chuyển cây con thành 4 cấp → too_deep; lên gốc đổi độ sâu cả cây con | `TestCategory_TreeDepthAndCycle` | `go test -race -count=1 -run 'Category\|Attributes\|SetClassification' -v ./internal/modules/catalog/` | `--- PASS: TestCategory_TreeDepthAndCycle (0.49s)` |
| Cùng tên cùng cha (cả ở gốc, cha NULL) → 409; slug trùng → 409; tenant khác cùng slug được | `TestCategory_Duplicates` | như trên | `--- PASS: TestCategory_Duplicates (0.33s)` |
| Lưu trữ khi còn mẫu active → category_in_use; còn con active → in_use; mẫu nháp không chặn; danh mục đã lưu trữ không gán, không thêm con | `TestCategory_Archive` | như trên | `--- PASS: TestCategory_Archive (0.54s)` |
| 8 thuộc tính, 30 giá trị tạo được, đọc lại đủ; giá trị trùng trong một thuộc tính → 409 | `TestAttributes_NhanhCounts` | như trên | `--- PASS: TestAttributes_NhanhCounts (0.40s)` |
| SetClassification 3 nhãn → đọc lại đủ, 1 `product.updated` (seq 2), `catalog.product_tags_v` đúng 3 dòng, 1 audit, 1 báo hiệu; thay toàn bộ còn 1 dòng | `TestSetClassification_ReplaceAll` | như trên | `--- PASS: TestSetClassification_ReplaceAll (0.34s)` |
| version cũ → ErrVersionConflict; mẫu, nhãn không có → ErrNotFound; 101 nhãn → ErrInvalidInput | `TestSetClassification_Errors` | `go test -race -count=1 ./internal/modules/catalog/` | `ok elimaz.com.vn/crm/internal/modules/catalog 101.095s` |
| Gán giá trị thuộc tính, nhãn, danh mục của tenant khác → 404 (ErrNotFound), không ghi dòng nào | `TestSetClassification_OtherTenantValue` | như hàng 2 | `--- PASS: TestSetClassification_OtherTenantValue (0.55s)` |
| Hai người đổi cha chéo nhau: đúng 1 được, 1 cycle; thêm con và chuyển cha cùng lúc không sai độ sâu (10 lần mỗi ca) | `TestCategory_ConcurrentCrossMove` | như hàng 2 | `--- PASS: TestCategory_ConcurrentCrossMove (0.63s)` |
| API: mã HTTP `category_too_deep` 422, `category_cycle` 422, `already_exists` 409, `category_in_use` 409, `version_conflict` 409, `forbidden` 403 cho người chỉ xem, tenant B 404 và không thấy danh mục của A, đọc bằng `catalog.read` | `TestCatalogAPI_Classification` | `go test -count=1 -run Classification -v ./internal/httpapi/` | `--- PASS: TestCatalogAPI_Classification (0.86s)` |
| Sáu view có đúng cột của `docs/views.md` | `TestViewsDocMatchesDatabase` (so mọi view `_v` trong database đã migrate với tài liệu) | `go test -race -count=1 -run TestViewsDoc -v ./tests/arch/` | `--- PASS: TestViewsDocMatchesDatabase (0.82s)` |
| Mã lỗi mới có trong hợp đồng và câu tiếng Việt | `TestErrorCodes_DeclaredInContract`, `console/src/app/errors.test.ts` | `go test -run TestErrorCodes ./internal/httpapi/`; `cd console && npm test` | `--- PASS`; `Tests 90 passed (90)` |
| Bảng mới đúng luật schema (RLS, unique, khóa ngoại ghép), câu sqlc, lint, hợp đồng | test kiến trúc, `sqlc vet`, golangci-lint, oasdiff | `go test -race ./tests/arch/...`; `make sqlc-vet oasdiff lint catalog-check` | `ok tests/arch`; `0 issues.`; apicheck không báo thay đổi phá vỡ; danh mục không chênh |
| Bản đã gộp `main` (509f61e, Task 317) vẫn qua | các gói trên | `git merge main` (fast-forward), `make gen`, `go test -race ./tests/arch/... ./internal/modules/catalog/ ./internal/httpapi/` | cả ba `ok` |
| Màn hình | `npm test`, `tsc`, `eslint` | `cd console && npm test && npx tsc --noEmit && npx eslint .` | `Tests 90 passed`, không lỗi |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký `SetClassification` (Q2): thêm `actor`, là phương thức của `Writer`, trả `ProductClassification`.
- Thêm `GetClassification`, `ListCategories`, `ListAttributes`, `ListTags` (hàm đọc cho ba operation `list*` có trong kế hoạch và cho test "đọc lại đủ").
- Thêm mã lỗi `already_exists` (Q1). `archiveCategory` nhận thân `{version}` (schema chung `VersionRequest`).
- `sqlc.yaml`: cột uuid cho phép rỗng của catalog sinh ra `*uuid.UUID` (thay `pgtype.UUID`) để không chép hàm đổi kiểu của `queue` (jscpd báo trùng). Các câu cũ của catalog không có cột uuid rỗng nên không đổi.
- `migrations/0120`: thêm ba index không có trong kế hoạch: `product_tags (tenant_id, tag_id)`, `product_attribute_values (tenant_id, attribute_value_id)` (website lọc theo nhãn, thuộc tính) và `products (tenant_id, category_id)` (lưu trữ đếm mẫu trong danh mục).

## Việc phát hiện thêm

- `make jscpd` đỏ trên `main` (509f61e) với 2 đoạn trùng không có trong mốc nền: `internal/platform/db/migrate_test.go` và `internal/platform/queue/errors_test.go`/`worker_test.go` (đã kiểm trên bản sạch của `HEAD`, trước khi có mã task này). Không phải của Task 34; cần task dọn hoặc cập nhật `jscpd-baseline.json`.
- Mẫu nằm trong danh mục đã lưu trữ: gửi lại phân loại với đúng `categoryId` cũ nhận 404 (Q4). Màn Task M1-50 cần cho nhân viên chọn danh mục khác hoặc bỏ danh mục.
- Task M1-50 (màn Sản phẩm) cần đọc phân loại của một mẫu: thêm `categoryId`, `attributeValueIds`, `tagIds` vào `Product` hoặc operation `GET …/classification` (Q5).
- `UpdateProduct` đổi mẫu sang `active` không khóa danh mục của mẫu: nếu đúng lúc đó danh mục đang được lưu trữ thì có thể còn một mẫu active trong danh mục đã lưu trữ (không hỏng dữ liệu; website bỏ danh mục lưu trữ). Muốn chặn hẳn thì `UpdateProduct` khóa chia sẻ danh mục trước mẫu (ADR-46 nhóm 4).
- Chưa có thao tác bỏ lưu trữ danh mục, sửa, xóa thuộc tính, giá trị, nhãn (kế hoạch M1 không có).

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, một lượt, trên `git diff --cached` của bản đã gộp `main` 509f61e).

- Phát hiện phải sửa: không có. Reviewer đã thử các ca đồng thời (đổi cha chéo nhau, thêm con lúc đổi cha tổ tiên, lưu trữ cùng lúc thêm con, gán hay đổi cha), thứ tự khóa ADR-46 nhóm 4, dời độ sâu cây con, lùi tx khi từ chối nghiệp vụ, tenant khác qua khóa ngoại ghép, quyền `crm_app`, hợp đồng (7 ghi có khóa chống trùng và `catalog.manage`, 3 đọc `catalog.read`), sáu view khớp `docs/views.md`.
- Reviewer không chạy được `go test` (hook chỉ cho lệnh đọc); kết quả test ở bảng bằng chứng là của người làm task.
- Ghi chú không bắt buộc (đã ghi lại, không sửa mã): mẫu nháp trong danh mục đã lưu trữ, gửi lại phân loại với `categoryId` cũ sẽ nhận 404 (đúng Q4; Task M1-50 cần biết); mục Ảnh hưởng nên kể thêm kiểu `*Input`, `ProductClassification`, hằng `MaxCategoryDepth`, `CategoryActive/Archived` và hai câu mới `SetProductCategory`, `GetProductCategory` trong `queries/catalog/products.sql` (đã bổ sung dưới đây).
