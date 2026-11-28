# Task 261 — Sản phẩm ẩn với khách, xem trước, hiện hàng loạt

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m7-dang-nhap-khoi-dong.md` (Task 261 = M7-16). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - `webcatalog.Products`, `webcatalog.ProductsByID`: thêm tham số cuối `preview bool` (false: bỏ mẫu `draft` như kế
    hoạch M5 nói; true: có mẫu `draft`, `WebProduct.Preview = true`). Chỗ gọi: `internal/httpapi/web/catalog.go` (2),
    `internal/queries/webcatalog/webcatalog_test.go` (10). `webcatalog.WebProduct` thêm trường `Preview bool`.
  - Mới (không đổi gì có sẵn): `catalog.Readiness`, `catalog.ReadinessPage`, `catalog.ReadinessPageResult`,
    `catalog.ProductReadiness`, `catalog.ReadinessFilter` (+ `ReadinessAll/Ready/NotReady`), `catalog.Writer.Publish`,
    `catalog.Writer.Unpublish`, `catalog.PublishInput`, `catalog.PublishResult`, `catalog.MaxLaunchIDs`,
    `catalog.MaxReadinessPage`; helper test `internal/testkit/shoptest/shelf.go`: `SeedShelf`, `Shelf`, `Lack`,
    `DraftProduct`, `AttachReadyImage`, `ShelfAttrCode` (dùng ở test catalog, httpapi; ở testkit vì SQL thô của dữ
    liệu thử chỉ được nằm ở đó — `TestDynSQLProductCode`). Lỗi dùng lại `catalog.ErrInvalidInput`.
  - Cấu trúc trong `internal/httpapi/web/catalog.go`: tách `cachedPage` (đọc trang qua `SpaceProductsPage`) dùng chung
    cho `page` và `byIDsPreview` (jscpd); test helper `serviceKey` của web gọi `serviceKeyWith` mới.
  - `web.Catalog.Products` thêm tham số `preview bool` (chỗ gọi duy nhất: `handlers_web_catalog.go`, đọc
    `identity.Can(u, "web.catalog.preview", nil)`).
  - Thân ba handler khung `GetCatalogLaunchReadiness`, `PublishProducts`, `UnpublishProducts`
    (`internal/httpapi/handlers_catalog_launch.go`).
- Gói phụ thuộc ngược (go list): `internal/queries/webcatalog` ← `internal/httpapi`, `internal/httpapi/web`.
  `internal/modules/catalog` ← nhiều gói, nhưng chỉ thêm ký hiệu mới; không đổi chữ ký có sẵn.
- Operation OpenAPI bị chạm, oasdiff: không sửa `openapi.yaml` (hợp đồng của Task 260 đã có ba operation và trường
  `WebProduct.preview`). Ba operation `getCatalogLaunchReadiness`, `publishProducts`, `unpublishProducts` hết trả 501:
  xóa ba dòng khỏi `tests/arch/testdata/m7_pending.txt`.
- Chỗ dùng trên màn hình: không có (màn mở bán là task màn sau). Website đọc qua `/api/v1/web/catalog/products`.
- Câu sqlc, bảng, pha migration: `migrations/0703_catalog_launch.sql` pha **mở rộng**: `catalog.products.published_at
  timestamptz NULL`, `catalog.attributes.required_for_shelf boolean NOT NULL DEFAULT false`, view
  `catalog.product_readiness_v` (cột đúng như `docs/views.md`: `product_id, status, ready, reasons`), GRANT UPDATE
  (published_at) cho crm_app. `queries/catalog/launch.sql` (mới): đọc người bán, khóa mẫu nháp/đang bán, đổi trạng
  thái hàng loạt, đọc điều kiện. `queries/webcatalog/products.sql`: `ProductPage`, `ProductsByIDs` thêm điều kiện
  `(@preview OR p.status <> 'draft')`.
- Bộ đệm web: khóa trang `p_<after>_<limit>` thêm `_p1` khi xem trước; đọc theo mã khi xem trước dùng một khóa
  `ids_p1_<sha256 mã đã sắp>` trong vùng `products_page` (vùng này bị làm mới ở mọi sự kiện sản phẩm), không dùng khóa
  theo mã của vùng `product`. Khóa thường không đổi.
- Mã sinh: `make gen` sinh lại `internal/*/internal/store/models.go` của mọi gói sqlc (hai cột mới vào struct bảng
  `CatalogProduct`, `CatalogAttribute` và struct view `CatalogProductReadinessV`), `catalog/internal/store/launch.sql.go`,
  `webcatalog/internal/store/products.sql.go`. Không đổi `openapi.yaml`, `console/src/api`.
- Test sẽ chạy: `internal/modules/catalog` (launch_test, launch_property_test — chạy lẻ bằng -run),
  `internal/queries/webcatalog` (preview_test, webcatalog_test sửa, explain_test sửa đối số), `internal/httpapi/web`
  (preview_test), `internal/httpapi` (handlers_catalog_launch_test, TestM7Contract_StatusCodes), `tests/arch`
  (pending list, views, sellerfilter, sqlusage — chạy ở ci-task). Vùng rủi ro cao: `catalog` không trong danh sách (chỉ `article_html.go`,
  `importer/apply.go`), nên test theo gói.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về điều
  kiện lên kệ); trong `catalog`: `Writer.productChanged` (phát `product.updated` + báo hiệu), `Writer.audit`,
  `identity.RecordAudit`; trong httpapi: `catalogWrite` / `refusableWrite` (WriteTx + Idempotency-Key), `catalogError`
  (đổi lỗi); `pageCursor`-kiểu mã trang có sẵn của danh sách mẫu (dùng chuỗi uuid như `nextCursor` của web).
- Dùng lại / viết mới vì: dùng lại `productChanged`, `audit`, `catalogWrite`, `ErrInvalidInput`. Viết mới câu khóa
  hàng loạt (`LockLaunchProducts`) và câu đổi trạng thái hàng loạt vì `UpdateProduct` của M1 sửa một mẫu theo version
  (gọi 200 lần sẽ đọc lại biến thể từng mẫu và so version người dùng không gửi). Điều kiện lên kệ viết một lần trong
  view `catalog.product_readiness_v` (một nguồn cho API, Publish dưới khóa, báo cáo Task 269).

## Soát thiết kế

5.1 Chủ dữ liệu: `catalog` (cột mới trên bảng của catalog, view của catalog). 5.2 Không ghi bảng module khác. 5.3 View
`product_readiness_v` đọc `platform.files.status` (ngoại lệ ADR-27 mục 4 giống khóa ngoại `product_images → files`, ghi
trong migration); `webcatalog` chỉ đọc view _v. 5.4 Hiện/ẩn: khóa + đổi + sự kiện + audit trong một transaction
(WriteTx). 5.5 `product.updated` qua `events.Emit` (worker xóa đệm web sau commit). 5.6 không áp dụng: không gọi ra
ngoài. 5.7 Mọi câu trong `tenancy.WithTenant` (WriteTx/TxAPI); cột mới trên bảng đã có RLS. 5.8 Module không biết HTTP;
handler mỏng. 5.9 publish/unpublish có `IdempotencyKey`, `x-permission: catalog.launch.publish` (hợp đồng Task 260).
5.10 Valkey chỉ đệm; khóa thường và khóa xem trước tách khóa đệm, không bao giờ chung. 5.11 không thêm thư viện. 5.12
Theo module mẫu catalog (Writer, sentinel, test `_test` trên PostgreSQL thật, rapid).

6.1 Hai yêu cầu cùng lúc: `SELECT … WHERE status = 'draft' … ORDER BY id FOR UPDATE`; yêu cầu sau chờ khóa, PostgreSQL
đánh giá lại điều kiện trên bản mới nên mẫu đã `active` rơi khỏi tập; UPDATE có `AND status = 'draft'`. 6.2 Gửi lại:
Idempotency-Key của httpapi (50 song song cùng khóa → một lần). 6.3 Thứ tự khóa: nhóm 4 chỉ khóa `catalog.products` theo
id tăng dần (không khóa biến thể, ảnh; các đường ghi biến thể/ảnh của M1 khóa mẫu trước nên bị xếp hàng sau). 6.4 Lỗi
tạm: WriteTx/RetryTx có sẵn; thân hàm gán lại mọi biến kết quả. 6.5 READ COMMITTED: điều kiện lên kệ đọc lại trong câu
sau khi giữ khóa (ảnh vừa commit ở transaction khác được thấy). 6.6–6.8 không áp dụng: không việc nền mới (sự kiện dùng
hàng đợi có sẵn). 6.9 Giới hạn 500 mã mỗi lần theo mã; `allReady` khóa mọi mẫu nháp của shop (đo 200 mẫu). 6.10 không
áp dụng: không đổi tiền (đơn cũ giữ giá dòng đơn). 6.11 rapid 50 lượt: sau Publish(allReady) mẫu mới `active` không có
lý do, mẫu có lý do vẫn `draft`. 6.12 Audit chỉ mã mẫu, trạng thái, version; không dữ liệu cá nhân. 6.13 Quyền
`catalog.launch.publish` (ScopeNone, owner/manager); `web.catalog.preview` chỉ là quyền khóa dịch vụ (Task 260 chặn gán
cho vai trò); đặt đơn với mẫu nháp vẫn bị chặn ở `order` (biến thể không `sellable`).

## Quyết định thay chủ shop

1. **Chữ ký Publish/Unpublish là phương thức của `catalog.Writer`** (`w.Publish(ctx, tx, in, actor, now)`), không phải
   hàm rời nhận `queue.JobQueue` như kế hoạch viết. Lý do: mọi đường ghi của catalog là phương thức của Writer (giữ
   Events, Queue, Realtime, RequestID; CLAUDE.md "Module có chiều người bán"); cùng hành vi. Phương án khác: hàm rời
   — phải truyền thêm Events registry và bus, trùng cấu trúc của Writer.
2. **Mã gửi lên không phải mẫu nháp** (đã `active`, ngừng bán, đã xóa, không có): Publish không đưa vào `published`
   lẫn `skipped` (không có gì để hiện; yêu cầu thứ hai sau khi A đã hiện trả `published: []`). Unpublish tương tự với
   mẫu không `active`. Lý do: `skipped` theo hợp đồng là "chưa đủ điều kiện, kèm lý do" và danh sách mã lý do không có
   mã nào cho "không phải nháp"; dễ đổi sau (thêm mã lý do). Phương án khác: 404/409 cả lô — một mã sai làm hỏng cả lô
   đêm mở.
3. **Mẫu `discontinued` không vào view điều kiện** (đúng kế hoạch: chỉ `draft`, `active`) và không hiện lại được bằng
   Publish (Publish chỉ nhận `draft`). Mở bán lại mẫu ngừng bán đi qua sửa mẫu của M1.
4. **Đệm xem trước theo mã nằm trong vùng `products_page`** (khóa `ids_p1_<băm>`), không thêm vùng đệm mới: vùng này bị
   làm mới ở mọi `product.updated`, nên hiện/ẩn thấy ngay trên website thử; khóa thường không bao giờ đọc khóa có `p1`.
   Phương án khác: vùng `product_preview` riêng (sửa `cache.Spaces`, ánh xạ của webcache và test của nó).
5. **`Changes` không đổi**: luồng thay đổi đã gồm mọi mẫu đổi (cả nháp, đã xóa) để website gỡ mẫu khi `ProductsByID`
   không trả; khóa thường gọi lại theo mã sẽ không nhận mẫu nháp. Mẫu vừa ẩn lại (`active → draft`) có `updated_at`
   mới nên vào luồng và website gỡ đúng.

## Phụ thuộc task để sau

(không có)

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Readiness: A ready; B no_image; C image_processing; D no_price; E missing_attr:chat_lieu | TestLaunch_Readiness, TestLaunch_ReadinessPage | (1) | `--- PASS: TestLaunch_Readiness (0.37s)`, `--- PASS: TestLaunch_ReadinessPage (0.86s)` |
| allReady → published [A], skipped 4 kèm lý do, A active, 1 sự kiện; gửi lại cùng khóa → cùng thân, Idempotent-Replayed, không thêm sự kiện | TestLaunch_PublishAllReady, TestLaunchAPI_PublishAllReady | (1), (4) | `--- PASS: TestLaunch_PublishAllReady (0.75s)`, `--- PASS: TestLaunchAPI_PublishAllReady (0.64s)` |
| 50 song song cùng khóa → một lần, 1 sự kiện; hai khóa khác cùng lúc → A hiện một lần | TestLaunchAPI_PublishParallel, TestLaunch_ConcurrentPublish | (4), (1) | `--- PASS: TestLaunchAPI_PublishParallel (1.13s)`, `--- PASS: TestLaunch_ConcurrentPublish (0.63s)` |
| productIds [A, B] → A hiện, B bỏ qua no_image | TestLaunch_PublishIDs, TestLaunchAPI_IDsUnpublishReadiness | (1), (4) | `--- PASS: TestLaunch_PublishIDs (0.70s)`, `--- PASS: TestLaunchAPI_IDsUnpublishReadiness (0.73s)` |
| B thêm ảnh ready ở transaction khác trước khi khóa → đánh giá lại dưới khóa và hiện | TestLaunch_ReevaluatedUnderLock | (1) | `--- PASS: TestLaunch_ReevaluatedUnderLock (0.32s)` |
| unpublish [A] → A draft, có sự kiện; giá và version biến thể không đổi (đơn cũ giữ giá ở dòng đơn) | TestLaunch_Unpublish | (1) | `--- PASS: TestLaunch_Unpublish (0.62s)` |
| Website: khóa thường không thấy A nháp; khóa có preview thấy A `preview: true`; khóa thường ngay sau (trong đệm) vẫn không thấy | TestWebCatalog_Preview, TestProducts_Preview, TestProducts_VariantFields | (3), (2) | `--- PASS: TestWebCatalog_Preview (3.41s)`, `--- PASS: TestProducts_Preview (0.55s)`, `--- PASS: TestProducts_VariantFields (0.29s)` |
| Đơn web với A nháp bị từ chối (422 item_unavailable, xem Khác kế hoạch), không có đơn | TestWebCreateOrder_DraftProduct | (3) | `--- PASS: TestWebCreateOrder_DraftProduct (0.35s)` |
| Hai người bán: mẫu người bán thứ hai hiện được, seller_id không đổi | TestLaunch_TwoSellers | (1) | `--- PASS: TestLaunch_TwoSellers (0.60s)` |
| 200 mẫu một lần gọi (ngưỡng 2 s ở test nhãn perf) | TestLaunch_Publish200 | (1) | `launch_test.go:348: Publish 200 mẫu: 415.400916ms`, `--- PASS: TestLaunch_Publish200 (2.98s)` |
| Rapid 50 lượt: sau Publish(allReady) mẫu active mới không lý do, mẫu có lý do vẫn draft | TestLaunch_PublishProperty | (1) | `--- PASS: TestLaunch_PublishProperty (5.68s)` |
| Ba operation hết 501 | TestM7Contract_StatusCodes | (4) | `--- PASS: TestM7Contract_StatusCodes (0.97s)` |
| Cổng tĩnh + test gói bị ảnh hưởng (testkit đổi → toàn bộ) | ci-task | ci-task.sh (cây ebdb5151, đúng cây đang stage trước khi gộp main fast-forward) | `CI-TASK XANH (từ xa trên github …)`, run 46113578126: static success, test (1)–(6) success |

Lệnh: (1) `go test -race -count=1 -v -run '^(TestLaunch_.*)$' ./internal/modules/catalog/`;
(2) `go test -race -count=1 -v -run '^(TestProducts_Preview|TestProducts_VariantFields)$' ./internal/queries/webcatalog/`;
(3) `go test -race -count=1 -v -run '^(TestWebCatalog_Preview|TestWebCreateOrder_DraftProduct)$' ./internal/httpapi/web/`;
(4) `go test -race -count=1 -v -run '^(TestLaunchAPI_.*|TestM7Contract_StatusCodes)$' ./internal/httpapi/`.
Các lệnh (1)–(4) chạy lại sau khi gộp main (fast-forward tới 48356ba9, không xung đột, `make gen` không chênh).

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có e2e cho task này)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Đơn web với mẫu nháp: kế hoạch ghi 422 `variant_not_for_sale`; đường đơn web của M5 trả **422 `item_unavailable`**
  kèm `variantIds` cho mọi biến thể không `sellable` (WB09, `TestWebCreateOrder_QuoteChangedAndUnavailable`). Giữ hợp
  đồng M5, không đổi mã: tính chất an toàn (đơn bị từ chối, không có đơn, kể cả khóa có `web.catalog.preview`) được
  kiểm ở `TestWebCreateOrder_DraftProduct`. `variant_not_for_sale` vẫn là mã của đường đơn nhân viên (`order.Create`).
- Test "gán `web.catalog.preview` cho vai trò nhân viên → bị từ chối" đã có từ Task 260
  (`TestEnsureDefaultRoles_M7` trong `internal/modules/identity/roles_m7_test.go`: `createRole(…, "web.catalog.preview")` → `ErrUnknownPermission`); tạo khóa có
  quyền này bằng `identity.CreateServiceKey` được kiểm ở `TestWebCatalog_Preview` (`serviceKeyWith`). Không viết lại.
- Ngưỡng "200 mẫu dưới 2 s" nằm ở test nhãn `perf` (`launch_perf_test.go`, Task 400: test đúng/sai không lấy giờ
  thật); test thường `TestLaunch_Publish200` chỉ kiểm đủ 200 mẫu và ghi số đo.
- Test HTTP song song nằm ở `internal/httpapi/handlers_catalog_launch_test.go`; test website ở
  `internal/httpapi/web/preview_test.go` (cần máy chủ, bộ đệm thật); `internal/queries/webcatalog/preview_test.go` kiểm
  phần truy vấn.
- Sửa test có sẵn `TestProducts_VariantFields` (webcatalog): mẫu nháp giờ chỉ có khi xem trước → đọc bằng
  `byIDWith(…, true)` và kiểm thêm `Preview`; `explain_test.go` thêm đối số `preview = false` cho hai câu.

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (reviewer, 28/11, trên `git diff --cached` sau khi gộp main fast-forward tới 48356ba9).

- Phát hiện phải sửa: không có. Đã kiểm: `identity.Can` nhận `web.catalog.preview` (quyền khóa dịch vụ); khóa đệm
  thường không đổi, khóa xem trước `_p1` / `ids_p1_<băm>` trong vùng `products_page` (làm mới ở mọi sự kiện sản phẩm);
  khóa `FOR UPDATE ORDER BY id` lọc trạng thái + UPDATE có `AND status = from_status` → mỗi mẫu hiện một lần; điều
  kiện đọc lại sau khóa; migration 0703 chỉ mở rộng, view `security_invoker`, cột khớp `docs/views.md`.
- Ảnh hưởng dự kiến khớp thực tế; mọi chỗ gọi chữ ký mới đã sửa; mã sinh không có dấu sửa tay.
- Ghi chú không bắt buộc: `TestLaunch_ReevaluatedUnderLock` ở READ COMMITTED không phân biệt "đọc sau khóa" với
  "đọc sau commit" (vẫn đủ cho tiêu chí); lần triển khai đầu, khóa đệm `product` theo mã cũ chỉ mất mẫu nháp khi hết
  hạn đệm hoặc có sự kiện mẫu (việc triển khai, không phải lỗi mã).
