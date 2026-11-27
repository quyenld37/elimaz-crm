# Task 513 — Thứ tự biến thể: vị trí + kéo thả trong CRM, web theo thứ tự

Kế hoạch: M5 `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (số do điều phối M5 cấp 27/11; chủ shop 27/11 sáng chọn «Kéo thả tự xếp», sau câu hỏi ở «Dừng để hỏi» của Task 511). Loại việc (quy trình mục 3): A (thêm cột, operation, màn; không phá vỡ).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**:
  - `catalog.Writer.ReorderVariants(ctx, tx, sellerID, productID, version, variantIDs, actor) error` (khuôn `ReorderImages`),
    sentinel mới `catalog.ErrVariantOrderMismatch` (danh sách không đúng tập biến thể của mẫu: thiếu, thừa, trùng, mã lạ).
    Không đổi chữ ký, kiểu có sẵn (`Variant` không thêm trường: thứ tự là thứ tự của danh sách).
  - `gen` (ogen sinh): operation `reorderProductVariants`, kiểu `ReorderProductVariantsRequest`; handler mới
    `(*handler).ReorderProductVariants` (`internal/httpapi/handlers_catalog_images.go`, dùng lại `imageWrite`: trả mẫu kèm ảnh).
  - `cmd/crm` (gói main): bước sắp thứ tự trong `fixtureApply.variants` (seed-web-fixture chạy lại: thứ tự theo tệp).
- Gói phụ thuộc ngược (go list): `catalog` ← `cmd/crm`, `internal/httpapi`, `internal/httpapi/web`, `internal/modules/content`,
  `inventory/invtest`, `order`, `testkit/shoptest`, `wiring/webcache`, `tools/loadkit` (không chỗ nào phải sửa: chỉ thêm);
  `queries/webcatalog` ← `internal/httpapi`, `internal/httpapi/web`, `wiring/webcache` (chỉ đổi thứ tự dòng).
- Operation OpenAPI bị chạm, oasdiff: thêm `PUT /api/v1/catalog/products/{id}/variants/order` (`reorderProductVariants`,
  `x-permission: catalog.manage`, `IdempotencyKey`, thân `{productVersion, variantIds[1..200]}`, 200 → `Product`);
  chỉ sửa mô tả của `Product.variants`, `WebProduct.variants` (thứ tự). Không phá vỡ (oasdiff chỉ thêm).
- Chỗ dùng trên màn hình: `console/src/screens/products/VariantGrid.tsx` (cột kéo thả + nút ↑ ↓, phím ↑ ↓ trên tay nắm),
  `editRules.ts` (giữ thứ tự máy chủ, bước lưu `order`, `applyOrder`, bỏ sắp lại theo màu × size của dòng đã lưu),
  `ProductEdit.tsx` (chạy bước `order`); test `VariantGrid.test.tsx`, `editRules.test.ts`, `ProductEdit.test.tsx`.
  Client sinh lại `console/src/api/**`.
- Câu sqlc, bảng, pha migration: migration mới **0532** (pha mở rộng, chỉ thêm): `catalog.variants.position int NOT NULL
  DEFAULT 0 CHECK (position >= 0)`, điền một lần bằng `row_number() OVER (PARTITION BY tenant_id, product_id ORDER BY
  created_at, id)` (tạm `NO FORCE ROW LEVEL SECURITY` như 0201, ép lại ngay), `GRANT UPDATE (position)`; trigger
  `variants_position` (`BEFORE INSERT`, hàm `catalog.variants_position_default`, quyền người chèn): câu chèn để vị trí 0 →
  cuối + 1 trong mẫu (Quyết định 8); `catalog.variants_v` nối `position` vào cuối (`docs/views.md`). `InsertVariant` chỉ đổi
  chú thích (không nhắc cột mới), `LockVariant` không đổi. Câu sửa: `ListProductVariants` `ORDER BY position, id`; câu mới
  `SetVariantPositions` (một UPDATE từ `unnest … WITH ORDINALITY`); `queries/webcatalog/products.sql` `VariantsOf` `ORDER BY product_id, position, variant_id`;
  Mã sinh lại: `catalog/internal/store`, `queries/webcatalog/internal/store`, `models.go` của mọi khối sqlc (cột mới của
  bảng/view chung).
- Test sẽ chạy: catalog `TestReorderVariants` (thứ tự, version, sự kiện, audit, biến thể mới vào cuối), `TestReorderVariants_Invalid`,
  `TestReorderVariants_Concurrent`, `TestVariantPositionBackfill` (migration điền đúng theo created_at, id),
  `TestVariantPositionTrigger` (chèn không vị trí → cuối, có vị trí → giữ, mẫu riêng); test migration dữ liệu cũ
  `TestMigrateM1Data`, `TestMigrateM1CashData`, `TestSchemaExtendsM1Orders`, `TestReturnItemsQty_BackfillM2` (đỏ ở lượt ci đầu); httpapi
  `TestCatalogAPI_ReorderVariants` (200, 409, 422, 403, 404, gửi lại cùng khóa); webcatalog `TestProducts_VariantOrder`;
cmd/crm `TestSeedWebFixture_VariantOrder`; console vitest
  `editRules.test.ts`, `VariantGrid.test.tsx`, `ProductEdit.test.tsx`; gói đổi và phụ thuộc qua `ci-task`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm sắp thứ tự chung);
  tìm `Reorder`: `catalog.Writer.ReorderImages` (cùng module, cùng khuôn: khóa mẫu theo version → kiểm đúng tập → ghi vị trí →
  `imagesChanged`), `fulfillment.ReorderWebCarriers` (module khác, giữ riêng). Màn: `ImageManager.tsx` (kéo thả HTML5, nút ← →),
  `imageRules.moveTo` (dời phần tử), mockup `m5/web-carriers.html` (nút ↑ ↓, `.ord`).
- Dùng lại / viết mới vì: dùng lại `lockProductVersion`, `bumpProduct` (product.updated cho luồng thay đổi website),
  `audit`, handler `imageWrite` + `productWithImages` (câu trả lời giống `reorderProductImages`), `moveTo` của `imageRules`
  cho màn, sentinel → 422 `invalid_input` (mã có sẵn, như `ErrInvalidWeight`: không thêm mã lỗi). Viết mới `ReorderVariants`
  (không gộp với `ReorderImages`: tập khác — ảnh đang hiện / mọi biến thể —, lỗi khác, lý do đổi khác); câu ghi một lần
  `SetVariantPositions` thay vòng lặp từng dòng (≤ 200 dòng, một câu).

## Soát thiết kế

- 5 (khớp kiến trúc): logic ở module `catalog` (Writer, transaction của bên gọi, audit + sự kiện trong cùng tx); handler mỏng
  qua `catalogWriteAsDefaultSeller` (WriteTx, người bán mặc định, ADR-29: mọi câu có `seller_id`); SQL tĩnh qua sqlc; web chỉ đọc
  view `variants_v` (cột mới nối cuối); lỗi đổi HTTP ở `errors_catalog.go`. Không thư viện mới (màn: kéo thả HTML5 như ảnh).
- 6.1 đồng thời: khóa dòng mẫu rồi so `version` (hai người cùng version: đúng một người thành công, test); thêm biến thể
  (`AddVariant`, `CreateProduct`, nhập Excel qua cùng Writer) giữ khóa mẫu khi trigger tính `max(position)+1` nên không trùng
  vị trí; chèn thẳng không khóa (chỉ test, seed) có thể trùng — vô hại vì đọc theo `(position, id)`.
- 6.2 gửi lại: `Idempotency-Key` (middleware, test gửi lại cùng khóa trả cùng kết quả, version tăng một lần).
- 6.3 thứ tự khóa: mẫu → biến thể (ADR-46 nhóm 4, như `UpdateVariant`); câu UPDATE biến thể không khóa bảng khác.
- 6.4, 6.5, 6.9: dùng WriteTx/RetryTx có sẵn; hàm không cộng dồn biến ngoài (chạy lại được).
- 6.6–6.8 việc nền, 429, tiến trình chết: không áp dụng (không việc nền, không gọi ngoài).
- 6.10 tiền: không áp dụng. 6.11 quy luật: vị trí là 1..n đúng tập biến thể sau mỗi lần sắp (test kiểm trên database).
- 6.12 dữ liệu cá nhân: không áp dụng (audit ghi mã biến thể trước/sau). 6.13 quyền: `catalog.manage` ở hợp đồng (route sinh),
  test 403 người chỉ xem; màn ẩn tay nắm, nút khi chỉ xem nhưng API vẫn chặn.
- Migration: pha mở rộng; bản cũ của máy chủ (nếu chạy song song khi triển khai) chèn biến thể không nhắc `position` →
  trigger đặt vào cuối, đúng như bản mới; không ràng buộc unique trên `(product_id, position)` để bản cũ và
  dữ liệu thử chèn thẳng (nhiều test) không vỡ.

## Quyết định thay chủ shop

1. **Đường và thân operation**: `PUT /api/v1/catalog/products/{id}/variants/order` thân `{productVersion, variantIds}` (khuôn
   có sẵn `…/images/order`) thay ví dụ `PUT /api/v1/products/{id}/variant-order {variantIds, version}` của điều phối: mọi
   đường danh mục nằm dưới `/api/v1/catalog/`, trường version của mẫu ở các thao tác con là `productVersion`. Đảo lại: đổi đường
   (chưa ai dùng). Câu trả lời là `Product` (như sắp ảnh) để màn nhận version mới.
2. **422 `invalid_input`** khi danh sách không đúng tập biến thể (đúng ý điều phối 422), dùng mã có sẵn `invalid_input` như
   `ErrInvalidWeight` thay vì thêm mã mới (không phải thêm câu tiếng Việt, không đổi `x-error-codes`). Sắp ảnh vẫn 400 như cũ.
3. **Không ràng buộc unique `(tenant_id, product_id, position)`**: thứ tự đọc là `(position, id)` nên trùng vị trí vẫn có thứ
   tự ổn định; unique làm vỡ bản cũ khi triển khai và các test chèn thẳng `catalog.variants` (mặc định 0). Đảo lại: migration
   thu hẹp sau thêm unique `DEFERRABLE`.
4. **Không index mới**: đọc biến thể theo mẫu đã có `variants_product_idx (tenant_id, product_id)`; sắp ≤ 200 dòng trong bộ nhớ.
5. **Không tăng `version` của biến thể khi sắp** (chỉ version mẫu +1, `updated_at` biến thể = now()): thứ tự là thuộc tính của
   mẫu; tăng version biến thể làm bản nháp đang mở báo xung đột oan khi sửa giá sau khi kéo.
6. **Màn: thứ tự là một phần của bản nháp, lưu bằng nút Lưu** (bước `order` sau bước thêm biến thể, trước bước sửa biến thể),
   không lưu ngay mỗi lần kéo như ảnh: lưới biến thể vốn là bản nháp có nút Lưu; dòng mới chưa lưu cũng kéo được. Dòng đã lưu
   giữ thứ tự của máy chủ; dòng mới (chưa ai kéo) xếp theo màu × size sau các dòng đã lưu như trước; đã kéo thì dòng mới
   sinh thêm vào cuối.
7. **Màu, cỡ (giá trị)**: không thêm thứ tự riêng cho giá trị màu/cỡ. Website và CRM suy thứ tự màu, cỡ từ lần xuất hiện đầu
   tiên trong danh sách biến thể (`draftFromProduct`; website đọc mảng `variants`), nên thứ tự biến thể đủ quyết định cả hai.

8. **Vị trí của dòng mới do trigger điền, không do câu `InsertVariant`** (đổi sau lượt ci-task đầu đỏ ở 4 test migration
   dựng dữ liệu trên schema cũ bằng `catalogtest`/`shiptest`, vốn gọi `InsertVariant`): câu chèn giữ nguyên như trước task,
   chạy trên mọi pha schema; bản cũ của máy chủ và dữ liệu thử chèn thẳng cũng vào cuối. Phương án khác: sửa từng test cũ
   chèn bằng câu riêng (nhiều chỗ, dễ sót) hay để `catalog.Variant` có trường `Position` (không ai dùng ngoài test: bỏ).

## Mockup và ảnh chụp

- `docs/mockups/m5/variant-order.html` (thẻ Biến thể của màn Sửa mẫu, cột Thứ tự: A đang sửa, B đang kéo + dòng mới + tên
  màu dài, C chỉ xem), ảnh `docs/mockups/m5/shots/variant-order-1512.png`, `variant-order-390.png`, `variant-order-360.png`;
  dòng (13) trong `docs/mockups/m5/README.md`. Tự soát theo hệ thiết kế M1 (`.ibtn`, `.ord` như web-carriers, màu có sẵn
  `--surface`, `--edge`), điều phối viên chốt theo Q-X8.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có e2e cho task này; đủ ca ở test gói và vitest)

## Cho website

- API web (`webProducts`, `webProductsByIds`, luồng thay đổi) trả `variants` theo vị trí nhân viên đặt (rồi mã). Sắp lại tăng
  `version` và `updated_at` của mẫu, phát `product.updated` → luồng thay đổi catalog trả mẫu đó, bộ đệm web làm mới như mọi
  lần sửa mẫu. Website **phải giữ đúng thứ tự mảng** `variants`: nếu mapping bên website đang sắp biến thể theo id, SKU, tên
  màu/cỡ thì bỏ, và suy thứ tự màu, cỡ theo lần xuất hiện đầu tiên trong mảng. Không trường mới (không có `position` trong
  `WebVariant`). Điều phối viên mở task website sau.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Sắp lại: thứ tự mới, vị trí 1..n, version mẫu +1 (biến thể giữ), product.updated, audit, biến thể mới vào cuối, version cũ 409 | `TestReorderVariants` | `go test -race -count=1 -run '^(TestReorderVariants\|TestReorderVariants_Invalid\|TestReorderVariants_Concurrent\|TestVariantPositionBackfill\|TestVariantPositionTrigger)$' -v ./internal/modules/catalog/` | `--- PASS: TestReorderVariants (2.18s)` |
| Danh sách sai tập (thiếu, thừa, trùng, mã lạ, mẫu người bán khác) → sentinel, không đổi | `TestReorderVariants_Invalid` | như trên | `--- PASS: TestReorderVariants_Invalid (2.18s)` |
| Hai người cùng version: đúng một thành công; vị trí vẫn 1..n | `TestReorderVariants_Concurrent` (và `TestReorderImages_Concurrent` dùng chung `oneWinner`) | như trên | `--- PASS: TestReorderVariants_Concurrent (2.14s)` |
| Migration điền vị trí theo (created_at, id), từng mẫu, RLS ép lại | `TestVariantPositionBackfill` | như trên | `--- PASS: TestVariantPositionBackfill (2.14s)` |
| Trigger: chèn không vị trí → cuối + 1; có vị trí → giữ; mẫu riêng | `TestVariantPositionTrigger` | `go test -race -count=1 -run '^TestVariantPositionTrigger$' ./internal/modules/catalog/` | `ok … catalog 1.885s` |
| Dữ liệu cũ dựng trên schema trước 0532 vẫn chạy | `TestMigrateM1Data`, `TestSchemaExtendsM1Orders`, `TestReturnItemsQty_BackfillM2`, `TestMigrateM1CashData` | `go test -count=1 -run '^…$' ./internal/modules/fulfillment/` (và order, cash) | `ok` cả ba gói (đỏ ở lượt ci đầu, trước trigger) |
| HTTP: 200 trả mẫu theo thứ tự, 409, 422 invalid_input, 403 chỉ xem, 404 tenant khác, gửi lại cùng khóa | `TestCatalogAPI_ReorderVariants` | `go test -race -count=1 -run '^TestCatalogAPI_ReorderVariants$' ./internal/httpapi/` | `ok … httpapi 2.290s` |
| API web trả biến thể theo vị trí rồi mã | `TestProducts_VariantOrder`, `TestProducts_VariantFields` | `go test -race -count=1 -run '^(TestProducts_VariantFields\|TestProducts_VariantOrder)$' -v ./internal/queries/webcatalog/` | `--- PASS` cả hai |
| seed-web-fixture: thứ tự như tệp, tệp đổi → sắp lại, biến thể chỉ ở CRM đứng sau | `TestSeedWebFixture_VariantOrder`, `TestSeedWebFixture_CreatedAtOrder` | `go test -race -count=1 -run '^(TestSeedWebFixture_VariantOrder\|TestSeedWebFixture_CreatedAtOrder)$' -v ./cmd/crm/` | `--- PASS` cả hai |
| Màn: luật thứ tự (giữ thứ tự máy chủ, moveRow, dòng mới, bước order, applyOrder, createBody) | `editRules.test.ts` › "thứ tự biến thể (Task 513)" (9 ca) | `cd console && npx vitest run src/screens/products/` | `Tests 158 passed (158)` |
| Màn: nút ↑ ↓, phím ↑ ↓ giữ tiêu điểm, kéo thả, đang lưu không kéo được (sau review), chỉ xem hiện số, tên dài | `VariantGrid.test.tsx` › "thứ tự (Task 513…)" (6 ca) | như trên | như trên |
| Màn: Lưu gửi addVariant → reorderProductVariants (version kế, khóa chống trùng) → updateVariant; 409 dừng | `ProductEdit.test.tsx` (2 ca mới; ca chỉ xem kiểm không nút) | như trên | như trên |
| Cả bộ màn, kiểu, lint | — | `npx vitest run`; `npx tsc --noEmit`; `npx eslint src/screens/products/` | `Tests 1706 passed (1706)`; không lỗi |
| Cổng tĩnh + test gói đổi và phụ thuộc | — | ci-task (lần 2) | `CI-TASK XANH (từ xa trên blacksmith…)`, run 45934306790; lần 1 đỏ 4 test migration → sửa bằng trigger (Quyết định 8) |
| Test kiến trúc, mã lặp, lint | — | `go test -count=1 ./tests/arch/`; `make jscpd`; `golangci-lint run` (catalog, webcatalog, cmd/crm, httpapi) | `ok`; `Found 0 clones`; `0 issues` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Ô màu của món trong bộ phối (`queries/content/looks.sql` `LookProductColors`, Task 488) vẫn xếp màu theo SKU nhỏ nhất
  (có test `TestLoadLookRefData_Colors` khóa luật đó), nên có thể khác thứ tự màu trên trang sản phẩm sau khi nhân viên kéo
  thả. Đổi sang «vị trí biến thể đầu tiên của màu» là đổi hành vi của Task 488 (màn bộ phối, `/api/v1/web/looks`), ngoài
  phạm vi danh mục web của task này: cần chủ shop chọn.
- `catalog.variants_v.position` và cột `position` không có index riêng; đọc theo mẫu dùng `variants_product_idx`.

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA. (1) Đang lưu, dòng biến thể vẫn kéo thả được (fieldset chỉ khóa nút) → `VariantGrid` nhận `busy`,
  `ProductEdit` truyền `busy={saving}`, khóa draggable/drop; ca vitest "đang lưu: dòng không kéo được". (2) Mục Ảnh hưởng tả
  sai `InsertVariant`/`LockVariant` → viết lại theo code (trigger). Ghi chú: câu báo lỗi mẫu 2 của `TestProducts_VariantOrder`
  sửa cho đúng. Backend không có lỗi tính đúng.
- Lượt 2: ĐẠT, không phát hiện phải sửa. Ghi chú không bắt buộc: lưu bắt đầu đúng lúc đang kéo thì `dragging` có thể còn
  giữ tới lần kéo sau (không đổi thứ tự sai; gần như không xảy ra).
