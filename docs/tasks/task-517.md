# Task 517 — Số đo một chữ số thập phân, nhập số đo từ Nhanh

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (task điều phối 27/11, sau Task 516). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (grep, gopls không chạy được trên worktree này; đếm tay):
  - `catalog.MeasureCm` → `catalog.MeasureMm` (đơn vị mm số nguyên = cm một chữ số thập phân): 3 trường dùng ở catalog
    (`ProductInput`, `ProductPatch`, `Product`), `imports_apply_write.go` (2), `products.go` (4), `cmd/crm/seed_web_fixture*.go` (4),
    `internal/httpapi/handlers_catalog.go` (3), test `measure_test.go`.
  - `catalog.MinMeasureCm/MaxMeasureCm` → `MinMeasureMm = 1`, `MaxMeasureMm = 10000`; mới `catalog.MeasureMmFromCm(float64) (int, bool)`,
    `catalog.MeasureCmOf(int) float64`.
  - `importer.MinMeasureCm/MaxMeasureCm`, `ImportPlannedProduct.LengthCm/WidthCm/HeightCm` → `…Mm` (json `lengthMm`…: bản kế hoạch nhập
    lưu ở `catalog.imports.plan` chỉ sống tới khi áp; bản đã lưu dạng cũ đọc ra 0 → mẫu mới thiếu số đo — chấp nhận, xem Quyết định 5).
  - Không xuất ra ngoài nhưng đổi: audit `productAudit` (`events.go`) khóa `lengthMm/widthMm/heightMm`; `fixtureMeasure.cm()` →
    `mm()` (`cmd/crm/seed_web_fixture*.go`); `measureIn` của httpapi trả thêm lỗi; mới `importer.MeasureMmFromCm`
    (`internal/importer/measure.go`), `cmd/crm/import_website_measure.go` (`wxMeasure`, `wxProductMeasure`, `measures`),
    `kindMeasure` thêm đầu `importKinds()`.
  - `webcatalog.MeasureCm` → `webcatalog.MeasureMm` (`WebProduct.MeasureCm` → `MeasureMm`); `web.MeasureOut` nhận mm, trả cm số thực.
- Gói phụ thuộc ngược (go list): `internal/modules/catalog` có 9 gói nhập (httpapi, httpapi/web, cmd/crm, order, inventory,
  catalogtest…; chỉ httpapi, cmd/crm dùng ký hiệu số đo); `internal/queries/webcatalog` ← `internal/httpapi/web`, `internal/httpapi`.
  models.go sqlc của mọi module sinh lại (đổi tên cột của bảng chung; gồm `reporting` có sau khi gộp main 9634b595).
- Operation OpenAPI (thêm 10 dòng duyệt ở `openapi/breaking-approved.yaml`): schema `MeasureCm` (dùng ở createProduct, updateProduct, Product, WebProduct): `integer/int32` → `number/double`
  (bội 0,1). Tên trường giữ nguyên. oasdiff có thể báo đổi kiểu trả về; website đọc `z.number().positive()` nên không vỡ.
- Chỗ dùng trên màn hình: `console/src/screens/products/editRules.ts` (ô số đo nhận «72,5» hay «72.5», hiện «72,5»),
  `ProductEdit.tsx` (gợi ý ô, `inputMode="decimal"`), `console/src/api/types.gen.ts` sinh lại.
- Câu sqlc, bảng, pha migration: `queries/catalog/products.sql` (SetProductMeasure, GetProduct, LockProduct, UpdateProduct),
  `queries/webcatalog/products.sql` (ProductPage, ProductsByIDs); bảng `catalog.products`, view `catalog.products_v`.
  Migration 0541 **thu hẹp** (RENAME cột bảng và view `length_cm`→`length_mm`…, nhân 10, thay CHECK 1–10000) có dòng
  `-- contract:`; `docs/views.md` sửa dòng `products_v`.
- Lệnh nhập website (Task 467): `cmd/crm/import_website*.go` thêm mục tùy chọn `productMeasures` của data.json và loại «số đo».
- Test sẽ chạy: catalog `TestProductMeasure_Create/_Update/_DatabaseCheck`, `TestImportMeasure_Apply/_Errors`,
  `TestImportRules*`; importer `TestValidateMeasure`, `TestMeasureMmFromCm_RoundTrip` (rapid); httpapi
  `TestCatalogAPI_ProductMeasure`; web `TestWebCatalogProducts_Measure`; webcatalog `TestProducts_Measure`; cmd/crm
  `TestNhanhMeasure`, `TestImportWebsite_ProductMeasures`, `TestImportWebsite_SampleDryRunApplyAgain`,
  `TestSeedWebFixture_Measure`, `TestParseWebFixture_Rejects`; db `TestMigration0541_MeasureCmToMm`; vitest
  `editRules.test.ts`, `ProductEdit.test.tsx`; cả gói ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có kiểu số thập phân cố định);
  grep `Decimal|tenths|FromCm` — không có. Cân nặng dùng gram số nguyên (cùng cách: đơn vị nhỏ số nguyên).
- Dùng lại / viết mới vì: dùng lại `optIntOf`/`optInt4`, `catalog.UpdateProduct` (có version, sự kiện, audit) cho lệnh nhập,
  `db.Savepoint`/`importCount` của lệnh nhập. Đổi cm↔mm viết một chỗ trong `importer` (gói con, catalog nhập nó; catalog chỉ gọi
  lại), dùng ở catalog, httpapi, cmd/crm.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`.

- 5.1 Đúng chủ dữ liệu: số đo thuộc catalog (`catalog.products`, schema catalog).
- 5.2 Không ghi bảng module khác: lệnh nhập (cmd/crm) ghi qua public API `catalog.Writer.UpdateProduct`; không SQL thẳng.
- 5.3 Đọc qua view: webcatalog đọc `catalog.products_v` (cột đổi tên `length_mm`…; `docs/views.md` cập nhật).
- 5.4 Nguyên tử: mỗi mẫu một savepoint trong transaction của loại «số đo mẫu»; lỗi một mẫu không hỏng mẫu khác.
- 5.5 Sau commit: `UpdateProduct` phát `product.updated` qua `events.Emit` trong tx (bộ đệm web xử lý sau commit).
- 5.6 Gọi ra ngoài: không áp dụng (không gọi Nhanh hay website; đọc data.json đã xuất).
- 5.7 Ngữ cảnh shop: lệnh nhập chạy trong `tenancy.WithTenant`; migration tạm `NO FORCE ROW LEVEL SECURITY` để nhân 10
  mọi tenant rồi ép lại ngay trong cùng transaction (như 0533), có test `relforcerowsecurity`.
- 5.8 Nghiệp vụ không biết HTTP: catalog chỉ nhận mm `int`; đổi số thực cm ↔ mm ở httpapi, cmd/crm (hàm của catalog).
- 5.9 Đường ghi: không thêm đường mới; createProduct, updateProduct đã có `Idempotency-Key`, `x-permission`.
- 5.10 PostgreSQL nguồn sự thật: không áp dụng (không dùng Valkey).
- 5.11 Thư viện: không thêm.
- 5.12 Theo module mẫu: catalog (version khóa dòng rồi so, audit trong tx của bên gọi, sentinel `ErrInvalidMeasure`).
- 6.1 Sửa đồng thời: `UpdateProduct` khóa dòng mẫu, so `version` đọc ngay trong tx; nhân viên sửa cùng lúc → lệnh nhập
  gặp `ErrVersionConflict`, mục đó ghi lỗi (chạy lại lệnh là xong).
- 6.2 Gửi lại: chạy lại lệnh nhập → mẫu đã có số đo bị bỏ qua (0 thay đổi, có test).
- 6.3 Thứ tự khóa: loại «số đo mẫu» chạy đầu (catalog nhóm 4 ADR-46, trước customer nhóm 5); mỗi savepoint chỉ khóa một
  dòng `catalog.products`.
- 6.4 Lỗi tạm database: lệnh vận hành chạy qua `tenancy.WithTenant` (RetryTx); thân hàm gán lại bảng đếm (như các loại khác).
- 6.5 Cách ly: mặc định READ COMMITTED + khóa dòng.
- 6.6–6.8 Việc nền, 429, tiến trình chết: không áp dụng (lệnh chạy tay; transaction tự lùi).
- 6.9 Treo lâu: không áp dụng ngoài giới hạn chung (mỗi mẫu một câu UPDATE).
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: rapid `TestMeasureMmFromCm_RoundTrip` (mọi mm đổi ra cm rồi về đúng; hai số lẻ bị từ chối).
- 6.12 Dữ liệu cá nhân: không có (số đo, mã mẫu; bảng in chỉ mã mẫu và lý do).
- 6.13 Quyền: không đổi (catalog.manage cho API; lệnh vận hành chạy trong container).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Migration 0541: cm → mm ×10 mọi tenant (RLS ép), rỗng giữ rỗng, view đổi tên cột, CHECK 1–10000 (23514), ép RLS lại | `TestMigration0541_MeasureCmToMm` | `go test -count=1 -run '^TestMigration0541_MeasureCmToMm$' ./internal/platform/db/` | `ok elimaz.com.vn/crm/internal/platform/db 1.670s` |
| Catalog lưu mm 1–10000 (72,5 cm = 725), sửa có version, audit `lengthMm`, CHECK database | `TestProductMeasure_Create`, `_Update`, `_DatabaseCheck` | `go test -count=1 -run '^(TestProductMeasure_Create\|TestProductMeasure_Update\|TestProductMeasure_DatabaseCheck\|TestImportMeasure_Apply\|TestImportMeasure_Errors\|TestImportRules.*)$' ./internal/modules/catalog/` | `ok …/catalog 0.918s` (sau sửa khóa audit) |
| Excel: «72,5», «72.5», «0,1», «1000,0» nhận; 0, 1000,1, 2,55, 1.000, chữ → `invalid_measure`; cùng mẫu 72,5 = 72.5 | `TestValidateMeasure`, `TestImportMeasure_Apply`, `TestImportMeasure_Errors` | `go test -count=1 ./internal/modules/catalog/internal/importer/` + lệnh trên | `ok …/importer 0.493s` |
| Đổi cm ↔ mm đúng vòng tròn, từ chối hai số lẻ, ngoài khoảng, NaN/Inf (rapid) | `TestMeasureMmFromCm_RoundTrip` | như trên | `ok` |
| API nhân viên `measureCm` số thực: 72.5, 0.1, 1000, 3.5 nhận; 0, 72.55, 1000.1, 0.05, 20.25 → 422 không đổi gì | `TestCatalogAPI_ProductMeasure` | `go test -count=1 -run '^TestCatalogAPI_ProductMeasure$' ./internal/httpapi/` | `ok …/httpapi 1.166s` |
| API web trả `measureCm` cm (725 mm → 72.5, 1 mm → 0.1), chỉ số đã nhập | `TestWebCatalogProducts_Measure`, `TestProducts_Measure` | `go test -count=1 -run '^TestWebCatalogProducts_Measure$' ./internal/httpapi/web/`; `-run '^TestProducts_Measure$' ./internal/queries/webcatalog/` | `ok …/web 1.031s`; `ok …/webcatalog 1.192s` |
| Seed-web-fixture nhận cm một số lẻ, từ chối 2,55 và 1000,1 | `TestSeedWebFixture_Measure`, `TestParseWebFixture_Rejects` | `go test -count=1 -run '^(TestSeedWebFixture_Measure\|TestParseWebFixture.*)$' ./cmd/crm/` | `ok …/cmd/crm 2.171s` |
| Luật nguồn Nhanh: mẫu có số → của mẫu; không thì SKU đầu có số; chỉ số > 0; số sai → lỗi | `TestNhanhMeasure` | `go test -count=1 -v -run '^(TestNhanhMeasure\|TestImportWebsite_ProductMeasures\|TestImportWebsite_SampleDryRunApplyAgain)$' ./cmd/crm/` | `--- PASS: TestNhanhMeasure` |
| Lệnh nhập: chạy khô đếm không ghi; apply ghi số đo mẫu CRM chưa có; mẫu lạ, số sai → lỗi mục đó; in «có số đo 4, không số đo 1»; chạy lại / website đổi số → giữ của CRM; mẫu thư mục chuẩn: 2 thêm rồi 2 bỏ qua, database không đổi khi chạy khô | `TestImportWebsite_ProductMeasures`, `TestImportWebsite_SampleDryRunApplyAgain` | như trên | `--- PASS: TestImportWebsite_ProductMeasures (0.56s)`, `--- PASS: TestImportWebsite_SampleDryRunApplyAgain (0.60s)` |
| Màn hình: hiện «72,5», nhận phẩy hay chấm, gửi số thực, câu lỗi mới | `editRules.test.ts` «số đo Dài/Rộng/Cao (Task 516, 517)», `ProductEdit.test.tsx` «ProductEdit — số đo (Task 516, 517)» | `npx vitest run src/screens/products/editRules.test.ts src/screens/products/ProductEdit.test.tsx`; `npx tsc --noEmit`; `npx eslint src/screens/products/` | `Tests 64 passed (64)`; tsc, eslint không lỗi |
| Cổng kiểm của task (cổng tĩnh + TOÀN BỘ test: migration thu hẹp) | ci-task | `scripts/agent/ci-task.sh` (lượt 1 bị dừng do giới hạn lệnh nền; lượt 2 ĐỎ chỉ ở oasdiff — mọi test xanh — sửa bằng dòng duyệt; lượt 3 xanh) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/45972750731` · `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 9634b59559dd4a01a33892777b850386d3373cf7)` |
| Cổng tĩnh tự soát | — | `make jscpd`; `golangci-lint run` 6 gói; `go test ./tests/arch/` | `Found 0 clones`; `0 issues`; `ok tests/arch 90.092s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đổi tên `catalog.MeasureCm` → `MeasureMm` (và webcatalog, hằng Min/Max, trường bản kế hoạch nhập) nằm cùng commit với
  đổi hành vi: tên cũ nói sai đơn vị ngay khi giá trị thành mm, tách riêng sẽ có một commit tên đúng mà nghĩa sai hay
  ngược lại. Tên trường API `measureCm` giữ nguyên (website đọc).
- Lệnh `import-website-data` nhận mục mới tùy chọn `productMeasures` của data.json (website chưa xuất mục này, xem Việc
  phát hiện thêm). Không đổi `version` của data.json: mục tùy chọn, bản xuất cũ vẫn đọc được.

## Việc phát hiện thêm

- Website (task web riêng, không làm ở đây): `scripts/export-admin-data-for-crm.ts` thêm mục
  `productMeasures: [{productId, model: {length, width, height} | null, skus: [{length, width, height} | null, …]}]`
  (cm của Nhanh «Kích thước», SKU theo thứ tự Nhanh như `variants` của `nhanh-projection.ts`; 0 là trống). CRM áp
  đúng luật `measureOf` (mẫu có số → của mẫu; không thì SKU đầu có số; chỉ số > 0). Chưa có mục này thì lệnh nhập không
  nhập số đo (0 dòng ở loại «số đo mẫu»).
- Website nguồn CRM: `measure.ts` đã in số lẻ («72,5») — không cần sửa; chỉ cần nối `measureCm` của WebProduct (đã ghi
  ở Task 516).

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có ca e2e cho task này; đủ ca ở test gói catalog, importer, httpapi, web, webcatalog, db, cmd/crm và vitest)

## Quyết định thay chủ shop

1. **Lưu mm số nguyên** (`length_mm`, `width_mm`, `height_mm`, CHECK 1–10000 = 0,1–1000,0 cm): chính xác tuyệt đối, Go
   vẫn dùng `int`/`pgtype.Int4` như cân nặng (gram). Khác: `numeric(5,1)` (phải DROP/CREATE view, Go dùng
   `pgtype.Numeric`); thêm cột mm song song giữ cột cm (để lại cột chết tới lần thu hẹp sau).
2. **Migration 0541 thu hẹp ngay** (RENAME cột bảng và view, nhân 10, thay CHECK; dòng `-- contract:`), trái luật «mở
   rộng một bản, thu hẹp bản sau» vì cột cm của 0535 chỉ có ở `main`, chưa lên máy thật (không có bản đang chạy nào
   đọc nó). Đảo ngược: một migration đổi tên lại và chia 10 (số lẻ sẽ mất phần lẻ). Số 0541: main và mọi nhánh `task-*`
   dùng tới 0540 (kiểm 27/11 cả worktree chưa commit).
3. **API giữ tên `measureCm`, đơn vị cm, kiểu `integer` → `number`** (bội 0,1; sai → 422 `invalid_input`, không làm
   tròn). Website đọc `z.number().positive()` nên không vỡ. Màn hình nhận «72,5» hay «72.5», hiện «72,5». oasdiff báo
   phá vỡ (đổi kiểu yêu cầu ở createProduct, updateProduct; nới kiểu trả về ở 8 operation trả Product và
   webCatalogProducts): thêm 10 dòng vào `openapi/breaking-approved.yaml` theo quyền chủ shop giao 27/11 (đây đúng là
   yêu cầu của task). Điều phối viên xóa các dòng này sau khi gộp main (luật của tệp).
4. **Nhập từ Nhanh:** số có hơn một chữ số thập phân hay trên 1000 cm → lỗi của mẫu đó (không làm tròn); số ≤ 0 là
   trống như website; mẫu CRM đã có số đo nào → giữ của CRM (chạy lại 0 thay đổi, website đổi số cũng không ghi đè);
   mẫu không có trên CRM → lỗi. Ghi qua `catalog.Writer.UpdateProduct` (version, sự kiện `product.updated` cho bộ đệm
   web, audit `catalog.product.update` với `actor_id` = tài khoản hệ thống nhập, `request_id` = `import-website-data`;
   Writer ghi `actor_type` staff như mọi đường của catalog). Loại «số đo mẫu» chạy đầu (catalog nhóm 4 của ADR-46,
   trước góp ý nhóm 5).
5. **Bản kế hoạch nhập Excel đã lưu** dạng `lengthCm` (chỉ có từ Task 516, chưa lên máy thật): sau bản này đọc ra không
   có số đo → áp sẽ bỏ số đo của bản đó (mẫu mới thiếu số đo, mẫu có sẵn giữ). Chấp nhận: kiểm lại tệp là đủ.
6. **Khóa audit** đổi `lengthCm` → `lengthMm` (giá trị mm) cho đúng đơn vị.

## Reviewer

Kết luận: ĐẠT (lượt 2, 62 tệp đã stage, sau khi gộp main 9634b595)

- Lượt 1: CẦN SỬA — tệp task thiếu mục «Soát thiết kế» (bị mất khi viết lại mục Ảnh hưởng); mã không có lỗi. Đã thêm
  mục (5.1–5.12, 6.1–6.13), stage lại.
- Lượt 2: phát hiện phải sửa: không có. Ảnh hưởng khớp từng dòng (không còn tên cũ `MeasureCm`, `length_cm`,
  `MinMeasureCm`, `.cm()` ngoài migration cũ và phần vận đơn không liên quan). Luật `measure()` khớp `measureOf` của
  `nhanh-projection.ts`. Bảng bằng chứng: mọi test có thật và kiểm đúng tiêu chí. RetryTx không cộng dồn; sửa đồng
  thời → `ErrVersionConflict` của riêng mẫu đó.
- Ghi chú không bắt buộc (không sửa): mẫu có số đo sai được đếm vào «có số đo» (đã test, tài liệu nêu); migration 0541
  nhân 10 không tăng `version`/`updated_at` (website nhận cùng giá trị cm).

