# Task 39 — Danh sách và tìm sản phẩm

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 39, M1-39). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu đã có. Thêm mới: gói `internal/queries`
  (`ListProducts`, `ProductFilter`, `ProductRow`, `Cursor` (`String`) + `ParseCursor`, `MaxListLimit`, `MaxSearchLimit`, `SearchVariants`,
  `VariantHit`, `ErrInvalidInput`; 0 chỗ dùng cũ vì gói hiện chỉ có `doc.go`); `gen.Handler` thêm 2 phương thức
  (`ListProducts`, `SearchVariants`) do ogen sinh, `*handler` hiện thực trong `handlers_lists.go`. `errors.go` thêm
  `queriesError` vào chuỗi đổi lỗi (1 chỗ gọi, dòng 179).
- Gói phụ thuộc ngược (go list): `internal/queries` — chưa ai nhập (chỉ `internal/httpapi` sẽ nhập).
  `internal/httpapi` — `cmd/crm`, `internal/httpapi/gen`, `internal/platform/realtime`, `tests/arch`, `tests/e2e`.
- Operation OpenAPI bị chạm, oasdiff: thêm `GET /api/v1/catalog/products` `listProducts` (`catalog.read`) và
  `GET /api/v1/catalog/variants/search` `searchVariants` (`catalog.read`); schema mới `ProductListItem`,
  `ProductPage`, `VariantSearchHit`. Chỉ thêm → oasdiff không báo phá vỡ. Đường `/api/v1/catalog/products` đã có
  `post` (createProduct): thêm `get` cùng đường.
- Chỗ dùng trên màn hình: chưa có màn dùng (màn danh sách sản phẩm là Task M1-50 theo mockup
  `docs/mockups/m1/san-pham-danh-sach.html`); `console/src/api/**` sinh lại theo `make gen`, `tsc` chạy trong `make ci`.
- Câu sqlc, bảng, pha migration: không câu sqlc mới (SQL dựng lúc chạy ở `internal/queries/*_dynsql.go`). Migration
  `0123_catalog_search.sql` pha mở rộng: chỉ `CREATE INDEX`, `CREATE FUNCTION`, `ALTER FUNCTION … OWNER`,
  `REVOKE/GRANT EXECUTE`. Đọc view `catalog.products_v`, `catalog.variants_v`, `catalog.categories_v`,
  `catalog.product_tags_v`, `inventory.stock_available_v`; hàm đọc bảng `catalog.products`, `catalog.variants`.
- Test sẽ chạy: `go test -race ./internal/queries/...` (mới: `products_dynsql_test.go`, `explain_test.go`),
  `./internal/httpapi/...` (handler mới), `./tests/arch/...` (sqlusage, dynsql, viewsdoc, routes, boundaries),
  `./internal/platform/tenancy/...` (CheckSchemaRules trên migration mới); một lần `make ci` trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `textnorm.Fold` (bỏ dấu cho tìm không dấu — đúng
  việc), `tenancy.WithTenant`, `testkit.OpenStaff`/`StartPostgres`, `testkit.EnableAutoExplain`, `ExplainAsApp`,
  `RequireNoSeqScan` (Task M0-05), mẫu hàm SECURITY DEFINER chủ `crm_search` của `tenancy/search_test.go` (lọc
  `app.tenant_id`, thoát `% _ \`), `canAt` + `warehouseScope` của `handlers_inventory.go` (kiểm phạm vi kho),
  `readAsDefaultSeller` của `handlers_catalog.go` (người bán từ phiên), cách trang theo con trỏ của
  `ListStockDocuments`. Tìm "cursor", "keyset", "search" trong danh mục: không có kiểu con trỏ dùng chung.
- Dùng lại / viết mới vì: dùng lại tất cả các thứ trên. Viết mới `queries.Cursor` (cặp `updated_at, id`, mã hóa
  base64url): con trỏ của phiếu kho chỉ là một uuid (sắp theo id), không đủ cho thứ tự `(updated_at, id)`; chỉ một
  chỗ dùng nên để trong gói `queries` (chưa đưa vào `internal/platform`; Task 334 cùng gói dùng lại).

## Soát thiết kế

- 5.1 Chủ dữ liệu: không thêm dữ liệu; index và hàm tìm trên bảng `catalog` nằm ở schema `catalog`.
- 5.2 Không ghi bảng module khác: chỉ đọc.
- 5.3 Đọc module khác qua view: `internal/queries` chỉ đọc view `_v` có trong `docs/views.md` và gọi hàm
  `catalog.search_*` (hàm của catalog công bố cho đọc; ghi vào `docs/views.md` mục mới "Hàm tìm công bố").
- 5.4–5.6: không áp dụng (chỉ đọc, không sự kiện, không gọi ngoài).
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; view `security_invoker` nên RLS áp. Hàm tìm SECURITY DEFINER
  chạy bằng `crm_search` (BYPASSRLS, để dùng được index trigram — đã đo ở M0-05) nên **tự lọc**
  `tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`; ngoài ngữ cảnh → 0 dòng (có test);
  `search_path` cố định `pg_catalog, pg_temp`, mọi tên viết đủ schema; chỉ `crm_app` được EXECUTE.
- 5.8 Không biết HTTP: `internal/queries` trả sentinel `ErrInvalidInput`; đổi mã ở `errors.go`.
- 5.9 Đường ghi: không có (hai operation GET, `x-permission: catalog.read`).
- 5.10 Valkey: không áp dụng. 5.11 Thư viện: không thêm (pg_trgm có sẵn từ 0001).
- 5.12 Module mẫu: đường đọc mẫu `GET /api/v1/me`; trang theo con trỏ như `listStockDocuments`.
- 6.1–6.3: không áp dụng (không sửa, không khóa).
- 6.4 Lỗi tạm: đọc trong `db.TxAPI`; không ghi nên chạy lại vô hại.
- 6.5 Cách ly: READ COMMITTED; một trang là một câu nên nhất quán trong trang. Giữa hai trang dữ liệu có thể đổi:
  keyset `(updated_at, id)` không lặp dòng chưa đổi; mẫu bị sửa giữa chừng nhảy lên đầu (chấp nhận, như mọi danh sách).
- 6.6–6.8: không áp dụng.
- 6.9 Treo lâu: `Limit` ≤ 100 (tìm ≤ 100, ứng viên của bộ lọc `Q` ≤ 1.000); có test EXPLAIN không Seq Scan trên 10.000
  mẫu / 50.000 biến thể.
- 6.10 Tiền: giá đọc là `int64` đồng (min/max), không tính toán.
- 6.11: không thuộc vùng rủi ro cao; quy luật trang (đủ, không trùng) có test 250 mẫu, 50 cái trùng `updated_at`.
- 6.12 Dữ liệu cá nhân: không có (sản phẩm). Chuỗi tìm không ghi vào log.
- 6.13 Quyền: `catalog.read` qua guard; `warehouseId` (cột "Có thể bán") cần thêm `inventory.read` ở đúng kho đó
  (`canAt`), thiếu → 403 (có test).

## Quyết định thay chủ shop

- **Q1 — Hàm tìm nhận thêm người bán và chuỗi gốc.** Kế hoạch: `catalog.search_variants(q text, lim int)`. Chọn:
  `catalog.search_variants(p_seller uuid, q text, raw text, lim int)` (`q` không dấu chữ thường; `raw` chuỗi gốc bỏ
  khoảng trắng đầu cuối, để so barcode — sửa sau reviewer: barcode được phép có chữ thường lẫn hoa). Vì: hàm chạy bằng `crm_search` bỏ qua RLS, lọc người bán
  sau `LIMIT` ở ngoài hàm sẽ làm mất dòng khi có nhiều người bán (ADR-29: mọi hàm đọc nhận `sellerID`). API vẫn không
  có tham số `sellerId` (người bán lấy từ phiên, đúng A6). Phương án khác: lọc ngoài hàm — sai khi có người bán thứ hai.
- **Q2 — Cách khớp tên.** Kế hoạch: `name_search % textnorm(q)` (độ giống trigram, ngưỡng 0,3). Chọn: các từ của
  `textnorm.Fold(q)` xuất hiện theo thứ tự trong `name_search` (`LIKE '%dam%lua%'`), vẫn qua index GIN trigram (đúng
  cách đã đo ở M0-05). Vì: `%` bỏ sót tên dài ("dam" so với tên 15 từ có độ giống < 0,3) — mockup có tên rất dài.
  Phương án khác: `<%` (word_similarity) — kết quả mờ, khó đoán cho nhân viên. Đổi lại chỉ sửa thân hàm.
- **Q3 — Lọc `Q` của danh sách mẫu.** Kế hoạch có `ProductFilter.Q` nhưng không nói cách tìm dưới RLS. Chọn: thêm hàm
  thứ hai `catalog.search_products(p_seller, q, raw, lim) RETURNS SETOF uuid` (cùng cách khớp: mã mẫu, SKU theo tiền tố;
  barcode đúng; tên không dấu), danh sách lọc `product_id IN (…)` với tối đa 1.000 mẫu khớp. Vì: tìm thẳng trên view có
  RLS là Seq Scan (M0-05). Phương án khác: lấy mẫu từ `search_variants` (giới hạn 100 biến thể, mẫu không có biến thể
  không tìm ra).
- **Q4 — Tìm theo mã mẫu, barcode.** Ghi chú giao việc: tìm theo mã mẫu, tên, SKU, barcode. Chọn: mã mẫu và SKU khớp
  tiền tố, không phân biệt hoa thường (index `text_pattern_ops` mới trên `products.code`, `variants.sku` — database
  dùng collation `en_US.utf8` nên index unique sẵn có không dùng được cho `LIKE 'x%'`); barcode khớp **đúng cả chuỗi gốc, phân biệt hoa thường như lúc ghi**
  bằng index unique đã có `(tenant_id, seller_id, barcode)` — không tạo index barcode thứ hai như kế hoạch ghi (trùng).
- **Q5 — Ảnh bìa trong API danh sách.** Mockup cần ảnh bìa và danh mục. Bảng `catalog.product_images` là của Task 44
  (chưa có trên main). Chọn: API trả danh mục (`categoryId`, `categoryPath` tên từ gốc tới lá); **chưa trả ảnh bìa**.
  Task 44 (hoặc 50) thêm trường tùy chọn `cover` vào `ProductListItem` — thêm trường không phá vỡ hợp đồng. Ghi ở
  "Việc phát hiện thêm".
- **Q6 — Kho và cột "Có thể bán".** `warehouseId` không bắt buộc; có thì trả `available` (tổng `available` của các
  biến thể ở kho đó, có thể âm) và cần `inventory.read` ở kho đó (403 nếu không); không có thì không trả `available`.
- **Q7 — Lọc danh mục** gồm cả danh mục con, cháu (cây tối đa 3 cấp; mockup lọc "Váy" ra cả "Váy > Váy dài").
- **Q8 — Con trỏ** là chuỗi mờ (base64url của `updated_at` + `id`); hỏng → 400 `invalid_input`. Thứ tự: sửa gần nhất
  trước. `limit` mặc định 50 (mockup tải 50 mẫu một lần), tối đa 100. Tìm biến thể: `limit` mặc định 20, tối đa 100;
  `q` sau khi bỏ khoảng trắng phải dài 1–100 ký tự. Mẫu đã xóa (`deleted_at`) không hiện ở cả hai.
- **Q9 — Thêm index** `catalog.products (tenant_id, seller_id, updated_at DESC, id DESC) WHERE deleted_at IS NULL`
  cho trang đầu không Seq Scan (kế hoạch không liệt kê; ghi ở "Khác kế hoạch").

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước (Step 2) | `TestProducts`, `TestPlansHaveNoSeqScan`, `TestListsAPI` | `go test ./internal/queries/...`; `go test -run TestListsAPI ./internal/httpapi/` | `[build failed] undefined: productsSQL`; `listProducts: mã 501 … not_implemented` |
| `searchVariants("dam lua")` ra đúng mẫu, không dòng tenant khác (10.000 mẫu, 50.000 biến thể + 1.000/5.000 của tenant B) | `TestProducts/SearchByName`, `/SearchOtherTenant` | `go test -count=1 -run TestProducts -v ./internal/queries/...` | `--- PASS: TestProducts/SearchByName`, `--- PASS: TestProducts/SearchOtherTenant` |
| `"DAM01-"` ra các SKU tiền tố đó (không ra DAM011-S, không của B; chữ thường cũng được) | `TestProducts/SearchBySKUPrefix` | như trên | `--- PASS: TestProducts/SearchBySKUPrefix` |
| Barcode đủ 13 số ra đúng 1 | `TestProducts/SearchByBarcode` | như trên | `--- PASS: TestProducts/SearchByBarcode` |
| Tìm mã mẫu theo tiền tố, `sellable` đúng | `TestProducts/SearchByProductCode` | như trên | `--- PASS` |
| Ngoài ngữ cảnh tenant: hàm tìm trả 0 dòng (cả hai hàm) | `TestProducts/SearchOutsideTenant` | như trên | `--- PASS` |
| `% _ \` không khớp mọi thứ; q rỗng/quá dài, limit ngoài 1–100 → ErrInvalidInput | `TestProducts/SearchInvalid`, `/ListInvalid`, `/CursorRoundTrip` | như trên | `--- PASS` |
| Phân trang: 250 mẫu, 50 trùng `updated_at` → đúng 250, không trùng, không sót, đúng thứ tự (trang 37, con trỏ qua chuỗi) | `TestProducts/PagesAll` | như trên | `--- PASS: TestProducts/PagesAll` |
| Cột: số biến thể, giá nhỏ/lớn nhất, có thể bán ở kho (âm −10), đường danh mục | `TestProducts/ListColumns` | như trên | `--- PASS` |
| Lọc trạng thái, danh mục gồm con (3333), nhãn (1428), Q (500), Q + trạng thái, Q theo SKU | `TestProducts/ListFilters` | như trên | `--- PASS: TestProducts/ListFilters` |
| `ExplainAsApp` bằng crm_app trong ngữ cảnh tenant: tìm tên qua hàm, tìm SKU, barcode, danh sách trang đầu, có kho, lọc đủ → `RequireNoSeqScan` đạt và có nút index | `TestPlansHaveNoSeqScan/*` (6 ca) | `go test -race -count=1 -v -run TestPlans ./internal/queries/...` | `--- PASS: TestPlansHaveNoSeqScan (242.30s)`, 6 ca con PASS (máy đang tải nặng, load 24) |
| HTTP: thiếu `catalog.read` 403; kho không có `inventory.read` 403; kho w2 với quyền w1 403; kho w1 → `available` 10; `nextCursor` và trang 2; con trỏ hỏng, q toàn khoảng trắng → 400 `invalid_input`; tìm SKU; tenant B không thấy của A | `TestListsAPI` | `go test -count=1 -run TestListsAPI ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 1.516s` |
| Kiến trúc, schema (sqlusage, dynsql, viewsdoc, routes, boundaries; CheckSchemaRules) | `tests/arch`, `internal/platform/tenancy` | `go test -count=1 ./tests/arch/... ./internal/platform/tenancy/...` | tenancy `ok`; arch chỉ còn lỗi lint (đã sửa, `golangci-lint run ./internal/queries/... ./internal/httpapi/...` → `0 issues.`) |
| Cổng kiểm toàn bộ (sau khi gộp main 9dee868) | `make ci` | `make ci`, rồi các bước sau `test` chạy riêng: `make build vet lint gen-check oasdiff catalog-check jscpd test-guard gitleaks console-check` (+ `vuln`) | `go test -race ./...`: 38 gói ok, gồm `internal/queries 102.5s`, `internal/httpapi 552.2s`, `tests/arch 77.8s`, `tests/e2e 177.7s`; chỉ `internal/imager TestImager_HangKilledWithinDeadline` hỏng (`script giả chưa chạy … pid: no such file`, gói không bị task chạm; chạy riêng `-count=3` → `ok`). Bước sau: `No vulnerabilities found.`, oasdiff đạt (lần đầu báo `listSuppliers q maxLength 200→100` do tôi thay nhầm — đã trả lại 200), gitleaks `no leaks found`, EXIT 0 |
| Sửa sau reviewer lần 1: mẫu chưa có dòng số dư ở kho → `available` 0; barcode chữ hoa lẫn thường (`Ab12Cd`) tìm đúng cả chuỗi, một phần thì không | `TestProducts/ListColumns`, `/SearchByBarcode` (ca mới, mẫu NOSTOCK) | `go test -count=1 ./internal/queries/...` | `ok elimaz.com.vn/crm/internal/queries 82.450s` |
| Cổng kiểm sau khi sửa và gộp main lần 2–3 (fa4a934, rồi 096106f Task 396) | `make ci` | `make ci` (sau fa4a934) → mọi gói test `ok` (40 dòng ok: `internal/queries 233.1s`, `internal/httpapi 375.4s`, `tests/arch`, `tests/e2e 84.8s`), `No vulnerabilities found.`, `Found 0 clones.`; `test-guard` chỉ báo vì main vừa có Task 396 → gộp 096106f, rồi `make build vet lint gen-check sqlc-vet oasdiff catalog-check jscpd test-guard gitleaks console-check` → EXIT 0, `no leaks found`; test lại các gói chạm và gói Task 396 đổi (dòng dưới) | xem dòng dưới |
| Test lại sau gộp 096106f: gói của task, gói Task 396 đổi, kiến trúc, tenancy | — | `go test -race -count=1 ./internal/queries/... ./internal/httpapi/... ./tests/arch/... ./internal/platform/idempotency/... ./internal/platform/tenancy/...` | `ok queries 260.7s`, `ok httpapi 220.6s`, `ok tests/arch 45.9s`, `ok idempotency 133.3s`, `ok tenancy 150.9s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Hàm tìm có tham số người bán (Q1); khớp tên theo từ có thứ tự thay cho `%` (Q2); thêm hàm `catalog.search_products` (Q3); không thêm index barcode trùng, thêm index `text_pattern_ops` cho mã mẫu và index trang đầu (Q4, Q9).

## Việc phát hiện thêm

- Lần chạy `make ci` sau gộp fa4a934: PostgreSQL dev của task (crm-t39) tự khởi động lại giữa bộ test (`untracked child process … exited with exit code 2`, rồi `automatic recovery`) khi máy tải nặng; mọi gói dùng DB hỏng tức thì với `57P03`. Không liên quan mã; chạy lại thì đạt. Một lần khác `inventory TestReserve_NoDeadlockOnCrossOrder` hỏng ở bước dọn (`DROP DATABASE … timeout` 30 s của testkit), chạy riêng đạt.
- `internal/imager` `TestImager_HangKilledWithinDeadline` hỏng một lần trong `make ci` khi máy tải nặng (load 24, nhiều phiên song song): test chờ script giả ghi pid trong 1 s. Test nhạy thời gian; nên nới chờ hoặc đợi tín hiệu thay vì thời hạn cố định (task của imager).
- Ảnh bìa trong `listProducts` (mockup danh sách sản phẩm): thêm trường `cover` khi có `catalog.product_images_v` (Task 44) — Task 44 hoặc Task 50 làm.

## Reviewer

Kết luận: ĐẠT (lần 2, trên bản đã gộp main 096106f).

- Lần 1 (bản gộp 9dee868): CẦN SỬA, hai phát hiện về tính đúng: (1) mẫu chưa có dòng số dư ở kho thì mất trường
  `available` thay vì 0 → sửa bằng `coalesce(sum(...), 0)`, test mẫu NOSTOCK; (2) barcode có chữ thường lẫn hoa không
  tìm được vì `q` đã qua `textnorm.Fold` → hai hàm nhận thêm `raw` (chuỗi gốc bỏ khoảng trắng) và so `barcode = raw`,
  test barcode `Ab12Cd`. Ghi chú: tên `Cursor` trong mục Ảnh hưởng — đã sửa; `search_products` cắt 1.000 mẫu không theo
  thứ tự (đã ghi ở Q3 và mô tả OpenAPI, giữ nguyên).
- Lần 2: ĐẠT, không phát hiện phải sửa. Chữ ký hàm khớp ở định nghĩa, `ALTER OWNER`, `REVOKE`, `GRANT` và mọi chỗ gọi;
  hai hàm vẫn tự lọc tenant, người bán, cố định `search_path`, thoát ký tự LIKE. Ghi chú tài liệu (chữ ký ở Q3) đã sửa.
