# Task 489 — Danh mục web: createdAt sản phẩm + mốc giờ máy chủ trong luồng thay đổi

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `webcatalog.Changes` thêm giá trị trả thứ năm `serverTime`
  (chỗ gọi: `internal/httpapi/web/catalog.go` 1, `internal/queries/webcatalog/webcatalog_test.go` 2); `webcatalog.WebProduct`
  thêm trường `CreatedAt` (chỉ thêm; dựng ở `assemble`, đọc ở `web.Catalog.product`). Mã sinh `gen.WebProduct`
  (thêm `CreatedAt OptDateTime`), `gen.WebCatalogChanges` (thêm `ServerTime time.Time`), store `ProductPageRow`,
  `ProductsByIDsRow` (thêm `CreatedAt`; hai kiểu vẫn cùng cột để đổi kiểu `store.ProductPageRow(r)`).
- Gói phụ thuộc ngược (go list): `internal/queries/webcatalog` ← `internal/httpapi/web` ← `internal/httpapi` (chỉ hai gói).
- Operation OpenAPI bị chạm, oasdiff: `webCatalogProducts` (schema `WebProduct` thêm `createdAt` tùy chọn),
  `webCatalogChanges` (schema `WebCatalogChanges` thêm `serverTime` bắt buộc trong câu trả lời). Chỉ thêm ở phía trả:
  oasdiff không phá.
- Chỗ dùng trên màn hình: không (route `/api/v1/web/*` chỉ cho website; `console/src/api` sinh lại có thêm trường, không
  màn hình nào dùng).
- Câu sqlc, bảng, pha migration: `queries/webcatalog/products.sql` `ProductPage`, `ProductsByIDs` thêm `p.created_at`;
  migration mới `0529_catalog_products_v_created_at.sql` pha mở rộng: `CREATE OR REPLACE VIEW catalog.products_v` nối
  `created_at` (cột có sẵn của `catalog.products` từ 0114) vào cuối, giữ mọi cột cũ; `docs/views.md` cập nhật dòng.
- Test sẽ chạy: `internal/queries/webcatalog` (TestProducts_CreatedAt mới, TestChanges sửa), `internal/httpapi/web`
  (TestWebCatalogChanges, TestWebCatalogProducts_Shape sửa), `tests/arch` (viewsdoc, sqlusage), oasdiff, cả hai gói ở
  ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có gì về mốc giờ
  máy chủ cho website); câu `Now` có sẵn trong `queries/webcatalog/changes.sql`; tên `serverTime` đã dùng ở
  `webFlashCurrent` của kế hoạch M5 (cùng nghĩa: giờ máy chủ cho website).
- Dùng lại / viết mới vì: dùng lại câu `Now` (now() của transaction đọc) cho `serverTime`, dùng lại cột `created_at`
  có sẵn của bảng; không viết hàm mới. Đặt tên trường `serverTime` giống `webFlashCurrent` để website dùng một tên.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: `created_at` thuộc catalog (cột có sẵn); catalog công bố qua view của mình (migration chỉ đổi view
  của catalog).
- 5.2 Không ghi bảng module khác: chỉ đọc.
- 5.3 Đọc qua view: `internal/queries/webcatalog` chỉ đọc `catalog.products_v` (nối thêm cột), luật sqlusage giữ.
- 5.4, 5.5, 5.6: không áp dụng (không ghi, không sự kiện, không gọi ra ngoài).
- 5.7 Ngữ cảnh shop: lời đọc vẫn trong `tenancy.WithTenant`; view giữ `security_invoker` nên RLS của bảng áp dụng.
- 5.8 Nghiệp vụ không biết HTTP: `webcatalog` không nhập gen; lớp `web` chép sang kiểu ogen.
- 5.9 Đường ghi: không áp dụng (chỉ GET).
- 5.10 PostgreSQL là nguồn sự thật: `serverTime` là now() của PostgreSQL (cùng đồng hồ với `until`, `updated_at`), không
  phải giờ tiến trình Go. Bộ đệm Valkey: `createdAt` để TÙY CHỌN trong `WebProduct` vì bản đệm cũ (≤ 60 s, viết trước
  khi nâng cấp) không có trường; bắt buộc thì ogen giải mã bản đệm cũ lỗi → 500 tới khi hết hạn (xem Quyết định).
- 5.11 Không thư viện mới. 5.12 Theo mẫu có sẵn của Task 190 (cùng gói, cùng cách test).
- 6.1–6.4: không áp dụng (không ghi, không khóa). 6.5 Mức cách ly: một transaction đọc; `now()` là mốc bắt đầu
  transaction nên `serverTime` ≤ mọi thay đổi chưa commit lúc đó — website lấy `serverTime − 30 s` làm mốc của bản sao
  đầu là an toàn (như `until`).
- 6.6–6.8: không áp dụng. 6.9 Thêm một câu `SELECT now()` khi trang đủ limit (rẻ). 6.10 Tiền: không chạm. 6.11 Không vùng
  rủi ro cao. 6.12 Dữ liệu cá nhân: không (ngày tạo mẫu). 6.13 Quyền: giữ `web.catalog.read` của route có sẵn.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `WebProduct.createdAt` = `created_at` của mẫu (UTC), giữ khi sửa mẫu, có ở `Products` (mọi trang) và `ProductsByID` | `TestProducts_CreatedAt` (`internal/queries/webcatalog`) | `go test -race -count=1 -run '^(TestChanges\|TestProducts_CreatedAt)$' ./internal/queries/webcatalog/` | `ok elimaz.com.vn/crm/internal/queries/webcatalog 2.191s` |
| HTTP `createdAt` RFC 3339 = `created_at` (ogen ghi theo giây, như `updatedAt`) | `TestWebCatalogProducts_Shape` → `checkCreatedAt` (`internal/httpapi/web`) | `go test -race -count=1 -run '^(TestWebCatalogChanges\|TestWebCatalogProducts_Shape\|TestWebCatalog_CacheAndFallback\|TestWebCatalog_ETag)$' ./internal/httpapi/web/` | `ok elimaz.com.vn/crm/internal/httpapi/web 4.154s` |
| Bản đệm cũ (không `createdAt`) vẫn giải mã được | `TestWebProduct_CacheWithoutCreatedAt` | `go test -count=1 -run '^TestWebProduct_CacheWithoutCreatedAt$' ./internal/httpapi/web/` | `ok elimaz.com.vn/crm/internal/httpapi/web 1.145s` |
| `serverTime` = now() của lần đọc: bằng `until` khi chưa đủ limit, ≥ `until` của lần đọc đầy đủ khi `hasMore` | `TestChanges` (thêm 2 kiểm) | như dòng 1 | `ok` |
| HTTP `serverTime` RFC 3339, bằng `until` khi `hasMore` false | `TestWebCatalogChanges` | như dòng 2 | `ok` |
| View có cột mới đúng `docs/views.md`; SQL chỉ dùng view; lint | `tests/arch` (viewsdoc, sqlusage, lint) | ci-task | `ok elimaz.com.vn/crm/tests/arch 28.258s` |
| Hợp đồng chỉ thêm (oasdiff không phá), mã sinh khớp | oasdiff, gen-check | ci-task | `== oasdiff: 1 giây` (không lỗi), gen-check qua |
| Gói đổi và phụ thuộc | cả gói `webcatalog`, `httpapi/web`, `httpapi` (-race) + gói dùng tới | ci-task | `ok .../httpapi/web 16.307s`, `ok .../queries/webcatalog 17.177s`, `ok .../httpapi 21.764s`; `CI-TASK XANH (so với điểm tách nhánh 3cae208)` |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

1. **`createdAt` tùy chọn (không nằm trong `required`) trong `WebProduct`.** Lý do: bản đệm Valkey của mẫu và trang
   (60 s) viết trước khi nâng cấp không có trường; nếu bắt buộc, bộ giải mã ogen báo thiếu trường → 500 cho website
   tới 60 s sau khi deploy. CRM luôn gửi trường; vắng chỉ trong ≤ 60 s đó. Phương án khác: bắt buộc + đổi khóa đệm
   (phải sửa cả webcache.Invalidator) — lớn hơn, khó đảo ngược hơn.
2. **`serverTime` bắt buộc trong `WebCatalogChanges`, là now() của transaction đọc** (cùng đồng hồ với `until`), có ở
   mọi câu trả lời kể cả khi `hasMore`. Website dùng nó làm mốc cho bản sao đầu: gọi `changes` lấy `serverTime` trước khi
   tải cả danh mục, rồi `since = serverTime − 30 s`. Không thêm `asOf` vào `WebProductPage` (trang đệm 60 s nên mốc của
   trang là lúc tải, dễ hiểu nhầm; giao việc chỉ nói luồng thay đổi). Phương án khác: header `Date` (giờ tiến trình Go,
   không cùng đồng hồ PostgreSQL).
3. Tên `serverTime` theo `webFlashCurrent` của kế hoạch M5 (một tên cho cùng nghĩa).

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Bộ nhập mẫu (`catalog/internal/importer`) không mang ngày tạo gốc: mẫu nhập từ Nhanh/website có `created_at` = lúc
  nhập, nên "Mới nhất" sau khi chuyển dữ liệu sẽ theo thứ tự nhập. Muốn giữ đúng như website hiện tại, việc chuyển dữ
  liệu (M7) cần chép ngày tạo gốc vào `catalog.products.created_at`. Chưa làm (ngoài phạm vi).
- ogen ghi `date-time` theo giây (cả `updatedAt`, `until`, `serverTime`, `createdAt`): đủ cho sắp xếp và mốc −30 s
  (cắt xuống nên `since` càng sớm, an toàn). Ghi để website biết.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên `git diff --cached` sau khi ci-task xanh).

- Phát hiện phải sửa: không có.
- Đối chiếu Ảnh hưởng: khớp; thêm `CreatedAt` trong `CatalogProductsV` của 11 gói store là mã sinh của sqlc, không chỗ
  gọi viết tay, không câu `SELECT *` trên `products_v`. Migration giữ đủ cột của 0502 theo thứ tự, `security_invoker`;
  `crm_app` đã có SELECT trên bảng.
- Bằng chứng: các test kiểm đúng tiêu chí (createdAt cố định +07 → UTC, mọi trang; bản đệm cũ giải mã được; serverTime
  cùng đồng hồ với until ở cả hai nhánh).
- Ghi chú không bắt buộc: dòng "Kế hoạch" nên trỏ M5 (đã ghi nguồn M5 ở dưới); ETag đổi một lần khi bản đệm cũ hết hạn
  (vô hại).

Nguồn kế hoạch của các route: M5 `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 190);
số task theo bảng toàn cục (M5-b4).
