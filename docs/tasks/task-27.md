# Task 27 — Catalog lõi

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 27 = M1-27). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu cũ.
  - Gói mới `internal/modules/catalog` (công khai theo Interfaces của kế hoạch, cộng `Writer`, xem "Khác kế hoạch"),
    `internal/modules/catalog/internal/{store,article,barcode}`, `internal/platform/textnorm` (`Fold`).
  - `httpapi.Deps`: thêm trường `Events *events.Registry` (8 chỗ dựng `Deps{…}`: `cmd/crm/main.go:280`,
    `tests/arch/routes_test.go:44`, `internal/platform/realtime/env_test.go:68`, 5 chỗ trong test của `httpapi`; trường
    mới để trống vẫn chạy, nên không chỗ nào phải sửa ngoài `cmd/crm`).
  - `gen.Handler` (mã sinh): thêm 5 phương thức; `handler` của `httpapi` hiện thực đủ (thiếu thì không dựng được).
- Gói phụ thuộc ngược (go list): `catalog` mới nên chưa ai nhập; `httpapi` được nhập bởi `cmd/crm`, `tests/arch`,
  `internal/platform/realtime` (test), `tests/e2e`. `textnorm` mới.
- Operation OpenAPI bị chạm, oasdiff: thêm 5 operation `createProduct`, `getProduct`, `updateProduct`, `addVariant`,
  `updateVariant` và schema mới; không sửa operation cũ → oasdiff không có thay đổi phá vỡ.
- Chỗ dùng trên màn hình: chưa có màn nào dùng (màn Sản phẩm là Task M1-50). Client sinh `console/src/api/**` (Task 17, đã gộp main) được sinh lại bằng `make gen`: chỉ thêm hàm, kiểu của 5 operation; `tsc`, `eslint`, `npm test` của console chạy lại.
- Câu sqlc, bảng, pha migration: migration mới `0114_catalog.sql` (pha mở rộng: chỉ `CREATE`): bảng
  `catalog.sellers`, `catalog.products`, `catalog.variants`, `catalog.barcode_counters` (RLS), view `catalog.sellers_v`,
  `products_v`, `variants_v` (cột đúng `docs/views.md`). Khối sqlc mới `queries/catalog` →
  `internal/modules/catalog/internal/store`. Thứ tự khóa: khớp dòng nhóm 4 đã có ở `docs/adr/0046-thu-tu-khoa.md`
  (`sellers` → `products` → `variants` → `barcode_counters`).
- `cmd/crm/seed.go`: thêm người bán `ELIMAZ`; `cmd/crm/main.go`: nối `Events` cho api.
- Test sẽ chạy: `go test -race` cho `./internal/modules/catalog/...`, `./internal/platform/textnorm/...`,
  `./internal/httpapi/...`, `./tests/arch/...` (viewsdoc, sqlusage, boundaries, lint OpenAPI, routes),
  `./internal/platform/tenancy/...` (luật schema trên database đã migrate), `./tests/e2e/...` (seed-dev, khởi động),
  `./cmd/...`; fuzz `FuzzRenderArticleHTML` 60 s cục bộ. `internal/modules/identity` không đổi nên không chạy lại riêng.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có `textnorm`,
  `Fold`, bỏ dấu (grep `textnorm|Fold|dấu`), không có hàm đổi lỗi unique theo tên ràng buộc dùng chung (identity có
  `isUniqueViolation` riêng, không xuất ra). Không có bộ dựng HTML nào trong repo. Dùng lại: `tenancy.WithTenant`,
  `httpapi.WriteTx` (khóa chống trùng), `identity.RecordAudit`, `identity.Can` (qua `x-permission`),
  `(*events.Registry).Emit`, `queue.NewQueue`, `realtime.Bus.Publish`, `testkit.OpenStaff`, `testkit.AccessKeyServer`,
  `serveAs`/`expect`/`testServer` của test `httpapi`, rapid (đã có trong `go.mod`).
- Dùng lại / viết mới vì: `textnorm.Fold` viết mới theo kế hoạch (thư viện chuẩn không bỏ dấu; không thêm
  `x/text`): đặt ở `internal/platform/textnorm` vì M2 (tìm khách), M4 dùng lại. Kiểm lỗi unique theo tên ràng buộc viết
  riêng trong `catalog` (lý do thay đổi riêng của module: tên ràng buộc của bảng catalog). Dựng HTML bằng
  `html/template` của thư viện chuẩn (kế hoạch R7).

## Soát thiết kế

5. Khớp kiến trúc:
- 5.1 Người bán, mẫu, biến thể, barcode thuộc `catalog` (04 mục 3; Q3 của kế hoạch: `sellers` ở `catalog`); mọi bảng
  trong schema `catalog`.
- 5.2 Không ghi bảng module khác; audit qua `identity.RecordAudit` (public API), sự kiện qua `events`, báo hiệu qua
  `realtime` (gói nền tảng).
- 5.3 Không đọc bảng module khác. `catalog` công bố `sellers_v`, `products_v`, `variants_v` cho mốc sau.
- 5.4 Một lần ghi (mẫu + biến thể + barcode + sự kiện + audit + khóa chống trùng) trong một transaction `WriteTx`.
- 5.5 Sự kiện `product.updated`, `price.changed` ghi bằng `Emit` trong transaction (outbox), consumer chạy sau commit.
- 5.6 Không áp dụng: không gọi ra ngoài.
- 5.7 Mọi câu chạy trong `tenancy.WithTenant`/`WriteTx`; bảng mới có RLS (`platform.enable_tenant_rls`),
  `UNIQUE (tenant_id, id)`, unique đầu `tenant_id`, khóa ngoại ghép `tenant_id`; luật (g) ADR-29: mọi unique của bảng có
  `seller_id` có `seller_id` ngay sau `tenant_id` (xem quyết định Q-T27-1).
- 5.8 `catalog` không nhập `net/http` hay `gen`; handler mỏng ở `internal/httpapi/handlers_catalog.go`.
- 5.9 Bốn route ghi có `IdempotencyKey` và `x-permission: catalog.manage`; `getProduct` có `catalog.read`.
- 5.10 Không áp dụng: không dùng Valkey.
- 5.11 Không thêm thư viện: `html/template`, `encoding/json`, `unicode` là thư viện chuẩn; rapid đã có.
- 5.12 Theo module mẫu `identity` (gói gốc công khai, `internal/store` do sqlc sinh, test `_test` trên PostgreSQL thật)
  và đường ghi mẫu (`WriteTx`, sentinel, `errors.go`).

6. Đúng khi chạy đồng thời:
- 6.1 Sửa mẫu, biến thể có `version`: khóa dòng (`SELECT … FOR UPDATE`) rồi so `version`, `UPDATE … WHERE id AND
  version`; hai người cùng `version` → đúng một thành công (test). Barcode: bộ đếm `INSERT … ON CONFLICT DO UPDATE SET
  next = next + 1 RETURNING` (một câu, khóa dòng của tenant), 100 goroutine → 100 mã khác nhau (test). SKU, barcode,
  (màu, size) trùng: unique của database quyết định, đổi sang sentinel theo tên ràng buộc.
- 6.2 `Idempotency-Key` qua `WriteTx`; lỗi nghiệp vụ không lưu (final = false) như mẫu Task 26.
- 6.3 Thứ tự khóa nhóm 4: `products` → `variants` → `barcode_counters`. Sửa biến thể khóa mẫu trước (tăng `version`
  của mẫu cho `Seq` của `product.updated`) rồi biến thể. Dòng bộ đếm barcode lấy sau khi đã chèn mẫu; khi một lần tạo
  chèn nhiều biến thể, một số biến thể chèn sau khi đã giữ dòng bộ đếm (chèn dòng mới không khóa dòng cũ; chỉ chờ nhau
  khi hai transaction chèn cùng SKU chưa commit — deadlock hiếm đó được `RetryTx` chạy lại, rồi bên sau nhận
  `sku_taken`).
- 6.4 `WriteTx` chạy qua `RetryTx` (ADR-46): lỗi tạm → chạy lại cả hàm; hàm của task không giữ trạng thái giữa các lần
  (kết quả gán lại mỗi lần).
- 6.5 READ COMMITTED mặc định cộng khóa dòng và câu có điều kiện.
- 6.6–6.8 Không áp dụng: không có việc nền, không gọi bên ngoài (consumer sự kiện là của mốc sau).
- 6.9 Giới hạn của `db.TxAPI` giữ nguyên. Bài viết ≤ 200 KB, ≤ 200 khối, mẫu ≤ 200 biến thể mỗi lần tạo.
- 6.10 Giá `int64` đồng, `CHECK (price BETWEEN 0 AND 9007199254740991)`, OpenAPI `minimum: 0, maximum:
  9007199254740991`; không cộng nhân tiền ở task này.
- 6.11 Bộ dựng HTML bài viết là vùng rủi ro cao (R7): thuộc tính rapid 500 lượt + fuzz: hoặc lỗi, hoặc chỉ thẻ, thuộc
  tính trong danh sách cho phép.
- 6.12 Không có dữ liệu cá nhân trong catalog; audit, sự kiện, báo hiệu chỉ mang mã và số (không tên, không bài viết).
- 6.13 Mặc định từ chối: route mới khai `x-permission`; tenant khác đọc mẫu → 404 (RLS); hàm đọc nhận `sellerID`.

## Quyết định thay chủ shop

- **Q-T27-1 — Unique (màu, size) có `seller_id`.** Kế hoạch ghi `UNIQUE (tenant_id, product_id, color, size)`; luật (g)
  của `tenancy.CheckSchemaRules` (Task 25, ADR-29) đòi mọi unique của bảng có `seller_id` có `seller_id` ngay sau
  `tenant_id`. Chọn `UNIQUE (tenant_id, seller_id, product_id, color, size)`: cùng ý nghĩa (mẫu chỉ thuộc một người bán,
  khóa ngoại ghép bảo đảm), không cần ngoại lệ `seller-exempt`. Phương án khác: giữ đúng chữ kế hoạch và thêm ngoại lệ
  `seller-exempt` vào `docs/views.md` (thêm một ngoại lệ không cần thiết).
- **Q-T27-2 — Ghi sự kiện, báo hiệu, audit từ module: kiểu `catalog.Writer`.** `events.Emit` là phương thức của
  `*events.Registry` cần `queue.JobQueue`, báo hiệu cần `realtime.Bus`, audit cần mã yêu cầu; hàm gói theo đúng chữ ký kế
  hoạch không có chỗ nhận các thứ đó (Task 24 đã ghi `events.Emit` là phương thức). Chọn bốn hàm ghi (`CreateProduct`,
  `UpdateProduct`, `AddVariant`, `UpdateVariant`) là phương thức của `catalog.Writer{Events, Queue, Realtime,
  RequestID}` (dựng mỗi yêu cầu), giữ nguyên tham số còn lại; hàm đọc (`GetProduct`, `VariantForSale`,
  `VariantsForSale`, `DefaultSeller`, `EnsureDefaultSeller`, `DecodeArticle`, `RenderArticleHTML`) giữ đúng chữ ký kế
  hoạch (mốc sau chỉ dùng hàm đọc). Vì: mọi lần ghi (kể cả nhập Excel Task M1-47) tự có sự kiện và audit, không phụ
  thuộc bên gọi nhớ. Phương án khác: biến toàn cục (cấm bởi lint), handler tự phát sự kiện (dễ quên ở đường ghi thứ hai).
- **Q-T27-3 — Giá vượt giới hạn qua API ra 400 `bad_request`, không phải 422.** Kế hoạch ghi "`9007199254740992` → 422
  từ lớp kiểm của ogen" và "giá `-1` → `invalid_price`". Mã thật: ogen trả `DecodeRequestError` (400), `errors.go` đổi
  thành 400 `bad_request` cho mọi route; Global Constraints đặt `minimum: 0` cho mọi trường tiền nên `-1` cũng bị ogen
  chặn trước (400). Chọn giữ cách đổi lỗi chung (không đổi hành vi các route M0, M1 khác); `invalid_price` 422 vẫn có
  cho lỗi của module (gọi không qua API, ví dụ nhập Excel): test module `-1` → `ErrInvalidPrice`, test API `-1` và
  `9007199254740992` → 400 `bad_request`. Phương án khác: đổi mọi lỗi kiểm của ogen sang 422 (đổi hành vi chung, cần
  task riêng).
- **Q-T27-4 — Mã mẫu trùng.** Kế hoạch có `UNIQUE (tenant_id, seller_id, code)` nhưng không có lỗi tương ứng. Thêm
  `ErrCodeTaken` → 409 `code_taken`; mã mẫu chuẩn hóa viết hoa, cùng mẫu với SKU (`^[A-Z0-9][A-Z0-9._-]{0,63}$`, kiểm ở
  Go và OpenAPI, không thêm CHECK database để dễ nới). Lỗi dữ liệu khác (tên rỗng, màu rỗng, mã sai mẫu) →
  `ErrInvalidInput` → 400 `invalid_input` (cùng mã của identity).
- **Q-T27-5 — Bài viết qua API không khai lược đồ khối ở yêu cầu.** Để khối lạ (`{"type":"html"}`) ra 422
  `invalid_article` như kế hoạch, thân yêu cầu khai `article` là mảng phần tử tự do (`items: {}`, tối đa 200), module
  kiểm bằng `article.Validate`; phản hồi khai lược đồ `ArticleBlock` đầy đủ. Lưu dạng chuẩn (mã hóa lại từ khối đã
  kiểm).
- **Q-T27-6 — `SaleVariant.Name`.** Kế hoạch không ghi cách ghép: chọn `"<tên mẫu> - <màu> / <size>"` (tên dòng đơn
  M2 hiện đủ biến thể). Đổi là sửa một hàm.
- **Q-T27-7 — Người bán mặc định.** `DefaultSeller` trả người bán `kind = own`, `status = active` có mã nhỏ nhất (dữ
  liệu khởi tạo chỉ có `ELIMAZ`); không có → `ErrNotFound` (API 404). `crm_app` chỉ có `SELECT` trên `sellers`;
  `EnsureDefaultSeller` chạy bằng chủ bảng ở `seed-dev` (Task M1-37 mở ghi khi cần).
- (Lượt 2) main tiến thêm (Task 315 gộp `75f8e58`) trong lúc đang giải xung đột: hủy lần gộp dở (`git merge --abort`)
  rồi gộp lại với main mới, để `test-guard` (so với main) và reviewer thấy đúng bản cuối. Khác: commit lần gộp cũ rồi
  gộp thêm lần nữa (hai commit gộp, khó duyệt hơn).
- (Lượt 2) Test chập chờn do máy quá tải: chạy riêng lại các gói đó, không sửa, không tắt test (ngoài phạm vi Task 27;
  ghi ở Việc phát hiện thêm). Khác: sửa hạn chờ trong test của task khác (trái luật "chỉ làm đúng task").

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| KH02 (không ảnh): DAM01 2 màu × 3 size, giá 350000, nhập 180000 → 6 biến thể, `GetProduct` đúng, `name_search = "dam lua xoe"`, 1 `product.updated` seq 1, 1 audit, 1 báo hiệu `catalog.product` | `TestCreateProduct_SixVariants` | `go test -race -count=1 ./internal/modules/catalog/...` (bản đã gộp main `6e1a93f`) | `ok elimaz.com.vn/crm/internal/modules/catalog 29.360s` |
| KH06: SKU trùng (khác hoa thường) → `ErrSKUTaken`; cùng (màu, size) → `ErrDuplicateOption`; SKU trùng trong một lần tạo; mã mẫu trùng → `ErrCodeTaken`; barcode trùng → `ErrBarcodeTaken`; người bán thứ hai cùng SKU → được | `TestCreateProduct_DuplicateSKU` | như trên | `ok` |
| Barcode tự sinh: 3 mã EAN-13 khác nhau tiền tố 2; 100 goroutine → 100 mã khác nhau | `TestCreateProduct_GeneratedBarcodes`, `TestValidEAN13`, `TestFormatInternal` | như trên; `go test ./internal/modules/catalog/internal/barcode` | `ok` |
| Giá `-1`, `9007199254740992` (giá bán, giá nhập) → `ErrInvalidPrice`; dữ liệu sai → `ErrInvalidInput`; màu hex chuẩn hóa | `TestCreateProduct_InvalidPrice` | như trên | `ok` |
| `UpdateProduct` version cũ → `ErrVersionConflict`; hai người cùng version → đúng 1 thành công, 1 xung đột; không có → `ErrNotFound` | `TestUpdateProduct_Version` | như trên | `ok` |
| Đổi giá 350000 → 390000: 1 `price.changed` (seq 2, đúng payload), 1 `product.updated` seq 2 đúng payload, audit `before.price = 350000`, `after.price = 390000`, có `request_id`; đổi màu không phát `price.changed`; bỏ giá nhập, ngừng bán biến thể | `TestUpdateVariant_PriceChanged` | như trên | `ok` |
| `AddVariant` tăng version mẫu; version mẫu cũ → `ErrVersionConflict` | `TestAddVariant_ProductVersion` | như trên | `ok` |
| KH12: mẫu `draft`, `discontinued`, biến thể ngừng bán → `ErrNotForSale`; `GetProduct` vẫn trả mẫu ngừng bán; người bán khác → `ErrNotFound`; `VariantsForSale` 3 id → map 1 phần tử; `Name = "Đầm lụa xòe - Đỏ / S"` | `TestVariantForSale_Discontinued` | như trên | `ok` |
| C8: 3 khối lưu và đọc lại đúng, `<script>` giữ nguyên văn; `{"type":"html"}` và 201 khối → `ErrInvalidArticle` (tạo và sửa) | `TestArticle_RejectsUnknownBlock`, `TestValidate_Valid`, `TestValidate_Rejects` (27 ca: loại lạ, trường lạ, khóa trùng, null, NUL, điều khiển, quá 5.000, 51 mục, quá 200 KB, UTF-8 hỏng…) | như trên | `ok` |
| R7 golden: heading, paragraph hai dòng, list 3 mục, image 12 biến thể → đúng từng byte `testdata/article/golden.html`; gọi hai lần bằng nhau; image chưa có `Image` → bỏ khối; không JPEG → bỏ; JPEG gần 640 nhất; http localhost; rỗng → `""` | `TestRenderArticleHTML_Golden` | như trên | `ok` |
| R7 chống chèn mã: không có thẻ `script`, `svg`; chữ hiện `&lt;script&gt;`; `alt="&#34; onerror=&#34;alert(1)"`; URL `javascript:`, `data:`, `//…`, userinfo, http ngoài máy, `svg`, dấu phẩy, khoảng trắng, rộng 0, level 4, `html`, NUL, 201 khối → `ErrInvalidArticle` và chuỗi rỗng | `TestRenderArticleHTML_EscapesInjection` | như trên | `ok` |
| R7 thuộc tính 500 lượt (rapid): khối ngẫu nhiên → lỗi `ErrInvalidArticle` kèm `""`, hoặc chỉ thẻ, thuộc tính trong danh sách cho phép (bộ tách thẻ trong test) | `TestRenderArticleHTML_Property` (`testkit.SetRapidChecks(500)`) | như trên | `ok` |
| R7 fuzz 60 s | `FuzzRenderArticleHTML` (5 hạt giống) | `go test -run '^$' -fuzz FuzzRenderArticleHTML -fuzztime 60s ./internal/modules/catalog` | `elapsed: 1m0s, execs: 1218042 … PASS` |
| PQ08: tenant B `GetProduct` mẫu của A → `ErrNotFound`; người bán khác → `ErrNotFound`; `catalog.variants_v` bằng `crm_app` ngoài ngữ cảnh → 0 dòng; mẫu nháp → `sellable = false` | `TestCatalog_TenantIsolation` | như trên | `ok` |
| API: 201, gửi lại cùng khóa → cùng mẫu; không có `sellerId`; `getProduct` `articleHtml` bằng `RenderArticleHTML(article)`, có `Lụa &lt;b&gt;mềm&lt;/b&gt;<br>mát`; `article: []` → `articleHtml: ""`; Xem sửa → 403; version cũ → 409 `version_conflict`; khối html → 422 `invalid_article`; tenant B → 404; mẫu không có → 404 | `TestCatalogAPI_CreateGetUpdate` | `go test -race -count=1 ./internal/httpapi/...` | `ok elimaz.com.vn/crm/internal/httpapi 48.156s` |
| API biến thể: 409 `sku_taken`, `duplicate_option`, `barcode_taken`, `code_taken`; giá `-1`, `9007199254740992` → 400 `bad_request` (Q-T27-3); giá `9007199254740991` nhận; SKU viết hoa, barcode 13 số; sửa giá + `costPrice: null`; version cũ 409; không có 404; audit có `request_id` | `TestCatalogAPI_VariantErrors` | như trên | `ok` |
| View `sellers_v`, `products_v`, `variants_v` đúng cột, thứ tự `docs/views.md`; luật schema (RLS, unique, khóa ngoại, luật (g)) | `viewsdoc_test`, `TestCheckSchemaRules*` trên database đã migrate | `go test -race -count=1 ./tests/arch/... ./internal/platform/tenancy/...` | `ok elimaz.com.vn/crm/tests/arch 5.164s`; `ok …/tenancy 41.274s` |
| `seed-dev` có đúng 1 người bán ELIMAZ (`own`, `active`) sau hai lần chạy | `TestSeedDev` | `go test -race -count=1 ./tests/e2e/...` | `ok elimaz.com.vn/crm/tests/e2e 47.144s` |
| `Fold`: 12 cặp (gồm "Áo Thun Ơ", "ĐỒNG HỒ", hai khoảng trắng, dạng NFD) | `TestFold` | `go test ./internal/platform/textnorm/` | `ok` |
| Cổng kiểm khác | lint, jscpd, oasdiff, sqlc vet, danh mục, test-guard, gitleaks, client màn hình | `golangci-lint run ./...`; `make jscpd`; `make oasdiff`; `make sqlc-vet`; `make catalog`; `make test-guard`; `gitleaks dir`; `cd console && npx tsc --noEmit && npx eslint . && npm test` | `0 issues` (sau khi xóa `console/node_modules`, xem Việc phát hiện thêm); `Found 0 clones`; không thay đổi phá vỡ; `ok`; danh mục có `textnorm`; qua; `no leaks found`; tsc, eslint không lỗi, `Tests 35 passed` |

## Lượt 2 (gộp main `75f8e58`)

Lượt 1 (commit `be4bc75`) đã ĐẠT. Lượt 2 chỉ gộp main (Task 16, 17, 18, 41, 42, 314, 315 và `console/go.mod`) bằng
`git merge main` (không rebase); không thêm hành vi mới của catalog.

- **Ảnh hưởng:** xung đột ở `cmd/crm/main.go` (Deps có cả `Files: store` của Task 41 và `Events: newEvents()` của Task
  27; giữ nguyên bước `syncDefaultRoles` sau migrate của Task 315), `internal/httpapi/server.go` (nhập cả `events` và
  `files`; `Deps` có cả `Files` và `Events`), `internal/httpapi/errors.go` (giữ cả hai khối mã lỗi; `catalogError` và
  `filesError` là hai hàm riêng, gọi nối tiếp sau `identityError`; không mã lỗi nào trùng sentinel của gói khác).
  `openapi/openapi.yaml` tự gộp (đủ 3 đường `files`, 4 đường `catalog`, phần `x-authenticated` của Task 315). Mã sinh
  (`internal/httpapi/gen/**`, `routes_gen.go`, `console/src/api/**`): lấy bản của main rồi `make gen`; sqlc sinh lại
  `catalog/internal/store/models.go` (thêm kiểu của bảng `files`). Migration: `0114_catalog.sql` và `0124_files.sql`
  khác số, không chạm nhau. Test chạy: `make ci` (toàn bộ).
- **Tái dùng:** không viết mã mới; `make catalog` không đổi danh mục.
- **Soát thiết kế:** chỉ phần chạm: đổi lỗi sang HTTP vẫn ở một chỗ (`errors.go`); quyền của 5 operation catalog vẫn
  là `x-permission` (luật OpenAPI của Task 315 qua ở `tools/routegen`, `tests/arch`).

Bằng chứng lượt 2:

| Tiêu chí | Lệnh | Kết quả |
|---|---|---|
| Sinh lại mã không chênh | `make ci` (bước `gen-check`) | qua |
| Build, vet, lint | `make ci` (`build`, `vet`, `lint`) | `0 issues.` |
| Catalog, HTTP, identity (Task 315), e2e, kiến trúc, tenancy, files trên bản gộp | `make ci` (bước `test`: `go test -race -count=1 ./...`) | `ok …/modules/catalog 79.478s`, `ok …/httpapi 271.618s`, `ok …/identity 347.319s`, `ok …/tests/e2e 128.992s`, `ok …/tests/arch 34.996s`, `ok …/tenancy 143.054s`, `ok …/files 148.979s` |
| Ba gói đỏ do máy quá tải (load ~25), không khác main | `go test -race -count=1 ./internal/platform/commands/ ./internal/platform/db/ ./internal/platform/queue/` | `ok commands 33.338s`, `ok db 21.722s`, `ok queue 62.576s` |
| Các cổng còn lại | `make vuln oasdiff catalog-check jscpd test-guard gitleaks console-check` | thoát 0; `Found 0 clones`; `no leaks found`; console `Tests 35 passed (35)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Hàm ghi là phương thức của `catalog.Writer` (Q-T27-2).
- Unique (màu, size) có `seller_id` (Q-T27-1).
- Giá âm hay vượt giới hạn qua API: 400 `bad_request` (Q-T27-3).
- Thêm `ErrCodeTaken`, `ErrInvalidInput` (Q-T27-4).
- `testkit.SetRapidChecks(n)` (mới): đặt số lượt rapid trong `TestMain`; viết vì jscpd báo trùng với `TestMain` của
  identity (identity giữ nguyên, Task 315 đang sửa identity song song).
- `sqlc.yaml`: override cột `catalog.variants.cost_price` thành `*int64` (bỏ hàm đổi `pgtype.Int8`, jscpd báo giống
  `pgUUID` của queue).
- `ProductPatch`, `VariantPatch` (kế hoạch nhắc tên, không ghi trường): trường con trỏ, nil giữ nguyên;
  `VariantPatch.ClearCostPrice` bỏ giá nhập; barcode không sửa thành rỗng (chỉ tự sinh lúc tạo).
- `GetProduct` không trả mẫu đã xóa mềm (`deleted_at`): xem như không có. M1 chưa có đường xóa mẫu.
- Bộ dựng HTML: `figcaption` chỉ có khi có `caption`; ảnh không có bản JPEG nào → bỏ khối (kế hoạch chỉ nói "thiếu 640
  thì bản JPEG gần nhất"). URL ảnh có dấu phẩy, khoảng trắng, nháy → `ErrInvalidArticle` (srcset tách theo dấu phẩy).
- `catalog.go` có hằng `DefaultSellerCode`, `MaxMoney`; `events.go` công khai kiểu payload `ProductUpdated`,
  `PriceChanged` và tên topic để consumer của mốc sau giải mã.

## Việc phát hiện thêm

- (Lượt 2) Khi máy quá tải (nhiều phiên song song, load ~25), các test theo thời gian chập chờn:
  `TestImager_HangKilledWithinDeadline` (imager), `TestRecordAfterLongExecute` (commands),
  `TestRetryTxLockTimeoutRecoversWhenLockReleased` (db), `TestBestEffortConcurrentConversation` (queue). Chạy riêng đều
  qua. Đề xuất task riêng: nới hạn chờ hoặc đo theo tín hiệu thay vì đồng hồ. Mục `console/node_modules` của lượt 1 đã
  được `console/go.mod` của main giải.
- (Lượt 2, ghi chú reviewer) Số migration do kế hoạch cấp theo task (`0114` Task 27, `0124` Task 41), nên gộp vào
  main không theo thứ tự số. goose (`db.Migrate`) mặc định từ chối migration số nhỏ hơn bản đã chạy: database dev đã
  migrate main có `0124` sẽ báo lỗi khi gặp `0114`, phải dựng lại (`docker compose -p <dự án> down -v`). Chưa có môi
  trường thật nên không hỏng gì; trước khi có máy chủ, cần quyết (task riêng): cho phép ngoài thứ tự ở dev, hoặc cấp
  số theo thứ tự gộp.

- Sau Task 17 (đã gộp main), `make gen` chạy `console-deps` (npm cài `console/node_modules`); thư viện npm `flatted` có
  tệp `.go` nên `golangci-lint run ./...` và `TestLintCleanOnRealCode` (tests/arch) đỏ trên worktree vừa `make gen`.
  Đề xuất: loại `console/node_modules` khỏi `.golangci.yml` (task riêng, ngoài phạm vi Task 27). Ở đây đã xóa thư mục
  cài cục bộ (đã `.gitignore`) trước khi chạy lại cổng kiểm.
- `docs/views.md` chưa có mục "Sự kiện công bố" (kế hoạch ghi là có); Task 27 thêm mục với ba dòng (`stock.changed` theo
  dạng của Task M1-28).

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, một lần, trên `git diff --cached` của bản đã gộp main `6e1a93f`).

- Phát hiện phải sửa: không có. Reviewer đã kiểm riêng: RLS, unique, khóa ngoại ghép, luật (g) của migration; quyền
  `crm_app` chỉ đúng cột, không DELETE; thứ tự khóa khớp ADR-46 nhóm 4; lỗi nghiệp vụ làm tx lùi và nhả khóa chống
  trùng (`WriteTx` → `errNotFinal`); Seq sự kiện là version mới; đọc `product_id` không khóa vẫn an toàn (`crm_app`
  không sửa được cột đó, không DELETE).
- Ghi chú không bắt buộc (không sửa trong task này, chuyển cho task sau):
  - `DefaultSeller` sắp theo `id` (uuidv7: người bán tạo sớm nhất); chú thích và Q-T27-7 ghi "mã nhỏ nhất" nghĩa là
    `id`, không phải `code`. Hiện mỗi tenant một người bán nên không sai; Task M1-37 (hai người bán) nên ghi rõ.
  - `GetSellerByCode` chỉ dựa RLS để lọc tenant (RLS bị ép cả với chủ bảng, nên đúng); có thể thêm điều kiện
    `tenant_id`.
  - Hàm ghi khóa mẫu, biến thể theo id, không lọc người bán (đúng chữ ký kế hoạch); khi mở người bán đối tác (Task
    M1-37), đường API sửa nên kiểm cả người bán.
  - `ErrInvalidArticle` từ dữ liệu cũ đã lưu (không phải từ yêu cầu) cũng ra 422; hiếm, không hỏng dữ liệu.
  - Chưa có test gửi lại cùng khóa sau một lần 409; dựa trên `WriteTx` đã có test ở Task 12, 26.

### Reviewer lượt 2

Kết luận: ĐẠT (agent `reviewer`, một lần, trên `git diff --cached` của bản gộp main `75f8e58`).

- Phát hiện phải sửa: không có. Reviewer so bản gộp với cả hai cha (`75f8e58`, `be4bc75`): `main.go` có `Files`,
  `Events` và `syncDefaultRoles` (Task 315); `server.go` có `Files`, `Events`; `errors.go` có `catalogError`,
  `filesError`; `openapi.yaml` có 3 đường files, 5 operation catalog, `x-authenticated`; mã sinh khớp hợp đồng;
  `git diff --cached --check` sạch; không test nào bị tắt hay bớt câu kiểm.
- Ghi chú không bắt buộc: (1) `hien-tai.md` đang stage bản của main; hook không cho phiên stage tệp này,
  `finish-task.sh` đặt lại trạng thái. (2) Thứ tự số migration `0114`/`0124`: ghi ở Việc phát hiện thêm.
