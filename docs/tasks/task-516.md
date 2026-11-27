# Task 516 — Số đo sản phẩm Dài/Rộng/Cao cm trong CRM và API web measureCm

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (mục chốt thay chủ shop 27/11 «Số đo sản phẩm: LÀM»). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): thêm `catalog.MeasureCm`, `catalog.MinMeasureCm`, `catalog.MaxMeasureCm`, `catalog.ErrInvalidMeasure`; thêm trường `Measure` vào `catalog.Product`, `catalog.ProductInput`, `catalog.ProductPatch` (con trỏ `*MeasureCm`). Chỗ dựng các kiểu này (thêm trường, không đổi chữ ký nên chỗ cũ vẫn biên dịch): `ProductInput{` ở `httpapi/handlers_catalog.go` (CreateProduct), `imports_apply_write.go` (createPlanned), `cmd/crm/seed_web_fixture_apply.go` (createProduct); `ProductPatch{` ở `handlers_catalog.go` (UpdateProduct), `imports_apply_write.go` (updateProductFields), `seed_web_fixture_apply.go`; `Writer.UpdateProduct` có 3 chỗ gọi ngoài test (giữ chữ ký). `webcatalog.WebProduct` thêm `MeasureCm` (kiểu `webcatalog.MeasureCm`), dùng ở `httpapi/web/catalog.go`. Hàm xuất mới `web.MeasureOut` (gói `internal/httpapi/web`) dùng ở 2 chỗ: `web/catalog.go` (WebProduct) và `httpapi/handlers_catalog.go` (productOut). Hàm riêng của catalog `weightOf`/`weightParam` đổi tên thành `optIntOf`/`optInt4` (chưa xuất, 2 + 3 chỗ dùng trong gói) để số đo dùng chung, không viết hàm trùng. `importer.PlannedProduct` thêm `LengthCm`, `WidthCm`, `HeightCm`; thêm hằng cột `ColLengthCm`, `ColWidthCm`, `ColHeightCm`, mã lỗi `CodeInvalidMeasure`, hằng `MinMeasureCm`/`MaxMeasureCm` (bản sao, `importrules_test.go` so).
- Gói phụ thuộc ngược (go list): gói nhập `internal/modules/catalog` hoặc `internal/queries/webcatalog`: `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `internal/modules/catalog/catalogtest`, `internal/modules/content`, `internal/modules/order`, `internal/modules/promotion`, `internal/wiring/webcache`, `tools/loadkit`.
- Operation OpenAPI bị chạm, oasdiff: schema mới `MeasureCm` (`length`, `width`, `height`: integer int32, đều tùy chọn). Thêm trường tùy chọn `measureCm` vào `Product` (trả về của `createProduct`, `getProduct`, `updateProduct` …), `CreateProductRequest`, `UpdateProductRequest` (nullable: null bỏ cả ba số), `WebProduct` (`webCatalogProducts`, đọc theo mã). Chỉ thêm trường tùy chọn → oasdiff không phá vỡ.
- Chỗ dùng trên màn hình: `console/src/screens/products/ProductEdit.tsx` (thẻ Thông tin: ba ô Dài/Rộng/Cao), `editRules.ts` (Draft, validate, createBody, savePlan, applyProduct, draftFromProduct); client sinh `console/src/api/**` sinh lại.
- Câu sqlc, bảng, pha migration: migration mới `0535_catalog_product_measure.sql` (pha mở rộng, chỉ thêm): `catalog.products` thêm `length_cm`, `width_cm`, `height_cm` int NULL CHECK 1–1000, GRANT UPDATE ba cột cho crm_app; `catalog.products_v` nối ba cột vào cuối (CREATE OR REPLACE). Câu sqlc: `queries/catalog/products.sql` GetProduct, LockProduct, UpdateProduct đọc/ghi ba cột; câu mới SetProductMeasure (đặt số đo của mẫu vừa tạo, chỉ chạy khi có số đo; InsertProduct giữ nguyên không nhắc cột mới để `catalogtest.Product` trên schema trước 0535 — test migrate dữ liệu M1/M2 của order, fulfillment, cash — vẫn chạy, như cách Task 513 làm với InsertVariant); `queries/webcatalog/products.sql` ProductPage, ProductsByIDs. `docs/views.md` dòng products_v.
- Test sẽ chạy: `internal/modules/catalog` (test mới `TestProductMeasure*`, `TestImportMeasure*`), `internal/modules/catalog/internal/importer` (ca cột số đo), `internal/queries/webcatalog` (ca measureCm), `internal/httpapi` (`TestCatalogProductMeasure*`), `internal/httpapi/web` (ca measureCm), `cmd/crm` (seed fixture số đo), console vitest `editRules.test.ts`, `ProductEdit.test.tsx`; tests/arch; ci-task cho gói đổi + phụ thuộc (catalog là vùng rủi ro cao cho `article_html.go`, `importer/apply.go` — chạy theo luật ci-task).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có kiểu số đo/kích thước (chỉ `metrics` là đo lường của tiến trình, không liên quan); tìm `measure`, `dimension`, `length_cm` trong `internal/`, `cmd/`: chỉ có `fixtureMeasure` của seed-web-fixture (Task 218, đọc tệp website, chưa ghi vào CRM).
- Dùng lại / viết mới vì: dùng lại hàm đổi `pgtype.Int4` ↔ `*int` của cân nặng trong catalog (đổi tên `optIntOf`/`optInt4`, vì jscpd báo bản viết mới trùng); một hàm `web.MeasureOut` cho cả API nhân viên và website (httpapi đã nhập web; web không nhập được httpapi); `webcatalog` viết hàm nhỏ `measureOf` riêng vì gói không nhập catalog. Test console gom `openNewOneVariant`, `saveCreated`, `sentUpdate` (jscpd). Làm theo đúng mẫu cân nặng biến thể (Task 190: CHECK ở cột, sentinel `ErrInvalidWeight` → `invalid_input` ở `errors_catalog.go`, trường tùy chọn ở OpenAPI) và mẫu sửa có version của `Writer.UpdateProduct` (khóa dòng, so version). Số đo là của mẫu (không phải biến thể) nên viết kiểu mới `catalog.MeasureCm`; `webcatalog` có kiểu riêng cùng hình (gói không nhập catalog, như `WebVariant.WeightGrams`). Importer giữ bản sao giới hạn 1–1000 như các luật khác (so bằng `importrules_test.go`). Seed-web-fixture dùng lại `fixtureMeasure` đã đọc sẵn.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: số đo là thuộc tính của mẫu → module catalog, cột trong `catalog.products`.
- 5.2 Không ghi bảng module khác: chỉ catalog ghi; importer, seed, HTTP đi qua `Writer`.
- 5.3 Đọc qua view: website đọc qua `catalog.products_v` (nối ba cột), gói `queries/webcatalog` không nhắc bảng gốc.
- 5.4 Nguyên tử: số đo ghi cùng câu UPDATE/INSERT của mẫu trong tx của bên gọi (cùng sự kiện product.updated, audit).
- 5.5 Sau commit: sự kiện `product.updated` có sẵn (version tăng) làm website đọc lại; không thêm gì.
- 5.6 Gọi ra ngoài: không áp dụng: không gọi dịch vụ ngoài.
- 5.7 Ngữ cảnh shop: mọi câu chạy trong tx tenant có sẵn; cột mới trên bảng đã có RLS.
- 5.8 Không biết HTTP: module chỉ nhận `MeasureCm`; handler mỏng đổi `gen.MeasureCm`.
- 5.9 Đường ghi: dùng lại `createProduct`/`updateProduct` (đã có `x-permission: catalog.manage`, `IdempotencyKey`); không thêm operation.
- 5.10 Valkey: bộ đệm web giữ bản WebProduct ≤ 60 s như các trường khác; không quyết định gì dựa trên nó.
- 5.11 Thư viện: không thêm.
- 5.12 Module mẫu: catalog (Task 27) — sửa có version, Writer tự ghi audit/sự kiện; test PostgreSQL thật ở gói `_test`.
- 6.1 Hai người cùng sửa: `UpdateProduct` khóa dòng mẫu rồi so version; số đo đi chung đường đó → đúng một người thắng (test có sẵn của UpdateProduct; thêm ca version cũ khi đổi số đo).
- 6.2 Gửi lại: `Idempotency-Key` của createProduct/updateProduct có sẵn.
- 6.3 Thứ tự khóa: không đổi (chỉ một dòng mẫu, ADR-46 nhóm 4).
- 6.4 Lỗi tạm: không đổi (WriteTx/RetryTx có sẵn); hàm không cộng dồn biến ngoài.
- 6.5 Cách ly: mặc định + khóa dòng có sẵn.
- 6.6–6.8 Việc nền: nhập Excel chạy trong lệnh `catalog.import.apply` có sẵn; số đo chỉ thêm vào bước sửa mẫu của savepoint đó, không đổi cách thử lại.
- 6.9 Treo lâu: không áp dụng: không thêm câu nặng (ba cột trên dòng đã đọc).
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: kiểm biên 0, 1, 1000, 1001 và mọi tổ hợp có/không ba số bằng test bảng; database có CHECK chặn ngoài khoảng dù đi đường khác.
- 6.12 Dữ liệu cá nhân: không có (số đo sản phẩm); audit thêm ba số.
- 6.13 Quyền: dùng quyền có sẵn `catalog.manage` (ghi), `catalog.read` (đọc), `web.catalog.read` (website); không thêm quyền.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tạo mẫu có/không số đo, đọc lại; 0, -1, 1001 → ErrInvalidMeasure, không tạo | `TestProductMeasure_Create` | `go test -race -count=1 -run '^(TestProductMeasure_Create\|TestProductMeasure_Update\|TestImportMeasure_Apply)$' ./internal/modules/catalog/` | `ok elimaz.com.vn/crm/internal/modules/catalog 3.358s` |
| Sửa số đo: nil giữ, đặt/bỏ từng số, biên 1/1000, sai → không đổi version, version cũ → ErrVersionConflict, audit có số đo | `TestProductMeasure_Update` | như trên | như trên |
| CHECK của cột chặn 0, 1001 cả ba cột | `TestProductMeasure_DatabaseCheck` | `go test -race -count=1 -run '^(TestProductMeasure_Create\|TestProductMeasure_Update\|TestProductMeasure_DatabaseCheck\|TestImportMeasure_Apply\|TestImportMeasure_Errors\|TestImporterRulesMatchCatalog\|TestImportTemplate\|TestUpdateVariant_Weight)$' ./internal/modules/catalog/` | `ok elimaz.com.vn/crm/internal/modules/catalog 3.150s` |
| Nhập Excel: mẫu mới nhận số đo, mẫu có sẵn ô có số đặt / ô trống giữ, tệp không cột số đo giữ | `TestImportMeasure_Apply` | như trên | như trên |
| Nhập Excel: ô sai → invalid_measure đúng ô; khác số giữa dòng cùng mẫu → inconsistent_product_fields | `TestImportMeasure_Errors`, `TestValidateMeasure` | như trên; `go test -race -count=1 ./internal/modules/catalog/internal/importer/` | `ok …/catalog/internal/importer 2.129s` |
| Tệp mẫu có ba cột số đo, không bắt buộc ở tiêu đề; luật 1–1000 khớp catalog | `TestTemplateRoundTrip`, `TestImporterRulesMatchCatalog` | như trên | như trên |
| API nhân viên: tạo/đọc/sửa/null/0/1001 → 422 invalid_input; sửa tên giữ số đo | `TestCatalogAPI_ProductMeasure`, `TestDomainErrorSnapshot` | `go test -race -count=1 -run '^(TestCatalogAPI_ProductMeasure\|TestCatalogAPI_UpdateVariantWeight\|TestDomainErrorSnapshot)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.397s` |
| API web trả `measureCm` chỉ số đã nhập; mẫu không số đo không có trường | `TestWebCatalogProducts_Measure` | `go test -race -count=1 -run '^(TestWebCatalogProducts_Measure\|TestWebCatalogProducts_Weight\|TestWebCatalogProducts_Shape)$' ./internal/httpapi/web/` | `ok elimaz.com.vn/crm/internal/httpapi/web 2.679s` |
| webcatalog đọc ba cột qua products_v (ProductsByID, Products) | `TestProducts_Measure` | `go test -race -count=1 -run '^(TestProducts_Measure\|TestProducts_VariantFields)$' ./internal/queries/webcatalog/` | `ok elimaz.com.vn/crm/internal/queries/webcatalog 2.120s` |
| Seed fixture ghi/đổi/bỏ số đo, chạy lại 0 thay đổi; số đo sai → errBadFixture | `TestSeedWebFixture_Measure`, `TestParseWebFixture_Rejects` | `go test -race -count=1 -run '^(TestSeedWebFixture_Measure\|TestSeedWebFixture_CreateRerunUpdate)$' ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 2.859s` |
| Test dựng dữ liệu trên schema cũ vẫn chạy (đỏ ở lượt ci-task đầu) | `TestSchemaExtendsM1Orders`, `TestReturnItemsQty_BackfillM2`, `TestMigrateM1Data`, `TestMigrateM1CashData` | `go test -count=1 -run … ./internal/modules/{order,fulfillment,cash}/` | `ok …/order 2.394s`, `ok …/fulfillment 2.663s`, `ok …/cash 1.722s` |
| Màn hình: luật số đo (đọc, kiểm, tạo, sửa gửi measureCm/null, lưu xong không còn thay đổi) | `editRules.test.ts` khối «số đo Dài/Rộng/Cao (Task 516)» (5 ca) | `npx vitest run src/screens/products/` | `Test Files 11 passed, Tests 166 passed` |
| Màn hình: ba ô hiện số đã lưu, sửa gửi đúng; sai → câu dưới ô, không gửi; tạo gửi measureCm | `ProductEdit.test.tsx` khối «ProductEdit — số đo (Task 516)» (3 ca) | `npx vitest run src/screens/products/ProductEdit.test.tsx` | `Tests 27 passed` |
| Cổng kiểm của task (cổng tĩnh + test gói đổi, phụ thuộc, -race gói viết tay) | ci-task | `ci-task.sh` (lượt 1 ĐỎ: console-check prettier + 4 test schema cũ, đã sửa; lượt 2 bị dừng giữa chừng do giới hạn lệnh nền; lượt 3 xanh) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/45948165782` · `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 6848b446…)` |
| Cổng tĩnh tự soát | — | `make jscpd`; `golangci-lint run` 5 gói; `go test ./tests/arch/`; `npx tsc --noEmit`; `npx eslint src/screens/products/`; `npx prettier --check src` | `Found 0 clones`; `0 issues`; `ok tests/arch 468s`; tsc, eslint không lỗi; `All matched files use Prettier code style!` |

## Quyết định thay chủ shop

1. **Số nguyên cm 1–1000.** Website nhận số dương bất kỳ (`z.number().positive()`), nhưng chốt 27/11 ghi «1–1000»; số nguyên đủ cho số đo quần áo, khớp ô nhập, CHECK đơn giản. Khác: cho số lẻ (numeric(6,1)) — có thể mở rộng sau bằng migration chỉ thêm/đổi kiểu (đảo được).
2. **Số migration 0535** (không phải 0534): lúc bắt đầu main dùng tới 0533 và worktree task-200 có tệp chưa commit trùng 0533 (khả năng đổi sang 0534); bỏ trống 0534 tránh trùng. Sau khi gộp main, Task 200 lấy 0540 → 0535 không trùng (đã kiểm main và mọi nhánh task-*). Khác: 0534 (rủi ro trùng lúc đó).
3. **Sửa (PATCH):** `measureCm` vắng = giữ nguyên; `null` = bỏ cả ba số; một đối tượng = đặt đúng ba số đó (số vắng là bỏ số đó). Màn hình gửi cả đối tượng khi một trong ba ô đổi (đúng luật «chỉ gửi trường người dùng đổi» ở mức trường `measureCm`). Khác: ba trường phẳng `lengthCm`… (nhiều trường, khó gửi "bỏ").
4. **Nhập Excel:** ba cột tùy chọn «Dài (cm)», «Rộng (cm)», «Cao (cm)» có trong tệp mẫu nhưng không bắt buộc ở tiêu đề (tệp mẫu cũ vẫn nhập được); ô trống ở mẫu có sẵn là giữ nguyên (như Mô tả, Danh mục); ô có số thì đặt số đó; các dòng cùng mã mẫu phải giống nhau. Không xóa số đo qua Excel (xóa ở màn sản phẩm). Khác: ô trống = xóa (dễ mất dữ liệu khi dùng tệp cũ).
5. **Website:** `measureCm` vắng khi mẫu chưa có số nào; chỉ có các số đã nhập (đúng `measureOf` của website: «only the figures entered»).
6. **Màn hình không có mockup riêng:** ba ô số nhỏ cùng hàng trong thẻ Thông tin, dưới Mô tả ngắn, dùng đúng `Field`, lớp ô nhập có sẵn (không màu, cỡ chữ mới). Khác: chờ mockup (chặn việc, chủ shop đã chốt «LÀM»).
7. **Seed-web-fixture** ghi số đo của tệp fixture website vào mẫu CRM (để web nguồn CRM có khối «Số đo» như hôm nay khi chạy e2e); số không nguyên hay ngoài 1–1000 → lỗi tệp.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Seed-web-fixture ghi số đo của tệp fixture (không ghi trong mô tả task; cần để web nguồn CRM có khối «Số đo» khi chạy e2e — xem Quyết định 7).
- Đổi tên hai hàm chưa xuất của catalog (`weightOf`→`optIntOf`, `weightParam`→`optInt4`) trong cùng commit hành vi: chỉ là tên, để dùng chung cho số đo (tránh hàm trùng jscpd báo); không đổi hành vi cân nặng (`TestUpdateVariant_Weight` vẫn qua).

## Việc phát hiện thêm

- Website (task web riêng): `apps/storefront-crm/src/server/crm/mapping.ts` chưa đọc `measureCm` của WebProduct (CRM giờ đã trả); cần sinh lại `contracts.gen.ts` và nối `measureCm` vào sản phẩm để khối «Số đo» hiện ở web nguồn CRM.
- Lệnh nhập dữ liệu website vào CRM (Task 467, chạy khô) chưa mang số đo Nhanh («Kích thước») sang; website nhận cả số lẻ, CRM chỉ nhận số nguyên 1–1000 → khi nối cần quy tắc làm tròn hoặc báo lỗi.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có ca e2e cho task này; đủ ca ở test gói catalog, httpapi, web, webcatalog, cmd/crm và vitest)

## Reviewer

Kết luận: ĐẠT (lượt 1, 55 tệp đã stage, sau khi gộp main 6848b446)

- Phát hiện phải sửa: không có.
- Ảnh hưởng dự kiến so với thực tế: khớp từng dòng (ký hiệu catalog, 3 chỗ dựng ProductInput/ProductPatch, đổi tên optIntOf/optInt4 không còn tên cũ, web.MeasureOut 2 chỗ, webcatalog, importer, migration 0535 chỉ thêm, sqlc SetProductMeasure, OpenAPI và mã sinh, errors, views.md, console). models.go của các module là mã sqlc sinh lại.
- Hợp đồng website: CRM chỉ trả length/width/height ≥ 1, bỏ trường khi không có số nào → khớp `contracts.ts`. Fixture website chỉ có số nguyên 1–1000.
- Tái dùng, cách cũ còn sót, test bị xóa/tắt, luật dự án: không có vấn đề.
- Ghi chú không bắt buộc (không sửa): `TestProductMeasure_DatabaseCheck` có thể kiểm thêm SQLSTATE 23514 thay vì chỉ `err != nil`.
