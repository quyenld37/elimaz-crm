# Task 190 — M5-07: Danh mục cho website: sản phẩm, thay đổi, phân loại

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 190, M5-07; đối chiếu giả định
`docs/m5/doi-chieu-gia-dinh.md` dòng A2, A2b, A2c, A2d và mục 2 "updated_at", mục 3 "ảnh xử lý xong sau khi gắn").
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `catalog.VariantPatch` thêm `WeightGrams *int`, `ClearWeight bool`; `catalog.Variant` thêm `WeightGrams *int`
    (chỉ thêm trường; mọi chỗ dựng hai kiểu theo tên trường: `imports_apply_write.go` (2 chỗ), `handlers_catalog.go`,
    test của catalog — không phải sửa). Sentinel mới `catalog.ErrInvalidWeight`.
  - Mới: gói `internal/queries/webcatalog` (`Products`, `ProductsByID`, `Changes`, `Taxonomy`, `AttachImages`,
    `WebProduct`, `WebVariant`, `WebAttr`, `WebImage`, `WebTaxonomy`…, `DefaultWarehouseCode`, `MaxPage`, `MaxIDs`,
    `MaxChanges`); gói `internal/httpapi/web` (`Catalog`, `NewCatalog`); `httpapi.Deps.ReadCache`.
  - `webcache` (Task 185) không đổi, chỉ được nối ở `cmd/crm`.
- Gói phụ thuộc ngược (go list): `internal/modules/catalog` được nhập bởi `cmd/crm`, `internal/httpapi`,
  `catalog/catalogtest`, `inventory`, `order`, `promotion`, `wiring/webcache`, `testkit/shoptest`; `internal/httpapi`
  bởi `cmd/crm`. Gói mới chỉ `internal/httpapi` (và `cmd/crm` qua Deps) nhập.
- Operation OpenAPI bị chạm, oasdiff: thêm `webCatalogProducts`, `webCatalogChanges`, `webCatalogTaxonomy`
  (`x-auth: service_key`, `x-permission: web.catalog.read`); `updateVariant` thêm trường tùy chọn `weightGrams`
  (nullable), `Variant` thêm trường tùy chọn `weightGrams` — chỉ thêm, oasdiff không báo phá vỡ.
- Chỗ dùng trên màn hình: client sinh `console/src/api/**` có thêm kiểu (sinh lại bằng `make gen`); màn hình không
  đổi (ô cân nặng trên màn sản phẩm cần mockup, ghi ở "Việc phát hiện thêm").
- Câu sqlc, bảng, pha migration:
  - `migrations/0502_catalog_web.sql` (pha mở rộng): `catalog.variants.weight_grams int NULL CHECK 1–50000`, quyền
    UPDATE cột cho `crm_app`; `CREATE OR REPLACE VIEW catalog.variants_v` thêm `weight_grams` ở cuối,
    `catalog.products_v` thêm `version` ở cuối (Q-X2: giữ mọi cột cũ, chỉ nối cuối); index
    `products (tenant_id, updated_at)`, `variants (tenant_id, product_id)`, `stock_balances (tenant_id, updated_at)` cho
    luồng thay đổi và đọc theo trang không Seq Scan. `updated_at` của
    `variants_v`, `stock_available_v` đã có (đối chiếu A2) nên không thêm.
  - Chỗ đọc `variants_v` (cột thêm ở cuối, không ai `SELECT *`): `queries/catalog/variants.sql`,
    `queries/inventory/{documents,stocktakes,balances}.sql`, `queries/ord/create.sql`, `migrations/0123` (hàm tìm),
    `internal/queries/{picking,products,balances,aged}_dynsql.go`, `invtest/seed.go`. Chỗ đọc `products_v`:
    `internal/queries/{fileproducts,products}_dynsql.go`. Không chỗ nào đổi.
  - `queries/catalog/variants.sql`: `ListProductVariants`, `LockVariant`, `UpdateVariant` thêm `weight_grams`.
  - Khối sqlc mới `queries/webcatalog` → `internal/queries/webcatalog/internal/store` (`products.sql`: ActiveSellers,
    WarehouseByCode, ProductPage, ProductsByIDs, VariantsOf, ImagesOf, TagIDsOf, AttributeValueIDsOf; `changes.sql`:
    Changes, Now; `taxonomy.sql`: Categories, Attributes, AttributeValues, Tags), chỉ nhắc view `_v`.
  - `tests/arch/sqlusage_test.go`: `queries/webcatalog` chỉ được nhắc view `_v` đã công bố của `catalog`,
    `inventory`, `promotion`, `ord`, `fulfillment`.
- `cmd/crm/main.go`: vai trò `api` dựng `cache.NewReadCache` trên Valkey bộ đệm (`VALKEY_CACHE_URL`; trống → Conn nil,
  luôn đọc PostgreSQL có giới hạn) và `webcache.Invalidator` (Start/Stop có chủ); vai trò `worker` đăng ký
  `webcache.Consumer` vào sổ sự kiện.
- Test sẽ chạy: `go test -race -run` đúng test mới của `internal/queries/webcatalog`, `internal/httpapi/web`,
  `internal/modules/catalog` (cân nặng), `cmd/crm` (nối bộ đệm), `tests/arch`; rồi `scripts/agent/ci-task.sh` một lần
  (gói đổi + phụ thuộc; `catalog` không thuộc vùng rủi ro cao trừ `article_html.go`/`importer/apply.go` không chạm;
  migration đổi → ci-task chạy toàn bộ).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục `cache`
  (`ReadCache`, `Space*`, `ErrBusy`), `files` (`PublicVariants`, `SelectPreset`), `catalog` (`ArticleHTML`,
  `ImageView`), `testkit` (`ExplainAsApp`, `RequireNoSeqScan`, `StartValkey`, `OpenStaff`), `shoptest.SeedCatalog`,
  `catalogtest`, `invtest`, `webcache` (Consumer, Invalidator); tìm "etag", "If-None-Match": chưa có trong mã.
- Dùng lại / viết mới vì:
  - Ảnh sản phẩm: `files.PublicVariants` một lần cho cả trang + `files.SelectPreset` (cùng luật chọn bộ kích thước với
    `catalog.ProductImages`). Không gọi `catalog.ProductImages` vì nó đọc từng mẫu (200 lời gọi một trang, trái A2c).
  - Bài viết: `catalog.ArticleHTML` của M1 (A2d), không tự dựng HTML.
  - Bộ đệm: `cache.ReadCache.GetOrLoad` (Task 185), xóa khóa bằng `webcache.Invalidator` sẵn có; ETag là sha256 thân
    viết mới (vài dòng, chỉ lớp web dùng).
  - Kho mặc định: `inventory.DefaultWarehouseCode` không nhập được từ `internal/queries` (luật f) → hằng
    `webcatalog.DefaultWarehouseCode` có test so bằng với hằng của inventory.
  - Dữ liệu thử: 450 mẫu × 3 biến thể dựng bằng SQL `generate_series` như `internal/queries/fixture_test.go`
    (`SeedCatalog` chỉ tạo một mẫu mỗi người bán); ca sửa giá, nhập kho, ngừng bán, bài viết, ảnh đi qua API công khai
    (`catalog.Writer`, `invtest.SeedStock`, `files` test).

## Soát thiết kế

Khớp kiến trúc (mục 5):
- 5.1: cột `weight_grams` thuộc `catalog.variants` (catalog sở hữu biến thể, QĐ30). Gói `webcatalog` không có bảng.
- 5.2: không ghi bảng module khác; ghi cân nặng đi qua `catalog.Writer.UpdateVariant`.
- 5.3: `webcatalog` chỉ đọc view `_v` công bố (sqlusage_test); ảnh qua `files.PublicVariants` (không nhắc schema
  `platform` trong SQL).
- 5.4, 5.5: không áp dụng (đường đọc). Đổi cân nặng phát `product.updated` như mọi lần sửa biến thể → consumer
  `webcache` làm mới bộ đệm sau commit.
- 5.6: không áp dụng (không gọi ra ngoài; Valkey là hạ tầng của mình, ngoài transaction).
- 5.7: mọi câu đọc chạy trong `tenancy.WithTenant(…, db.TxAPI)` của tenant lấy từ khóa dịch vụ; view `security_invoker`.
- 5.8: module không biết HTTP; ETag, 304, 400 ở `internal/httpapi/web`; `cache.ErrBusy` → 503 `retry_later`
  `Retry-After: 1` ở `errors.go` (cùng nhánh `db.ErrRetryLater`).
- 5.9: không có đường ghi mới (updateVariant đã có khóa chống trùng).
- 5.10: Valkey chỉ làm nhanh: hỏng → `GetOrLoad` đọc PostgreSQL dưới semaphore (`db_fallback`), có test Valkey dừng.
  Không bao giờ gọi Valkey trong transaction: `load` mở transaction bên trong lời gọi bộ đệm, không ngược lại.
- 5.11: không thêm thư viện.
- 5.12: theo mẫu `internal/queries` (chỉ view, test PostgreSQL thật) và module mẫu catalog cho cân nặng.

Đúng khi chạy đồng thời (mục 6):
- 6.1: sửa cân nặng đi qua khóa mẫu → biến thể có `version` sẵn có. Đọc trang theo khóa `id` (keyset), không OFFSET.
- 6.2: không áp dụng (đọc). Lượt tải gộp cùng khóa trong tiến trình (singleflight của ReadCache).
- 6.3: không khóa dòng nào (chỉ SELECT).
- 6.4: lỗi tạm trong `load` → `db.ErrRetryLater` qua `WithTenant` → 503.
- 6.5: một transaction đọc READ COMMITTED cho cả trang (mẫu, biến thể, tồn, ảnh, bài viết cùng một tx: các câu có thể
  thấy commit xen giữa — chấp nhận được với dữ liệu hiển thị, bản kế sẽ khớp khi sự kiện làm mới bộ đệm).
- 6.6, 6.7, 6.8: không áp dụng (không việc nền mới; consumer `webcache` của Task 185).
- 6.9: `TxAPI` (câu 5 s); trang ≤ 200 mẫu, `ids` ≤ 100, thay đổi ≤ 1.000; EXPLAIN không Seq Scan trên 50.000 biến thể.
- 6.10: giá `int64` đồng; không trả giá nhập (`cost_price`) ra JSON (test quét tên trường).
- 6.11: không áp dụng (không quy luật tiền/tồn mới).
- 6.12: khóa Valkey chỉ có mã tenant, mã sản phẩm, con trỏ trang (không dữ liệu cá nhân); log cảnh báo bài viết lỗi
  chỉ có mã sản phẩm.
- 6.13: quyền `web.catalog.read` của khóa dịch vụ; thẻ Access của nhân viên không vào được (`x-auth: service_key`).

## Quyết định thay chủ shop

- QĐ-190-1 (luồng thay đổi gồm tồn): `changes` hợp mốc của `products_v` và `stock_available_v` (test kế hoạch đòi "nhập
  kho cho v2 → có trong Changes"; `available` của biến thể nằm trong sản phẩm). Không có nhánh `variants_v` riêng: mọi
  lần ghi biến thể của `catalog.Writer` (tạo, sửa giá, ngừng bán, cân nặng) đã tăng `updated_at` của mẫu trong cùng
  transaction (`bumpProduct`), test `TestChanges` đổi giá qua `Writer.UpdateVariant`. Bỏ nhánh này vì nối `variants_v`
  (view nối bảng mẫu) theo `updated_at` luôn ra kế hoạch đọc cả bảng mẫu. Khác: giữ nhánh biến thể và nới tiêu chí
  EXPLAIN — không chọn.
- QĐ-190-2 (`until` và `hasMore`): chưa đủ `limit` → `until` = `now()` của transaction đọc (không dùng mốc lớn nhất đã
  trả: khi không có thay đổi, `since = until − 30 s` sẽ lùi mãi); đủ `limit` → `until` = mốc của mã cuối, `hasMore =
  true` (trường mới của hợp đồng). Nhiều hơn `limit` mẫu cùng một mốc (nhập Excel hàng loạt trong một transaction) thì
  `until` không tiến: hợp đồng ghi website tải lại cả danh mục theo trang khi gặp đúng ca đó. Khác: con trỏ (mốc, mã) —
  đổi giao diện kế hoạch nhiều hơn.
- QĐ-190-3 (ảnh xử lý xong sau khi gắn, đối chiếu mục 3): chọn nhánh "chỉ trả ảnh có biến thể, bộ đệm 60 s tự lấy lại"
  — tệp chuyển `ready` sau khi gắn không đổi `products_v.updated_at`, nên bản sao của website chỉ thấy ảnh mới ở lần
  `products?ids=` kế (khi mẫu đổi) hoặc lần tải lại cả danh mục. Ghi ở "Việc phát hiện thêm" (catalog nghe sự kiện tệp
  `ready` để tăng version mẫu). Khác: `changes` đọc mốc của tệp — cần nhắc schema `platform`, trái B11.
- QĐ-190-4 (`WebImage` lồng biến thể): mỗi ảnh là `{position, color?, variants: [{width, format, url}]}` thay vì một
  dòng phẳng mỗi biến thể như chữ ký kế hoạch; giống `ProductImage` của màn hình, website dựng `srcset` trực tiếp.
- QĐ-190-5 (cân nặng 0 → 422): schema `weightGrams` không khai `minimum` (ogen sẽ trả 400); kiểm ở `catalog`
  (`ErrInvalidWeight`) → 422 `invalid_input` (mã có sẵn, không thêm mã). `null` bỏ cân nặng (`ClearWeight`), như
  `costPrice`.
- QĐ-190-6 (`ErrBusy`): `cache.ErrBusy` đổi ở `errors.go` cùng nhánh `db.ErrRetryLater` (503 `retry_later`,
  `Retry-After: 1`) — bảng `errorMapping` của vùng không đặt được header.
- QĐ-190-7 (bảng nhỏ đọc riêng): câu trên bảng lớn không nối người bán, kho, nhãn, thuộc tính; bốn bảng nhỏ đọc bằng
  câu riêng rồi truyền mã/ghép trong Go, để mọi câu trên bảng lớn qua `RequireNoSeqScan` (PostgreSQL luôn đọc tuần tự
  bảng vài dòng — đúng, nhưng làm hỏng tiêu chí). Biến thể và số dư theo mẫu đọc bằng `LATERAL … OFFSET 0`.
- QĐ-190-8 (đăng ký consumer `webcache`): đăng ký ở `newModules` cho mọi vai trò (không chỉ worker như kế hoạch ghi),
  vì `Emit` ở api tạo việc theo sổ của chính tiến trình đó; chỉ worker chạy việc.
- QĐ-190-9 (`version` trên `products_v`): nối cột `version` vào cuối `products_v` (0502) để trả `version` của mẫu như
  kiểu `WebProduct` của kế hoạch.

## Bằng chứng

Lệnh chung: `scripts/agent/ci-task.sh` (23/11, sau khi gộp main f89693d) → `CI-TASK XANH (so với điểm tách nhánh
f89693d…)`. Trích phần test -race của gói sửa trực tiếp: `ok cmd/crm 5.257s`, `ok internal/httpapi 148.257s`,
`ok internal/httpapi/web 4.918s`, `ok internal/modules/catalog 105.170s`, `ok internal/queries/webcatalog 4.419s`,
`ok tests/arch 72.235s`; không -race: `ok internal/wiring/webcache 1.354s`, `ok tests/e2e 223.313s`. Cổng tĩnh
(build, vet, lint, gen-check, sqlc-vet, jscpd "Found 0 clones", gitleaks "no leaks found", oasdiff, test-guard,
console-check) qua.

| Tiêu chí (kế hoạch Task 190 Step 1) | Test | Lệnh | Kết quả |
|---|---|---|---|
| 450 mẫu, limit 200 → 3 trang 200/200/50, đủ, không trùng, đúng thứ tự | `TestProducts_Pages` | ci-task (webcatalog -race) | ok 4.419s |
| `ids` 101 mã → 400; 2 mã → 2 mẫu | `TestProductsByID`, `TestWebCatalogProducts_Params` | ci-task (webcatalog, httpapi/web) | ok |
| Đổi giá v1 → `Changes(T−30 s)` có mẫu v1, không mẫu khác; nhập kho v2 → có mẫu v2 | `TestChanges`, `TestWebCatalogChanges` | ci-task | ok |
| Người bán `suspended` → ẩn; `active` → hiện | `TestProducts_SellerSuspended` | ci-task | ok |
| Biến thể ngừng bán → `discontinued`, `sellable=false` | `TestProducts_VariantFields`, `TestWebCatalogProducts_Shape` | ci-task | ok |
| Bài viết: bằng `catalog.ArticleHTML`, một `<picture>`, `src` từ `FILES_PUBLIC_BASE_URL/img/`, khối `lost` bỏ, `<script>` thoát, không `article` thô | `TestWebCatalogProducts_Shape` | ci-task (httpapi/web) | ok 4.918s |
| Ảnh: 2 `ready` theo `position`, ảnh đang xử lý bỏ; trang 200 mẫu gọi `PublicVariants` đúng 1 lần | `TestProducts_Images` | ci-task (webcatalog) | ok |
| `weightGrams` rỗng → vắng; 450 → 450; 0 → 422 | `TestUpdateVariant_Weight`, `TestCatalogAPI_UpdateVariantWeight`, `TestWebCatalogProducts_Weight` | ci-task (catalog, httpapi, web) | ok |
| SQL `queries/webcatalog` chỉ nhắc view `_v` | `TestSQLUsageViewRules` (+ mẫu `testdata/sqlusage/queries/webcatalog/{ok,bad}.sql`) | ci-task (tests/arch) | ok 72.235s |
| Không trường JSON chứa `cost`/`import` | `TestWebCatalogProducts_Shape` (quét tên trường) | ci-task | ok |
| `If-None-Match` đúng → 304 thân rỗng | `TestWebCatalog_ETag` | ci-task | ok |
| Lần hai từ bộ đệm; Valkey dừng → 200 `db_fallback` | `TestWebCatalog_CacheAndFallback` | ci-task | ok |
| `price.changed` → consumer → Invalidator → đọc lại thấy giá mới (chờ theo sự kiện) | `TestWebCache_PriceChangedRefreshesProduct` | ci-task (cmd/crm -race) | ok 5.257s |
| Thiếu `web.catalog.read` → 403; thẻ Access → 401 | `TestWebCatalog_Auth` | ci-task | ok |
| EXPLAIN `Products`, `Changes` (và các câu khác) trên 50.000 biến thể không Seq Scan | `TestPlansHaveNoSeqScan` | ci-task (webcatalog) | ok |
| Phân loại (danh mục, thuộc tính, nhãn) | `TestTaxonomyAndClassification` | ci-task | ok |
| Hằng kho mặc định khớp `inventory` | `TestDefaultWarehouseCode` | ci-task | ok |
| Mã lỗi mới đổi đúng HTTP (`ErrInvalidWeight` 422, `ErrBadQuery`/`ErrBadLimit` 400) | `TestDomainErrorSnapshot` | ci-task (httpapi) | ok 148.257s |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chữ ký: `Products(ctx, tx, after, limit, view ImageView)`, `ProductsByID(ctx, tx, ids, view)` nhận thêm cách dựng
  đường dẫn ảnh (gói không đọc cấu hình); `Changes` trả thêm `more bool` (QĐ-190-2). `WebImage` lồng (QĐ-190-4).
- Test đặt theo tầng: HTTP (ETag, 304, 401/403, 400, hình dạng JSON, Valkey dừng) ở `internal/httpapi/web/catalog_test.go`
  (gói `web_test` nhập `httpapi`); luồng sự kiện → consumer → Invalidator → giá mới ở `cmd/crm/webcache_test.go` (nối
  thật như `main.go`; `internal/httpapi/web` không được nhập `internal/wiring`, luật m); `weightGrams` của `updateVariant`
  ở `internal/httpapi/handlers_catalog_weight_test.go`.
- "Trong 1 s đọc lại thấy giá mới": test chờ theo sự kiện (chốt chống treo 30 s), ghi thời gian đo vào log (~20 ms),
  không lấy ngưỡng giờ thật làm tiêu chí đúng/sai (luật Task 400).
- Thứ tự làm: mã khung (migration, SQL, gói, handler) viết trước khi test chạy được lần đầu vì test cần mã sinh
  (`make gen`) để biên dịch; mỗi test mới chạy đỏ/xanh trên mã thật (ví dụ EXPLAIN đỏ ở 5 câu → sửa SQL → xanh;
  `TestProducts_Images` đỏ vì `position > 0`; test bộ đệm đỏ vì lượt resync → sửa test).

## Việc phát hiện thêm

- Ảnh gắn lúc tệp chưa `ready`: khi `file.process` xong, mẫu không tăng `updated_at` → website không biết tải lại mẫu
  (QĐ-190-3). Đề xuất task catalog: nghe sự kiện tệp ready của `product_image` đang gắn → `bumpProduct`.
- Người bán đổi `status` (suspended ↔ active) không tăng `updated_at` của mẫu → `changes` không báo; website chỉ thấy
  ở lần tải lại cả danh mục. Đề xuất: đổi trạng thái người bán phát sự kiện, hoặc website tải lại định kỳ.
- Tồn đổi (`stock.changed`) chỉ xóa khóa `SpaceAvailability` (Task 185); `available` trong sản phẩm đệm 60 s có thể cũ
  tới 60 s (số chính xác lúc đặt hàng và ở `webAvailability` của M5-08).
- Màn sản phẩm chưa có ô cân nặng (`weightGrams` đã có trong API) — cần mockup (Điểm dừng B).

## Reviewer

Kết luận: ĐẠT (23/11, review `git diff --cached` sau khi gộp main f89693d và ci-task XANH).

- Không có phát hiện phải sửa. Đã kiểm: không lộ `cost_price`; mọi đọc trong `tenancy.WithTenant(…, TxAPI)` theo
  tenant của khóa dịch vụ; SQL `queries/webcatalog` chỉ nhắc view `_v` (có test kiến trúc); 0502 giữ nguyên cột,
  phép nối, `security_invoker` của view cũ, chỉ nối cuối; `product.updated` xóa khóa mẫu và trang; `loadProducts`
  `clear(m)` đầu thân hàm (an toàn khi `RetryTx` chạy lại); Valkey không gọi trong transaction; bảng bằng chứng khớp
  tên test thật.
- Ghi chú không bắt buộc (không sửa trong task này): `cache.ErrBusy` → 503 chưa có test riêng (đi chung nhánh
  `db.ErrRetryLater`); `byIDs` đệm `null` 60 s cho mẫu của người bán đang ngừng (khớp "Việc phát hiện thêm"); ba
  index của 0502 không `CONCURRENTLY` (luật cho phép, dữ liệu hiện nhỏ); mục Ảnh hưởng chưa liệt kê `catalog/events.go`
  (audit thêm `weightGrams`), `httpapi/errors.go`, `errors_catalog.go`, hằng `catalog.MinWeightGrams`/`MaxWeightGrams`
  — bổ sung ở dòng dưới.
- Bổ sung Ảnh hưởng theo reviewer: `catalog/events.go` (`variantAudit` ghi `weightGrams`), hằng xuất mới
  `catalog.MinWeightGrams`, `catalog.MaxWeightGrams`; `internal/httpapi/errors.go` (`cache.ErrBusy`),
  `errors_catalog.go` (3 dòng: `ErrInvalidWeight`, `web.ErrBadQuery`, `webcatalog.ErrBadLimit`).
